# hiss

A small modal terminal editor for working alongside Claude Code.

Claude doesn't write your files: it proposes edits, and you review them inline,
accept or reject each one, tweak it before accepting, or comment and let Claude
revise. Navigation is inspired by Helix.

## In action

Review a change set hunk by hunk, and discuss it inline.

<a href="docs/screenshots/review-light.png"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/review-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/review-light.png">
  <img alt="An inline review in hiss: Claude proposes a JSON response as a set of three hunks; one is accepted, and a comment thread under the next asks about errors." src="docs/screenshots/review-light.png" width="600">
</picture></a>

Follow a guided code tour, with notes anchored to the relevant lines.

<a href="docs/screenshots/code-tour-light.png"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/code-tour-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/code-tour-light.png">
  <img alt="A guided code tour in hiss: Claude explains how a request becomes a response in three inline steps, and answers a comment on the last one." src="docs/screenshots/code-tour-light.png" width="600">
</picture></a>

Answer Claude's questions without leaving the editor.

<a href="docs/screenshots/ask-light.png"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/ask-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/ask-light.png">
  <img alt="A question popup in hiss: Claude asks whether to keep the plain-text greeting, offering three answers over the pending hunks." src="docs/screenshots/ask-light.png" width="600">
</picture></a>

Run the test under the cursor with `enter`; Claude sees the result too.

<a href="docs/screenshots/run-light.png"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/run-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/run-light.png">
  <img alt="A test run in hiss: enter on TestGreet streams go test output into a panel, which notes the run didn't include two pending hunks." src="docs/screenshots/run-light.png" width="600">
</picture></a>

Find the change you want to review with the fuzzy hunk picker (`space h`).

<a href="docs/screenshots/fuzzy-finder-light.png"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/fuzzy-finder-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/screenshots/fuzzy-finder-light.png">
  <img alt="The fuzzy hunk picker in hiss: typing json filters five pending hunks to the four that match, across three files." src="docs/screenshots/fuzzy-finder-light.png" width="600">
</picture></a>

Click a screenshot for full size. `docs/screenshots/shoot.sh` regenerates them from the real UI.

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
- **See what it breaks.** In Go files, gopls checks the code as if the pending
  hunks were accepted: problems in a hunk show on its green lines, ones it
  causes elsewhere on those lines. Claude can check its own with get_diagnostics,
  and run `go vet` / `go test` on them before you review, through go_overlay.
- **Discuss it.** `#` comments on the hunk or the line under the cursor; Claude
  replies inline or sends a revision, even after its turn has ended.
- **Get a tour.** Ask "walk me through the auth flow": Claude shares a trail of
  steps with notes, which you follow with `]s` and can comment on.
- **Pick up where you left off.** Pending hunks and their threads are kept in
  `.hiss/proposals.json`, so they survive a crash or restart; `:q!` discards
  them. A hunk whose old text has changed on disk in the meantime is dropped.
- **Point at code.** Select something in hiss, then ask Claude about "this"
  ("what does this do?"): it can read your selection.

| Keys | |
| --- | --- |
| `]h` / `[h` | next / previous pending hunk |
| `+` / `-` / `#` | accept / reject / comment |
| `]s` / `[s` | next / previous trail step |
| `space f` `b` `h` `t` | pick a file, buffer, hunk or trail step |
| `:sa` / `:sr` | accept / reject the hunk's change set: what Claude proposed in that turn |
| `:haa` / `:hra` | accept / reject all hunks |

## Editing

- `/` searches as you type, `n` / `N` repeat, `*` searches for the selection.
- `:<line>` goes to a line; `C-o` / `C-i` jump back and forward.
- `:` shows the commands as you type; `tab` completes them.
- Lines are numbered, and long lines wrap at the edge of the window, proposed
  ones too; `j` / `k` still move by line.
- `:note <text>` (`:n`) adds a line to `.hiss/notes.md` with the date, the
  commit (`+` if the file has changed since) and the cursor's `path:line`;
  `space n` lists the notes, newest first.
- In Go files, gopls's diagnostics are marked in the gutter and underlined; the
  cursor line's message shows below, and `]d` / `[d` go to the next / previous.
  `gd` goes to a definition and `gr` lists references, also from proposed lines;
  `space s` lists the file's symbols.
  Resting the cursor on a name highlights its other uses; on `func` or
  `return`, the function's returns.
- In a Go test, `enter` runs it with `go test -run` on the files as saved and
  streams the output into a panel below the text, so the cursor stays put;
  `esc` hides it. In a table case (a `name:` field or a map key) or a
  `t.Run("…")`, it runs just that subtest. `space o` opens the whole output
  as `*out*`, where `enter` on a `file:line` goes to it and on a
  `--- FAIL: TestX/case` line runs that test again. `C-c` cancels, and `:rerun` (`:rr`) runs it
  again from anywhere.
- `.hissrun` at the project root tells `enter` what to run in other files. The
  paragraph under the cursor (or the selection, or with `input = file` the
  whole file) goes to the command's stdin; with `find`, a regexp searched
  upward from the cursor gives the command `$1`…. Commands run with `sh -c` in
  the project root, with `$file`, `$dir`, `$line` and `$root` set. In
  Markdown, a fenced block runs with the rule for its language.

  ```ini
  [sql]
  match = *.sql
  cmd   = psql "$DATABASE_URL" -X -f -

  # requests.http holds curl commands; secrets stay in your shell's env
  [http]
  match = *.http
  cmd   = sh
  ```

  `[env]` sections name environments. Runs source the active one's script
  (with `set -a`, fresh each time) and get its name as `$env`; the first is
  active at start, `:env <name>` switches and the status line shows it.
  `confirm = true` asks before each run there. A rule's `env = <name>` always
  runs in that one, and `[go]` sets the built-in Go runner's `env` and
  `share` (a rule matching `*.go` replaces the runner instead).

  ```ini
  [env]
  name   = dev
  source = scripts/env-dev.sh

  [env]
  name    = staging
  source  = scripts/env-staging.sh
  confirm = true

  [go]
  env = dev
  ```

  Queries and requests worth keeping go in `.hiss/runs/`; `space r` lists the
  ones a rule matches. With `share = true`, Claude gets the command, its exit
  status and the last 20 lines of output with your next prompt (or its next
  tool call); Go runs share unless `[go]` says `share = false`, rules don't
  unless asked.
- Colours follow your terminal's light or dark background; `HISS_THEME=light` or
  `dark` overrides.

## Status

v0.4: early and small. An agent turn's proposals are reviewed as one change
set, and agents can vet and test them before you do; run output streams into
a panel below the text. Since v0.3, `enter` runs the Go test, SQL query or
HTTP request under the cursor, next to gopls diagnostics and navigation.

## License

MIT, see [LICENSE](LICENSE).
