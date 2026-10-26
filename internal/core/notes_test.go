package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func readNotes(t *testing.T, e *Editor) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.Root, notesFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNoteOutsideGit(t *testing.T) {
	e := newEditor(t, "a\nb\n") // f.txt in a temp dir, which is not a git repository
	e.Root = filepath.Dir(e.Buf.Path)
	e.now = func() time.Time { return time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC) }
	keys(e, "j : n o t e space s l o w enter")
	keys(e, ": n space a g a i n enter")
	want := "2026-10-26  f.txt:2  slow\n2026-10-26  f.txt:2  again\n"
	if got := readNotes(t, e); got != want {
		t.Errorf("notes %q, want %q", got, want)
	}
	if e.Msg != "noted in "+notesFile {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestNoteCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	e := newEditor(t, "a\n")
	dir := filepath.Dir(e.Buf.Path)
	e.Root = dir
	run := func(args ...string) string {
		t.Helper()
		out, err := git(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	if got := noteCommit(dir, "f.txt", false); got != "" {
		t.Errorf("before git init: %q, want none", got)
	}
	run("init", "-q")
	run("add", "f.txt")
	run("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "x")
	hash := run("rev-parse", "--short", "HEAD")
	cases := []struct {
		name  string
		path  string
		dirty bool
		want  string
	}{
		{"committed", "f.txt", false, hash},
		{"unsaved", "f.txt", true, hash + "+"},
		{"no file", "", false, hash},
	}
	for _, c := range cases {
		if got := noteCommit(dir, c.path, c.dirty); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o600)
	if got := noteCommit(dir, "f.txt", false); got != hash+"+" {
		t.Errorf("changed on disk: %q, want %q", got, hash+"+")
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("c\n"), 0o600)
	if got := noteCommit(dir, "new.txt", false); got != hash+"+" {
		t.Errorf("untracked: %q, want %q", got, hash+"+")
	}
}
