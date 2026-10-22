package core

import (
	"strings"
	"testing"
)

func TestCommentReplyAndDelivery(t *testing.T) {
	e := newEditor(t, "x := 1\n")
	p := propose(t, e, "1", "2")
	keys(e, "] h")
	e.execute("comment why two?")
	e.execute("comment   ")
	if len(p.Thread) != 1 || p.Thread[0].Text != "why two?" {
		t.Fatalf("thread = %+v (msg %q)", p.Thread, e.Msg)
	}
	ds := e.TakeComments([]*Proposal{p})
	if len(ds) != 1 || ds[0].Text != "why two?" || len(e.TakeComments([]*Proposal{p})) != 0 {
		t.Errorf("deliveries = %+v", ds)
	}
	if err := e.Reply("claude", p.ID, "because the spec says so"); err != nil {
		t.Fatal(err)
	}
	if err := e.Reply("claude", 99, "x"); err == nil {
		t.Error("reply to unknown proposal accepted")
	}
	if h := e.HunkAtCursor(); h == nil || len(h.Notes) != 2 || !h.Notes[0].Mine || h.Notes[1].Mine {
		t.Errorf("notes = %+v", h)
	}
}

func TestWatchComments(t *testing.T) {
	e := newEditor(t, "a\n")
	e.Hook(HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	e.Hook(HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__propose_edit"})
	p := propose(t, e, "a", "b")
	if _, sup := e.WatchComments("s1", "w1", true); sup {
		t.Fatal("first watcher superseded")
	}
	keys(e, "] h")
	e.execute("comment no")
	if ds, _ := e.WatchComments("s1", "w1", false); len(ds) != 1 || ds[0].Proposal != p {
		t.Errorf("deliveries = %+v", ds)
	}
	e.WatchComments("s1", "w2", true)
	if _, sup := e.WatchComments("s1", "w1", false); !sup {
		t.Error("older watcher not superseded by newer")
	}
	e.Hook(HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	if _, sup := e.WatchComments("s1", "w2", false); !sup {
		t.Error("watcher not superseded by a new turn")
	}
	if _, sup := e.WatchComments("nope", "w", true); !sup {
		t.Error("unknown session should supersede")
	}
}

func TestWithdrawAndSupersede(t *testing.T) {
	e := newEditor(t, "a b\n")
	old := propose(t, e, "a", "A")
	keys(e, "] h")
	e.execute("comment uppercase both")
	undo, err := e.Withdraw("claude", old.ID)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := e.ProposeEdit("claude", e.Buf.Path, "a b", "A B", false, nil, "")
	if err != nil {
		t.Fatalf("revision overlapping the withdrawn proposal: %v", err)
	}
	e.Supersede(old, ps[0])
	if old.Status != Superseded || len(ps[0].Thread) != 1 || e.PendingCount() != 1 {
		t.Errorf("old=%s thread=%d pending=%d", old.Status, len(ps[0].Thread), e.PendingCount())
	}
	_ = undo

	other := propose(t, e, "\n", "!\n")
	undo, err = e.Withdraw("claude", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	undo()
	if e.PendingCount() != 2 || other.Status != Pending {
		t.Errorf("undo did not restore: pending=%d", e.PendingCount())
	}
	if _, err := e.Withdraw("someone-else", other.ID); err == nil {
		t.Error("withdrew another agent's proposal")
	}
	if _, err := e.Withdraw("claude", old.ID); err == nil {
		t.Error("withdrew a superseded proposal")
	}
}

func TestThreadLinesWrap(t *testing.T) {
	ls := threadLines([]Comment{{Author: "you", Text: "one two three four five six seven"}}, 20)
	got := make([]string, len(ls))
	for i, l := range ls {
		got[i] = l.Text
		if len(l.Text) > 20 {
			t.Errorf("line %q longer than 20", l.Text)
		}
	}
	want := "you: one two three|     four five six|     seven" // continuation lines align under "one"
	if strings.Join(got, "|") != want {
		t.Errorf("got %q", strings.Join(got, "|"))
	}
}
