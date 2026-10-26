package core

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Superseded is the status of a proposal the agent replaced with a revision.
const Superseded Status = "superseded"

// Comment is one message in a proposal's review thread.
type Comment struct {
	Author    string // "you" or the agent
	Text      string
	At        string // diff line commented on: "+3" (3rd proposed line) or "-12" (buffer line 12); "" for the hunk
	Line      string // that line's text, prefixed "+ " or "- "
	delivered bool   // for the user's comments: the agent has been given it
}

// NoteLine is one drawn row of a thread.
type NoteLine struct {
	Mine bool // written by the user
	Note bool // a trail step's note rather than a comment
	Text string
}

// Delivery is a user comment handed to an agent.
type Delivery struct {
	Proposal *Proposal // the proposal commented on, or nil for a trail step
	Step     *Step
	Text     string
	Line     string // diff line the comment is on, if any
}

const nothingToComment = "nothing to comment on here: move to a pending hunk or a trail step"

// startComment opens the command line on ":comment " if there is something
// under the cursor to comment on.
func (e *Editor) startComment() {
	if e.HunkAtCursor() == nil && e.stepAtCursor() == nil {
		e.Msg = nothingToComment
		return
	}
	e.Mode, e.Cmd = Command, "comment "
}

// comment adds the user's comment to the pending hunk under the cursor or,
// failing that, to the trail step on the cursor's line.
func (e *Editor) comment(text string) {
	h, s := e.HunkAtCursor(), e.stepAtCursor()
	switch {
	case h == nil && s == nil:
		e.Msg = nothingToComment
	case text == "":
		e.Msg = "empty comment"
	case h == nil:
		s.Thread = append(s.Thread, Comment{Author: You.Author, Text: text})
		e.Msg = fmt.Sprintf("commented on step %d; %s will see it", s.N, e.trail.Agent)
	default:
		c := Comment{Author: You.Author, Text: text}
		line := e.line()
		lineText := string(e.Buf.Text.Slice(e.Buf.Text.LineStart(line), e.Buf.LineEnd(line)))
		switch r := e.DiffRow(); {
		case r < 0: // a tweaked hunk's old line i, which was buffer line First+i
			i := len(h.Removed) + r
			c.At, c.Line = fmt.Sprintf("-%d", h.First+i+1), "- "+h.Removed[i]
		case r > 0:
			c.At, c.Line = fmt.Sprintf("+%d", r), "+ "+h.Added[r-1]
		case h.Tweaked:
			c.At, c.Line = fmt.Sprintf("+%d", line-h.First+1), "+ "+lineText
		case h.Removes:
			c.At, c.Line = fmt.Sprintf("-%d", line+1), "- "+lineText
		}
		h.P.Thread = append(h.P.Thread, c)
		e.Msg = fmt.Sprintf("commented on #%d; %s will see it", h.P.ID, h.P.Agent)
	}
}

// Reply adds an agent's answer to a proposal's thread.
func (e *Editor) Reply(agent string, id int, text string) error {
	p := e.proposals[id]
	if p == nil {
		return fmt.Errorf("no proposal #%d", id)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("empty reply")
	}
	p.Thread = append(p.Thread, Comment{Author: agent, Text: text})
	e.notify(agent, fmt.Sprintf("replied on #%d: %s", id, text))
	return nil
}

// TakeComments returns the user's comments on ps that the agent hasn't seen,
// and marks them seen.
func (e *Editor) TakeComments(ps []*Proposal) []Delivery {
	var out []Delivery
	for _, p := range ps {
		for i := range p.Thread {
			if c := &p.Thread[i]; c.Author == You.Author && !c.delivered {
				c.delivered = true
				out = append(out, Delivery{Proposal: p, Text: c.Text, Line: c.Line})
			}
		}
	}
	return out
}

