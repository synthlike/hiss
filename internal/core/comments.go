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
	Text string
}

// Delivery is a user comment handed to an agent.
type Delivery struct {
	Proposal *Proposal
	Text     string
	Line     string // diff line the comment is on, if any
}

func (e *Editor) commentOnHunk(text string) {
	h := e.HunkAtCursor()
	switch {
	case h == nil:
		e.Msg = "no pending hunk on this line"
	case text == "":
		e.Msg = "empty comment"
	default:
		c := Comment{Author: You.Author, Text: text}
		if r := e.DiffRow(); r > 0 {
			c.At, c.Line = fmt.Sprintf("+%d", r), "+ "+h.Added[r-1]
		} else if h.Removes {
			line := e.line()
			c.At = fmt.Sprintf("-%d", line+1)
			c.Line = "- " + string(e.Buf.Text.Slice(e.Buf.Text.LineStart(line), e.Buf.LineEnd(line)))
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
				out = append(out, Delivery{p, c.Text, c.Line})
			}
		}
	}
	return out
}

// WatchComments serves the background watcher Claude Code runs when a session
// stops. The first call registers the watcher; it is superseded when a newer
// watcher registers or the session starts another turn.
func (e *Editor) WatchComments(session, watcher string, register bool) (ds []Delivery, superseded bool) {
	s := e.sessions[session]
	if s == nil {
		return nil, true
	}
	if register {
		s.watcher = watcher
	}
	if s.watcher != watcher {
		return nil, true
	}
	var ps []*Proposal
	for _, p := range e.proposals {
		if p.Session == session {
			ps = append(ps, p)
		}
	}
	slices.SortFunc(ps, func(a, b *Proposal) int { return a.ID - b.ID })
	return e.TakeComments(ps), false
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
	e.notify(p.Agent, fmt.Sprintf("revised #%d as #%d (]h to review)", old.ID, p.ID))
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
		for _, w := range strings.Fields(c.Text) {
			if !empty && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width {
				out = append(out, NoteLine{mine, line})
				line = indent
			}
			line += " " + w
			empty = false
		}
		out = append(out, NoteLine{mine, line})
	}
	return out
}
