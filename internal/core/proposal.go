package core

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/synthlike/hiss/internal/text"
)

type Status string

const (
	Pending  Status = "pending"
	Accepted Status = "accepted"
	Rejected Status = "rejected"
	Stale    Status = "stale"
)

// Proposal is an agent's suggested replacement of a region of a buffer. While
// pending, Start and End track the region as the buffer is edited.
type Proposal struct {
	ID        int
	Agent     string
	Session   string // agent session that proposed it, if known from hooks
	Turn      int    // the session's turn at the time
	Rationale string
	Buf       *Buffer
	Start     int
	End       int
	New       string
	Status    Status
	Reason    string    // why it was rejected or went stale
	Thread    []Comment // review discussion between the user and the agent
}

// ProposeEdit proposes replacing old with new in the file at path, using the
// semantics of Claude Code's Edit tool: old must occur exactly once unless all
// is set. With a base version, the edit is refused if the user has changed the
// matched text since the agent read that version.
func (e *Editor) ProposeEdit(agent, path, old, new string, all bool, base *int, rationale string) ([]*Proposal, error) {
	if old == new {
		return nil, errors.New("old_string and new_string are the same")
	}
	b, err := e.OpenBuffer(path)
	if err != nil {
		return nil, err
	}
	content := b.Text.Bytes()
	var starts []int
	switch {
	case old == "" && len(content) > 0:
		return nil, errors.New("old_string is empty; use write_file to replace a whole file")
	case old == "":
		starts = []int{0}
	default:
		for i := 0; ; {
			j := bytes.Index(content[i:], []byte(old))
			if j < 0 {
				break
			}
			starts = append(starts, i+j)
			i += j + len(old)
		}
	}
	switch {
	case len(starts) == 0:
		return nil, fmt.Errorf("old_string not found in %s (version %d); re-read it with read_buffer", b.Name, b.Version)
	case len(starts) > 1 && !all:
		return nil, fmt.Errorf("old_string occurs %d times in %s; include more context to make it unique, or set replace_all", len(starts), b.Name)
	}
	for _, s := range starts {
		if err := e.checkRegion(b, s, s+len(old), base); err != nil {
			return nil, err
		}
	}
	var ps []*Proposal
	for _, s := range starts {
		ps = append(ps, e.addProposal(agent, rationale, b, s, s+len(old), new))
	}
	e.announce(agent, b, len(ps))
	return ps, nil
}

// WriteFile proposes replacing the whole file at path, which may not exist yet.
func (e *Editor) WriteFile(agent, path, content string, base *int, rationale string) (*Proposal, error) {
	b, err := e.OpenBuffer(path)
	if err != nil {
		return nil, err
	}
	if b.Text.String() == content {
		return nil, fmt.Errorf("%s already has this content", b.Name)
	}
	if err := e.checkRegion(b, 0, b.Text.Len(), base); err != nil {
		return nil, err
	}
	p := e.addProposal(agent, rationale, b, 0, b.Text.Len(), content)
	e.announce(agent, b, 1)
	return p, nil
}

func (e *Editor) checkRegion(b *Buffer, start, end int, base *int) error {
	if base != nil {
		if *base > b.Version || *base < 0 {
			return fmt.Errorf("%s has no version %d (current is %d)", b.Name, *base, b.Version)
		}
		if b.ChangedSince(*base, start, end) {
			return fmt.Errorf("the user changed this text in %s since version %d; re-read it with read_buffer", b.Name, *base)
		}
	}
	for _, q := range b.pending {
		if overlaps(start, end, q.Start, q.End) {
			return fmt.Errorf("this overlaps pending proposal #%d in %s; wait for the user to review it", q.ID, b.Name)
		}
	}
	return nil
}

func (e *Editor) addProposal(agent, rationale string, b *Buffer, start, end int, new string) *Proposal {
	e.nextID++
	p := &Proposal{ID: e.nextID, Agent: agent, Rationale: rationale, Buf: b,
		Start: start, End: end, New: new, Status: Pending}
	if s := e.Caller(); s != nil {
		p.Session, p.Turn = s.ID, s.Turn
	}
	e.proposals[p.ID] = p
	i, _ := slices.BinarySearchFunc(b.pending, start, func(q *Proposal, s int) int { return q.Start - s })
	b.pending = slices.Insert(b.pending, i, p)
	return p
}

func (e *Editor) announce(agent string, b *Buffer, n int) {
	s := ""
	if n > 1 {
		s = "s"
	}
	e.Msg = fmt.Sprintf("%s proposed %d edit%s in %s (]h to review)", agent, n, s, b.Name)
}

// Proposal returns the proposal with id, or nil.
func (e *Editor) Proposal(id int) *Proposal { return e.proposals[id] }

