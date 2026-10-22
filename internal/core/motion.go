package core

import (
	"unicode"
	"unicode/utf8"
)

// Cursor returns the 0-based line and rune column of the cursor.
func (e *Editor) Cursor() (line, col int) { return e.PositionOf(e.Sel.Head) }

// PositionOf returns the 0-based line and rune column of a byte offset.
func (e *Editor) PositionOf(off int) (line, col int) {
	return e.Buf.Text.LineOf(off), e.column(off)
}

// SelRange returns the selected byte range [from, to) as drawn.
func (e *Editor) SelRange() (from, to int) { return e.selRange() }

func (e *Editor) selRange() (int, int) {
	lo, hi := min(e.Sel.Anchor, e.Sel.Head), max(e.Sel.Anchor, e.Sel.Head)
	if e.Mode == Insert {
		return lo, hi
	}
	return lo, e.nextRune(hi)
}

// line returns the line of the cursor.
func (e *Editor) line() int { return e.Buf.Text.LineOf(e.Sel.Head) }

// setHead moves the cursor, keeping the anchor in Select mode.
func (e *Editor) setHead(off int) {
	e.Sel.Head = off
	if e.Mode != Select {
		e.Sel.Anchor = off
	}
	e.goal = -1
}

func (e *Editor) left()  { e.setHead(e.prevRune(e.Sel.Head)) }
func (e *Editor) right() { e.setHead(e.nextRune(e.Sel.Head)) }

func (e *Editor) moveLines(n int) {
	if e.goal < 0 {
		e.goal = e.column(e.Sel.Head)
	}
	goal := e.goal
	target := min(max(e.line()+n, 0), e.Buf.Text.LineCount()-1)
	e.setHead(e.offsetAt(target, goal))
	e.goal = goal
}

// selectLine selects the cursor's line, or extends a line selection by a line.
func (e *Editor) selectLine() {
	t := e.Buf.Text
	from, _ := e.selRange()
	start := t.LineStart(t.LineOf(from))
	end := e.Buf.LineEnd(e.line())
	if e.Sel.Anchor == start && e.Sel.Head == end && e.line()+1 < t.LineCount() {
		end = e.Buf.LineEnd(e.line() + 1)
	}
	e.Sel = Selection{start, end}
}

type class int

const (
	space class = iota
	newline
	word
	punct
)

func (e *Editor) classAt(off int) class {
	if off >= e.Buf.Text.Len() {
		return newline
	}
	r, _ := utf8.DecodeRune(e.Buf.Text.Slice(off, min(off+utf8.UTFMax, e.Buf.Text.Len())))
	switch {
	case r == '\n':
		return newline
	case unicode.IsSpace(r):
		return space
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return word
	}
	return punct
}

// wordNext selects from the cursor to the start of the next word (Helix "w").
func (e *Editor) wordNext() {
	n := e.Buf.Text.Len()
	a := e.Sel.Head
	if next := e.nextRune(a); next < n && e.classAt(next) != e.classAt(a) {
		a = next
	}
	end := a
	if c := e.classAt(end); c != space {
		for end < n && e.classAt(end) == c {
			end = e.nextRune(end)
		}
	}
	for end < n && e.classAt(end) == space {
		end = e.nextRune(end)
	}
	e.selectSpan(a, max(e.prevRune(end), a))
}

// wordEnd selects from the cursor to the end of the word (Helix "e").
func (e *Editor) wordEnd() {
	n := e.Buf.Text.Len()
	a := e.Sel.Head
	if next := e.nextRune(a); next < n && e.classAt(next) != e.classAt(a) {
		a = next
	}
	end := a
	for end < n && e.classAt(end) == space {
		end = e.nextRune(end)
	}
	if c := e.classAt(end); end < n {
		for end < n && e.classAt(end) == c {
			end = e.nextRune(end)
		}
	}
	e.selectSpan(a, max(e.prevRune(end), a))
}

// wordPrev selects back to the start of the word (Helix "b").
func (e *Editor) wordPrev() {
	a := e.Sel.Head
	if a > 0 && e.classAt(e.prevRune(a)) != e.classAt(a) {
		a = e.prevRune(a)
	}
	start := a
	for start > 0 && e.classAt(start) == space {
		start = e.prevRune(start)
	}
	c := e.classAt(start)
	for start > 0 && e.classAt(e.prevRune(start)) == c {
		start = e.prevRune(start)
	}
	e.selectSpan(a, start)
}

// selectSpan selects anchor..head, or only moves the head in Select mode.
func (e *Editor) selectSpan(anchor, head int) {
	if e.Mode != Select {
		e.Sel.Anchor = anchor
	}
	e.Sel.Head = head
	e.goal = -1
}

// column returns the rune column of off within its line.
func (e *Editor) column(off int) int {
	start := e.Buf.Text.LineStart(e.Buf.Text.LineOf(off))
	return utf8.RuneCount(e.Buf.Text.Slice(start, off))
}

// offsetAt returns the offset of rune column col on line, clamped to the line end.
func (e *Editor) offsetAt(line, col int) int {
	start, end := e.Buf.Text.LineStart(line), e.Buf.LineEnd(line)
	b := e.Buf.Text.Slice(start, end)
	off := 0
	for i := 0; i < col && off < len(b); i++ {
		_, n := utf8.DecodeRune(b[off:])
		off += n
	}
	return start + off
}

func (e *Editor) nextRune(off int) int {
	t := e.Buf.Text
	if off >= t.Len() {
		return t.Len()
	}
	_, n := utf8.DecodeRune(t.Slice(off, min(off+utf8.UTFMax, t.Len())))
	return off + n
}

func (e *Editor) prevRune(off int) int {
	if off <= 0 {
		return 0
	}
	_, n := utf8.DecodeLastRune(e.Buf.Text.Slice(max(off-utf8.UTFMax, 0), off))
	return off - n
}

// scroll keeps the cursor line inside the visible rows.
func (e *Editor) scroll() {
	if e.Height <= 0 {
		return
	}
	line := e.line()
	if line < e.Top {
		e.Top = line
		return
	}
	if line >= e.Top+e.Height {
		e.Top = line - e.Height + 1
	}
	added := map[int]int{} // rows drawn after a line for pending hunks
	for _, h := range e.Hunks() {
		added[h.Last] += len(h.Added)
	}
	for e.Top < line {
		rows := line - e.Top + 1
		for l := e.Top; l < line; l++ {
			rows += added[l]
		}
		if rows <= e.Height {
			break
		}
		e.Top++
	}
}
