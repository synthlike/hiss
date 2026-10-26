# Using hiss

The details behind the [README](../README.md): working with Claude, editing,
and running tests, queries and requests.

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
- Colours follow your terminal's light or dark background; `HISS_THEME=light` or
  `dark` overrides.

## Running

`enter` runs what's under the cursor and streams the output into a panel below
the text, so the cursor stays put. `esc` hides the panel and `C-c` cancels.

- **Go tests.** In a test, `enter` runs it with `go test -run` on the files as
  saved; in a table case (a `name:` field or a map key) or a `t.Run("…")`,
  just that subtest.
- **Queries, requests, scripts.** `.hissrun` says what to run in other files
  (see below). In Markdown, a fenced block runs with the rule for its language.
- **Output.** `space o` opens the whole output as `*out*`, where `enter` on a
  `file:line` goes to it and on a `--- FAIL: TestX/case` line runs that test
  again. `[r` / `]r` step through the last 20 runs (kept until you quit), and
  `:rerun` (`:rr`) runs the one shown again from anywhere.
- **Saved runs.** Queries and requests worth keeping go in `.hiss/runs/`;
  `space r` lists the ones a rule matches.
- **Claude sees the result.** With `share = true`, Claude gets the command, its
  exit status and the last 20 lines of output with your next prompt (or its
  next tool call). Go runs share unless `[go]` says `share = false`; rules
  don't unless asked.
- **Claude proposes runs.** The hunk says how it will run (`▶ runs [sql] on
  dev`), and accepting it saves the file and runs it in the active
  environment. Claude hears how it went, with the output only when the rule
  shares.

### .hissrun

`.hissrun` at the project root holds the rules. Each `[name]` section matches
files by glob and gives a command, which runs with `sh -c` in the project root
with `$file`, `$dir`, `$line` and `$root` set. The paragraph under the cursor
(or the selection, or with `input = file` the whole file) goes to its stdin;
with `find`, a regexp searched upward from the cursor gives it `$1`….

```ini
[sql]
match = *.sql
cmd   = psql "$DATABASE_URL" -X -f -

# requests.http holds curl commands; secrets stay in your shell's env
[http]
match = *.http
cmd   = sh
```

`[env]` sections name environments. Runs source the active one's script (with
`set -a`, fresh each time) and get its name as `$env`. The first is active at
start; `:env <name>` switches (`:env` alone picks), and the status line shows
the name in teal. `confirm = true` asks before each run there and shows the
name in red. A rule's `env = <name>` always runs in that one. `[go]` sets the
built-in Go runner's `env` and `share`; a rule matching `*.go` replaces the
runner instead.

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