// WatchComments serves the background watcher Claude Code runs when a session
// stops. The first call registers the watcher; it is superseded when a newer
// watcher registers or the session starts another turn.
func (e *Editor) WatchComments(session, watcher string, register bool) WatchResult {
	s := e.sessions[session]
	if s == nil {
		return WatchResult{Superseded: true}
	}
	ps := e.sessionProposals(session)
	if register {
		s.watcher, s.watched = watcher, nil
		for _, p := range ps {
			if p.Status == Pending {
				s.watched = append(s.watched, p)
			}
		}
	}
	if s.watcher != watcher {
		return WatchResult{Superseded: true}
	}
	r := WatchResult{Comments: append(e.TakeComments(ps), e.takeTrailComments(session)...)}
	if len(s.watched) > 0 && !slices.ContainsFunc(s.watched, func(p *Proposal) bool { return p.Status == Pending }) {
		r.Reviewed, s.watched = s.watched, nil
	}
	return r
}

// SessionComments returns the user's new comments on a session's proposals,
// for hooks that pass them to the agent mid-turn.
func (e *Editor) SessionComments(session string) []Delivery {
	return append(e.TakeComments(e.sessionProposals(session)), e.takeTrailComments(session)...)
}

func (e *Editor) sessionProposals(session string) []*Proposal {
	var ps []*Proposal
	for _, p := range e.proposals {
		if p.Session == session {
			ps = append(ps, p)
		}
	}
	slices.SortFunc(ps, func(a, b *Proposal) int { return a.ID - b.ID })
	return ps
}

// WatchResult answers a watcher's poll.
type WatchResult struct {
	Superseded bool
	Comments   []Delivery
	Reviewed   []*Proposal // once every proposal pending at registration has been reviewed
}

// Withdraw takes an agent's pending proposal out of its buffer so a revision
// may overlap it. On success call Supersede, otherwise the returned undo.
func (e *Editor) Withdraw(agent string, id int) (undo func(), err error) {
	p := e.proposals[id]
	switch {
	case p == nil || p.Agent != agent:
		return nil, fmt.Errorf("no proposal #%d of yours", id)
	case p.Status != Pending:
		return nil, fmt.Errorf("proposal #%d is %s; only pending proposals can be superseded", id, p.Status)
	case p.Tweaked:
		return nil, fmt.Errorf("the user is editing proposal #%d; wait for their review", id)
	}
	b := p.Buf
	i := slices.Index(b.pending, p)
	b.pending = slices.Delete(b.pending, i, i+1)
	return func() { b.pending = slices.Insert(b.pending, i, p) }, nil
}

// Supersede marks old as replaced by p, which takes over its thread.
func (e *Editor) Supersede(old, p *Proposal) {
	old.Status, old.Reason = Superseded, fmt.Sprintf("revised as #%d", p.ID)
	p.Thread = append(old.Thread, p.Thread...)
	e.reviewNotice(p.Agent, fmt.Sprintf("revised #%d as #%d (]h to review)", old.ID, p.ID), 1)
}

// threadLines wraps a thread to width columns, each comment led by its author.
func threadLines(thread []Comment, width int) []NoteLine {
	width = max(width, 20)
	var out []NoteLine
	for _, c := range thread {
		mine := c.Author == You.Author
		label := c.Author
		if c.At != "" {
			label += " (" + c.At + ")"
		}
		indent := strings.Repeat(" ", utf8.RuneCountInString(label)+1)
		line, empty := label+":", true
		for w := range strings.FieldsSeq(c.Text) {
			if !empty && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width {
				out = append(out, NoteLine{Mine: mine, Text: line})
				line = indent
			}
			line += " " + w
			empty = false
		}
		out = append(out, NoteLine{Mine: mine, Text: line})
	}
	return out
}

// rationaleLines wraps a proposal's rationale as note rows: "#3 claude: why".
func rationaleLines(p *Proposal, width int) []NoteLine {
	if p.Rationale == "" {
		return nil
	}
	ls := threadLines([]Comment{{Author: fmt.Sprintf("#%d %s", p.ID, p.Agent), Text: p.Rationale}}, width)
	for i := range ls {
		ls[i].Note = true
	}
	return ls
}
