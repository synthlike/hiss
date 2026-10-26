package core

import (
	"strings"
	"testing"
)

// turn starts session's next turn and makes it the caller of hiss tools.
func turn(e *Editor, session string) {
	e.Hook(HookEvent{SessionID: session, Event: "UserPromptSubmit"})
	e.Hook(HookEvent{SessionID: session, Event: "PreToolUse", ToolName: "mcp__hiss__propose_edit"})
}

func TestChangeSets(t *testing.T) {
	e := newEditor(t, "a\nb\nc\nd\ne\n")
	turn(e, "s1")
	a, b, c := propose(t, e, "a\n", "A\n"), propose(t, e, "b\n", "B\n"), propose(t, e, "c\n", "C\n")
	turn(e, "s1") // the next turn is another set
	d := propose(t, e, "d\n", "D\n")
	turn(e, "s2") // so is another session's turn
	x := propose(t, e, "e\n", "E\n")

	if r, n := e.SetProgress(b); r != 0 || n != 3 {
		t.Errorf("first set: %d/%d reviewed, want 0/3", r, n)
	}
	if r, n := e.SetProgress(d); n != 1 {
		t.Errorf("second set: %d/%d, want one proposal", r, n)
	}
	if _, n := e.HalfReviewed(); n != 0 {
		t.Errorf("nothing reviewed yet, but a set of %d is half reviewed", n)
	}

	e.jumpToProposal(a)
	keys(e, "+")
	if r, n := e.HalfReviewed(); r != 1 || n != 3 {
		t.Errorf("after one: half reviewed %d/%d, want 1/3", r, n)
	}

	e.jumpToProposal(c)
	keys(e, ": s a enter")
	if b.Status != Accepted || c.Status != Accepted || d.Status != Pending || x.Status != Pending {
		t.Errorf(":sa: statuses %s %s, others %s %s", b.Status, c.Status, d.Status, x.Status)
	}
	if !strings.HasPrefix(e.Msg, "accepted 2 hunks of the set") {
		t.Errorf(":sa msg %q", e.Msg)
	}
	if _, n := e.HalfReviewed(); n != 0 {
		t.Errorf("all reviewed, but a set of %d is half reviewed", n)
	}
	if got := e.Buf.Text.String(); got != "A\nB\nC\nd\ne\n" {
		t.Errorf("text %q", got)
	}

	e.jumpToProposal(x)
	keys(e, ": s r space n o t space n o w enter")
	if x.Status != Rejected || x.Reason != "not now" || d.Status != Pending {
		t.Errorf(":sr: %s %q, other set %s", x.Status, x.Reason, d.Status)
	}
	keys(e, "g g : s a enter")
	if e.Msg != "no pending hunk on this line" {
		t.Errorf("off a hunk: msg %q", e.Msg)
	}
}

func TestChangeSetRevisions(t *testing.T) {
	e := newEditor(t, "a\nb\n")
	turn(e, "s1")
	a, b := propose(t, e, "a\n", "A\n"), propose(t, e, "b\n", "B\n")
	undo, err := e.Withdraw("claude", b.ID)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := e.ProposeEdit("claude", e.Buf.Path, "b\n", "BB\n", false, nil, "")
	if err != nil {
		undo()
		t.Fatal(err)
	}
	e.Supersede(b, ps[0])
	if set := e.SetOf(a); len(set) != 2 || set[1] != ps[0] {
		t.Errorf("set %v, want a and the revision", set)
	}
}