// ProposalsBy returns an agent's proposals in creation order.
func (e *Editor) ProposalsBy(agent string) []*Proposal {
	var ps []*Proposal
	for _, p := range e.proposals {
		if p.Agent == agent {
			ps = append(ps, p)
		}
	}
	slices.SortFunc(ps, func(a, b *Proposal) int { return a.ID - b.ID })
	return ps
}

// PendingCount returns the number of pending proposals in all buffers.
func (e *Editor) PendingCount() int {
	n := 0
	for _, b := range e.Bufs {
		n += len(b.pending)
	}
	return n
}

// Hunk is how a pending proposal is drawn: buffer lines First..Last are the old
// text (marked removed when the proposal replaces any), followed by Added lines
// and the review thread's Notes.
type Hunk struct {
	P           *Proposal
	First, Last int
	Removes     bool
	Added       []string
	Notes       []NoteLine
}

// Rows returns the number of rows drawn after the hunk's last line.
func (h Hunk) Rows() int { return len(h.Added) + len(h.Notes) }

// Hunks returns the pending hunks of the visible buffer in order.
func (e *Editor) Hunks() []Hunk {
	var hs []Hunk
	for _, p := range e.Buf.pending {
		hs = append(hs, e.hunk(p))
	}
	return hs
}

func (e *Editor) hunk(p *Proposal) Hunk {
	b := p.Buf
	t := b.Text
	first := t.LineOf(p.Start)
	last := t.LineOf(max(p.End-1, p.Start))
	lastEnd := b.LineEnd(last)
	res := string(t.Slice(t.LineStart(first), p.Start)) + p.New
	trim := p.End > lastEnd || p.End == t.Len() // the region ends a line, so a final "\n" isn't a new line
	if p.End <= lastEnd {
		res += string(t.Slice(p.End, lastEnd))
	}
	var added []string
	if res != "" {
		added = strings.Split(res, "\n")
		if trim && len(added) > 1 && added[len(added)-1] == "" {
			added = added[:len(added)-1]
		}
	}
	return Hunk{P: p, First: first, Last: last, Removes: p.End > p.Start, Added: added,
		Notes: threadLines(p.Thread, e.Width-2)}
}

// HunkAtCursor returns the pending hunk on the cursor's line, or nil.
func (e *Editor) HunkAtCursor() *Hunk {
	line := e.line()
	for _, h := range e.Hunks() {
		if line >= h.First && line <= h.Last {
			return &h
		}
	}
	return nil
}

func (e *Editor) reviewHunk(accept bool, reason string) {
	h := e.HunkAtCursor()
	if h == nil {
		e.Msg = "no pending hunk on this line"
		return
	}
	p := h.P
	p.Buf.pending = slices.DeleteFunc(p.Buf.pending, func(q *Proposal) bool { return q == p })
	if accept {
		e.checkpoint()
		p.Buf.Replace(p.Start, p.End, []byte(p.New),
			text.Origin{Author: p.Agent, Session: p.Session, Turn: p.Turn, Proposal: p.ID})
		p.Status = Accepted
		e.Sel = Selection{p.Start, p.Start}
		if err := p.Buf.Save(); err != nil {
			e.Msg = fmt.Sprintf("accepted #%d but saving failed: %v", p.ID, err)
		} else {
			e.Msg = fmt.Sprintf("accepted #%d, wrote %s", p.ID, p.Buf.Name)
		}
	} else {
		p.Status, p.Reason = Rejected, reason
		e.Msg = fmt.Sprintf("rejected #%d", p.ID)
	}
	if e.PendingCount() > 0 {
		msg := e.Msg
		e.jumpHunk(1)
		e.Msg = msg
	}
}

// jumpHunk moves to the next (dir 1) or previous (dir -1) pending hunk,
// continuing into other buffers and wrapping around.
func (e *Editor) jumpHunk(dir int) {
	type pos struct {
		b    int
		line int
		buf  *Buffer
	}
	var all []pos
	cur := 0
	for i, b := range e.Bufs {
		if b == e.Buf {
			cur = i
		}
		for _, p := range b.pending {
			all = append(all, pos{i, b.Text.LineOf(p.Start), b})
		}
	}
	if len(all) == 0 {
		e.Msg = "no pending hunks"
		return
	}
	here := pos{cur, e.line(), nil}
	less := func(a, b pos) bool { return a.b < b.b || a.b == b.b && a.line < b.line }
	target := all[0]
	if dir < 0 {
		target = all[len(all)-1]
	}
	for i := range all {
		p := all[i]
		if dir < 0 {
			p = all[len(all)-1-i]
		}
		if dir > 0 && less(here, p) || dir < 0 && less(p, here) {
			target = p
			break
		}
	}
	e.SwitchTo(target.buf)
	e.setHead(e.Buf.Text.LineStart(target.line))
	e.Top = max(target.line-2, 0)
}
