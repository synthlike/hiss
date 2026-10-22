// Package ui draws the editor with tcell and feeds it terminal events.
package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
)

const tabWidth = 4

// Screen is the part of tcell.Screen that drawing needs; tests fake it.
type Screen interface {
	Put(x, y int, str string, style tcell.Style) (string, int)
	Size() (width, height int)
}

var (
	styleText    = tcell.StyleDefault
	styleCursor  = tcell.StyleDefault.Reverse(true)
	styleStatus  = tcell.StyleDefault.Reverse(true)
	styleDim     = tcell.StyleDefault.Dim(true)
	styleSel     = tcell.StyleDefault.Background(tcell.ColorNavy)
	styleMenu    = tcell.StyleDefault.Background(tcell.ColorDarkSlateGray)
	styleMenuKey = styleMenu.Bold(true).Foreground(tcell.ColorYellow)
)

// Run draws and handles events until the editor quits. Work sent on reqs runs on
// this goroutine, which owns the editor.
func Run(ed *core.Editor, scr tcell.Screen, reqs <-chan func(*core.Editor)) {
	for !ed.Quit {
		scr.Clear()
		Draw(scr, ed)
		scr.Show()
		select {
		case ev := <-scr.EventQ():
			switch ev := ev.(type) {
			case *tcell.EventResize:
				ed.Resize(ev.Size())
				scr.Sync()
			case *tcell.EventKey:
				if k := KeyName(ev); k != "" {
					ed.HandleKey(k)
				}
			}
		case f := <-reqs:
			f(ed)
		}
	}
}

// KeyName converts a key event to the editor's key names: "x", "C-x", "A-x",
// "esc", "enter", "backspace", "tab", "left", ...
func KeyName(ev *tcell.EventKey) string {
	mods := ""
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		mods += "C-"
	}
	if ev.Modifiers()&tcell.ModAlt != 0 {
		mods += "A-"
	}
	k := ev.Key()
	switch {
	case k == tcell.KeyRune && ev.Str() == " ":
		return mods + "space"
	case k == tcell.KeyRune:
		return mods + ev.Str()
	case k >= tcell.KeyCtrlA && k <= tcell.KeyCtrlZ:
		return "C-" + string(rune('a'+k-tcell.KeyCtrlA))
	}
	if name, ok := keyNames[k]; ok {
		return mods + name
	}
	return ""
}

var keyNames = map[tcell.Key]string{
	tcell.KeyEsc: "esc", tcell.KeyEnter: "enter", tcell.KeyTab: "tab",
	tcell.KeyBacktab: "backtab", tcell.KeyBackspace: "backspace",
	tcell.KeyBackspace2: "backspace", tcell.KeyDelete: "del",
	tcell.KeyLeft: "left", tcell.KeyRight: "right", tcell.KeyUp: "up", tcell.KeyDown: "down",
	tcell.KeyHome: "home", tcell.KeyEnd: "end", tcell.KeyPgUp: "pgup", tcell.KeyPgDn: "pgdn",
}

// Draw paints the editor onto s, which the caller has cleared.
func Draw(s Screen, ed *core.Editor) {
	w, h := s.Size()
	t := ed.Buf.Text
	for row := 0; row < h-2; row++ {
		line := ed.Top + row
		if line >= t.LineCount() {
			s.Put(0, row, "~", styleDim)
			continue
		}
		start, end := t.LineStart(line), ed.Buf.LineEnd(line)
		drawLine(s, row, w, string(t.Slice(start, end)), start, ed)
	}

	dirty := ""
	if ed.Buf.Dirty() {
		dirty = " [+]"
	}
	left := fmt.Sprintf(" %s  %s%s", ed.Mode, ed.Buf.Name, dirty)
	line, col := ed.Cursor()
	right := fmt.Sprintf("%d:%d ", line+1, col+1)
	fill(s, 0, h-2, w, styleStatus)
	putString(s, 0, h-2, left, styleStatus)
	putString(s, w-len(right), h-2, right, styleStatus)

	if ed.Mode == core.Command {
		putString(s, 0, h-1, ":"+ed.Cmd, styleText)
	} else {
		putString(s, 0, h-1, ed.Msg, styleText)
	}
	if ed.Menu != nil {
		drawMenu(s, w, h-2, ed.Menu)
	}
}

// drawMenu draws a menu's key hints in a box ending just above row bottom.
func drawMenu(s Screen, w, bottom int, m *core.Menu) {
	width := len(m.Title) + 2
	for _, it := range m.Items {
		width = max(width, len(it.Key)+len(it.Desc)+4)
	}
	x, y := max(w-width-1, 0), bottom-len(m.Items)-1
	fill(s, x, y, width, styleMenu)
	putString(s, x+1, y, m.Title, styleMenu.Bold(true))
	for i, it := range m.Items {
		fill(s, x, y+1+i, width, styleMenu)
		putString(s, x+1, y+1+i, it.Key, styleMenuKey)
		putString(s, x+3+len(it.Key), y+1+i, it.Desc, styleMenu)
	}
}

// drawLine draws one line of text that starts at byte offset off.
func drawLine(s Screen, row, width int, str string, off int, ed *core.Editor) {
	cursor := ed.Sel.Head
	from, to := ed.SelRange()
	x := 0
	for str != "" && x < width {
		style := styleText
		if off == cursor {
			style = styleCursor
		} else if off >= from && off < to {
			style = styleSel
		}
		if str[0] == '\t' {
			n := tabWidth - x%tabWidth
			fill(s, x, row, n, style)
			str, off, x = str[1:], off+1, x+n
			continue
		}
		rest, cw := s.Put(x, row, str, style)
		if len(rest) == len(str) { // nothing consumed; skip a byte to make progress
			rest = str[1:]
		}
		off += len(str) - len(rest)
		str, x = rest, x+cw
	}
	if off == cursor && x < width { // cursor on the newline or end of file
		s.Put(x, row, " ", styleCursor)
	}
}

func fill(s Screen, x, y, n int, style tcell.Style) {
	for i := range n {
		s.Put(x+i, y, " ", style)
	}
}

func putString(s Screen, x, y int, str string, style tcell.Style) {
	for str != "" {
		rest, w := s.Put(x, y, str, style)
		if len(rest) == len(str) {
			rest = str[1:]
		}
		str, x = rest, x+w
	}
}
