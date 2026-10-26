package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRules(t *testing.T) {
	rules, err := parseRules(`# comment
[sql]
match = *.sql  queries/*.txt
cmd   = psql -X -f -
share = true

[gotest]
match = *_test.go
find  = ^func (Test\w+)\(
cmd   = go test -run "^$1$" .
input = file
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("%d rules", len(rules))
	}
	sql, gt := rules[0], rules[1]
	if sql.name != "sql" || sql.cmd != "psql -X -f -" || !sql.share || sql.file || sql.find != nil {
		t.Errorf("sql = %+v", sql)
	}
	if gt.find == nil || gt.find.String() != `^func (Test\w+)\(` || !gt.file || gt.share {
		t.Errorf("gotest = %+v", gt)
	}
	for path, want := range map[string]string{
		"a.sql": "sql", "db/b.sql": "sql", "queries/c.txt": "sql", "d.txt": "",
		"internal/x_test.go": "gotest", "x.go": "",
	} {
		got := ""
		if r := findRule(rules, path); r != nil {
			got = r.name
		}
		if got != want {
			t.Errorf("%s: rule %q, want %q", path, got, want)
		}
	}
}

func TestParseRulesErrors(t *testing.T) {
	cases := map[string]string{
		"match = *.sql\n":                         ".hissrun:1: match before the first [name]",
		"[a]\nmatch = *.sql\n":                    ".hissrun:1: [a] has no cmd",
		"[a]\ncmd = x\n[b]\n":                     ".hissrun:1: [a] has no match",
		"[a]\nmatch *.sql\n":                      ".hissrun:2: want [name] or key = value",
		"[a]\nmatch = *.sql\ncmd = x\nfind = (\n": ".hissrun:4: find: error parsing regexp: missing closing ): `(`",
		"[a]\ninput = all\n":                      ".hissrun:2: input is paragraph or file, not \"all\"",
		"[a]\nshare = yes\n":                      ".hissrun:2: share is true or false, not \"yes\"",
		"[a]\nmatch = [\n":                        ".hissrun:2: match \"[\": syntax error in pattern",
		"[a]\nrun = x\n":                          ".hissrun:2: unknown key \"run\" (match, cmd, find, input, share)",
	}
	for src, want := range cases {
		if _, err := parseRules(src); err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %s", src, err, want)
		}
	}
}

const testRules = `[upper]
match = *.txt
cmd   = tr a-z A-Z; echo "line=$line name=$(basename "$file") root=$(basename "$root")"

[whole]
match = *.all
input = file
cmd   = wc -l | tr -d ' '

[found]
match = *.go
find  = ^func (\w+)\((\w*)
cmd   = echo "found $1 [$2] in $(basename "$dir")"
`

// rulesEditor writes testRules and opens name with content in that project.
func rulesEditor(t *testing.T, name, content string) (*Editor, chan func(*Editor)) {
	t.Helper()
	e := newEditor(t, "")
	e.Root = filepath.Dir(e.Buf.Path)
	for f, s := range map[string]string{rulesFile: testRules, name: content} {
		if err := os.WriteFile(filepath.Join(e.Root, f), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ch := make(chan func(*Editor))
	e.Post = func(f func(*Editor)) { ch <- f }
	t.Cleanup(e.StopRuns)
	e.openFile(filepath.Join(e.Root, name))
	return e, ch
}

// runOutput presses keys, waits for the run and returns *out* without its
// header line and the closing status.
func runOutput(t *testing.T, e *Editor, ch chan func(*Editor), ks string) string {
	t.Helper()
	keys(e, ks)
	if e.runs.going == nil {
		t.Fatalf("%q started nothing: %q", ks, e.Msg)
	}
	pump(t, e, ch, finished(e))
	lines := strings.Split(strings.TrimSuffix(e.runs.out.Text.String(), "\n"), "\n")
	return strings.Join(lines[1:len(lines)-1], "\n")
}

func TestRuleParagraph(t *testing.T) {
	e, ch := rulesEditor(t, "q.txt", "one\n\nselect a\nfrom b\n\nthree\n")
	root := filepath.Base(e.Root)
	gotoText(t, e, "from")
	got := runOutput(t, e, ch, "enter")
	if want := "SELECT A\nFROM B\nline=4 name=q.txt root=" + root; got != want {
		t.Errorf("paragraph:\n%s\nwant:\n%s", got, want)
	}
	if h := e.runs.out.Text.String(); !strings.HasPrefix(h, "$ tr a-z A-Z;") || !strings.Contains(h, "  < q.txt:3-4\n") {
		t.Errorf("header: %q", h)
	}
	if !strings.HasPrefix(e.Msg, "upper: ok (") {
		t.Errorf("msg %q", e.Msg)
	}

	keys(e, "C-o")
	gotoText(t, e, "lect")
	if got := runOutput(t, e, ch, "v l l enter"); !strings.HasPrefix(got, "LEC\n") { // the selection, ending in a newline
		t.Errorf("selection: %q", got)
	}
	if e.Mode != Normal {
		t.Errorf("mode %s after running the selection", e.Mode)
	}

	keys(e, "C-o")
	e.setHead(e.Buf.Text.LineStart(1)) // the blank line
	keys(e, "enter")
	if e.Msg != "no paragraph under the cursor" || e.runs.going != nil {
		t.Errorf("blank line: msg %q", e.Msg)
	}
}

func TestRuleDropsCarriageReturns(t *testing.T) {
	e, ch := rulesEditor(t, "x.txt", "a\n")
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[crlf]\nmatch = *.txt\ncmd = printf 'H: 1\\r\\n\\r\\nbody\\n'\n"), 0o600)
	if got := runOutput(t, e, ch, "enter"); got != "H: 1\n\nbody" {
		t.Errorf("got %q", got)
	}
}

func TestRuleWholeFile(t *testing.T) {
	e, ch := rulesEditor(t, "x.all", "a\n\nb\n")
	if got := runOutput(t, e, ch, "enter"); got != "3" {
		t.Errorf("wc -l: %q", got)
	}
}

func TestRuleFind(t *testing.T) {
	e, ch := rulesEditor(t, "x.go", "package x\n\nfunc Alpha(n int) {\n\treturn\n}\n")
	gotoText(t, e, "return")
	if got := runOutput(t, e, ch, "enter"); got != "found Alpha [n] in "+filepath.Base(e.Root) {
		t.Errorf("find: %q", got)
	}
	if h := e.runs.out.Text.String(); !strings.Contains(h, "  # $1=Alpha  # $2=n\n") {
		t.Errorf("header: %q", h)
	}
	keys(e, "C-o g g enter")
	if !strings.HasPrefix(e.Msg, "[found]: nothing above the cursor matches") {
		t.Errorf("above every func: msg %q", e.Msg)
	}
}

func TestRuleFences(t *testing.T) {
	md := "# notes\n\n```txt\nhello\n\nworld\n```\n\n```go\nx\n```\n\n```py\ny\n```\n"
	e, ch := rulesEditor(t, "n.md", md)
	gotoText(t, e, "world")
	got := runOutput(t, e, ch, "enter")
	if !strings.HasPrefix(got, "HELLO\n\nWORLD\nline=6 name=n.md") {
		t.Errorf("fence: %q", got)
	}
	keys(e, "C-o")
	gotoText(t, e, "```go")
	keys(e, "enter")
	if e.Msg != "no rule in .hissrun for ```go blocks" {
		t.Errorf("find rule for a fence: msg %q", e.Msg)
	}
	gotoText(t, e, "y\n")
	keys(e, "enter")
	if e.Msg != "no rule in .hissrun for ```py blocks" {
		t.Errorf("no rule: msg %q", e.Msg)
	}
	gotoText(t, e, "# notes")
	keys(e, "enter")
	if e.Msg != "nothing to run here" {
		t.Errorf("outside fences: msg %q", e.Msg)
	}
}

func TestRuleBeatsBuiltIn(t *testing.T) {
	e, ch := rulesEditor(t, "x_test.go", "package x\n\nfunc TestA(t *testing.T) {\n}\n")
	gotoText(t, e, "}")
	if got := runOutput(t, e, ch, "enter"); got != "found TestA [t] in "+filepath.Base(e.Root) {
		t.Errorf("got %q", got)
	}
}

func TestBadRulesFile(t *testing.T) {
	e := newEditor(t, "a\n")
	e.Root = filepath.Dir(e.Buf.Path)
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[a]\n"), 0o600)
	keys(e, "enter")
	if e.Msg != ".hissrun:1: [a] has no match" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestRunPicker(t *testing.T) {
	e, ch := rulesEditor(t, "x.txt", "")
	keys(e, "space r")
	if e.Picker != nil || e.Msg != "no saved runs in .hiss/runs that a rule in .hissrun matches" {
		t.Fatalf("none saved: picker %v, msg %q", e.Picker != nil, e.Msg)
	}
	runs := filepath.Join(e.Root, savedRuns)
	os.MkdirAll(filepath.Join(runs, "db"), 0o755)
	for name, s := range map[string]string{"login.txt": "hi\n", "db/all.all": "a\nb\n", "notes.md": "x\n"} {
		os.WriteFile(filepath.Join(runs, name), []byte(s), 0o600)
	}
	keys(e, "space r")
	if e.Picker == nil {
		t.Fatalf("no picker: %q", e.Msg)
	}
	var got []string
	for _, it := range e.Picker.Items {
		got = append(got, it.Label+" "+it.Detail)
	}
	if want := []string{filepath.Join("db", "all.all") + " whole", "login.txt upper"}; !equal(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
	keys(e, "l o g enter")
	if e.Buf.Path != filepath.Join(runs, "login.txt") || e.runs.going != nil {
		t.Fatalf("opened %s, running %v", e.Buf.Path, e.runs.going != nil)
	}
	if got := runOutput(t, e, ch, "enter"); !strings.HasPrefix(got, "HI\n") {
		t.Errorf("running it: %q", got)
	}
}
