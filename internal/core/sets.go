package core

import (
	"fmt"
	"slices"
)

// A change set is what an agent proposed in one turn: its proposals with the
// same session and turn. Superseded proposals drop out and their revisions
// count instead. Without hooks there are no sessions or turns, so each
// agent's proposals form one set. :sa and :sr review the set of the hunk
// under the cursor; a set reviewed only in part may leave the tree broken,
// so the status line says so.

type setKey struct {
	agent, session string
	turn           int
}

func (p *Proposal) set() setKey { return setKey{p.Agent, p.Session, p.Turn} }

// SetOf returns the proposals in p's set, oldest first.
func (e *Editor) SetOf(p *Proposal) []*Proposal {
	var ps []*Proposal
	for _, q := range e.proposals {
		if q.set() == p.set() && q.Status != Superseded {
			ps = append(ps, q)
		}
	}
	slices.SortFunc(ps, func(a, b *Proposal) int { return a.ID - b.ID })
	return ps
}

// SetProgress returns how many proposals in p's set are reviewed, and how
// many it has.
func (e *Editor) SetProgress(p *Proposal) (reviewed, total int) {
	for _, q := range e.SetOf(p) {
		if q.Status != Pending {
			reviewed++
		}
		total++
	}
	return reviewed, total
}

// HalfReviewed returns the progress of the newest set with some proposals
// reviewed and some pending; total is 0 when there is none.
func (e *Editor) HalfReviewed() (reviewed, total int) {
	newest := 0
	for _, b := range e.Bufs {
		for _, p := range b.pending {
			if r, t := e.SetProgress(p); r > 0 && p.ID > newest {
				newest, reviewed, total = p.ID, r, t
			}
		}
	}
	return reviewed, total
}

// reviewSet accepts or rejects the pending proposals in the set of the hunk
// under the cursor.
func (e *Editor) reviewSet(accept bool, reason string) {
	h := e.HunkAtCursor()
	if h == nil {
		e.Msg = "no pending hunk on this line"
		return
	}
	n, failed := 0, 0
	for _, p := range e.SetOf(h.P) {
		if p.Status != Pending {
			continue
		}
		e.resolve(p, accept, reason)
		n++
		if accept && p.Reason != "" {
			failed++
		}
	}
	verb := "rejected"
	if accept {
		verb = "accepted"
	}
	msg := verb + " " + plural(n, "hunk") + " of the set"
	if failed > 0 {
		msg += fmt.Sprintf("; saving failed for %d", failed)
	}
	if e.PendingCount() > 0 {
		e.jumpHunk(1)
	}
	e.Msg = msg
}
