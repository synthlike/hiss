// Package lsp is a small Language Server Protocol client. It starts a server,
// keeps open documents in sync by sending their full text, reports the
// diagnostics the server publishes, and asks for definitions and references.
// Nothing else: no completion, no rename.
package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/synthlike/hiss/internal/text"
)

// Server is how to start a language server for one language.
type Server struct {
	Lang string   // LSP language ID, such as "go"
	Cmd  []string // command and arguments; it speaks LSP on stdio
}

var servers = map[string]Server{
	".go": {Lang: "go", Cmd: []string{"gopls"}},
}

// For returns the server for a file, by its extension.
func For(path string) (Server, bool) {
	s, ok := servers[filepath.Ext(path)]
	return s, ok
}

// Severity values; 0 means the server didn't say.
const (
	Error = iota + 1
	Warning
	Information
	Hint
)

// Position is a zero-based line and a character offset in it, counted in the
// client's negotiated encoding (see Client.UTF8).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Location is a range in a file.
type Location struct {
	Path  string
	Range Range
}

// Diagnostics replaces all earlier diagnostics for a file.
type Diagnostics struct {
	Path    string
	Version int  // document version they were computed for; -1 if the server didn't say
	UTF8    bool // positions count bytes, not UTF-16 code units
	Diags   []Diagnostic
}

// Client talks to one server. Its methods never block on the server: messages
// are queued and written by a background goroutine, which waits for the
// initialize handshake first.
type Client struct {
	w      io.Writer
	diags  func(Diagnostics) // called on the reader goroutine
	root   string
	wake   chan struct{}
	dead   chan struct{} // closed when the connection fails or ends
	once   sync.Once
	exited chan struct{} // closed when the process exits; nil without one
	kill   func()

	mu    sync.Mutex
	queue []outgoing
	calls map[int]chan reply
	next  int
	utf8  bool
	err   error
}

// reply is the answer to a request.
type reply struct {
	result json.RawMessage
	err    error
}

// outgoing is a queued message. build runs on the writer goroutine, so a
// change can carry a text.Text snapshot and be serialized off the loop.
type outgoing struct {
	change string // the document's URI, for a didChange
	build  func() any
}

// Start runs srv in root and sends it the initialize handshake. diags is
// called on a background goroutine whenever the server publishes diagnostics.
func Start(srv Server, root string, diags func(Diagnostics)) (*Client, error) {
	cmd := exec.Command(srv.Cmd[0], srv.Cmd[1:]...)
	cmd.Dir = root // stderr stays nil: servers log there, and the terminal belongs to the UI
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := newClient(out, in, root, diags)
	c.exited = make(chan struct{})
	c.kill = func() { cmd.Process.Kill() }
	go func() {
		err := cmd.Wait()
		c.fail(fmt.Errorf("%s exited: %v", srv.Cmd[0], err))
		close(c.exited)
	}()
	return c, nil
}

func newClient(r io.Reader, w io.Writer, root string, diags func(Diagnostics)) *Client {
	c := &Client{w: w, diags: diags, root: root, wake: make(chan struct{}, 1),
		dead: make(chan struct{}), calls: map[int]chan reply{}, kill: func() {}}
	go c.readLoop(r)
	go c.writeLoop()
	return c
}

// UTF8 reports whether positions count bytes rather than UTF-16 code units.
// It is false until the server has answered initialize.
func (c *Client) UTF8() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.utf8
}

// Err returns why the connection ended, or nil while it is up.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Open tells the server about a document and its text at version.
func (c *Client) Open(path, lang string, version int, t text.Text) {
	c.send(outgoing{build: func() any {
		return notification("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": URI(path), "languageId": lang, "version": version, "text": string(t.Bytes())}})
	}})
}

// Change sends a document's whole text at version. A change still queued for
// the same document is replaced, so a slow server only sees the latest text.
func (c *Client) Change(path string, version int, t text.Text) {
	uri := URI(path)
	c.send(outgoing{change: uri, build: func() any {
		return notification("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": string(t.Bytes())}}})
	}})
}

