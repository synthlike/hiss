package core

import (
	"strings"
	"unicode/utf8"
)

// More of Helix's normal mode: I, gs, %, f t F T, r, > <, J. Each works on
// the one selection hiss has.

// keyChar returns the character a key types, for keys that take one.
func keyChar(k string) (string, bool) {
	switch k {
	case "space":
		return " ", true
	case "tab":
		return "\t", true
	}
	return k, utf8.RuneCountInString(k) == 1
}

// awaitChar shows title until the next key and passes it to f if it types a
// character; esc or any other key cancels.
func (e *Editor) awaitChar(title string, f func(e *Editor, c string)) {
	e.Menu = &Menu{Title: title, Else: func(e *Editor, k string) {
		if c, ok := keyChar(k); ok {
			f(e, c)
		}
	}}
}

// firstNonBlank returns the offset of line's first character after its indent.
func (e *Editor) firstNonBlank(line int) int {
	return e.Buf.Text.LineStart(line) + len(e.indent(line))
}

// selectAll selects the whole text (%).
func (e *Editor) selectAll() {
	e.Sel = Selection{0, e.prevRune(e.Buf.Text.Len())}
	e.goal = -1
}

// findChar selects from the cursor to the next c on its line (f), or to just
// before it (t); back looks behind the cursor instead (F, T). Nothing moves
// if c isn't there.
func (e *Editor) findChar(c string, till, back bool) {
	t := e.Buf.Text
	start, end := t.LineStart(e.line()), e.Buf.LineEnd(e.line())
	line := string(t.Slice(start, end))
	head := e.Sel.Head - start
	if back {
		i := strings.LastIndex(line[:head], c)
		if i < 0 {
			return
		}
		if till {
			i += len(c)
		}
		e.selectSpan(e.Sel.Head, start+i)
		return
	}
	from := min(e.nextRune(e.Sel.Head), end) - start
	i := strings.Index(line[from:], c)
	if i < 0 {
		return
	}
	to := start + from + i
	if till {
		to = max(e.prevRune(to), e.Sel.Head)
	}
	e.selectSpan(e.Sel.Head, to)
}

// replaceChars replaces each character of the selection with c, keeping
// line breaks (r).
func (e *Editor) replaceChars(c string) {
	from, to := e.selRange()
	old := string(e.Buf.Text.Slice(from, to))
	var b strings.Builder
	for _, r := range old {
		if r == '\n' {
			b.WriteByte('\n')
		} else {
			b.WriteString(c)
		}
	}
	if b.String() == old {
		return
	}
	e.checkpoint()
	backward := e.Sel.Head < e.Sel.Anchor
	e.Buf.Replace(from, to, []byte(b.String()), You)
	e.Sel = Selection{from, e.prevRune(from + b.Len())}
	if backward {
		e.Sel.Anchor, e.Sel.Head = e.Sel.Head, e.Sel.Anchor
	}
}

// selLines returns the first and last lines the selection touches.
func (e *Editor) selLines() (int, int) {
	from, to := e.selRange()
	t := e.Buf.Text
	return t.LineOf(from), t.LineOf(max(to-1, from))
}

// lineEdits makes edits to whole lines as one undo step, carrying the
// selection along.
type lineEdits struct {
	e    *Editor
	done bool
}

func (l *lineEdits) replace(start, end int, s string) {
	e := l.e
	if !l.done {
		e.checkpoint()
		l.done = true
	}
	ed := e.Buf.Replace(start, end, []byte(s), You)
	e.Sel = Selection{ed.Map(e.Sel.Anchor, true), ed.Map(e.Sel.Head, true)}
	e.goal = -1
}

// indentLines adds a level of indent to each selected line that isn't empty
// (>), or removes up to one (<).
func (e *Editor) indentLines(add bool) {
	first, last := e.selLines()
	unit := e.indentUnit()
	width := len(unit) // spaces that make up a level when unindenting
	if unit == "\t" {
		width = 4
	}
	l := lineEdits{e: e}
	for line := first; line <= last; line++ {
		start, end := e.Buf.Text.LineStart(line), e.Buf.LineEnd(line)
		ind := e.indent(line)
		switch {
		case add && end > start:
			l.replace(start, start, unit)
		case !add && strings.HasPrefix(ind, "\t"):
			l.replace(start, start+1, "")
		case !add:
			n := len(ind) - len(strings.TrimLeft(ind, " "))
			l.replace(start, start+min(n, width), "")
		}
	}
}

// indentUnit is one level of indent in the buffer: a tab, or the spaces that
// start the first line indented with spaces. Unindented text gets tabs.
func (e *Editor) indentUnit() string {
	for line := range e.Buf.Text.LineCount() {
		switch ind := e.indent(line); {
		case ind == "":
		case ind[0] == '\t':
			return "\t"
		default:
			return ind[:len(ind)-len(strings.TrimLeft(ind, " "))]
		}
	}
	return "\t"
}

// joinLines joins the selected lines, or the cursor's line and the next,
// putting a space in place of each line break with the spaces around it (J).
// No space goes next to an empty line.
func (e *Editor) joinLines() {
	t := e.Buf.Text
	first, last := e.selLines()
	if last == first {
		last++
	}
	if end := t.LineCount() - 1; last == end && t.LineStart(end) == t.Len() {
		last-- // the empty line after a final newline isn't one to join
	}
	l := lineEdits{e: e}
	for range last - first {
		t := e.Buf.Text
		start, nl := t.LineStart(first), e.Buf.LineEnd(first)
		from := start + len(strings.TrimRight(string(t.Slice(start, nl)), " \t"))
		to := e.firstNonBlank(first + 1)
		sep := " "
		if from == start || to == e.Buf.LineEnd(first+1) {
			sep = ""
		}
		l.replace(from, to, sep)
	}
}
