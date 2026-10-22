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

## License

MIT, see [LICENSE](LICENSE).
