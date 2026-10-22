package core

import (
	"strings"
	"testing"
)

func TestQuitWithPendingHunks(t *testing.T) {
	e := newEditor(t, "a\nb\n")
	propose(t, e, "a", "A")
	propose(t, e, "b", "B")
	for _, cmd := range []string{"q", "wq"} {
		e.execute(cmd)
		if e.Quit || !strings.HasPrefix(e.Msg, "2 pending hunks") {
			t.Errorf(":%s with pending hunks: quit=%v msg=%q", cmd, e.Quit, e.Msg)
		}
	}
	e.execute("haa")
	e.execute("q")
	if !e.Quit {
		t.Errorf(":q after review: msg %q", e.Msg)
	}
}

func TestQuitBangDiscardsPendingHunks(t *testing.T) {
	e := newEditor(t, "a\n")
	propose(t, e, "a", "A")
	e.execute("q!")
	if !e.Quit {
		t.Error(":q! should quit")
	}
}
