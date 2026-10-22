# hiss

A small modal terminal editor for working alongside AI agents.

Agents such as Claude Code don't write files directly: they propose edits through
an MCP server built into hiss, and you review, accept or reject them inline. Every
edit records who made it, so you can see which agent turn wrote a line and undo
an agent's change while keeping your own.

Editing is selection-first and keyboard-driven, in the style of Helix.

## Status

Early and in progress.

## Build

Requires Go 1.27 or later.

```sh
go build -o hiss ./cmd/hiss
./hiss path/to/file
```

## Use with Claude Code

Install hiss and register its MCP bridge once for all projects:

```sh
go install ./cmd/hiss
claude mcp add --scope user hiss -- hiss mcp
```

Run `hiss <file>` in your project and start Claude Code in the same project.
`hiss mcp` finds the editor by project root (the nearest directory containing
`.git`), so Claude can start before or after the editor. Set `HISS_SOCKET` to
override the socket path.

Agents can currently list buffers, read them (including unsaved changes) and
see your selection.

## License

MIT, see [LICENSE](LICENSE).
