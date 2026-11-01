package core

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/synthlike/hiss/internal/text"
)

// Agents' edits apply and save right away, and stay in the undo history of
// their turn. An agent asks for review when it thinks the user should look,
// and some edits are always held; those become pending hunks. Applied edits
// are kept for the session only: after a restart, git is the way back.

// Settle decides what becomes of the proposals an agent just made in one
// buffer: with review, or when holdReason says so, they stay pending for the
// user; otherwise they apply. It returns the hold reason, if any.
func (e *Editor) Settle(agent string, ps []*Proposal, review bool) string {
	b := ps[0].Buf
	why := e.holdReason(b)
	if review || why != "" {
		e.announce(agent, b, len(ps))
		return why
	}
	for _, p := range ps {
		e.apply(p)
	}
	return ""
}

// holdReason says why an edit to b needs review whatever the agent thinks,
// or "".
func (e *Editor) holdReason(b *Buffer) string {
	if b.Dirty() {
		return "it has the user's unsaved changes"
	}
	rel, ok := e.underRoot(b.Path)
	if !ok {
		return "it is outside the project"
	}
	if rel == rulesFile {
		return rulesFile + " says what runs and what gets reviewed"
	}
	c, err := loadRules(e.Root)
	if err != nil {
		return rulesFile + " can't be read, so its [review] rules can't be checked"
	}
	if (&rule{match: c.review}).matches(rel) {
		return "a [review] rule in " + rulesFile + " matches it"
	}
	return ""
}

// appliedState is what undoing an applied proposal needs. The buffer as it
// was restores a run of edits exactly, even ones that overlap; the replaced
// text undoes one edit among others.
type appliedState struct {
	before        string            // the text it replaced
	snapshot      text.Text         // the whole buffer before it
	after         text.Text         // and after it
	starts, ends  map[*Proposal]int // the buffer's applied regions before it
	created, seen bool              // applying it created the file; the user cleared its gutter mark
}

// apply saves pending proposal p without review, keeping what undoing it needs.
func (e *Editor) apply(p *Proposal) {
	b := p.Buf
	_, err := os.Stat(b.Path)
	st := appliedState{before: string(b.Text.Slice(p.Start, p.End)), snapshot: b.Text,
		created: errors.Is(err, fs.ErrNotExist), starts: map[*Proposal]int{}, ends: map[*Proposal]int{}}
	for _, q := range b.applied {
		st.starts[q], st.ends[q] = q.Start, q.End
	}
	e.resolve(p, true, "")
	st.after = b.Text
	p.applied = st
	p.Status, p.End = Applied, p.Start+len(p.New)
	b.applied = append(b.applied, p)
}

// trackApplied keeps an applied proposal's region in step with edit [start,
// end) that became n bytes. An edit to the region grows it to cover the edit,
// and undoSet leaves a region that no longer holds the proposal's text alone.
func (p *Proposal) trackApplied(start, end, n int) {
	d := n - (end - start)
	switch {
	case end <= p.Start:
		p.Start, p.End = p.Start+d, p.End+d
	case start >= p.End:
	default:
		p.Start, p.End = min(p.Start, start), max(p.End, end)+d
	}
}

// appliedProposals returns the applied proposals in all buffers, oldest first.
func (e *Editor) appliedProposals() []*Proposal {
	var ps []*Proposal
	for _, b := range e.Bufs {
		ps = append(ps, b.applied...)
	}
	slices.SortFunc(ps, func(a, b *Proposal) int { return cmp.Compare(a.ID, b.ID) })
	return ps
}

// ChangedLines returns the lines of the visible buffer that agents changed
// and the user hasn't cleared with :cc, and those :review is showing, for the
// gutter.
func (e *Editor) ChangedLines() map[int]bool {
	lines := map[int]bool{}
	t := e.Buf.Text
	for _, p := range e.Buf.applied {
		if !p.applied.seen {
			for l := t.LineOf(p.Start); l <= t.LineOf(max(p.End-1, p.Start)); l++ {
				lines[l] = true
			}
		}
	}
	if e.trail != nil {
		for _, s := range e.trail.Steps {
			if s.Buf == e.Buf {
				for l := s.Line(); l < s.Line()+s.Lines; l++ {
					lines[l] = true
				}
			}
		}
	}
	return lines
}

// clearChanges drops the gutter marks. The changes can still be listed and undone.
func (e *Editor) clearChanges() {
	n := 0
	for _, p := range e.appliedProposals() {
		if !p.applied.seen {
			p.applied.seen = true
			n++
		}
	}
	e.Msg = "cleared " + plural(n, "change mark")
}

// openChangePicker lists what agents applied, newest first (space g). Enter
// only goes there; :su undoes a turn.
func (e *Editor) openChangePicker() {
	p := &Picker{Title: "changes"}
	for _, q := range slices.Backward(e.appliedProposals()) {
		detail := fmt.Sprintf("#%d %s", q.ID, q.Agent)
		if q.Rationale != "" {
			detail += ": " + q.Rationale
		}
		p.Items = append(p.Items, PickItem{
			Label:  fmt.Sprintf("%s:%d", q.Buf.Name, q.Buf.Text.LineOf(q.Start)+1),
			Detail: detail,
			Run: func(e *Editor) {
				e.pushJump()
				e.SwitchTo(q.Buf)
				e.showHunk(q)
			},
		})
	}
	if len(p.Items) == 0 {
		e.Msg = noChanges
		return
	}
	e.Picker = p
}

