package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/text"
)

// fakeServer is the server end of a client's pipes.
type fakeServer struct {
	t   *testing.T
	r   *bufio.Reader
	w   io.Writer
	got chan incoming
}

func newFake(t *testing.T, diags func(Diagnostics)) (*Client, *fakeServer) {
	t.Helper()
	cr, sw := io.Pipe() // server to client
	sr, cw := io.Pipe() // client to server
	t.Cleanup(func() { sw.Close(); cw.Close() })
	s := &fakeServer{t: t, r: bufio.NewReader(sr), w: sw, got: make(chan incoming, 100)}
	go func() {
		for {
			data, err := read(s.r)
			if err != nil {
				close(s.got)
				return
			}
			var m incoming
			json.Unmarshal(data, &m)
			s.got <- m
		}
	}()
	return newClient(cr, cw, "/proj", diags), s
}

func (s *fakeServer) next() incoming {
	s.t.Helper()
	select {
	case m := <-s.got:
		return m
	case <-time.After(2 * time.Second):
		s.t.Fatal("timed out waiting for a message from the client")
		return incoming{}
	}
}

func (s *fakeServer) expect(method string) incoming {
	s.t.Helper()
	m := s.next()
	if m.Method != method {
		s.t.Fatalf("got %q, want %q", m.Method, method)
	}
	return m
}

func (s *fakeServer) send(msg any) {
	s.t.Helper()
	if err := write(s.w, msg); err != nil {
		s.t.Fatal(err)
	}
}

func (s *fakeServer) initialize(encoding string) {
	s.t.Helper()
	m := s.expect("initialize")
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID,
		"result": map[string]any{"capabilities": map[string]any{"positionEncoding": encoding}}})
	s.expect("initialized")
}

func changedText(t *testing.T, m incoming) string {
	t.Helper()
	var p struct {
		ContentChanges []struct{ Text string } `json:"contentChanges"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil || len(p.ContentChanges) != 1 {
		t.Fatalf("bad didChange params %s", m.Params)
	}
	return p.ContentChanges[0].Text
}

func TestHandshakeComesFirst(t *testing.T) {
	c, s := newFake(t, nil)
	c.Open("/proj/a.go", "go", 1, text.New([]byte("package a\n"), text.Origin{}))
	s.initialize("utf-8")
	m := s.expect("textDocument/didOpen")
	if !strings.Contains(string(m.Params), `"uri":"file:///proj/a.go"`) || !strings.Contains(string(m.Params), `package a\n`) {
		t.Errorf("didOpen params = %s", m.Params)
	}
	if !c.UTF8() {
		t.Error("UTF8() = false after the server chose utf-8")
	}
}

func TestDefaultEncodingIsUTF16(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("")
	c.Close("/proj/a.go")
	s.expect("textDocument/didClose")
	if c.UTF8() {
		t.Error("UTF8() = true, but the server didn't pick an encoding")
	}
}

func TestQueuedChangesCoalesce(t *testing.T) {
	c, s := newFake(t, nil)
	t0 := text.New([]byte("a"), text.Origin{})
	c.Open("/proj/a.go", "go", 1, t0)
	t1 := t0.Insert(1, []byte("b"), text.Origin{})
	c.Change("/proj/a.go", 2, t1)
	c.Change("/proj/a.go", 3, t1.Insert(2, []byte("c"), text.Origin{}))
	s.initialize("utf-8")
	s.expect("textDocument/didOpen")
	if got := changedText(t, s.expect("textDocument/didChange")); got != "abc" {
		t.Errorf("change text = %q, want the latest, abc", got)
	}
	c.Close("/proj/a.go")
	s.expect("textDocument/didClose") // not a second didChange
}

func TestDiagnosticsAndServerRequests(t *testing.T) {
	got := make(chan Diagnostics, 1)
	_, s := newFake(t, func(d Diagnostics) { got <- d })
	s.initialize("utf-8")
	s.send(map[string]any{"jsonrpc": "2.0", "id": "cfg", "method": "workspace/configuration",
		"params": map[string]any{"items": []any{map[string]any{}, map[string]any{}}}})
	m := s.next()
	if string(m.ID) != `"cfg"` || string(m.Result) != "[null,null]" {
		t.Errorf("configuration answer: id %s result %s", m.ID, m.Result)
	}
	s.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{
		"uri": "file:///proj/a%20b.go", "version": 4, "diagnostics": []any{map[string]any{
			"range":    map[string]any{"start": map[string]any{"line": 2, "character": 1}, "end": map[string]any{"line": 2, "character": 5}},
			"severity": Error, "message": "undefined: x"}}}})
	select {
	case d := <-got:
		if d.Path != "/proj/a b.go" || d.Version != 4 || len(d.Diags) != 1 ||
			d.Diags[0].Message != "undefined: x" || d.Diags[0].Range.Start != (Position{2, 1}) {
			t.Errorf("diagnostics = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no diagnostics")
	}
}

func TestShutdown(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("utf-8")
	done := make(chan struct{})
	go func() { c.Shutdown(2 * time.Second); close(done) }()
	m := s.expect("shutdown")
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
	s.expect("exit")
	<-done
	if c.Err() == nil {
		t.Error("Err() = nil after Shutdown")
	}
}

func TestURIRoundTrip(t *testing.T) {
	for _, p := range []string{"/a/b.go", "/with space/ü.go", "/x#y?.go"} {
		if got := Path(URI(p)); got != p {
			t.Errorf("Path(URI(%q)) = %q (URI %s)", p, got, URI(p))
		}
	}
}

// TestGopls runs the real server, when it is installed.
func TestGopls(t *testing.T) {
	if testing.Short() {
		t.Skip("starts gopls")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package a\n\nfunc f() int { return x }\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a\n\ngo 1.21\n"), 0o600)
	os.WriteFile(path, []byte(src), 0o600)
	got := make(chan Diagnostics, 10)
	srv, _ := For(path)
	c, err := Start(srv, dir, func(d Diagnostics) { got <- d })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(5 * time.Second)
	c.Open(path, srv.Lang, 1, text.New([]byte(src), text.Origin{}))
	deadline := time.After(30 * time.Second)
	for {
		select {
		case d := <-got:
			if d.Path == path && len(d.Diags) > 0 {
				if !strings.Contains(d.Diags[0].Message, "x") || d.Diags[0].Range.Start.Line != 2 {
					t.Errorf("diagnostic = %+v", d.Diags[0])
				}
				return
			}
		case <-deadline:
			t.Fatalf("no diagnostics from gopls (err %v)", c.Err())
		}
	}
}
