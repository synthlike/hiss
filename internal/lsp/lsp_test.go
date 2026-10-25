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
		if d.Path != "/proj/a b.go" || d.Version != 4 || !d.UTF8 || len(d.Diags) != 1 ||
			d.Diags[0].Message != "undefined: x" || d.Diags[0].Range.Start != (Position{2, 1}) {
			t.Errorf("diagnostics = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no diagnostics")
	}
	s.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
		"params": map[string]any{"uri": "file:///proj/a.go", "diagnostics": []any{}}})
	select {
	case d := <-got:
		if d.Version != -1 {
			t.Errorf("Version = %d without one from the server, want -1", d.Version)
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
	s.w.(io.Closer).Close() // the server exits
	<-done
	if c.Err() == nil {
		t.Error("Err() = nil after Shutdown")
	}
}

func TestShutdownGivesUp(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("utf-8")
	start := time.Now()
	c.Shutdown(100 * time.Millisecond) // the server never answers
	if d := time.Since(start); d > time.Second {
		t.Errorf("Shutdown took %v with a 100ms timeout", d)
	}
}

func TestLocations(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("utf-8")
	type result struct {
		locs []Location
		err  error
	}
	got := make(chan result, 1)
	go func() {
		locs, err := c.Definition("/proj/a.go", Position{1, 2}, 2*time.Second)
		got <- result{locs, err}
	}()
	m := s.expect("textDocument/definition")
	if !strings.Contains(string(m.Params), `"position":{"line":1,"character":2}`) {
		t.Errorf("definition params = %s", m.Params)
	}
	rng := map[string]any{"start": map[string]any{"line": 4, "character": 5}, "end": map[string]any{"line": 4, "character": 6}}
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": []any{
		map[string]any{"targetUri": "file:///proj/b.go", "targetRange": rng, "targetSelectionRange": rng},
		map[string]any{"uri": "https://example.com/x", "range": rng}}})
	r := <-got
	if r.err != nil || len(r.locs) != 1 || r.locs[0].Path != "/proj/b.go" || r.locs[0].Range.Start != (Position{4, 5}) {
		t.Errorf("Definition = %+v, %v", r.locs, r.err)
	}

	go func() {
		locs, err := c.References("/proj/a.go", Position{0, 0}, 2*time.Second)
		got <- result{locs, err}
	}()
	m = s.expect("textDocument/references")
	if !strings.Contains(string(m.Params), `"includeDeclaration":true`) {
		t.Errorf("references params = %s", m.Params)
	}
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": 0, "message": "no identifier"}})
	if r := <-got; r.err == nil || r.err.Error() != "no identifier" {
		t.Errorf("References error = %v, want the server's", r.err)
	}
}

func TestHighlights(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("utf-8")
	type result struct {
		hs  []Range
		err error
	}
	got := make(chan result, 1)
	go func() {
		hs, err := c.Highlights("/proj/a.go", Position{1, 2}, 2*time.Second)
		got <- result{hs, err}
	}()
	m := s.expect("textDocument/documentHighlight")
	rng := map[string]any{"start": map[string]any{"line": 4, "character": 5}, "end": map[string]any{"line": 4, "character": 6}}
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": []any{map[string]any{"range": rng, "kind": 2}}})
	if r := <-got; r.err != nil || len(r.hs) != 1 || r.hs[0].Start != (Position{4, 5}) {
		t.Errorf("Highlights = %+v, %v", r.hs, r.err)
	}
	go func() {
		hs, err := c.Highlights("/proj/a.go", Position{}, 2*time.Second)
		got <- result{hs, err}
	}()
	m = s.expect("textDocument/documentHighlight")
	s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
	if r := <-got; r.err != nil || len(r.hs) != 0 {
		t.Errorf("Highlights for null = %+v, %v", r.hs, r.err)
	}
}

