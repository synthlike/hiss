package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The rules file maps files to commands for Enter, ahead of the built-in Go
// test runner:
//
//	[sql]
//	match = *.sql
//	cmd   = psql "$DATABASE_URL" -X -f -
//
// A rule sends the paragraph under the cursor to the command's stdin (or the
// whole file, with input = file, or the selection in Select mode). With find,
// it instead searches upward from the cursor for a regexp and passes its
// captures as $1…. Commands run with sh -c in the project root, with $file,
// $dir, $line and $root set; hiss substitutes nothing itself. In Markdown, a
// fenced block (```sql) runs with the rule for a file of that extension.
//
// [env] sections name environments; runs source the active one's script
// first, and a rule's env = <name> pins it to one. [go] configures the
// built-in Go runner.

// rulesFile is the rules file, relative to the project root.
const rulesFile = ".hissrun"

type rule struct {
	name  string
	match []string // globs: on the file name, or on the path from the root when they have a /
	cmd   string
	find  *regexp.Regexp
	file  bool   // input = file: the whole file is the input
	share bool   // the agent may see the output
	env   string // the environment it always runs in; "" for the active one
}

// env is an environment runs can be switched between with :env.
type env struct {
	name    string
	source  string // a script sourced with set -a before the command; "" for none
	confirm bool   // ask before running
}

// runConfig is what the rules file says.
type runConfig struct {
	rules   []rule
	envs    []env
	goEnv   string // the built-in test runner's env
	goShare bool
}

func (c *runConfig) env(name string) *env {
	for i := range c.envs {
		if c.envs[i].name == name {
			return &c.envs[i]
		}
	}
	return nil
}

