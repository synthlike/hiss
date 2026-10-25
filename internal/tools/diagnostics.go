package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/lsp"
)

type GetDiagnosticsIn struct {
	Path string `json:"path" jsonschema:"absolute path, or relative to the project root"`
}

type DiagnosticInfo struct {
	Severity string `json:"severity" jsonschema:"error, warning, info or hint"`
	Message  string `json:"message"`
	Proposal int    `json:"proposal,omitempty" jsonschema:"set when the problem is in this pending proposal's new text; line and column then count in that text"`
	Line     int    `json:"line" jsonschema:"1-based"`
	Column   int    `json:"column" jsonschema:"1-based, in characters"`
}

type DiagnosticsOut struct {
	Path        string           `json:"path"`
	Version     int              `json:"version"`
	UpToDate    bool             `json:"up_to_date" jsonschema:"the language server has checked the text as it is now"`
	Diagnostics []DiagnosticInfo `json:"diagnostics"`
}

func (o DiagnosticsOut) Text() string {
	var sb strings.Builder
	if len(o.Diagnostics) == 0 {
		fmt.Fprintf(&sb, "No problems in %s (version %d) with its pending proposals applied.\n", o.Path, o.Version)
	} else {
		fmt.Fprintf(&sb, "%s (version %d), checked with its pending proposals applied:\n", o.Path, o.Version)
	}
	for _, d := range o.Diagnostics {
		where := fmt.Sprintf("%d:%d", d.Line, d.Column)
		if d.Proposal != 0 {
			where = fmt.Sprintf("proposal #%d, %s", d.Proposal, where)
		}
		fmt.Fprintf(&sb, "  %s %s: %s\n", where, d.Severity, strings.ReplaceAll(d.Message, "\n", "\n    "))
	}
	if !o.UpToDate {
		sb.WriteString("The language server hasn't caught up with the latest text yet; call again shortly.\n")
	}
	return sb.String()
}

var severityNames = map[int]string{lsp.Error: "error", lsp.Warning: "warning", lsp.Information: "info", lsp.Hint: "hint"}

// diagnosticsWait is how long get_diagnostics waits for the server to catch up.
var diagnosticsWait = 10 * time.Second

// diagnosticsTool polls the editor from `hiss mcp` until the language server
// has checked the latest text, so the editor's loop never waits for it.
var diagnosticsTool = tool{
	name: "get_diagnostics",
	register: func(s *mcp.Server, fwd Forward) {
		mcp.AddTool(s, &mcp.Tool{
			Name: "get_diagnostics",
			Description: "Get the language server's errors and warnings for a file, checked as if your pending " +
				"proposals in it were accepted, so you see what a proposal breaks before the user reviews it. " +
				"Problems in a pending proposal's new text name the proposal, with positions in that text; " +
				"the rest have positions in the file as read_buffer shows it. Waits briefly for the server.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in GetDiagnosticsIn) (*mcp.CallToolResult, DiagnosticsOut, error) {
			deadline := time.Now().Add(diagnosticsWait)
			for {
				var out DiagnosticsOut
				raw, err := fwd(ctx, agentName(req), "get_diagnostics", in)
				if err == nil {
					err = json.Unmarshal(raw, &out)
				}
				if err != nil {
					return nil, out, err
				}
				if out.UpToDate || time.Now().After(deadline) {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Text()}}}, out, nil
				}
				select {
				case <-ctx.Done():
					return nil, out, ctx.Err()
				case <-time.After(pollInterval):
				}
			}
		})
	},
	exec: func(ed *core.Editor, _ string, args json.RawMessage) (any, error) {
		var in GetDiagnosticsIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		path := resolve(ed, in.Path)
		ds, fresh, err := ed.Diagnostics(path)
		if err != nil {
			return nil, err
		}
		out := DiagnosticsOut{Path: path, Version: ed.Find(path).Version, UpToDate: fresh, Diagnostics: []DiagnosticInfo{}}
		for _, d := range ds {
			out.Diagnostics = append(out.Diagnostics, DiagnosticInfo{
				Severity: severityNames[d.Severity], Message: d.Message,
				Proposal: d.Proposal, Line: d.Line, Column: d.Column,
			})
		}
		return out, nil
	},
}