func TestParseSymbols(t *testing.T) {
	sel := `"selectionRange":{"start":{"line":6,"character":5},"end":{"line":6,"character":6}}`
	tree := `[{"name":"T","kind":23,` + sel + `,"children":[{"name":"A","kind":8,` + sel + `}]},{"name":"f","kind":12,` + sel + `}]`
	syms, err := parseSymbols(json.RawMessage(tree))
	if err != nil || len(syms) != 3 || syms[0].Name != "T" || syms[1].Name != "T.A" || syms[1].Kind != 8 ||
		syms[2].Name != "f" || syms[0].Range.Start != (Position{6, 5}) {
		t.Errorf("DocumentSymbols = %+v, %v", syms, err)
	}
	loc := `"location":{"uri":"file:///a.go","range":{"start":{"line":3,"character":1},"end":{"line":3,"character":2}}}`
	flat := `[{"name":"A","kind":8,"containerName":"T",` + loc + `},{"name":"f","kind":12,` + loc + `}]`
	syms, err = parseSymbols(json.RawMessage(flat))
	if err != nil || len(syms) != 2 || syms[0].Name != "T.A" || syms[1].Name != "f" || syms[1].Range.Start != (Position{3, 1}) {
		t.Errorf("SymbolInformation = %+v, %v", syms, err)
	}
	if syms, err := parseSymbols(json.RawMessage("null")); err != nil || len(syms) != 0 {
		t.Errorf("null = %+v, %v", syms, err)
	}
	if KindName(12) != "function" || KindName(26) != "type parameter" || KindName(99) != "" {
		t.Error("KindName is off")
	}
}

func TestLocationsTimeOut(t *testing.T) {
	c, s := newFake(t, nil)
	s.initialize("utf-8")
	if _, err := c.Definition("/proj/a.go", Position{}, 50*time.Millisecond); err == nil {
		t.Error("Definition without an answer returned no error")
	}
}

func TestParseLocations(t *testing.T) {
	for _, res := range []string{"", "null", " []"} {
		if locs, err := parseLocations(json.RawMessage(res)); err != nil || len(locs) != 0 {
			t.Errorf("parseLocations(%q) = %v, %v", res, locs, err)
		}
	}
	locs, err := parseLocations(json.RawMessage(`{"uri":"file:///a.go","range":{"start":{"line":3,"character":0},"end":{"line":3,"character":1}}}`))
	if err != nil || len(locs) != 1 || locs[0].Path != "/a.go" || locs[0].Range.Start.Line != 3 {
		t.Errorf("single Location = %+v, %v", locs, err)
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

// TestGoplsDefinition asks the real server for a definition, when it is installed.
func TestGoplsDefinition(t *testing.T) {
	if testing.Short() {
		t.Skip("starts gopls")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := "package a\n\nfunc f() int { return g() }\n\nfunc g() int { return 1 }\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a\n\ngo 1.21\n"), 0o600)
	os.WriteFile(path, []byte(src), 0o600)
	srv, _ := For(path)
	c, err := Start(srv, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(5 * time.Second)
	c.Open(path, srv.Lang, 1, text.New([]byte(src), text.Origin{}))
	locs, err := c.Definition(path, Position{2, 22}, 30*time.Second) // the g in "return g()"
	if err != nil || len(locs) != 1 || locs[0].Path != path || locs[0].Range.Start != (Position{4, 5}) {
		t.Errorf("Definition = %+v, %v", locs, err)
	}
	locs, err = c.References(path, Position{4, 5}, 30*time.Second)
	if err != nil || len(locs) != 2 {
		t.Errorf("References = %+v, %v", locs, err)
	}
	hs, err := c.Highlights(path, Position{2, 0}, 30*time.Second) // func: itself and its return
	if err != nil || len(hs) != 2 {
		t.Errorf("Highlights = %+v, %v", hs, err)
	}
	syms, err := c.Symbols(path, 30*time.Second)
	if err != nil || len(syms) != 2 || syms[0].Name != "f" || KindName(syms[1].Kind) != "function" {
		t.Errorf("Symbols = %+v, %v", syms, err)
	}
}
