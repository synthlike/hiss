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
