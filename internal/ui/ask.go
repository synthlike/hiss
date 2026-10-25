package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
)

// drawAsk draws an agent's question as a box in the middle of a w×h screen:
// who asks, the question, the options, a line for typing an answer, then the
// keys. The box has a fixed width and grows as long text wraps.
func drawAsk(s Screen, w, h int, q *core.Question) {
	bw := min(w-2, 72)
	type row struct {
		text string
		st   tcell.Style
	}
	var rows []row
	add := func(st tcell.Style, texts ...string) {
		for _, t := range texts {
			rows = append(rows, row{t, st})
		}
	}
	add(styleMenu.Bold(true), q.Agent+" asks")
	add(styleMenu, wrap(q.Text, bw-2)...)
	add(styleMenu, "")
	for i, o := range q.Options {
		st := styleMenu
		if i == q.Selected {
			st = styleSel
		}
		add(st, wrap(o, bw-2)...)
	}
	switch {
	case q.Selected == len(q.Options):
		add(styleSel, chunk("> "+q.Input+"▏", bw-2)...)
	case q.Input == "":
		add(styleMenu.Dim(true), "> or type an answer")
	default:
		add(styleMenuKey, chunk("> "+q.Input, bw-2)...)
	}
	keys := "↑↓ choose · enter answer · esc later"
	if len(q.Options) == 0 {
		keys = "enter answer · esc later"
	}
	add(styleMenu.Dim(true), keys)

	// Centre the box in the text area, below the corner toast (rows 0 and 1)
	// unless it only fits by covering it.
	area := h - 2
	x, y := (w-bw)/2, min(max((area-len(rows))/2, 2), max(area-len(rows), 0))
	for _, r := range rows {
		if y >= area {
			break
		}
		fill(s, x, y, bw, r.st)
		drawText(s, x+1, y, x+bw-1, r.text, 0, style(r.st))
		y++
	}
}

// wrap breaks text into lines of at most width characters, at spaces.
func wrap(text string, width int) []string {
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		line := ""
		for word := range strings.FieldsSeq(para) {
			switch {
			case line == "":
				line = word
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// chunk breaks text into lines of width characters, keeping every space, for
// text being typed.
func chunk(text string, width int) []string {
	width = max(width, 1)
	var out []string
	for utf8.RuneCountInString(text) > width {
		i := 0
		for range width {
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
		}
		out, text = append(out, text[:i]), text[i:]
	}
	return append(out, text)
}
