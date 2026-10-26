package core

import "unicode/utf8"

// Long lines wrap at the screen's edge, after a space when one fits. The
// gutter before the text holds a mark column, the line number and a space.

// TabWidth is the distance between tab stops.
const TabWidth = 4

// Gutter returns the columns before the text.
func (e *Editor) Gutter() int {
	digits := 3
	for n := 1000; n <= e.Buf.Text.LineCount(); n *= 10 {
		digits++
	}
	return 1 + digits + 1
}

// TextWidth returns the columns left for text after the gutter.
func (e *Editor) TextWidth() int { return max(e.Width-e.Gutter(), 1) }

// Wrap returns the byte offsets where the rows of s start on screen; the
// first is 0. A row breaks after its last space or tab (not counting its
// indentation), or mid-word when it has none. Runes are one column wide and
// tabs reach the next tab stop, counted from the row's start. Without a known
// width, s takes one row.
func (e *Editor) Wrap(s string) []int {
	if e.Width <= 0 {
		return []int{0}
	}
	return wrapAt(s, e.TextWidth())
}

// wrapAt is Wrap for rows of w columns.
func wrapAt(s string, w int) []int {
	rows := []int{0}
	start, col, brk := 0, 0, 0 // brk is just after the row's last blank past its indentation
	word := false              // whether the row has a non-blank yet
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		cw := 1
		if r == '\t' {
			cw = TabWidth - col%TabWidth
		}
		if col+cw > w && i > start {
			if brk <= start {
				brk = i
			}
			rows = append(rows, brk)
			start, col, i, word = brk, 0, brk, false // count the rest of the row again from its new start
			continue
		}
		col, i = col+cw, i+n
		switch {
		case r != ' ' && r != '\t':
			word = true
		case word:
			brk = i
		}
	}
	return rows
}

// wrapRows returns how many rows lines take on screen.
func (e *Editor) wrapRows(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(e.Wrap(l))
	}
	return n
}

// lineWrap returns where the rows of buffer line l start, as offsets in the line.
func (e *Editor) lineWrap(l int) []int {
	t := e.Buf.Text
	return e.Wrap(string(t.Slice(t.LineStart(l), e.Buf.LineEnd(l))))
}

// rowOf returns the row of rows (from Wrap) that holds offset off.
func rowOf(rows []int, off int) int {
	r := 0
	for r+1 < len(rows) && rows[r+1] <= off {
		r++
	}
	return r
}
