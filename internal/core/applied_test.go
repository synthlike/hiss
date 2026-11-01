package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentEdit is an agent's edit as the tools make it: it applies unless held.
func agentEdit(t *testing.T, e *Editor, old, new string) *Proposal {
	t.Helper()
	ps, err := e.ProposeEdit("claude", e.Buf.Path, old, new, false, nil, "why")
	if err != nil {
		t.Fatal(err)
	}
	e.Settle("claude", ps, false)
	return ps[0]
}

func projectEditor(t *testing.T, content string) *Editor {
	t.Helper()
	e := newEditor(t, content)
	e.Root = filepath.Dir(e.Buf.Path)
	return e
}

func onDisk(t *testing.T, e *Editor) string {
	t.Helper()
	data, err := os.ReadFile(e.Buf.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEditAppliesAndSaves(t *testing.T) {
	e := projectEditor(t, "a\nb\nc\n")
	p := agentEdit(t, e, "b\n", "B\nB2\n")
	if p.Status != Applied || e.PendingCount() != 0 || e.ActiveToast() != nil {
		t.Errorf("status %s, %d pending, toast %v", p.Status, e.PendingCount(), e.ActiveToast())
	}
	if got := onDisk(t, e); got != "a\nB\nB2\nc\n" {
		t.Errorf("on disk %q", got)
	}
	if got := e.ChangedLines(); len(got) != 2 || !got[1] || !got[2] {
		t.Errorf("changed lines %v", got)
	}
	keys(e, ": c c enter")
	if got := e.ChangedLines(); len(got) != 0 || e.Msg != "cleared 1 change mark" {
		t.Errorf("after :cc: %v, msg %q", got, e.Msg)
	}
	keys(e, "space g enter") // the marks go; the list stays
	if e.line() != 1 {
		t.Errorf("space g: on line %d, want 1", e.line())
	}
}

func TestEditKeepsChangesMadeOutside(t *testing.T) {
	e := projectEditor(t, "a\nb\n")
	os.WriteFile(e.Buf.Path, []byte("a\nb\nfrom a shell\n"), 0o600)
	agentEdit(t, e, "a\n", "A\n")
	if got := onDisk(t, e); got != "A\nb\nfrom a shell\n" {
		t.Errorf("on disk %q", got)
	}
}

func TestEditsAreHeld(t *testing.T) {
	root := func(e *Editor, rules string) {
		os.WriteFile(filepath.Join(e.Root, rulesFile), []byte(rules), 0o600)
	}
	cases := []struct {
		name  string
		setup func(e *Editor)
		want  string
	}{
		{"unsaved changes", func(e *Editor) { keys(e, "i x esc") }, "it has the user's unsaved changes"},
		{"outside the project", func(e *Editor) { e.Root = t.TempDir() }, "it is outside the project"},
		{"no project", func(e *Editor) { e.Root = "" }, "it is outside the project"},
		{"review rule", func(e *Editor) { root(e, "[review]\nmatch = *.txt\n") }, "a [review] rule in .hissrun matches it"},
		{"broken rules", func(e *Editor) { root(e, "nonsense\n") }, ".hissrun can't be read, so its [review] rules can't be checked"},
	}
	for _, c := range cases {
		e := projectEditor(t, "a\n")
		c.setup(e)
		ps, err := e.ProposeEdit("claude", e.Buf.Path, "a", "b", false, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if why := e.Settle("claude", ps, false); why != c.want || ps[0].Status != Pending {
			t.Errorf("%s: held %q, status %s", c.name, why, ps[0].Status)
		}
	}
	e := projectEditor(t, "a\n") // the file with the [review] rules
	e.openFile(filepath.Join(e.Root, rulesFile))
	ps, err := e.WriteFile("claude", e.Buf.Path, "[sql]\n", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if why := e.Settle("claude", ps2(ps), false); ps.Status != Pending || !strings.Contains(why, rulesFile) {
		t.Errorf("the rules file: held %q, status %s", why, ps.Status)
	}
	e = projectEditor(t, "a\n")
	p := agentEdit(t, e, "a", "b") // no reason to hold it, and the agent asked
	if p.Status != Applied {
		t.Errorf("status %s", p.Status)
	}
	ps3, _ := e.ProposeEdit("claude", e.Buf.Path, "b", "c", false, nil, "")
	if e.Settle("claude", ps3, true); ps3[0].Status != Pending || e.ActiveToast() == nil {
		t.Errorf("review: status %s, toast %v", ps3[0].Status, e.ActiveToast())
	}
}

func ps2(p *Proposal) []*Proposal { return []*Proposal{p} }

func TestReviewRuleGlobs(t *testing.T) {
	e := projectEditor(t, "a\n")
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[review]\nmatch = migrations/* *.sol\n[review]\nmatch = go.mod\n"), 0o600)
	for rel, held := range map[string]bool{
		"migrations/1.sql": true, "src/a/C.sol": true, "go.mod": true, "migrations/old/1.sql": false, "main.go": false,
	} {
		b, err := e.OpenBuffer(filepath.Join(e.Root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if got := e.holdReason(b) != ""; got != held {
			t.Errorf("%s: held %v, want %v", rel, got, held)
		}
	}
}

func TestUndoSet(t *testing.T) {
	e := projectEditor(t, "one\ntwo\nthree\n")
	agentEdit(t, e, "two\n", "TWO\n")
	agentEdit(t, e, "TWO\n", "2\n2\n")  // the same text again
	agentEdit(t, e, "one\n", "")        // a deletion, right before it
	agentEdit(t, e, "three\n", "3\n")   // right after it
	agentEdit(t, e, "2\n3\n", "x\n3\n") // a piece of each of two earlier edits
	keys(e, ": s u space n o enter")
	if got := onDisk(t, e); got != "one\ntwo\nthree\n" {
		t.Errorf("on disk %q, msg %q", got, e.Msg)
	}
	if e.Msg != "undid 5 changes" || len(e.Buf.applied) != 0 || e.Buf.Dirty() {
		t.Errorf("msg %q, %d applied, dirty %v", e.Msg, len(e.Buf.applied), e.Buf.Dirty())
	}
	ds := e.TakeComments(e.ProposalsBy("claude"))
	if len(ds) != 1 || !strings.Contains(ds[0].Text, "I undid your changes") || !strings.HasSuffix(ds[0].Text, ": no") {
		t.Errorf("told the agent %+v", ds)
	}
	keys(e, ": s u enter")
	if e.Msg != noChanges {
		t.Errorf("nothing left: msg %q", e.Msg)
	}
}

func TestUndoSetKeepsWhatChangedSince(t *testing.T) {
	e := projectEditor(t, "aa\nb\nc\n")
	first := agentEdit(t, e, "aa\n", "AA\n")
	agentEdit(t, e, "c\n", "C\n")
	e.setHead(1)
	keys(e, "i x esc")   // the user edits the first change
	keys(e, ": w enter") // and saves it
	keys(e, ": s u enter")
	if got := onDisk(t, e); got != "AxA\nb\nc\n" {
		t.Errorf("on disk %q", got)
	}
	if e.Msg != "undid 1 change; changed since: f.txt:1" || first.Status != Applied {
		t.Errorf("msg %q, status %s", e.Msg, first.Status)
	}
	ds := e.TakeComments(e.ProposalsBy("claude"))
	if len(ds) != 1 || !strings.Contains(ds[0].Text, "edited since and stayed: f.txt:1") {
		t.Errorf("told the agent %+v", ds)
	}
}

func TestUndoSetLeavesUnsavedEditsUnsaved(t *testing.T) {
	e := projectEditor(t, "a\nb\n")
	agentEdit(t, e, "a\n", "A\n")
	gotoText(t, e, "b")
	keys(e, "i x esc")
	keys(e, ": s u enter")
	if got := e.Buf.Text.String(); got != "a\nxb\n" {
		t.Errorf("buffer %q", got)
	}
	if got := onDisk(t, e); got != "A\nb\n" || !e.Buf.Dirty() {
		t.Errorf("on disk %q, dirty %v", got, e.Buf.Dirty())
	}
}

func TestUndoSetRemovesCreatedFile(t *testing.T) {
	e := projectEditor(t, "a\n")
	path := filepath.Join(e.Root, "sub", "new.txt")
	p, err := e.WriteFile("claude", path, "hello\n", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	e.Settle("claude", ps2(p), false)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	keys(e, ": s u enter")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat after undo: %v, msg %q", err, e.Msg)
	}
	if e.Find(path).Dirty() {
		t.Error("the emptied buffer counts as unsaved")
	}
}

func TestUndoSetByTurn(t *testing.T) {
	e := projectEditor(t, "a\nb\nc\n")
	turn(e, "s")
	agentEdit(t, e, "a\n", "A\n")
	turn(e, "s")
	agentEdit(t, e, "b\n", "B\n")
	turn(e, "s")
	agentEdit(t, e, "c\n", "C\n")
	gotoText(t, e, "B") // the set under the cursor, not the newest
	keys(e, ": s u enter")
	if got := onDisk(t, e); got != "A\nb\nC\n" {
		t.Errorf("on disk %q", got)
	}
	gotoText(t, e, "b") // nothing here now: the newest
	keys(e, ": s u enter")
	if got := onDisk(t, e); got != "A\nb\nc\n" {
		t.Errorf("on disk %q", got)
	}
}

func TestUndoSetsOneAfterAnother(t *testing.T) {
	e := projectEditor(t, "a\nb\n")
	turn(e, "s")
	agentEdit(t, e, "a\n", "A\n")
	turn(e, "s")
	agentEdit(t, e, "A\nb\n", "whole\n") // over the first turn's change and more
	turn(e, "s")
	agentEdit(t, e, "whole\n", "again\n")
	for _, want := range []string{"whole\n", "A\nb\n", "a\nb\n"} {
		keys(e, ": s u enter")
		if got := onDisk(t, e); got != want {
			t.Errorf("on disk %q, want %q; msg %q", got, want, e.Msg)
		}
	}
}

func TestUndoSetKeepsAnotherSessionsChanges(t *testing.T) {
	e := projectEditor(t, "a\nb\nc\n")
	turn(e, "s1")
	first := agentEdit(t, e, "a\n", "A\n")
	turn(e, "s2")
	other := agentEdit(t, e, "b\n", "B\n")
	turn(e, "s1")
	last := agentEdit(t, e, "c\n", "C\n")
	last.Turn = first.Turn // one set, with the other session's change in between
	keys(e, ": s u enter")
	if got := onDisk(t, e); got != "a\nB\nc\n" || other.Status != Applied {
		t.Errorf("on disk %q, other %s, msg %q", got, other.Status, e.Msg)
	}
}

func TestAppliedEditsStayOutOfReview(t *testing.T) {
	e := projectEditor(t, "a\nb\nc\n")
	turn(e, "s")
	agentEdit(t, e, "a\n", "A\n")
	held := propose(t, e, "b\n", "B\n")
	propose(t, e, "c\n", "C\n")
	if r, n := e.SetProgress(held); r != 0 || n != 2 {
		t.Errorf("progress %d/%d", r, n)
	}
	keys(e, "g g j +") // accepted in review: not ours to undo
	keys(e, ": s u enter")
	if got := onDisk(t, e); got != "a\nB\nc\n" {
		t.Errorf("on disk %q", got)
	}
}
