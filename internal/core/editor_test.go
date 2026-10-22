package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/text"
)

func newEditor(t *testing.T, content string) *Editor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := New(b)
	e.Resize(80, 10)
	return e
}

// keys feeds space-separated key names.
func keys(e *Editor, ks string) {
	for _, k := range strings.Fields(ks) {
		e.HandleKey(k)
	}
}

func TestMovement(t *testing.T) {
	e := newEditor(t, "héllo\nab\nworld\n")
	cases := []struct {
		keys      string
		line, col int
	}{
		{"l l l", 0, 3},     // multibyte é counts as one column
		{"l l l j", 1, 2},   // clamped to end of the shorter line
		{"l l l j j", 2, 3}, // goal column restored
		{"g e", 3, 0},       // empty last line after trailing newline
		{"g e g g", 0, 0},
		{"l l g l", 0, 5},
		{"h h", 0, 0},
	}
	for _, c := range cases {
		e.moveTo(0)
		keys(e, c.keys)
		if l, col := e.Cursor(); l != c.line || col != c.col {
			t.Errorf("%q: cursor %d:%d, want %d:%d", c.keys, l, col, c.line, c.col)
		}
	}
}

func TestScrollFollowsCursor(t *testing.T) {
	e := newEditor(t, strings.Repeat("x\n", 50))
	keys(e, strings.Repeat("j ", 20))
	if e.Top != 20-e.Height+1 {
		t.Errorf("Top = %d, want %d", e.Top, 20-e.Height+1)
	}
	keys(e, "g g")
	if e.Top != 0 {
		t.Errorf("Top = %d after gg", e.Top)
	}
}

func TestQuitAndSave(t *testing.T) {
	e := newEditor(t, "hi\n")
	e.Buf.Replace(0, 0, []byte("oh "), text.Origin{Author: "you"})
	keys(e, ": q enter")
	if e.Quit || !strings.Contains(e.Msg, "unsaved") {
		t.Fatalf("quit with unsaved changes: Quit=%v Msg=%q", e.Quit, e.Msg)
	}
	keys(e, ": w q enter")
	if !e.Quit || e.Buf.Dirty() {
		t.Fatalf("Quit=%v Dirty=%v Msg=%q", e.Quit, e.Buf.Dirty(), e.Msg)
	}
	got, _ := os.ReadFile(e.Buf.Path)
	if string(got) != "oh hi\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveKeepsPermissions(t *testing.T) {
	e := newEditor(t, "x")
	if err := os.Chmod(e.Buf.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.Buf.Save(); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(e.Buf.Path)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestOpenMissingFile(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "new.go"))
	if err != nil || b.Text.Len() != 0 || b.Dirty() {
		t.Errorf("err=%v len=%d dirty=%v", err, b.Text.Len(), b.Dirty())
	}
}
