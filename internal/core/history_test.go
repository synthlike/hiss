package core

import (
	"strings"
	"testing"
	"time"
)

func TestRunHistory(t *testing.T) {
	e, ch := rulesEditor(t, "q.txt", "one\n\ntwo\n")
	keys(e, "[ r")
	if e.Msg != "nothing run yet" {
		t.Errorf("before any run: msg %q", e.Msg)
	}
	runOutput(t, e, ch, "enter")
	gotoText(t, e, "two")
	if got := runOutput(t, e, ch, "enter"); !strings.HasPrefix(got, "TWO\n") {
		t.Fatalf("second run: %q", got)
	}
	now := e.now()
	e.now = func() time.Time { return now.Add(2 * time.Minute) }
	if got := e.OutInfo(); got != "run 2/2 · q.txt · ok · 2m ago" {
		t.Errorf("info %q", got)
	}

	keys(e, "[ r")
	if !strings.HasPrefix(e.OutTail(3)[0], "ONE") || e.OutInfo() != "run 1/2 · q.txt · ok · 2m ago" {
		t.Errorf("older: %q, info %q", e.OutTail(3), e.OutInfo())
	}
	keys(e, "[ r")
	if e.Msg != "no older runs (2 runs kept)" {
		t.Errorf("past the oldest: msg %q", e.Msg)
	}
	keys(e, "] r ] r")
	if !strings.HasPrefix(e.OutTail(3)[0], "TWO") || e.Msg != "no newer runs" {
		t.Errorf("newer: %q, msg %q", e.OutTail(3), e.Msg)
	}

	keys(e, "[ r : r r enter") // reruns the one shown
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(e.OutTail(3)[0], "ONE") || e.OutInfo() != "run 3/3 · q.txt · ok · just now" {
		t.Errorf("rerun the older: %q, info %q", e.OutTail(3), e.OutInfo())
	}

	keys(e, "esc [ r")
	if !e.runs.panel || e.runs.at != 2 {
		t.Errorf("[r on a hidden panel: panel %v, at %d", e.runs.panel, e.runs.at)
	}
	keys(e, "space o [ r")
	if e.Msg != "run 2/3 · q.txt · ok · 2m ago" || !strings.Contains(e.Buf.Text.String(), "TWO") {
		t.Errorf("in *out*: msg %q", e.Msg)
	}
}

func TestRunHistoryWhileRunning(t *testing.T) {
	e, ch := rulesEditor(t, "q.txt", "one\n")
	runOutput(t, e, ch, "enter")
	e.startRun(&run{title: "slow", dir: e.Root, argv: []string{"sleep", "60"}})
	keys(e, "[ r")
	if e.Msg != "slow is still running (C-c to cancel)" {
		t.Errorf("msg %q", e.Msg)
	}
	if got := e.OutInfo(); got != "running slow (C-c)" {
		t.Errorf("info %q", got)
	}
}

func TestRunHistoryKeepsTheLast(t *testing.T) {
	e := newEditor(t, "")
	e.outBuffer()
	for i := range keptRuns + 5 {
		e.keep(&run{what: string(rune('a' + i))}, "ok")
	}
	if h := e.runs.history; len(h) != keptRuns || h[0].r.what != "f" || e.runs.at != keptRuns-1 {
		t.Errorf("%d kept, oldest %q, at %d", len(h), h[0].r.what, e.runs.at)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		3 * time.Second: "just now", 40 * time.Second: "40s ago", 59 * time.Minute: "59m ago",
		5 * time.Hour: "5h ago", 72 * time.Hour: "3d ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}
