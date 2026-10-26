package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// persistEditor returns an editor rooted at a project holding a.go, keeping
// proposals as the hiss serving the project does.
func persistEditor(t *testing.T, root, content string) *Editor {
	t.Helper()
	path := filepath.Join(root, "a.go")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := New(b)
	e.Root = root
	e.Resize(80, 20)
	e.RestoreProposals()
	return e
}

func proposeIn(t *testing.T, e *Editor, old, new string) *Proposal {
	t.Helper()
	ps, err := e.ProposeEdit("claude", filepath.Join(e.Root, "a.go"), old, new, false, nil, "why "+new)
	if err != nil {
		t.Fatal(err)
	}
	return ps[0]
}

func TestProposalsSurviveRestart(t *testing.T) {
	root := t.TempDir()
	e := persistEditor(t, root, "one\ntwo\nthree\n")
	proposeIn(t, e, "one\n", "ONE\n")
	p := proposeIn(t, e, "three\n", "THREE\n")
	if err := e.Reply("claude", p.ID, "shout it"); err != nil {
		t.Fatal(err)
	}
	e.SaveProposals()

	e2 := persistEditor(t, root, "")
	if e2.Msg != "restored 2 pending hunks (]h to review)" {
		t.Errorf("msg = %q", e2.Msg)
	}
	q := e2.Proposal(p.ID)
	if q == nil || q.Status != Pending || q.Rationale != "why THREE\n" || q.Start != 8 || q.End != 14 {
		t.Fatalf("restored #%d = %+v", p.ID, q)
	}
	if len(q.Thread) != 1 || q.Thread[0].Text != "shout it" {
		t.Errorf("thread = %+v", q.Thread)
	}
	if n := proposeIn(t, e2, "two\n", "TWO\n"); n.ID != 3 {
		t.Errorf("next id = %d, want 3", n.ID)
	}
}

func TestRestoreDropsChangedText(t *testing.T) {
	root := t.TempDir()
	e := persistEditor(t, root, "one\ntwo\n")
	proposeIn(t, e, "one\n", "ONE\n")
	proposeIn(t, e, "two\n", "TWO\n")
	e.SaveProposals()

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("one\n2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e2 := persistEditor(t, root, "")
	if e2.Msg != "restored 1 pending hunk (]h to review); dropped 1 whose text changed" {
		t.Errorf("msg = %q", e2.Msg)
	}
	if e2.PendingCount() != 1 || e2.Proposal(1) == nil {
		t.Errorf("pending = %d", e2.PendingCount())
	}
}

func TestRestorePicksTheNearestRepeat(t *testing.T) {
	root := t.TempDir()
	e := persistEditor(t, root, "x\nx\nx\n")
	ps, err := e.ProposeEdit("claude", filepath.Join(root, "a.go"), "x\n", "y\n", true, nil, "")
	if err != nil || len(ps) != 3 {
		t.Fatalf("%v, %d proposals", err, len(ps))
	}
	e.resolve(ps[0], false, "")
	e.resolve(ps[2], false, "")
	e.SaveProposals()

	e2 := persistEditor(t, root, "")
	if q := e2.Proposal(ps[1].ID); q == nil || q.Start != 2 {
		t.Errorf("restored = %+v, want it on line 2", q)
	}
}

func TestKeptProposalsGoWhenNonePending(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, proposalsFile)
	e := persistEditor(t, root, "one\n")
	p := proposeIn(t, e, "one\n", "ONE\n")
	e.SaveProposals()
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	e.resolve(p, false, "no")
	e.SaveProposals()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("%s after the last review: %v", proposalsFile, err)
	}

	proposeIn(t, e, "one\n", "1\n")
	e.SaveProposals()
	e.execute("q!")
	if _, err := os.Stat(file); !os.IsNotExist(err) || !e.Quit {
		t.Errorf("%s after :q!: %v", proposalsFile, err)
	}
}

func TestOnlyTheServingEditorKeepsProposals(t *testing.T) {
	root := t.TempDir()
	e := persistEditor(t, root, "one\n")
	e.persist = persist{} // like a second hiss, which doesn't restore
	proposeIn(t, e, "one\n", "ONE\n")
	e.SaveProposals()
	if _, err := os.Stat(filepath.Join(root, proposalsFile)); !os.IsNotExist(err) {
		t.Errorf("written without restoring first: %v", err)
	}
}

func TestTweakedProposalIsKeptAsProposed(t *testing.T) {
	root := t.TempDir()
	e := persistEditor(t, root, "one\n")
	proposeIn(t, e, "one\n", "ONE\n")
	keys(e, "g g j i Z esc") // tweak: type on the proposed line
	if !e.Proposal(1).Tweaked {
		t.Fatal("not tweaked")
	}
	e.SaveProposals()
	data, err := os.ReadFile(filepath.Join(root, proposalsFile))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `"old": "one\n"`) || !strings.Contains(s, `"new": "ONE\n"`) {
		t.Errorf("kept:\n%s", s)
	}
}
