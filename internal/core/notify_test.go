package core

import (
	"testing"
	"time"
)

func TestToastWaitsForFocus(t *testing.T) {
	e := newEditor(t, "x\n")
	clock := time.Now()
	e.now = func() time.Time { return clock }

	e.SetFocused(false)
	e.notify("claude", "asks: Commit?")
	clock = clock.Add(time.Minute)
	if e.ActiveToast() == nil {
		t.Fatal("toast expired while hiss was unfocused")
	}
	e.SetFocused(true)
	clock = clock.Add(toastFor - time.Second)
	if e.ActiveToast() == nil {
		t.Fatal("toast gone before toastFor after focusing")
	}
	clock = clock.Add(time.Second)
	if e.ActiveToast() != nil {
		t.Error("toast still shown toastFor after focusing")
	}

	e.notify("claude", "while focused") // counts down right away
	clock = clock.Add(toastFor)
	if e.ActiveToast() != nil {
		t.Error("focused toast should expire after toastFor")
	}
}

func TestNoProposalToastWhileReviewing(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	propose(t, e, "a\n", "A\n")
	if e.ActiveToast() == nil {
		t.Fatal("no toast for the first proposal")
	}
	e.Toast = Toast{}
	propose(t, e, "b\n", "B\n") // #1 is still pending: the user is reviewing
	if tst := e.ActiveToast(); tst != nil {
		t.Errorf("toast %q while reviewing", tst.Text)
	}
	e.SetFocused(false) // away: say it, for the desktop notification too
	propose(t, e, "c\n", "C\n")
	if e.ActiveToast() == nil || len(e.TakeNotices()) != 2 {
		t.Error("no toast or notice while unfocused")
	}
}
