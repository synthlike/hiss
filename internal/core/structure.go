package core

import (
	"bytes"
	"regexp"
	"sort"
	"strings"

	"github.com/synthlike/hiss/internal/syntax"
)

// Moving by the text's structure without parsing it: mm by brackets, ]e by
// indentation, ]f by the lines that start functions.

// Lang returns the language the buffer on screen is highlighted in, or nil:
// by its name, else by a #! line; *out* is JSON when the output starts like it.
func (e *Editor) Lang() *syntax.Lang {
	b := e.Buf
	n := b.Text.Len()
	if b == e.runs.out {
		start := min(e.runs.header, n)
		body := bytes.TrimLeft(b.Text.Slice(start, min(start+256, n)), " \t\n")
		if len(body) > 0 && (body[0] == '{' || body[0] == '[') {
			return syntax.JSON
		}
		return nil
	}
	if l := syntax.ForPath(b.Path); l != nil {
		return l
	}
	return syntax.ForShebang(b.Text.Slice(0, min(n, 256)))
}

// matchBracket moves to the bracket that matches the one under the cursor
// (mm). Brackets in strings and comments don't count, unless the cursor is
// in one itself.
func (e *Editor) matchBracket() {
	src := e.Buf.Text.Bytes()
	at := e.Sel.Head
	if at >= len(src) {
		e.Msg = "not on a bracket"
		return
	}
	const opens, closes = "([{", ")]}"
	open, close := src[at], src[at]
	dir := 1
	if i := strings.IndexByte(opens, open); i >= 0 {
		close = closes[i]
	} else if i := strings.IndexByte(closes, close); i >= 0 {
		open, dir = opens[i], -1
	} else {
		e.Msg = "not on a bracket"
		return
	}
	var skip []syntax.Span // sorted strings and comments
	if l := e.Lang(); l != nil {
		for _, s := range l.Lex(src) {
			if s.Kind == syntax.String || s.Kind == syntax.Comment {
				skip = append(skip, s)
			}
		}
	}
	in := func(off int) bool {
		i := sort.Search(len(skip), func(i int) bool { return skip[i].End > off })
		return i < len(skip) && skip[i].Start <= off
	}
	if in(at) {
		skip = nil
	}
	depth := 0
	for off := at; off >= 0 && off < len(src); off += dir {
		if c := src[off]; (c == open || c == close) && !in(off) {
			if c == open {
				depth += dir
			} else {
				depth -= dir
			}
			if depth == 0 {
				e.setHead(off)
				return
			}
		}
	}
	e.Msg = "no matching bracket"
}

// isEntry reports whether line is one ]e stops at: not blank, not only a
// comment and not only closing brackets.
func (e *Editor) isEntry(line int) bool {
	t := e.Buf.Text
	s := strings.TrimSpace(string(t.Slice(t.LineStart(line), e.Buf.LineEnd(line))))
	if strings.Trim(s, ")]},;") == "" {
		return false
	}
	for _, c := range []string{"//", "#", "/*", "*/", "* ", "--"} {
		if strings.HasPrefix(s, c) {
			return false
		}
	}
	return true
}

// jumpEntry goes to the next (dir > 0) or previous entry: the nearest line
// indented as much as the cursor's, without leaving the enclosing block. In
// pretty-printed JSON that's the next element or key; in code, the next
// statement or, at the top, the next declaration (]e, [e).
func (e *Editor) jumpEntry(dir int) {
	width := len(e.indent(e.line()))
	for l := e.line() + dir; l >= 0 && l < e.Buf.Text.LineCount(); l += dir {
		if !e.isEntry(l) {
			continue
		}
		w := len(e.indent(l))
		if w < width {
			break
		}
		if w == width {
			e.setHead(e.firstNonBlank(l))
			return
		}
	}
	e.Msg = map[bool]string{true: "no next entry", false: "no previous entry"}[dir > 0]
}

// funcLine matches a line that starts a function: after the indent and
// modifiers like pub or async, the first word is func, function, def, fn or fun.
var funcLine = regexp.MustCompile(`^[ \t]*(?:(?:export|default|pub(?:\([^)]*\))?|async|static|public|private|protected|override|unsafe|const|extern|inline|suspend)[ \t]+)*(?:func|function|def|fn|fun)\b`)

// jumpFunc goes to the start of the next (dir > 0) or previous function
// (]f, [f).
func (e *Editor) jumpFunc(dir int) {
	t := e.Buf.Text
	for l := e.line() + dir; l >= 0 && l < t.LineCount(); l += dir {
		if funcLine.Match(t.Slice(t.LineStart(l), e.Buf.LineEnd(l))) {
			e.setHead(e.firstNonBlank(l))
			return
		}
	}
	e.Msg = map[bool]string{true: "no next function", false: "no previous function"}[dir > 0]
}