// loadRules reads the project's rules; no file means no rules.
func loadRules(root string) (*runConfig, error) {
	if root == "" {
		return parseRules("")
	}
	data, err := os.ReadFile(filepath.Join(root, rulesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return parseRules("")
	}
	if err != nil {
		return nil, err
	}
	return parseRules(string(data))
}

func parseRules(src string) (*runConfig, error) {
	c := &runConfig{goShare: true}
	var r *rule
	var en *env
	section := "" // "rule", "env" or "go"
	start := 0    // the line the section starts on
	type pin struct {
		env  string
		line int
	}
	var pins []pin // envs rules pin to, checked once all are read
	done := func() error {
		switch {
		case section == "env" && en.name == "":
			return fmt.Errorf("%s:%d: [env] has no name", rulesFile, start)
		case section == "env" && c.env(en.name) != nil:
			return fmt.Errorf("%s:%d: a second [env] named %s", rulesFile, start, en.name)
		case section == "env":
			c.envs = append(c.envs, *en)
		case section != "rule":
		case len(r.match) == 0:
			return fmt.Errorf("%s:%d: [%s] has no match", rulesFile, start, r.name)
		case r.cmd == "":
			return fmt.Errorf("%s:%d: [%s] has no cmd", rulesFile, start, r.name)
		default:
			c.rules = append(c.rules, *r)
		}
		return nil
	}
	for i, l := range strings.Split(src, "\n") {
		n := i + 1
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			if err := done(); err != nil {
				return nil, err
			}
			name := strings.TrimSpace(l[1 : len(l)-1])
			start = n
			switch name {
			case "env":
				section, en = "env", &env{}
			case "go":
				section = "go"
			default:
				section, r = "rule", &rule{name: name}
			}
			continue
		}
		key, val, ok := strings.Cut(l, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%s:%d: %s", rulesFile, n, fmt.Sprintf(format, a...))
		}
		boolean := func() (bool, error) {
			b, err := strconv.ParseBool(val)
			if err != nil {
				return false, bad("%s is true or false, not %q", key, val)
			}
			return b, nil
		}
		var err error
		switch {
		case !ok:
			return nil, bad("want [name] or key = value")
		case section == "":
			return nil, bad("%s before the first [name]", key)
		case section == "env":
			switch key {
			case "name":
				en.name = val
			case "source":
				en.source = val
			case "confirm":
				en.confirm, err = boolean()
			default:
				return nil, bad("unknown key %q in [env] (name, source, confirm)", key)
			}
		case section == "go":
			switch key {
			case "env":
				c.goEnv, pins = val, append(pins, pin{val, n})
			case "share":
				c.goShare, err = boolean()
			default:
				return nil, bad("unknown key %q in [go] (env, share)", key)
			}
		default:
			switch key {
			case "match":
				r.match = strings.Fields(val)
				for _, g := range r.match {
					if _, err := filepath.Match(g, ""); err != nil {
						return nil, bad("match %q: %v", g, err)
					}
				}
			case "cmd":
				r.cmd = val
			case "find":
				re, err := regexp.Compile(val)
				if err != nil {
					return nil, bad("find: %v", err)
				}
				r.find = re
			case "input":
				switch val {
				case "paragraph":
					r.file = false
				case "file":
					r.file = true
				default:
					return nil, bad("input is paragraph or file, not %q", val)
				}
			case "share":
				r.share, err = boolean()
			case "env":
				r.env, pins = val, append(pins, pin{val, n})
			default:
				return nil, bad("unknown key %q (match, cmd, find, input, share, env)", key)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if err := done(); err != nil {
		return nil, err
	}
	for _, p := range pins {
		if c.env(p.env) == nil {
			return nil, fmt.Errorf("%s:%d: no [env] is named %s", rulesFile, p.line, p.env)
		}
	}
	return c, nil
}

// matches reports whether r applies to path, given as it is relative to the
// project root.
func (r *rule) matches(rel string) bool {
	for _, g := range r.match {
		name := filepath.Base(rel)
		if strings.Contains(g, "/") {
			name = filepath.ToSlash(rel)
		}
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
	}
	return false
}

func findRule(rules []rule, rel string) *rule {
	for i := range rules {
		if rules[i].matches(rel) {
			return &rules[i]
		}
	}
	return nil
}

// runRule runs r for the cursor with input, from lines from to to, as stdin;
// a find rule without input searches for its captures instead.
func (e *Editor) runRule(r *rule, input string, from, to int) {
	b := e.Buf
	line := e.line()
	run := &run{title: r.name, dir: e.Root, share: r.share, pin: r.env,
		env: []string{"file=" + b.Path, "dir=" + filepath.Dir(b.Path),
			"line=" + strconv.Itoa(line+1), "root=" + e.Root}}
	var show strings.Builder
	show.WriteString(r.cmd)
	if r.find != nil && input == "" {
		caps, at := e.findUp(r.find)
		if at < 0 {
			e.Msg = fmt.Sprintf("%s: nothing above the cursor matches %s", r.name, r.find)
			return
		}
		run.argv = append([]string{"sh", "-c", r.cmd, "sh"}, caps...)
		for i, c := range caps {
			fmt.Fprintf(&show, "  # $%d=%s", i+1, c)
		}
		run.warnDir = filepath.Dir(b.Path) // it reads the files on disk
	} else {
		run.argv = []string{"sh", "-c", r.cmd}
		run.stdin = input
		fmt.Fprintf(&show, "  < %s:%d-%d", e.relName(b), from+1, to+1)
	}
	run.show = show.String()
	e.startRun(run)
}

// findUp searches upward from the cursor's line for re and returns the
// first match's captures and line, or -1.
func (e *Editor) findUp(re *regexp.Regexp) ([]string, int) {
	t := e.Buf.Text
	for l := e.line(); l >= 0; l-- {
		if m := re.FindStringSubmatch(string(t.Slice(t.LineStart(l), e.Buf.LineEnd(l)))); m != nil {
			return m[1:], l
		}
	}
	return nil, -1
}

// paragraph returns the lines around the cursor up to blank lines, and the
// first and last of them, or false on a blank line.
func (e *Editor) paragraph() (string, int, int, bool) {
	b := e.Buf
	blank := func(l int) bool {
		return strings.TrimSpace(string(b.Text.Slice(b.Text.LineStart(l), b.LineEnd(l)))) == ""
	}
	cur := e.line()
	if cur >= b.Lines() || blank(cur) {
		return "", 0, 0, false
	}
	from, to := cur, cur
	for from > 0 && !blank(from-1) {
		from--
	}
	for to+1 < b.Lines() && !blank(to+1) {
		to++
	}
	return e.lines(from, to), from, to, true
}

// lines returns lines from to to of the buffer, each ending in a newline.
func (e *Editor) lines(from, to int) string {
	b := e.Buf
	s := string(b.Text.Slice(b.Text.LineStart(from), b.LineEnd(to)))
	return s + "\n"
}

// selection returns the selected text, ending in a newline, and its first
// and last lines, leaving Select mode.
func (e *Editor) selection() (string, int, int) {
	from, to := e.selRange()
	e.Mode = Normal
	t := e.Buf.Text
	s := string(t.Slice(from, to))
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, t.LineOf(from), t.LineOf(max(to-1, from))
}

// fence finds the fenced block around the cursor in Markdown: its language
// and its contents' first and last lines (last < first when it's empty).
func (e *Editor) fence() (lang string, from, to int, ok bool) {
	b := e.Buf
	cur := e.line()
	open := -1
	for l := range b.Lines() {
		s := strings.TrimLeft(string(b.Text.Slice(b.Text.LineStart(l), b.LineEnd(l))), " ")
		if !strings.HasPrefix(s, "```") {
			continue
		}
		if open < 0 {
			open, lang = l, strings.TrimSpace(strings.TrimPrefix(s, "```"))
			if i := strings.IndexAny(lang, " {"); i >= 0 {
				lang = lang[:i]
			}
			continue
		}
		if open <= cur && cur <= l {
			return lang, open + 1, l - 1, lang != ""
		}
		open = -1
	}
	return "", 0, 0, false
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// runByRule runs the rule for the cursor, if there is one, and reports
// whether there was.
func (e *Editor) runByRule(rules []rule) bool {
	b := e.Buf
	if b.Path == "" || len(rules) == 0 {
		return false
	}
	rel := b.Path
	if r, ok := e.underRoot(b.Path); ok {
		rel = r
	}
	sel := e.Mode == Select
	if r := findRule(rules, rel); r != nil {
		switch {
		case sel:
			s, from, to := e.selection()
			e.runRule(r, s, from, to)
		case r.file:
			e.runRule(r, b.Text.String(), 0, max(b.Lines()-1, 0))
		case r.find != nil:
			e.runRule(r, "", 0, 0)
		default:
			s, from, to, ok := e.paragraph()
			if !ok {
				e.Msg = "no paragraph under the cursor"
				return true
			}
			e.runRule(r, s, from, to)
		}
		return true
	}
	if !isMarkdown(b.Path) {
		return false
	}
	lang, from, to, ok := e.fence()
	if !ok {
		return false
	}
	r := findRule(rules, "block."+lang)
	if r == nil || r.find != nil {
		e.Msg = fmt.Sprintf("no rule for %s blocks in %s", lang, rulesFile)
		return true
	}
	s := ""
	if to >= from {
		s = e.lines(from, to)
	}
	if sel {
		s, from, to = e.selection()
	}
	e.runRule(r, s, from, max(to, from))
	return true
}

// savedRuns is where queries and requests worth keeping live, relative to
// the project root. Like the rest of .hiss/, it's not committed.
const savedRuns = ".hiss/runs"

// openRunPicker lists the files in savedRuns that a rule matches; enter
// opens one, and enter in it runs it.
func (e *Editor) openRunPicker() {
	c, err := e.loadRunConfig()
	if err != nil {
		e.Msg = err.Error()
		return
	}
	rules := c.rules
	dir := filepath.Join(e.Root, savedRuns)
	p := &Picker{Title: "saved runs"}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(e.Root, path)
		if r := findRule(rules, rel); r != nil {
			name, _ := filepath.Rel(dir, path)
			p.Items = append(p.Items, PickItem{Label: name, Detail: r.name,
				Run: func(e *Editor) { e.openFile(path) }})
		}
		return nil
	})
	if len(p.Items) == 0 {
		e.Msg = "no saved runs in " + savedRuns + " that a rule in " + rulesFile + " matches"
		return
	}
	e.Picker = p
}
