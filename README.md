# hiss

A small modal terminal editor for working alongside Claude Code.

Claude doesn't write your files: it proposes edits, and you review them inline,
accept or reject each one, tweak it before accepting, or comment and let Claude
revise. Navigation is inspired by Helix.

## Install

Requires Go 1.27 or later.

```sh
go install github.com/synthlike/hiss/cmd/hiss@latest
```

## Set up Claude Code

```sh
claude mcp add --scope user hiss -- hiss mcp
hiss setup --write   # in your project: adds hiss to .claude/settings.json
```

The settings route Claude's edits through hiss (they deny its built-in Edit and
Write tools) and add hooks so hiss knows what Claude is doing. Shell commands can
still write files, so this is a convention, not a sandbox.

Then run `hiss` in your project and Claude Code next to it, in either order.

## Working with Claude

- **Ask for a change.** It appears as a pending hunk: old lines in red, proposed
  lines in green, Claude's reason above. `]h` takes you to it.
- **Review it.** `+` accepts and saves. `-` rejects, asking for an optional reason
  to tell Claude.
- **Tweak it.** Edit the green lines, then `+`. Claude is told what you changed.
- **Discuss it.** `#` comments on the hunk or the line under the cursor; Claude
  replies inline or sends a revision, even after its turn has ended.
- **Get a tour.** Ask "walk me through the auth flow": Claude shares a trail of
  steps with notes, which you follow with `]s` and can comment on.
- **Point at code.** Select something in hiss, then ask Claude about "this"
  ("what does this do?"): it can read your selection.

| Keys | |
| --- | --- |
| `]h` / `[h` | next / previous pending hunk |
| `+` / `-` / `#` | accept / reject / comment |
| `]s` / `[s` | next / previous trail step |
| `space f` `b` `h` `t` | pick a file, buffer, hunk or trail step |
| `:haa` / `:hra` | accept / reject all hunks |

## Editing

- `/` searches as you type, `n` / `N` repeat, `*` searches for the selection.
- `:<line>` goes to a line; `C-o` / `C-i` jump back and forward.
- `:` shows the commands as you type; `tab` completes them.
- Colours follow your terminal's light or dark background; `HISS_THEME=light` or
  `dark` overrides.

## Status

v0.1: early and small. Next up (v0.2): LSP diagnostics, go to definition and
references.

## License

MIT, see [LICENSE](LICENSE).
