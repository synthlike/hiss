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
./hiss path/to/file   # or just ./hiss, then :e <path> to open a file
```

## Use with Claude Code

Install hiss, register its MCP bridge, and print the Claude Code settings it needs:

```sh
go install ./cmd/hiss
claude mcp add --scope user hiss -- hiss mcp
hiss setup   # merge the output into .claude/settings.json in your project
```

The settings deny Claude's built-in Edit and Write tools, allow hiss's tools, and
add hooks (`hiss hook`) that tell hiss which turn each proposal came from and
whether Claude is working, idle, waiting on you, or awaiting your review; the
status line shows this. Shell commands can still write files, so this routes
edits through hiss by convention rather than enforcement.

Run `hiss <file>` in your project and start Claude Code in the same project.
`hiss mcp` finds the editor by project root (the nearest directory containing
`.git`), so Claude can start before or after the editor. Set `HISS_SOCKET` to
override the socket path.

Agents read buffers (including unsaved changes) and your selection, and propose
edits with the same `old_string` / `new_string` rules as Claude Code's Edit tool.
Proposals appear inline as pending hunks; agents wait for your decision with
`await_review`.

## Reviewing

| Keys | Action |
| --- | --- |
| `]h` / `[h` | next / previous pending hunk (across buffers) |
| `+` | accept the hunk under the cursor (applies and saves) |
| `-` | reject it; `:hr <reason>` (`:hunk-reject`) tells the agent why |
| `#` | comment on it, on the `+`/`-` line under the cursor, or on a trail step; the agent replies or revises |
| `j` / `k` | move through the proposed `+` lines too |
| `]s` / `[s` | next / previous step of the trail the agent shared (`show_trail`) |
| `space f` / `b` / `h` / `t` | pick a file / buffer / pending hunk / trail step |
| `:haa` / `:hra [reason]` | accept or reject every pending hunk (`:hunk-accept-all` / `:hunk-reject-all`) |
| `:tc` | stop showing the trail (`:trail-clear`) |

Typing `:` lists the commands, each with a short and a long name (`ha` /
`hunk-accept`); `tab` completes the name.

To adjust a proposal before accepting it, edit its `+` lines (`i`, `c`, `d`, `o`,
`p`, `x` …): the proposed text moves into the buffer, with the old lines shown
below it, and `+` accepts it as you left it. The agent is told about your edits and
given the saved text; `-` restores the old text, and undoing past the first edit
turns it back into a plain proposal. Editing the old (`-`) lines of a pending hunk
instead makes it stale; the agent is told to re-read.

Comments reach Claude while it waits for your review, or wake it after its turn
ends (via the `hiss hook watch` Stop hook that `hiss setup` adds).

## License

MIT, see [LICENSE](LICENSE).
