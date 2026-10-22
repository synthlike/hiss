# hiss

A small modal terminal editor in Go where AI agents propose edits over MCP and the
user reviews them inline. Aim for 80/20: small, pleasant and useful beats complete.

## Layout

- `cmd/hiss`: entry point (`hiss [files]`, `hiss mcp`, `hiss hook <event>`).
- `internal/text`: immutable piece table with per-piece authorship (`Origin`) and
  position mapping across edits.
- `internal/core`: the event loop. One goroutine owns all editor state; input, MCP,
  LSP, hooks and workers talk to it over channels and never touch state directly.
- `internal/proposal`, `internal/ui`, `internal/mcpserver`, `internal/agent`,
  `internal/syntax`, `internal/lsp`: added as the build order reaches them.

## Rules

- Verify library APIs with `go doc` before using them; don't rely on memory.
- Ask before adding a dependency. Approved: `github.com/gdamore/tcell/v3`,
  `github.com/tree-sitter/go-tree-sitter` (+ grammars), `github.com/modelcontextprotocol/go-sdk`.
- Offsets in `internal/text` are byte offsets; rune/cell handling belongs to the UI.
- Work in small runnable steps: `go vet ./... && go test ./...` passes before each commit.
- Background goroutines read immutable `text.Text` snapshots only.
