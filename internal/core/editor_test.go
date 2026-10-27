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
	for k := range strings.FieldsSeq(ks) {
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
		e.Sel = Selection{}
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

func TestSaveCreatesDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "pkg", "a.go")
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b.Replace(0, 0, []byte("package pkg\n"), You)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "package pkg\n" {
		t.Errorf("file = %q", got)
	}
}

func TestOpenMissingFile(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "new.go"))
	if err != nil || b.Text.Len() != 0 || b.Dirty() {
		t.Errorf("err=%v len=%d dirty=%v", err, b.Text.Len(), b.Dirty())
	}
}

func TestEditing(t *testing.T) {
	cases := []struct {
		name, content, keys, want string
	}{
		{"insert", "world\n", "i h e l l o space esc", "hello world\n"},
		{"append", "ab\n", "a X esc", "aXb\n"},
		{"append at line end", "ab\ncd\n", "A X esc", "abX\ncd\n"},
		{"insert at first non-blank", "  ab\n", "g l I X esc", "  Xab\n"},
		{"goto first non-blank", "  ab\n", "g l g s i X esc", "  Xab\n"},
		{"select all", "ab\ncd\n", "% d", ""},
		{"find char", "a(b)c\n", "f ) d", "c\n"},
		{"till char", "a(b)c\n", "t ) d", ")c\n"},
		{"find char back", "a(b)c\n", "l l l l F ( d", "a\n"},
		{"till char back", "a(b)c\n", "l l l l T ( d", "a(\n"},
		{"find space", "a b\n", "f space d", "b\n"},
		{"find stays on the line", "ab\nc)\n", "f ) d", "b\nc)\n"},
		{"replace char", "abc\n", "r z", "zbc\n"},
		{"replace keeps line breaks", "ab\ncd\n", "x x r -", "--\n--\n"},
		{"esc cancels replace", "abc\n", "r esc d", "bc\n"},
		{"indent with tabs", "a\n\tb\n\n", "x x x >", "\ta\n\t\tb\n\n"},
		{"indent like the file", "x\n  y\n", ">", "  x\n  y\n"},
		{"unindent", "\t\ta\n    b\n  c\n", "x x x <", "\ta\nb\nc\n"},
		{"join", "a  \n   b\nc\n", "J", "a b\nc\n"},
		{"join selection", "a\nb\nc\n", "x x x J", "a b c\n"},
		{"join empty line", "a\n\nb\n", "J", "a\nb\n"},
		{"join last line", "a\n", "J", "a\n"},
		{"undo join", "a\nb\nc\n", "x x x J u", "a\nb\nc\n"},
		{"delete word", "foo bar baz\n", "w w d", "foo baz\n"},
		{"change word", "foo bar\n", "e c q u x esc", "qux bar\n"},
		{"delete back word", "foo bar\n", "g l h b d", "foo \n"},
		{"delete line", "one\ntwo\nthree\n", "j x d", "one\nthree\n"},
		{"delete two lines", "one\ntwo\nthree\n", "x x d", "three\n"},
		{"yank paste line", "one\ntwo\n", "x y p", "one\none\ntwo\n"},
		{"open below keeps indent", "\tif x {\n}\n", "o y esc", "\tif x {\n\ty\n}\n"},
		{"open above", "b\n", "O a esc", "a\nb\n"},
		{"enter indents", "  x\n", "g l i enter y esc", "  x\n  y\n"},
		{"backspace", "abc\n", "g l i backspace backspace esc", "a\n"},
		{"select mode extend", "abcdef\n", "l v l l l d", "af\n"},
		{"undo", "abc\n", "x d u", "abc\n"},
		{"undo groups insert session", "\n", "i a b c esc u", "\n"},
		{"redo", "abc\n", "d u U", "bc\n"},
		{"delete on empty buffer", "", "d", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEditor(t, c.content)
			keys(e, c.keys)
			if got := e.Buf.Text.String(); got != c.want {
				t.Errorf("text = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEmptyDeleteStaysClean(t *testing.T) {
	e := newEditor(t, "")
	keys(e, "d")
	if e.Buf.Dirty() {
		t.Error("no-op delete marked buffer dirty")
	}
}

func TestUndoRestoresAuthorship(t *testing.T) {
	e := newEditor(t, "abc\n")
	claude := text.Origin{Author: "claude"}
	e.Buf.Replace(0, 3, []byte("xyz"), claude)
	keys(e, "x d u")
	spans := e.Buf.Text.Spans(0, e.Buf.Text.Len())
	if len(spans) == 0 || spans[0].Origin != claude {
		t.Errorf("spans after undo = %v", spans)
	}
}

func TestWordSelection(t *testing.T) {
	e := newEditor(t, "foo.bar  baz\n")
	cases := []struct {
		keys     string
		from, to int
	}{
		{"w", 0, 3},      // "foo"
		{"w w", 3, 4},    // "."
		{"w w w", 4, 9},  // "bar  "
		{"e e e", 4, 7},  // "bar"
		{"g l b", 9, 12}, // "baz"
	}
	for _, c := range cases {
		e.Sel, e.Mode = Selection{}, Normal
		keys(e, c.keys)
		if from, to := e.SelRange(); from != c.from || to != c.to {
			t.Errorf("%q: selection [%d,%d) %q, want [%d,%d)", c.keys, from, to,
				e.Buf.Text.Slice(from, to), c.from, c.to)
		}
	}
}

func TestMenus(t *testing.T) {
	e := newEditor(t, "x\n")
	keys(e, "space")
	if e.Menu == nil || e.Menu.Title != "space" {
		t.Fatalf("menu = %v", e.Menu)
	}
	keys(e, "z")
	if e.Menu != nil {
		t.Error("unknown key should close the menu")
	}
}

func TestScratch(t *testing.T) {
	e := New(Scratch())
	e.Resize(80, 10)
	keys(e, "i h i esc : w enter")
	if !strings.Contains(e.Msg, "no file name") {
		t.Errorf("save without name: %q", e.Msg)
	}
	path := filepath.Join(t.TempDir(), "named.txt")
	e.execute("w " + path)
	if got, _ := os.ReadFile(path); string(got) != "hi" || e.Buf.Dirty() {
		t.Errorf("file = %q dirty=%v msg=%q", got, e.Buf.Dirty(), e.Msg)
	}
}

func TestOpenFileClosesUntouchedScratch(t *testing.T) {
	e := New(Scratch())
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.execute("e " + path)
	if e.Buf.Path != path || len(e.Bufs) != 1 {
		t.Errorf("buf=%q bufs=%d msg=%q", e.Buf.Path, len(e.Bufs), e.Msg)
	}
}