const noChanges = "no changes applied by agents"

// undoSet reverts the applied change set under the cursor, or else the newest
// one, telling the agent. A change whose text has been edited since stays,
// and so does anything the user accepted in review.
func (e *Editor) undoSet(reason string) {
	all := e.appliedProposals()
	if len(all) == 0 {
		e.Msg = noChanges
		return
	}
	target := all[len(all)-1]
	line := e.line()
	for _, p := range e.Buf.applied {
		t := e.Buf.Text
		if t.LineOf(p.Start) <= line && line <= t.LineOf(max(p.End-1, p.Start)) {
			target = p
		}
	}
	byBuf := map[*Buffer][]*Proposal{}
	for _, p := range all {
		if p.set() == target.set() {
			byBuf[p.Buf] = append(byBuf[p.Buf], p)
		}
	}
	var undone []int
	var stuck, failed []string
	for b, ps := range byBuf {
		b.Refresh()
		dirty := b.Dirty()
		u, s := e.undoIn(b, ps)
		undone, stuck = append(undone, u...), append(stuck, s...)
		switch {
		case len(u) == 0, dirty: // the user's unsaved edits aren't ours to write
		case b.Text.Len() == 0 && ps[0].applied.created:
			b.saved = b.Version
			if err := os.Remove(b.Path); err != nil {
				failed = append(failed, e.relName(b))
			}
		default:
			if err := b.Save(); err != nil {
				failed = append(failed, e.relName(b))
			}
		}
	}
	slices.Sort(undone)
	slices.Sort(stuck)
	slices.Sort(failed)
	msg, tell := "undid "+plural(len(undone), "change"), "I undid your changes"
	if len(undone) == 0 {
		msg, tell = "undid nothing", "I tried to undo your changes"
	}
	for _, id := range undone {
		tell += fmt.Sprintf(" #%d", id)
	}
	if len(stuck) > 0 {
		msg += "; changed since: " + strings.Join(stuck, ", ")
		tell += "; these were edited since and stayed: " + strings.Join(stuck, ", ")
	}
	if len(failed) > 0 {
		msg += "; saving failed for " + strings.Join(failed, ", ")
		tell += "; saving failed for " + strings.Join(failed, ", ")
	}
	if reason != "" {
		tell += ": " + reason
	}
	e.Msg = msg
	target.Thread = append(target.Thread, Comment{Author: You.Author, Text: tell})
}

// undoIn reverts ps, a set's applied proposals in b, oldest first. It returns
// the ids it undid, and where the ones it couldn't are.
func (e *Editor) undoIn(b *Buffer, ps []*Proposal) (undone []int, stuck []string) {
	if unbroken(b, ps) {
		first := ps[0].applied
		e.checkpointBuf(b)
		e.moveCursor(b, b.Restore(first.snapshot))
		for _, p := range ps {
			b.applied = slices.DeleteFunc(b.applied, func(q *Proposal) bool { return q == p })
			p.Status = Undone
			undone = append(undone, p.ID)
		}
		for _, q := range b.applied { // restoring reduced the text to one edit, which says less
			q.Start, q.End = first.starts[q], first.ends[q]
		}
		return undone, nil
	}
	for _, p := range slices.Backward(ps) {
		if string(b.Text.Slice(p.Start, p.End)) != p.New {
			stuck = append(stuck, fmt.Sprintf("%s:%d", e.relName(b), b.Text.LineOf(p.Start)+1))
			continue
		}
		b.applied = slices.DeleteFunc(b.applied, func(q *Proposal) bool { return q == p })
		e.checkpointBuf(b)
		e.moveCursor(b, b.Replace(p.Start, p.End, []byte(p.applied.before), text.Origin{}))
		p.Status = Undone
		undone = append(undone, p.ID)
	}
	return undone, stuck
}

// unbroken reports whether b holds just what ps left, each having started
// from the one before, so the buffer can simply go back. It compares text, not
// versions: undoing a later set leaves what this one left.
func unbroken(b *Buffer, ps []*Proposal) bool {
	for i, p := range ps[1:] {
		if !bytes.Equal(p.applied.snapshot.Bytes(), ps[i].applied.after.Bytes()) {
			return false
		}
	}
	return bytes.Equal(b.Text.Bytes(), ps[len(ps)-1].applied.after.Bytes())
}

// moveCursor keeps b's cursor in the text through edit ed.
func (e *Editor) moveCursor(b *Buffer, ed text.Edit) {
	sel := &b.sel
	if b == e.Buf {
		sel = &e.Sel
	}
	sel.Anchor, sel.Head = ed.Map(sel.Anchor, false), ed.Map(sel.Head, false)
}
