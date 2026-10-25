package core

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/lsp"
	"github.com/synthlike/hiss/internal/text"
)

type sent struct {
	op      string
	version int
	text    string
}

type fakeServer struct{ sent []sent }

func (f *fakeServer) Open(path, lang string, version int, t text.Text) {
	f.sent = append(f.sent, sent{"open", version, t.String()})
}

func (f *fakeServer) Change(path string, version int, t text.Text) {
	f.sent = append(f.sent, sent{"change", version, t.String()})
}

func (f *fakeServer) Shutdown(time.Duration) { f.sent = append(f.sent, sent{op: "shutdown"}) }

// fakeLSP makes the editor start f for every language, and runs posted work
// right away.
func fakeLSP(t *testing.T, e *Editor) (f *fakeServer, diags *func(lsp.Diagnostics)) {
	f = &fakeServer{}
	diags = new(func(lsp.Diagnostics))
	old := startServer
	t.Cleanup(func() { startServer = old })
	startServer = func(_ lsp.Server, _ string, d func(lsp.Diagnostics)) (langServer, error) {
		*diags = d
		return f, nil
	}
	e.Post = func(fn func(*Editor)) { fn(e) }
	return f, diags
}

func newGoEditor(t *testing.T, content string) *Editor {
	t.Helper()
	b, err := Open(filepath.Join(t.TempDir(), "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte(content), text.Origin{})
	e := New(b)
	e.Resize(80, 10)
	return e
}

func TestSyncLSP(t *testing.T) {
	e := newGoEditor(t, "package a\n")
	f, _ := fakeLSP(t, e)
	e.SyncLSP()
	e.SyncLSP() // nothing changed
	keys(e, "i x y esc")
	e.SyncLSP()
	want := []sent{{"open", 0, "package a\n"}, {"change", 2, "xypackage a\n"}}
	if len(f.sent) != len(want) || f.sent[0] != want[0] || f.sent[1] != want[1] {
		t.Errorf("sent %+v, want %+v", f.sent, want)
	}
	e.StopLSP()
	if f.sent[len(f.sent)-1].op != "shutdown" {
		t.Errorf("StopLSP didn't shut the server down: %+v", f.sent)
	}
}

func TestSyncLSPSkipsOtherFiles(t *testing.T) {
	e := newEditor(t, "hello\n") // f.txt
	f, _ := fakeLSP(t, e)
	e.SyncLSP()
	if len(f.sent) != 0 {
		t.Errorf("sent %+v for a .txt file", f.sent)
	}
}

func TestDiagnostics(t *testing.T) {
	e := newGoEditor(t, "package a\n")
	_, diags := fakeLSP(t, e)
	e.SyncLSP()
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path, Diags: []lsp.Diagnostic{
		{Severity: lsp.Error, Message: "a"}, {Severity: lsp.Error, Message: "b"},
		{Severity: lsp.Warning, Message: "c"}, {Severity: lsp.Hint, Message: "d"}}})
	if errs, warns := e.Buf.DiagCounts(); errs != 2 || warns != 1 {
		t.Errorf("DiagCounts() = %d, %d; want 2, 1", errs, warns)
	}
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path})
	if n := len(e.Buf.Diags()); n != 0 || len(e.Buf.anchors) != 0 {
		t.Errorf("empty diagnostics left %d diagnostics and %d anchors", n, len(e.Buf.anchors))
	}
}

func at(line, char int) lsp.Range {
	return lsp.Range{Start: lsp.Position{Line: line, Character: char}, End: lsp.Position{Line: line, Character: char + 1}}
}

func diagText(e *Editor, d *Diag) string {
	start, end := d.Range()
	return string(e.Buf.Text.Slice(start, end))
}

func TestDiagnosticsFollowEdits(t *testing.T) {
	e := newGoEditor(t, "package a\nvar x = y\n")
	_, diags := fakeLSP(t, e)
	e.SyncLSP() // the server has version 0
	keys(e, "O / / space n e w esc")
	e.SyncLSP() // and the edited text, but answers for version 0 first
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path, Version: 0, UTF8: true, Diags: []lsp.Diagnostic{{Range: at(1, 8), Message: "undefined: y"}}})
	ds := e.Buf.Diags()
	if len(ds) != 1 || diagText(e, ds[0]) != "y" || ds[0].Severity != lsp.Error {
		t.Fatalf("diagnostics %+v, want one error on y", ds)
	}
	keys(e, "g g i z z esc") // edits after placing move it too
	if got := diagText(e, ds[0]); got != "y" {
		t.Errorf("after another edit the diagnostic covers %q", got)
	}
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path, Version: 99, Diags: []lsp.Diagnostic{{Range: at(0, 0)}}})
	if got := e.Buf.Diags(); len(got) != 1 || got[0] != ds[0] {
		t.Errorf("diagnostics for an unknown version replaced the old ones: %+v", got)
	}
}

func TestDiagnosticsInUTF16(t *testing.T) {
	e := newGoEditor(t, "é😀x\n")
	_, diags := fakeLSP(t, e)
	e.SyncLSP()
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path, Version: -1, Diags: []lsp.Diagnostic{
		{Range: at(0, 3)}, {Range: at(5, 0)}}}) // é is one unit, 😀 two; line 5 is past the end
	ds := e.Buf.Diags()
	if len(ds) != 2 || diagText(e, ds[0]) != "x" {
		t.Fatalf("diagnostics %+v, want the first on x", ds)
	}
	if start, _ := ds[1].Range(); start != e.Buf.Text.Len() {
		t.Errorf("a position past the end starts at %d, want %d", start, e.Buf.Text.Len())
	}
}

func TestDiagAtCursorAndJumps(t *testing.T) {
	e := newGoEditor(t, "a b\nc\nd e\n")
	_, diags := fakeLSP(t, e)
	e.SyncLSP()
	(*diags)(lsp.Diagnostics{Path: e.Buf.Path, Version: 0, UTF8: true, Diags: []lsp.Diagnostic{
		{Range: at(2, 2), Severity: lsp.Warning, Message: "warn e\nmore"},
		{Range: at(0, 2), Severity: lsp.Hint, Message: "hint b"},
		{Range: at(2, 0), Severity: lsp.Error, Message: "error d"}}})
	if d := e.DiagAtCursor(); d == nil || d.Message != "hint b" {
		t.Errorf("on line 0: %+v, want the hint on its line", d)
	}
	keys(e, "] d")
	if line, col := e.Cursor(); line != 2 || col != 0 {
		t.Errorf("]d went to %d:%d, want 2:0", line, col)
	}
	keys(e, "l l") // onto e
	if d := e.DiagAtCursor(); d == nil || d.Summary() != "warn e" {
		t.Errorf("on e: %+v, want the warning under the cursor", d)
	}
	keys(e, "] d") // wraps
	if line, _ := e.Cursor(); line != 0 {
		t.Errorf("]d from the last line went to line %d, want 0", line)
	}
	keys(e, "[ d")
	if line, _ := e.Cursor(); line != 2 {
		t.Errorf("[d from the first line went to line %d, want 2", line)
	}
	keys(e, "C-o")
	if line, _ := e.Cursor(); line != 0 {
		t.Errorf("C-o after [d went to line %d, want 0", line)
	}
}
