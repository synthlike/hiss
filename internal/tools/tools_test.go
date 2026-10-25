package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
)

// setup starts an editor loop behind a real socket and an MCP client talking to
// a bridge server, mirroring `hiss` plus `hiss mcp`.
func setup(t *testing.T, content string) (*mcp.ClientSession, chan<- func(*core.Editor)) {
	t.Helper()
	return setupWith(t, content, false)
}

// setupWith is setup, optionally running gopls on the project as hiss does:
// main.go is in a module, and the loop syncs buffers after every request.
func setupWith(t *testing.T, content string, withLSP bool) (*mcp.ClientSession, chan<- func(*core.Editor)) {
	t.Helper()
	root := t.TempDir()
	if withLSP {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.21\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	buf, err := core.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(buf)
	ed.Root = root

	reqs := make(chan func(*core.Editor))
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	t.Cleanup(func() { cancel(); <-stopped })
	if withLSP {
		ed.Post = func(f func(*core.Editor)) {
			select {
			case reqs <- f:
			case <-ctx.Done():
			}
		}
	}
	go func() { // the editor's event loop
		defer close(stopped)
		for {
			select {
			case f := <-reqs:
				f(ed)
				ed.SyncLSP()
			case <-ctx.Done():
				ed.StopLSP()
				return
			}
		}
	}()

	dir, err := os.MkdirTemp("/tmp", "hiss-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := rpc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go rpc.Serve(ln, func(req rpc.Request) rpc.Response {
		reply := make(chan rpc.Response, 1)
		reqs <- func(ed *core.Editor) { reply <- Exec(ed, req) }
		return <-reply
	})

	server := mcp.NewServer(&mcp.Implementation{Name: "hiss", Version: "test"}, nil)
	Register(server, func(ctx context.Context, agent, tool string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return rpc.Call(ctx, sock, rpc.Request{Agent: agent, Tool: tool, Args: raw})
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, reqs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestReadBufferSeesUnsavedEdits(t *testing.T) {
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")
	done := make(chan bool)
	reqs <- func(ed *core.Editor) {
		for _, k := range []string{"i", "/", "/", "space", "h", "i", "enter", "esc"} {
			ed.HandleKey(k)
		}
		done <- true
	}
	<-done

	res := call(t, cs, "read_buffer", map[string]any{"path": "main.go"})
	want := "     1\t// hi\n     2\tpackage main\n     3\t\n     4\tfunc main() {}\n"
	if got := text(res); !strings.HasSuffix(got, want) || !strings.Contains(got, "version 6, 4 lines") {
		t.Errorf("read_buffer text:\n%s\nwant suffix:\n%s", got, want)
	}

	res = call(t, cs, "read_buffer", map[string]any{"path": "main.go", "offset": 4, "limit": 1})
	if got := text(res); !strings.HasSuffix(got, "     4\tfunc main() {}\n") || strings.Contains(got, "     3\t") {
		t.Errorf("offset/limit:\n%s", got)
	}
}

func TestListBuffersAndSelection(t *testing.T) {
	cs, reqs := setup(t, "alpha beta\n")
	reqs <- func(ed *core.Editor) { ed.HandleKey("w"); ed.HandleKey("w") }

	var lb ListBuffersOut
	res := call(t, cs, "list_buffers", map[string]any{})
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &lb); err != nil || len(lb.Buffers) != 1 ||
		!strings.HasSuffix(lb.Buffers[0].Path, "main.go") || lb.Buffers[0].Lines != 1 {
		t.Fatalf("list_buffers = %s (%v)", raw, err)
	}

	var sel SelectionOut
	res = call(t, cs, "get_selection", map[string]any{})
	raw, _ = json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &sel); err != nil {
		t.Fatal(err)
	}
	if sel.Text != "beta" || sel.Start != (Position{1, 7}) || sel.End != (Position{1, 10}) {
		t.Errorf("selection = %+v", sel)
	}
}

func TestErrorsReachTheAgent(t *testing.T) {
	cs, _ := setup(t, "x\n")
	res := call(t, cs, "read_buffer", map[string]any{"path": "other.go"})
	if !res.IsError || !strings.Contains(text(res), "does not exist") {
		t.Errorf("IsError=%v text=%q", res.IsError, text(res))
	}
}

// onEditor runs f on the editor loop and waits for it.
func onEditor(reqs chan<- func(*core.Editor), f func(*core.Editor)) {
	done := make(chan bool)
	reqs <- func(ed *core.Editor) {
		defer func() { done <- true }() // also when f calls t.Fatal, which exits the goroutine
		f(ed)
	}
	<-done
}

func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return out
}

func TestProposeReviewAwait(t *testing.T) {
	pollInterval = 10 * time.Millisecond
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")

	res := call(t, cs, "read_buffer", map[string]any{"path": "main.go"})
	version := structured[ReadBufferOut](t, res).Version
	res = call(t, cs, "propose_edit", map[string]any{
		"path": "main.go", "old_string": "func main() {}", "new_string": "func main() { run() }",
		"base_version": version, "rationale": "call run",
	})
	if res.IsError {
		t.Fatalf("propose_edit: %s", text(res))
	}
	id := structured[ProposeOut](t, res).IDs[0]

	res = call(t, cs, "get_proposals", map[string]any{})
	if got := text(res); !strings.Contains(got, "pending") {
		t.Fatalf("get_proposals before review: %s", got)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		onEditor(reqs, func(ed *core.Editor) {
			for _, k := range []string{"]", "h", "+"} {
				ed.HandleKey(k)
			}
		})
	}()
	res = call(t, cs, "await_review", map[string]any{"ids": []int{id}, "timeout_seconds": 5})
	if got := text(res); !strings.Contains(got, "accepted") {
		t.Fatalf("await_review: %s", got)
	}
	disk, _ := os.ReadFile(structured[ProposalsOut](t, res).Proposals[0].Path)
	if !strings.Contains(string(disk), "func main() { run() }") {
		t.Errorf("accepted edit not saved: %q", disk)
	}
}

