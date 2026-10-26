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

// rulesFile is the rules file, relative to the project root.
const rulesFile = ".hissrun"

type rule struct {
	name  string
	match []string // globs: on the file name, or on the path from the root when they have a /
	cmd   string
	find  *regexp.Regexp
	file  bool // input = file: the whole file is the input
	share bool // the agent may see the output
}

// loadRules reads the project's rules; no file means no rules.
func loadRules(root string) ([]rule, error) {
	if root == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(root, rulesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseRules(string(data))
}

func parseRules(src string) ([]rule, error) {
	var rules []rule
	var r *rule
	start := 0 // the line r starts on
	done := func() error {
		switch {
		case r == nil:
			return nil
		case len(r.match) == 0:
			return fmt.Errorf("%s:%d: [%s] has no match", rulesFile, start, r.name)
		case r.cmd == "":
			return fmt.Errorf("%s:%d: [%s] has no cmd", rulesFile, start, r.name)
		}
		rules = append(rules, *r)
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
			r, start = &rule{name: strings.TrimSpace(l[1 : len(l)-1])}, n
			continue
		}
		key, val, ok := strings.Cut(l, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%s:%d: %s", rulesFile, n, fmt.Sprintf(format, a...))
		}
		switch {
		case !ok:
			return nil, bad("want [name] or key = value")
		case r == nil:
			return nil, bad("%s before the first [name]", key)
		}
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
			b, err := strconv.ParseBool(val)
			if err != nil {
				return nil, bad("share is true or false, not %q", val)
			}
			r.share = b
		default:
			return nil, bad("unknown key %q (match, cmd, find, input, share)", key)
		}
	}
	if err := done(); err != nil {
		return nil, err
	}
	return rules, nil
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
	run := &run{title: r.name, dir: e.Root, share: r.share,
		env: []string{"file=" + b.Path, "dir=" + filepath.Dir(b.Path),
			"line=" + strconv.Itoa(line+1), "root=" + e.Root}}
	var show strings.Builder
	show.WriteString(r.cmd)
	if r.find != nil && input == "" {
		caps, at := e.findUp(r.find)
		if at < 0 {
			e.Msg = fmt.Sprintf("[%s]: nothing above the cursor matches %s", r.name, r.find)
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
func (e *Editor) runByRule() bool {
	rules, err := loadRules(e.Root)
	if err != nil {
		e.Msg = err.Error()
		return true
	}
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
		e.Msg = fmt.Sprintf("no rule in %s for ```%s blocks", rulesFile, lang)
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
