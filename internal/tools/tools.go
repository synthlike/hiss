// Package tools defines the MCP tools agents use to work with hiss. Each tool is
// defined once: the editor executes it on its event loop, and `hiss mcp`
// registers it with an MCP server that forwards calls to the editor.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
)

// Instructions are sent to MCP clients when they connect.
const Instructions = `hiss is the user's text editor, and the user reviews every change you make in it.

- Do not change files with Edit, Write or shell commands. Propose changes with
  propose_edit (same rules as Edit: old_string must match exactly once) or, for
  new files and full rewrites, write_file.
- Read files with read_buffer: open buffers may have unsaved changes. Pass the
  version it returns as base_version when proposing.
- Each proposal is pending until the user accepts or rejects it. Call
  await_review before depending on a change (running tests, building on it).
  Accepted changes are saved to disk; for rejected ones, read the reason and
  adjust. A stale proposal means the user edited that text: re-read and retry.
- After proposing code changes, check them with get_diagnostics; fix what they
  break with a revision (supersedes) before the user reviews them.
- If you end your turn with proposals still pending, you are woken when the
  user has reviewed them all; continue the task then.
- The user may comment on a pending proposal. Comments arrive through
  await_review or get_proposals, right after your other tool calls, or wake
  you after your turn ends. Answer them promptly with
  reply; to change the proposal, propose a revision with supersedes set to its id.
- To walk the user through code (a flow, call sites, affected places), use
  show_trail rather than listing file:line in chat. Keep notes to one line and
  don't number them. The user may comment on a step; answer with reply (step).
- Use get_selection to see what the user is pointing at.
- When you need the user to decide or confirm something, ask with the ask
  tool rather than in chat or with AskUserQuestion: it pops up in the editor
  and waits for the answer. Offer short options when the answers are predictable.`

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
		Description: "List files open in the user's editor, with their versions, unsaved changes and pending proposals.",
	}, listBuffers),
	define(&mcp.Tool{
		Name: "read_buffer",
		Description: "Read a file as the editor has it, including unsaved changes (files that aren't open are read " +
			"from disk). Lines are numbered like cat -n; the number and tab are not part of the text. " +
			"Returns the version to pass as base_version.",
	}, readBuffer),
	define(&mcp.Tool{
		Name: "propose_edit",
		Description: "Propose replacing old_string with new_string in a file, for the user to review in the editor. " +
			"Same matching rules as the Edit tool. Returns proposal ids.",
	}, proposeEdit),
	define(&mcp.Tool{
		Name:        "write_file",
		Description: "Propose creating a file or replacing its whole content, for the user to review.",
	}, writeFile),
	define(&mcp.Tool{
		Name:        "get_proposals",
		Description: "Report the status of your proposals without waiting.",
	}, getProposals),
	awaitReview,
	awaitPoll,
	diagnosticsTool,
	replyTool,
	showTrailTool,
	askTool,
	askPoll,
	watchComments,
	hook,
	define(&mcp.Tool{
		Name:        "get_selection",
		Description: "Get the user's cursor and selected text.",
	}, getSelection),
}

// hook carries Claude Code hook events from `hiss hook`; agents can't call it.
var hook = tool{
	name: "hook",
	exec: func(ed *core.Editor, _ string, args json.RawMessage) (any, error) {
		var ev core.HookEvent
		if err := json.Unmarshal(args, &ev); err != nil {
			return nil, err
		}
		ed.Hook(ev)
		var out HookOut
		if ev.Event == "PostToolUse" || ev.Event == "UserPromptSubmit" {
			out.Comments = commentInfos(ed.SessionComments(ev.SessionID))
		}
		return out, nil
	},
}

// HookOut is the editor's answer to a hook event: new comments the hook
// should hand to the agent right away.
type HookOut struct {
	Comments []CommentInfo `json:"comments,omitempty"`
}

// Register adds every tool to s, forwarding calls with fwd.
func Register(s *mcp.Server, fwd Forward) {
	for _, t := range all {
		if t.register != nil {
			t.register(s, fwd)
		}
	}
}

// Exec runs a forwarded call. It must run on the editor's goroutine.
func Exec(ed *core.Editor, req rpc.Request) rpc.Response {
	for _, t := range all {
		if t.name != req.Tool || t.exec == nil {
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
	Pending int    `json:"pending,omitempty" jsonschema:"pending proposals in this buffer"`
}

type ListBuffersOut struct {
	Root    string       `json:"root"`
	Buffers []BufferInfo `json:"buffers"`
}

func listBuffers(ed *core.Editor, _ string, _ struct{}) (ListBuffersOut, error) {
	out := ListBuffersOut{Root: ed.Root, Buffers: []BufferInfo{}}
	for _, b := range ed.Bufs {
		out.Buffers = append(out.Buffers, BufferInfo{
			Path: b.Path, Version: b.Version, Lines: b.Lines(), Dirty: b.Dirty(),
			Active: b == ed.Buf, Pending: b.PendingCount(),
		})
	}
	return out, nil
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
	path := resolve(ed, in.Path)
	b := ed.Find(path)
	if b == nil {
		var err error
		if b, err = core.Open(path); err != nil {
			return ReadBufferOut{}, err
		}
		if b.Text.Len() == 0 {
			if _, err := os.Stat(path); err != nil {
				return ReadBufferOut{}, fmt.Errorf("%s does not exist", path)
			}
		}
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

// resolve makes an agent's path absolute, relative to the project root.
func resolve(ed *core.Editor, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(ed.Root, path)
	}
	return filepath.Clean(path)
}
