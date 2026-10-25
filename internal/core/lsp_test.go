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
	if len(e.Buf.Diags) != 0 {
		t.Errorf("empty diagnostics didn't clear the old ones: %+v", e.Buf.Diags)
	}
}
