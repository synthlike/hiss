package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
)

// setup starts an editor loop behind a real socket and an MCP client talking to
// a bridge server, mirroring `hiss` plus `hiss mcp`.
func setup(t *testing.T, content string) (*mcp.ClientSession, chan<- func(*core.Editor)) {
	t.Helper()
	root := t.TempDir()
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
	t.Cleanup(cancel)
	go func() { // the editor's event loop
		for {
			select {
			case f := <-reqs:
				f(ed)
			case <-ctx.Done():
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
	if !res.IsError || !strings.Contains(text(res), "not open in hiss") {
		t.Errorf("IsError=%v text=%q", res.IsError, text(res))
	}
}
