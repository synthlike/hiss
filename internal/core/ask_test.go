package core

import "testing"

func TestAskPickOption(t *testing.T) {
	e := newEditor(t, "x\n")
	q := e.Ask("claude", "Commit?", []string{"yes", "no"})
	if e.Asking != q {
		t.Fatal("question should open in normal mode")
	}
	keys(e, "j down enter") // j is typed; down then moves from the typed answer to the first option
	if !q.Answered || q.Answer != "yes" || e.Asking != nil {
		t.Errorf("answered=%v answer=%q", q.Answered, q.Answer)
	}
	if e.Buf.Text.Len() != 2 {
		t.Error("keys reached the buffer")
	}
}

func TestAskTypedAnswer(t *testing.T) {
	e := newEditor(t, "x\n")
	q := e.Ask("claude", "Name?", nil)
	keys(e, "enter") // nothing typed: no answer
	if q.Answered {
		t.Fatal("empty answer accepted")
	}
	keys(e, "f o o space b backspace enter")
	if q.Answer != "foo " {
		t.Errorf("answer = %q", q.Answer)
	}
}

func TestAskWaitsUntilNormalMode(t *testing.T) {
	e := newEditor(t, "x\n")
	keys(e, "i")
	q := e.Ask("claude", "Commit?", []string{"yes"})
	if e.Asking != nil {
		t.Fatal("question opened while typing")
	}
	keys(e, "a esc")
	if e.Asking != q {
		t.Fatal("question should open back in normal mode")
	}
}

func TestAskCloseReopenWithdraw(t *testing.T) {
	e := newEditor(t, "x\n")
	q := e.Ask("claude", "Commit?", []string{"yes"})
	keys(e, "esc j")
	if e.Asking != nil || e.QuestionCount() != 1 {
		t.Fatalf("esc should close it for later: asking=%v", e.Asking)
	}
	keys(e, ": a n s w e r enter")
	if e.Asking != q {
		t.Fatal(":answer should reopen it")
	}
	e.DropQuestion(q.ID)
	if e.Asking != nil || e.Question(q.ID) != nil || e.Msg != "claude withdrew its question" {
		t.Errorf("after withdraw: asking=%v msg=%q", e.Asking, e.Msg)
	}
}
