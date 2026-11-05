# Using hiss

The details behind the [README](../README.md): working with Claude, editing,
and running tests, queries and requests.

## Working with Claude

- **Ask for a change.** Routine edits apply and save right away, with a faint
  mark in the gutter. `space g` lists what agents changed, newest first, and
  `:cc` clears the marks. `:su [reason]` undoes a turn's applied changes, the
  one under the cursor or else the newest, and tells Claude; text edited since
  stays, and so does what you accepted in review. This history lasts the session;
  after a restart, git is the way back.
- **Get asked to look.** Claude asks for review when you should see something
  before it lands: auth, deletions, migrations, public APIs, changes beyond what
  you asked, real choices. It appears as a pending hunk: old lines in red,
  proposed lines in green, Claude's reason above. `]h` takes you to it. Edits are
  also held when the file has your unsaved changes or is outside the project,
  for `.hissrun` itself, for proposed runs, and for files `[review]` names.
- **Review it.** `+` accepts and saves. `-` rejects, asking for an optional reason
  to tell Claude.
- **Tweak it.** Edit the green lines, then `+`. Claude is told what you changed.
- **See what it breaks.** In files with a language server, it checks
  the code as if the pending hunks were accepted: problems in a hunk show on its
  green lines, ones it causes elsewhere on those lines. Claude can check its own
  with get_diagnostics, and in Go run `go vet` / `go test` on them before you
  review, through go_overlay.
- **Review what landed.** Claude sends a change for review like a trail, narrowed
  to what matters and with notes on what to check; `:review` (`:rv`) starts one
  yourself: uncommitted changes, else the last commit, or a revision or range
  (`:rv HEAD~2`, `:rv main..fix`). `]s` walks the hunks, with what was removed
  under the new lines. `+` accepts a hunk. `-` rejects it, asking for an optional
  reason: the old text goes back in your working tree, unsaved edits stay
  unsaved, and history isn't touched. If the text has changed since, it stays
  and Claude hears that. `#` comments, and Claude answers as you go. The review
  finishes when every hunk is decided, or with `:rf [word for Claude]`, and
  Claude gets a report. `:rs <path>` and `:rl <path>` save and load a
  review as JSON, with its diff, verdicts and discussion, so it can be picked up
  later. Untracked and deleted files aren't shown.
- **Review a GitHub PR.** `:pr <n>` finds the PR with `gh`, fetches it from the
  remote that points at its repository and checks its head out detached; the
  tree must be clean. It walks the PR's hunks like `:review`, but the code is
  the author's: `+` marks a hunk viewed and `-` flags it, with an optional
  reason, and neither touches the files. `#` still talks to Claude. `:prd`
  (`:pr-done`) checks out the branch you started from.
- **Comment on GitHub.** `:gc <text>` drafts an official comment on the line
  under the cursor, or on the selected lines within one hunk. GitHub only takes
  comments on lines its diff shows, the changed lines and three around each
  hunk, so others are refused right away. Drafts show in purple under their
  line and are kept in `.hiss/pr/<n>.json` as you write them; `:pr <n>` picks
  them up again, and warns if the PR has moved on since. `:gcd` drops the drafts
  on a line. Asked for comments, Claude drafts them too, but they publish in your
  name, so each waits until `:gca` accepts it. Comments go on the new side only
  for now, and a draft can't be edited: drop it and write it again.
- **Send the review.** `:prs [approve|changes] [text]` (`:pr-submit`) sends
  your accepted drafts as one review of the commit you reviewed, with `text` as
  its summary; `changes` needs one. It asks first, and says so if the author has
  pushed since. Sent drafts are dropped; if GitHub refuses, they stay and its
  reason is shown. GitHub doesn't let you approve or request changes on your own
  PR.
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
| `:pr <n>` / `:prd` | review GitHub PR n / go back |
| `:gc <text>` / `:gca` / `:gcd` | draft a GitHub comment / accept Claude's / drop the line's |
| `:prs [approve\|changes] [text]` | send the drafts as one review |

## Editing

- Keys follow Helix, with one selection: besides the basics, `A` / `I` insert
  at the line's end / first non-blank, `gs` goes there, `%` selects the file,
  `f` `t` `F` `T` select to a character on the line, `r` replaces the
  selection's characters, `>` / `<` indent and `J` joins lines.
