package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// proposeRun proposes content for name in e's project, to run on accept.
func proposeRun(t *testing.T, e *Editor, name, content string) *Proposal {
	t.Helper()
	path := filepath.Join(e.Root, name)
	if err := e.CanRun(path); err != nil {
		t.Fatal(err)
	}
	p, err := e.WriteFile("claude-code", path, content, nil, "try it")
	if err != nil {
		t.Fatal(err)
	}
	e.RunOnAccept(p)
	return p
}

func TestCanRun(t *testing.T) {
	e, _ := rulesEditor(t, "q.txt", "")
	for name, want := range map[string]string{
		"q.txt": "", "notes.md": "",
		"q.xyz": "no rule in .hissrun matches q.xyz, so it can't run; write it where one does, such as .hiss/runs/",
	} {
		got := ""
		if err := e.CanRun(filepath.Join(e.Root, name)); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

func TestRunProposalOnAccept(t *testing.T) {
	e, ch := rulesEditor(t, "q.txt", "one\n")
	p := proposeRun(t, e, "q.txt", "one\n\n\nthree\n") // runs what it adds, past the blank lines
	keys(e, "] h")
	e.Msg = ""
	if got := strings.Join(e.HunkInfo(), " "); !strings.Contains(got, "try it  ▶ runs [upper]  (+ accept and run · - reject") {
		t.Errorf("info %q", got)
	}
	if msg := e.resolve(p, true, ""); msg != "accepted #1, wrote q.txt" || !p.Running() {
		t.Fatalf("accept: %q, running %v, msg %q", msg, p.Running(), e.Msg)
	}
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(p.Ran, "ran upper: ok (") || p.Running() || p.RanTail != "" {
		t.Errorf("ran %q, running %v, tail %q", p.Ran, p.Running(), p.RanTail)
	}
	if out := e.runs.out.Text.String(); !strings.Contains(out, "THREE\n") || strings.Contains(out, "ONE") {
		t.Errorf("*out*:\n%s", out)
	}
	if e.Buf.Name != "q.txt" {
		t.Errorf("on %s", e.Buf.Name)
	}
}

func TestRunProposalShares(t *testing.T) {
	e, ch := rulesEditor(t, "q.txt", "")
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[s]\nmatch = *.txt\nshare = true\ncmd = cat\n"), 0o600)
	p := proposeRun(t, e, "q.txt", "select 1\n")
	e.resolve(p, true, "")
	pump(t, e, ch, finished(e))
	if p.RanTail != "select 1\n" {
		t.Errorf("tail %q", p.RanTail)
	}
	if r := e.TakeRun("s1"); r != nil {
		t.Errorf("also reported to every session: %+v", r)
	}
}

func TestRunProposalConfirm(t *testing.T) {
	e, _ := envEditor(t, "a.txt")
	e.execute("env staging")
	p := proposeRun(t, e, "a.txt", "hello\n")
	if got := e.runNote(p); got != "runs [greet] on staging, asks first" {
		t.Errorf("note %q", got)
	}
	e.resolve(p, true, "")
	if e.Menu == nil || !p.Running() {
		t.Fatalf("no confirm: menu %v, running %v", e.Menu, p.Running())
	}
	keys(e, "esc")
	if p.Running() || p.Ran != "not run: the user didn't confirm running it on staging" {
		t.Errorf("after esc: running %v, ran %q", p.Running(), p.Ran)
	}
}

func TestRunProposalNotRun(t *testing.T) {
	e, _ := rulesEditor(t, "q.txt", "")
	e.startRun(&run{title: "slow", dir: e.Root, argv: []string{"sleep", "60"}})
	p := proposeRun(t, e, "q.txt", "x\n")
	msg := e.resolve(p, true, "")
	if want := "not run: slow is still running (C-c to cancel)"; p.Ran != want || p.Running() || !strings.HasSuffix(msg, "; "+want) {
		t.Errorf("ran %q, msg %q", p.Ran, msg)
	}
	if e.Msg != "" { // resolve's caller shows msg instead
		t.Errorf("msg %q", e.Msg)
	}
}

func TestRunProposalKept(t *testing.T) {
	e, _ := rulesEditor(t, "q.txt", "one\n")
	p := proposeRun(t, e, "q.txt", "one\ntwo\n")
	if sp := e.saved(p); !sp.Run || sp.RunAt != 4 {
		t.Errorf("saved run %v at %d", sp.Run, sp.RunAt)
	}
}
