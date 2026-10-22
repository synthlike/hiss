package ui

import (
	"unicode/utf8"

	"github.com/synthlike/hiss/internal/core"
)

// drawToast draws a notice in the top-right corner of a screen w columns wide,
// in the same box style as menus: a bold title over the text.
func drawToast(s Screen, w int, t *core.Toast) {
	width := min(max(utf8.RuneCountInString(t.Title), utf8.RuneCountInString(t.Text))+2, w)
	x := w - width
	fill(s, x, 0, width, styleMenu)
	putString(s, x+1, 0, t.Title, styleMenu.Bold(true))
	fill(s, x, 1, width, styleMenu)
	putString(s, x+1, 1, t.Text, styleMenu)
}