func TestAwaitReviewTimesOut(t *testing.T) {
	pollInterval = 10 * time.Millisecond
	cs, _ := setup(t, "x\n")
	res := call(t, cs, "write_file", map[string]any{"path": "new.txt", "content": "hello\n"})
	id := structured[ProposeOut](t, res).IDs[0]
	start := time.Now()
	res = call(t, cs, "await_review", map[string]any{"ids": []int{id}, "timeout_seconds": 1})
	out := structured[ProposalsOut](t, res)
	if !out.TimedOut || out.Proposals[0].Status != "pending" || time.Since(start) < time.Second {
		t.Errorf("out = %+v after %v", out, time.Since(start))
	}
}

func TestCommentEndsAwaitAndRevisionSupersedes(t *testing.T) {
	pollInterval = 10 * time.Millisecond
	cs, reqs := setup(t, "a := 1\n")
	res := call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "1", "new_string": "2"})
	id := structured[ProposeOut](t, res).IDs[0]
	go func() {
		time.Sleep(50 * time.Millisecond)
		onEditor(reqs, func(ed *core.Editor) {
			ed.HandleKey("]")
			ed.HandleKey("h")
			ed.HandleKey(":")
			for _, k := range "comment make it 3" {
				if k == ' ' {
					ed.HandleKey("space")
				} else {
					ed.HandleKey(string(k))
				}
			}
			ed.HandleKey("enter")
		})
	}()
	res = call(t, cs, "await_review", map[string]any{"ids": []int{id}, "timeout_seconds": 5})
	out := structured[ProposalsOut](t, res)
	if len(out.Comments) != 1 || out.Comments[0].Text != "make it 3" || out.TimedOut {
		t.Fatalf("await_review = %+v", out)
	}

	if res = call(t, cs, "reply", map[string]any{"id": id, "text": "sure"}); res.IsError {
		t.Fatalf("reply: %s", text(res))
	}
	res = call(t, cs, "propose_edit", map[string]any{
		"path": "main.go", "old_string": "a := 1", "new_string": "a := 3", "supersedes": id,
	})
	if res.IsError {
		t.Fatalf("revision: %s", text(res))
	}
	rev := structured[ProposeOut](t, res).IDs[0]
	res = call(t, cs, "get_proposals", map[string]any{"ids": []int{id, rev}})
	if got := text(res); !strings.Contains(got, "superseded (revised as #") || !strings.Contains(got, "pending") {
		t.Errorf("after revision: %s", got)
	}
	onEditor(reqs, func(ed *core.Editor) {
		if p := ed.Proposal(rev); len(p.Thread) != 2 {
			t.Errorf("revision thread = %+v", p.Thread)
		}
	})
}

func TestStaleBaseVersionReachesAgent(t *testing.T) {
	cs, reqs := setup(t, "a := 1\n")
	onEditor(reqs, func(ed *core.Editor) {
		for _, k := range []string{"l", "l", "i", "x", "esc"} { // edit inside "a := 1"
			ed.HandleKey(k)
		}
	})
	res := call(t, cs, "propose_edit", map[string]any{
		"path": "main.go", "old_string": "a x:= 1", "new_string": "a := 2", "base_version": 0,
	})
	if !res.IsError || !strings.Contains(text(res), "changed this text") {
		t.Errorf("IsError=%v %s", res.IsError, text(res))
	}
}

func TestGetDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("starts gopls")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	cs, _ := setupWith(t, "package main\n\nfunc main() {\n\tprintln(1)\n}\n", true)
	res := call(t, cs, "get_diagnostics", map[string]any{"path": "main.go"})
	if out := structured[DiagnosticsOut](t, res); !out.UpToDate || len(out.Diagnostics) != 0 {
		t.Fatalf("before proposing: %s", text(res))
	}
	call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "println(1)", "new_string": "println(1)\n\tundefinedThing()"})
	res = call(t, cs, "get_diagnostics", map[string]any{"path": "main.go"})
	out := structured[DiagnosticsOut](t, res)
	if !out.UpToDate || len(out.Diagnostics) != 1 {
		t.Fatalf("after proposing: %s", text(res))
	}
	d := out.Diagnostics[0]
	if d.Proposal != 1 || d.Line != 2 || d.Column != 2 || d.Severity != "error" || !strings.Contains(d.Message, "undefinedThing") {
		t.Errorf("diagnostic = %+v, want an error in proposal #1 at 2:2", d)
	}
	if !strings.Contains(text(res), "proposal #1, 2:2 error:") {
		t.Errorf("text = %q", text(res))
	}
}

func TestGetDiagnosticsWithoutServer(t *testing.T) {
	cs, _ := setup(t, "")
	res := call(t, cs, "get_diagnostics", map[string]any{"path": "notes.txt"})
	if !res.IsError || !strings.Contains(text(res), "no language server for .txt") {
		t.Errorf("got %s", text(res))
	}
}
