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

// moveLines moves n rows down (or up when n < 0), stepping through the diff
// lines drawn around pending hunks as well as buffer lines.
func (e *Editor) moveLines(n int) {
	if e.goal < 0 {
		e.goal = e.column(e.Sel.Head)
	}
	goal, line, row := e.goal, e.line(), e.DiffRow()
	last := e.Buf.Text.LineCount() - 1
	for ; n > 0; n-- {
		if row < e.diffBelow(line) {
			row++
		} else if line < last {
			line = line + 1
			row = -e.diffAbove(line)
		}
	}
	for ; n < 0; n++ {
		if row > -e.diffAbove(line) {
			row--
		} else if line > 0 {
			line--
			row = e.diffBelow(line)
		}
	}
	e.setHead(e.offsetAt(line, goal))
	e.goal, e.row, e.rowSet = goal, row, true
}

// DiffRow returns n>0 when the cursor is on the n-th proposed line drawn below
// its hunk, -n when it is n rows above a tweaked hunk's first line on one of
// its old lines, or 0 when it is on the buffer line.
func (e *Editor) DiffRow() int {
	line := e.line()
	return min(max(e.row, -e.diffAbove(line)), e.diffBelow(line))
}

// diffBelow returns how many proposed lines are drawn below line.
func (e *Editor) diffBelow(line int) int {
	for _, h := range e.Hunks() {
		if h.Last == line {
			return h.Below()
		}
	}
	return 0
}

// diffAbove returns how many old lines of a tweaked hunk are drawn above line.
func (e *Editor) diffAbove(line int) int {
	for _, h := range e.Hunks() {
		if h.First == line {
			return len(h.Removed)
		}
	}
	return 0
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

// lineRows returns the rows drawn above each line (hunk rationales and a
// tweaked hunk's old lines) and a func counting every row drawn for a line,
// including its wrapped rows and the pending hunks and trail notes below it.
func (e *Editor) lineRows() (above map[int]int, block func(int) int) {
	above = map[int]int{}
	extra := map[int]int{}
	for _, h := range e.Hunks() {
		above[h.First] += len(h.Above) + e.wrapRows(h.Removed)
		extra[h.Last] += e.wrapRows(h.Added) + len(h.Notes)
	}
	for l, rows := range e.TrailRows() {
		extra[l] += len(rows)
	}
	return above, func(l int) int { return above[l] + len(e.lineWrap(l)) + extra[l] }
}

// cursorSub returns the cursor's row within its line's block, given the rows
// drawn above the line: on the line, the wrapped row holding the cursor; on
// a proposed or old line, that line's first row.
func (e *Editor) cursorSub(above int) int {
	line, d := e.line(), e.DiffRow()
	if d == 0 {
		return above + rowOf(e.lineWrap(line), e.Sel.Head-e.Buf.Text.LineStart(line))
	}
	for _, h := range e.Hunks() {
		switch {
		case d > 0 && h.Last == line:
			return above + len(e.lineWrap(line)) + e.wrapRows(h.Added[:d-1])
		case d < 0 && h.First == line:
			return above - e.wrapRows(h.Removed[len(h.Removed)+d:])
		}
	}
	return above
}

// scroll keeps the cursor line inside the visible rows.
func (e *Editor) scroll() {
	if e.screen > 0 {
		e.Height = max(e.screen-2-e.PanelRows(), 1) // the status and message lines, and the output panel
	}
	if e.Height <= 0 {
		return
	}
	above, block := e.lineRows()
	line := e.line()
	sub := e.cursorSub(above[line]) // the cursor's row within its line's block
	e.TopSub = min(e.TopSub, block(e.Top)-1)
	if line < e.Top || line == e.Top && sub <= e.TopSub {
		e.Top, e.TopSub = line, sub
		if sub == above[line] {
			e.TopSub = 0 // on the line itself: show what is drawn above it too
		}
	}
	if line >= e.Top+e.Height {
		e.Top, e.TopSub = line-e.Height+1, 0
	}
	rows := sub + 1 - e.TopSub // rows from the top of the screen to the cursor
	for l := e.Top; l < line; l++ {
		rows += block(l)
	}
	for ; rows > e.Height; rows-- { // scroll one row at a time
		if e.TopSub < block(e.Top)-1 {
			e.TopSub++
		} else {
			e.Top, e.TopSub = e.Top+1, 0
		}
	}
}
