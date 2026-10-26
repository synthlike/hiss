package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestParseNote(t *testing.T) {
	cases := []struct {
		in   string
		want noteLine
	}{
		{"2026-10-26  d372d90+  a/b.go:12  slow  here", noteLine{"2026-10-26", "d372d90+", "a/b.go", 12, "slow  here"}},
		{"2026-10-26  b.go:3  no commit", noteLine{date: "2026-10-26", path: "b.go", line: 3, text: "no commit"}},
		{"2026-10-26  d372d90  no location", noteLine{date: "2026-10-26", commit: "d372d90", text: "no location"}},
		{"written by hand", noteLine{text: "written by hand"}},
	}
	for _, c := range cases {
		if got := parseNote(c.in); got != c.want {
			t.Errorf("%q: %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestNotePicker(t *testing.T) {
	e := newEditor(t, "x\n")
	e.Root = filepath.Dir(e.Buf.Path)
	keys(e, "space n")
	if e.Picker != nil || e.Msg != "no notes yet (:n <text> to add one)" {
		t.Errorf("no notes: picker %v, msg %q", e.Picker != nil, e.Msg)
	}
	os.WriteFile(filepath.Join(e.Root, "g.go"), []byte("1\n2\n3\n"), 0o600)
	os.MkdirAll(filepath.Join(e.Root, ".hiss"), 0o755)
	notes := "2026-10-25  abcd123  g.go:2  older\n\n" +
		"2026-10-26  abcd123+  g.go:9  past the end\n" +
		"2026-10-26  gone.go:1  deleted\n"
	os.WriteFile(filepath.Join(e.Root, notesFile), []byte(notes), 0o600)
	keys(e, "space n")
	if e.Picker == nil {
		t.Fatalf("no picker: %q", e.Msg)
	}
	var got []string
	for _, it := range e.Picker.Items {
		got = append(got, it.Label+" "+it.Detail)
	}
	if want := []string{"gone.go:1 deleted", "g.go:9 past the end", "g.go:2 older"}; !slices.Equal(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
	keys(e, "o l d enter")
	if e.Buf.Name != "g.go" || e.line() != 1 {
		t.Errorf("older: at %s line %d, want g.go line 1", e.Buf.Name, e.line())
	}
	keys(e, "space n p a s t enter")
	if e.line() != 2 {
		t.Errorf("past the end: line %d, want the last, 2", e.line())
	}
	keys(e, "space n d e l enter")
	if e.Buf.Name != "g.go" || e.Msg != "gone.go doesn't exist any more" {
		t.Errorf("deleted: at %s, msg %q", e.Buf.Name, e.Msg)
	}
}

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
