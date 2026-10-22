// Package tools defines the MCP tools agents use to work with hiss. Each tool is
// defined once: the editor executes it on its event loop, and `hiss mcp`
// registers it with an MCP server that forwards calls to the editor.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
)

// Instructions are sent to MCP clients when they connect.
const Instructions = `hiss is the user's text editor. Buffers open in hiss may contain unsaved
changes, so read them with read_buffer rather than from disk. Use get_selection
to see what the user is pointing at.`

// Forward sends a tool call to the editor and returns its JSON result.
type Forward func(ctx context.Context, agent, tool string, args any) (json.RawMessage, error)

type tool struct {
	name     string
	register func(s *mcp.Server, fwd Forward)
	exec     func(ed *core.Editor, agent string, args json.RawMessage) (any, error)
}

// texter lets a result choose the text the model reads instead of raw JSON.
type texter interface{ Text() string }

func define[In, Out any](t *mcp.Tool, run func(ed *core.Editor, agent string, in In) (Out, error)) tool {
	return tool{
		name: t.Name,
		register: func(s *mcp.Server, fwd Forward) {
			mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
				var out Out
				raw, err := fwd(ctx, agentName(req), t.Name, in)
				if err != nil {
					return nil, out, err
				}
				if err := json.Unmarshal(raw, &out); err != nil {
					return nil, out, err
				}
				if tx, ok := any(out).(texter); ok {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: tx.Text()}}}, out, nil
				}
				return nil, out, nil
			})
		},
		exec: func(ed *core.Editor, agent string, args json.RawMessage) (any, error) {
			var in In
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return nil, fmt.Errorf("bad arguments: %w", err)
				}
			}
			return run(ed, agent, in)
		},
	}
}

func agentName(req *mcp.CallToolRequest) string {
	if req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
			return p.ClientInfo.Name
		}
	}
	return "agent"
}

var all = []tool{
	define(&mcp.Tool{
		Name:        "list_buffers",
		Description: "List files open in the user's editor, with their versions and whether they have unsaved changes.",
	}, listBuffers),
	define(&mcp.Tool{
		Name: "read_buffer",
		Description: "Read a file open in the editor, including unsaved changes. Lines are numbered " +
			"like cat -n; the number and tab are not part of the text. Returns the buffer version.",
	}, readBuffer),
	define(&mcp.Tool{
		Name:        "get_selection",
		Description: "Get the user's cursor and selected text.",
	}, getSelection),
}

// Register adds every tool to s, forwarding calls with fwd.
func Register(s *mcp.Server, fwd Forward) {
	for _, t := range all {
		t.register(s, fwd)
	}
}

// Exec runs a forwarded call. It must run on the editor's goroutine.
func Exec(ed *core.Editor, req rpc.Request) rpc.Response {
	for _, t := range all {
		if t.name != req.Tool {
			continue
		}
		out, err := t.exec(ed, req.Agent, req.Args)
		if err != nil {
			return rpc.Response{Error: err.Error()}
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return rpc.Response{Error: err.Error()}
		}
		return rpc.Response{Result: raw}
	}
	return rpc.Response{Error: "unknown tool " + req.Tool}
}

type BufferInfo struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
	Lines   int    `json:"lines"`
	Dirty   bool   `json:"dirty" jsonschema:"has unsaved changes"`
	Active  bool   `json:"active" jsonschema:"is the buffer the user is looking at"`
}

type ListBuffersOut struct {
	Root    string       `json:"root"`
	Buffers []BufferInfo `json:"buffers"`
}

func listBuffers(ed *core.Editor, _ string, _ struct{}) (ListBuffersOut, error) {
	b := ed.Buf
	return ListBuffersOut{Root: ed.Root, Buffers: []BufferInfo{{
		Path: b.Path, Version: b.Version, Lines: b.Lines(), Dirty: b.Dirty(), Active: true,
	}}}, nil
}

type ReadBufferIn struct {
	Path   string `json:"path" jsonschema:"absolute path, or relative to the project root"`
	Offset int    `json:"offset,omitempty" jsonschema:"1-based line to start at"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of lines (default 2000)"`
}

type ReadBufferOut struct {
	Path       string `json:"path"`
	Version    int    `json:"version"`
	TotalLines int    `json:"total_lines"`
	FirstLine  int    `json:"first_line"`
	Content    string `json:"content"`
}

func (r ReadBufferOut) Text() string {
	return fmt.Sprintf("%s (version %d, %d lines)\n%s", r.Path, r.Version, r.TotalLines, r.Content)
}

func readBuffer(ed *core.Editor, _ string, in ReadBufferIn) (ReadBufferOut, error) {
	b, err := findBuffer(ed, in.Path)
	if err != nil {
		return ReadBufferOut{}, err
	}
	t := b.Text
	first := max(in.Offset, 1) - 1
	limit := in.Limit
	if limit <= 0 {
		limit = 2000
	}
	total := b.Lines()
	var sb strings.Builder
	for line := first; line < min(first+limit, total); line++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", line+1, t.Slice(t.LineStart(line), b.LineEnd(line)))
	}
	return ReadBufferOut{Path: b.Path, Version: b.Version, TotalLines: total, FirstLine: first + 1, Content: sb.String()}, nil
}

type Position struct {
	Line   int `json:"line" jsonschema:"1-based"`
	Column int `json:"column" jsonschema:"1-based, in characters"`
}

type SelectionOut struct {
	Path   string   `json:"path"`
	Mode   string   `json:"mode"`
	Cursor Position `json:"cursor"`
	Start  Position `json:"start"`
	End    Position `json:"end" jsonschema:"inclusive"`
	Text   string   `json:"text" jsonschema:"selected text, truncated to 10000 bytes"`
}

func getSelection(ed *core.Editor, _ string, _ struct{}) (SelectionOut, error) {
	from, to := ed.SelRange()
	pos := func(off int) Position {
		line, col := ed.PositionOf(off)
		return Position{line + 1, col + 1}
	}
	last := from
	if to > from {
		last = to - 1
	}
	return SelectionOut{
		Path: ed.Buf.Path, Mode: ed.Mode.String(),
		Cursor: pos(ed.Sel.Head), Start: pos(from), End: pos(last),
		Text: string(ed.Buf.Text.Slice(from, min(to, from+10000))),
	}, nil
}

func findBuffer(ed *core.Editor, path string) (*core.Buffer, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(ed.Root, path)
	}
	if filepath.Clean(path) == ed.Buf.Path {
		return ed.Buf, nil
	}
	return nil, fmt.Errorf("%s is not open in hiss; read it from disk instead", path)
}