// Close tells the server a document is no longer open.
func (c *Client) Close(path string) {
	c.send(outgoing{build: func() any {
		return notification("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": URI(path)}})
	}})
}

// Definition asks where the symbol at pos in path is defined, waiting up to
// timeout for the answer.
func (c *Client) Definition(path string, pos Position, timeout time.Duration) ([]Location, error) {
	return c.locations("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": URI(path)}, "position": pos}, timeout)
}

// References asks where the symbol at pos in path is used, its declaration
// included, waiting up to timeout for the answer.
func (c *Client) References(path string, pos Position, timeout time.Duration) ([]Location, error) {
	return c.locations("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": URI(path)}, "position": pos,
		"context": map[string]any{"includeDeclaration": true}}, timeout)
}

// Highlights asks which ranges in path relate to the symbol or keyword at
// pos: other uses of an identifier, or a function's results and returns.
// It waits up to timeout for the answer.
func (c *Client) Highlights(path string, pos Position, timeout time.Duration) ([]Range, error) {
	r, err := c.wait("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": URI(path)}, "position": pos}, timeout)
	if err != nil {
		return nil, err
	}
	var hs []struct {
		Range Range `json:"range"`
	}
	if res := bytes.TrimSpace(r); len(res) > 0 && string(res) != "null" {
		if err := json.Unmarshal(res, &hs); err != nil {
			return nil, err
		}
	}
	out := make([]Range, len(hs))
	for i, h := range hs {
		out[i] = h.Range
	}
	return out, nil
}

func (c *Client) locations(method string, params any, timeout time.Duration) ([]Location, error) {
	r, err := c.wait(method, params, timeout)
	if err != nil {
		return nil, err
	}
	return parseLocations(r)
}

// wait sends a request and waits up to timeout for its result.
func (c *Client) wait(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var r reply
	select {
	case r = <-c.call(method, params):
	case <-c.dead:
		return nil, c.Err()
	case <-ctx.Done():
		return nil, errors.New("no answer from the language server")
	}
	return r.result, r.err
}

// parseLocations reads a result that is null, a Location, or a list of
// Locations or LocationLinks. Locations in other kinds of URI are left out.
func parseLocations(res json.RawMessage) ([]Location, error) {
	type loc struct {
		URI                  string `json:"uri"`
		Range                Range  `json:"range"`
		TargetURI            string `json:"targetUri"`
		TargetSelectionRange Range  `json:"targetSelectionRange"`
	}
	var ls []loc
	switch res = bytes.TrimSpace(res); {
	case len(res) == 0 || string(res) == "null":
		return nil, nil
	case res[0] == '{':
		ls = make([]loc, 1)
		if err := json.Unmarshal(res, &ls[0]); err != nil {
			return nil, err
		}
	default:
		if err := json.Unmarshal(res, &ls); err != nil {
			return nil, err
		}
	}
	var out []Location
	for _, l := range ls {
		if l.TargetURI != "" {
			l.URI, l.Range = l.TargetURI, l.TargetSelectionRange
		}
		if p := Path(l.URI); p != "" {
			out = append(out, Location{p, l.Range})
		}
	}
	return out, nil
}

// Shutdown asks the server to exit, waiting up to timeout before killing it.
func (c *Client) Shutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout) // stays done, unlike a time.After channel
	defer cancel()
	select {
	case <-c.call("shutdown", nil):
		c.send(outgoing{build: func() any { return notification("exit", nil) }})
	case <-c.dead:
	case <-ctx.Done():
	}
	gone := c.exited // the process, or without one the server closing the connection
	if gone == nil {
		gone = c.dead
	}
	select {
	case <-gone:
	case <-ctx.Done():
	}
	c.kill()
	c.fail(errors.New("shut down"))
}