- `]f` / `[f` go to the next / previous function. `]e` / `[e` go to the next /
  previous line indented like the cursor's, within its block: the next element
  of a JSON array, the next statement, or at the top of a Go file the next
  declaration. `mm` goes to the matching bracket.
- `/` searches as you type, `n` / `N` repeat, `*` searches for the selection.
- `:<line>` goes to a line; `C-o` / `C-i` jump back and forward.
- `:` shows the commands as you type; `tab` completes them.
- Lines are numbered, and long lines wrap at the edge of the window, proposed
  ones too; `j` / `k` still move by line.
- `:note <text>` (`:n`) adds a line to `.hiss/notes.md` with the date, the
  commit (`+` if the file has changed since) and the cursor's `path:line`;
  `space n` lists the notes, newest first.
- Files get a language server from a built-in table, the first one on your
  PATH; no config. Install one later and restart hiss to use it.

  | Files | Servers, in order |
  | --- | --- |
  | Go | gopls |
  | Solidity | `forge lsp` (checks with solar) |
  | Rust | rust-analyzer |
  | TypeScript, JavaScript | `tsc --lsp` (TypeScript 7), typescript-language-server |
  | Python | basedpyright, pyright, pylsp |
  | C, C++ | clangd |
  | Zig | zls |
  | Lua | lua-language-server |
  | Shell (`.sh`, `.bash`) | bash-language-server |
  | Terraform | terraform-ls |

  Without one, `gd` says what to install. Servers start in the project root.
- With a language server,
  diagnostics are marked in the gutter and underlined; the cursor line's message
  shows below, and `]d` / `[d` go to the next / previous.
  `gd` goes to a definition and `gr` lists references, also from proposed lines;
  `space s` lists the file's symbols.
  Resting the cursor on a name highlights its other uses; on `func` or
  `return`, the function's returns.
- Light highlighting for Go, Solidity, JS/TS and other C-like languages, shell
  (also dotfiles and `#!` scripts), Python, Ruby, YAML, TOML, JSON, SQL, `.http`,
  Terraform, Dockerfiles, Markdown and Lisps (Common Lisp, Scheme, Racket,
  Clojure, Emacs Lisp, Fennel, Janet).
- Colours follow your terminal's light or dark background; `HISS_THEME=light` or
  `dark` overrides.

## Running

`enter` runs what's under the cursor and streams the output into a panel below
the text, so the cursor stays put. `esc` hides the panel and `C-c` cancels.

- **Go tests.** In a test, `enter` runs it with `go test -run` on the files as
  saved; in a table case (a `name:` field or a map key) or a `t.Run("…")`,
  just that subtest.
- **Foundry tests.** In a `.t.sol` test (`test…`, `invariant…` or `table…`),
  `enter` runs it with `forge test --use solar -vvv` from the nearest
  `foundry.toml`, on the files as saved. Solar's code generation is experimental
  and its gas figures differ from solc's; a rule matching `*.t.sol` runs
  something else instead.
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
  next tool call). Test runs share unless `[go]` or `[forge]` says
  `share = false`; rules don't unless asked.
- **Claude proposes runs.** The hunk says how it will run (`▶ runs [sql] on
  dev`), and accepting it saves the file and runs it in the active
  environment. Claude hears how it went, with the output only when the rule
  shares.

### .hissrun

Agents' edits to some files should always wait for you, whatever the agent
thinks. `[review]` names them, by the same globs as rules: without a `/` on the
file name, with one on the path from the root, and `*` stays within a directory.

```ini
[review]
match = migrations/* *.sol
```

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
name in red. A rule's `env = <name>` always runs in that one. `[go]` and
`[forge]` set the built-in test runners' `env` and `share`; a rule matching
`*.go` or `*.t.sol` replaces the runner instead.

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

To run Foundry tests with the project's own compiler:

```ini
[tests]
match = *.t.sol
find  = function ((?:test|invariant|table)\w*)
cmd   = cd "$(dirname "$file")" && forge test --match-test "^$1\(" -vvv
```
