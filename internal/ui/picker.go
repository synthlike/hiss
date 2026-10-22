package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
)

// drawPicker draws an open picker as a box near the top of a w×h screen: a
// title with the match count, the query, then matches with the selection
// highlighted.
func drawPicker(s Screen, w, h int, p *core.Picker) {
	ms := p.Matches()
	bw := min(w-2, 90)
	x, top := (w-bw)/2, 1
	limit := x + bw - 1
	rows := max(min(len(ms), h-5, 15), 1)

	fill(s, x, top, bw, styleMenu)
	drawText(s, x+1, top, limit, fmt.Sprintf("%s  %d/%d", p.Title, len(ms), len(p.Items)), 0, style(styleMenu.Bold(true)))
	fill(s, x, top+1, bw, styleMenu)
	drawText(s, x+1, top+1, limit, "> "+p.Query+"▏", 0, style(styleMenuKey))
	if len(ms) == 0 {
		fill(s, x, top+2, bw, styleMenu)
		drawText(s, x+1, top+2, limit, "no matches", 0, style(styleMenu.Dim(true)))
		return
	}
	first := max(p.Selected-rows+1, 0)
	for i := 0; i < rows && first+i < len(ms); i++ {
		it, y := ms[first+i], top+2+i
		st := styleMenu
		if first+i == p.Selected {
			st = styleSel
		}
		fill(s, x, y, bw, st)
		col := drawText(s, x+1, y, limit, it.Label, 0, style(st))
		drawText(s, col+2, y, limit, it.Detail, 0, style(st.Dim(true)))
	}
}

// style returns a styleAt function for drawText that always gives st.
func style(st tcell.Style) func(int) tcell.Style { return func(int) tcell.Style { return st } }