func (c *Client) send(m outgoing) {
	select {
	case <-c.dead: // nobody would write it
		return
	default:
	}
	c.mu.Lock()
	if n := len(c.queue); m.change != "" && n > 0 && c.queue[n-1].change == m.change {
		c.queue[n-1] = m
	} else {
		c.queue = append(c.queue, m)
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// call queues a request and returns a channel that receives its reply.
func (c *Client) call(method string, params any) <-chan reply {
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.calls[id] = ch
	c.mu.Unlock()
	c.send(outgoing{build: func() any { return request(id, method, params) }})
	return ch
}

func (c *Client) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.dead)
	})
}

func (c *Client) writeLoop() {
	if err := c.handshake(); err != nil {
		c.fail(err)
		return
	}
	for {
		select {
		case <-c.wake:
		case <-c.dead:
			return
		}
		c.mu.Lock()
		q := c.queue
		c.queue = nil
		c.mu.Unlock()
		for _, m := range q {
			if err := write(c.w, m.build()); err != nil {
				c.fail(err)
				return
			}
		}
	}
}

// handshake sends initialize directly, ahead of anything queued, and
// initialized once the server has answered.
func (c *Client) handshake() error {
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.calls[id] = ch
	c.mu.Unlock()
	root := URI(c.root)
	err := write(c.w, request(id, "initialize", map[string]any{
		"processId":        os.Getpid(),
		"clientInfo":       map[string]any{"name": "hiss"},
		"rootUri":          root,
		"workspaceFolders": []any{map[string]any{"uri": root, "name": filepath.Base(c.root)}},
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{"utf-8", "utf-16"}},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{},
				"publishDiagnostics": map[string]any{"versionSupport": true},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"documentHighlight":  map[string]any{},
			},
		},
	}))
	if err != nil {
		return err
	}
	var r reply
	select {
	case r = <-ch:
	case <-c.dead:
		return c.Err()
	}
	var init struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	json.Unmarshal(r.result, &init)
	c.mu.Lock()
	c.utf8 = init.Capabilities.PositionEncoding == "utf-8"
	c.mu.Unlock()
	return write(c.w, notification("initialized", map[string]any{}))
}

type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		data, err := read(br)
		if err != nil {
			c.fail(err)
			return
		}
		var m incoming
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && m.ID != nil:
			c.answer(m)
		case m.Method == "textDocument/publishDiagnostics":
			var p struct {
				URI         string       `json:"uri"`
				Version     *int         `json:"version"`
				Diagnostics []Diagnostic `json:"diagnostics"`
			}
			if json.Unmarshal(m.Params, &p) == nil && c.diags != nil {
				d := Diagnostics{Path: Path(p.URI), Version: -1, UTF8: c.UTF8(), Diags: p.Diagnostics}
				if p.Version != nil {
					d.Version = *p.Version
				}
				c.diags(d)
			}
		case m.Method != "": // other notifications, such as log messages
		default:
			id, err := strconv.Atoi(string(m.ID))
			if err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.calls[id]
			delete(c.calls, id)
			c.mu.Unlock()
			if ch != nil {
				r := reply{result: m.Result}
				if m.Error != nil {
					r.err = errors.New(m.Error.Message)
				}
				ch <- r
			}
		}
	}
}

// answer replies to a request from the server. hiss has no settings to give
// and nothing to register, so every answer is empty.
func (c *Client) answer(m incoming) {
	var result any
	if m.Method == "workspace/configuration" {
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		json.Unmarshal(m.Params, &p)
		result = make([]any, len(p.Items)) // one null per item asked for
	}
	c.send(outgoing{build: func() any {
		return map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}
	}})
}

func request(id int, method string, params any) any {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return m
}

func notification(method string, params any) any {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	return m
}

// write sends one message with its Content-Length header.
func write(w io.Writer, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// read returns the body of the next message.
func read(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, "Content-Length") {
			if n, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, fmt.Errorf("bad header %q", line)
			}
		}
	}
	if n < 0 {
		return nil, errors.New("message without Content-Length")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(r, body)
	return body, err
}

// URI returns the file URI for an absolute path.
func URI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// Path returns the path of a file URI, or "" for another kind of URI.
func Path(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}
