// Package ui draws the editor with tcell and feeds it terminal events.
package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
)

const (
	tabWidth = 4
	gutter   = 2 // columns before the text: a hunk mark and a space
)

// Screen is the part of tcell.Screen that drawing needs; tests fake it.
type Screen interface {
	Put(x, y int, str string, style tcell.Style) (string, int)
	Size() (width, height int)
}

var (
	styleText      = tcell.StyleDefault
	styleCursor    = tcell.StyleDefault.Reverse(true)
	styleStatus    = tcell.StyleDefault.Reverse(true)
	styleDim       = tcell.StyleDefault.Dim(true)
	styleSel       = tcell.StyleDefault.Background(tcell.ColorNavy)
	styleMenu      = tcell.StyleDefault.Background(tcell.ColorDarkSlateGray)
	styleMenuKey   = styleMenu.Bold(true).Foreground(tcell.ColorYellow)
	styleRemoved   = tcell.StyleDefault.Foreground(tcell.ColorRed)
	styleAdded     = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	styleYourNote  = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	styleAgentNote = tcell.StyleDefault.Foreground(tcell.ColorTeal)
)

// Run draws and handles events until the editor quits. Work sent on reqs runs on
// this goroutine, which owns the editor.
func Run(ed *core.Editor, scr tcell.Screen, reqs <-chan func(*core.Editor)) {
	tick := time.NewTicker(time.Second) // redraw time-based state such as agent status
	defer tick.Stop()
	scr.EnableFocus()
	focused := true
	for !ed.Quit {
		for _, n := range ed.TakeNotices() {
			if !focused { // the toast is enough when the user is looking
				scr.ShowNotification("hiss: "+n.Title, n.Text)
			}
		}
		scr.Clear()
		Draw(scr, ed)
		scr.Show()
		select {
		case <-tick.C:
		case ev := <-scr.EventQ():
			switch ev := ev.(type) {
			case *tcell.EventFocus:
				focused = ev.Focused
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
	hunks := ed.Hunks()
	cursor := ed.Sel.Head
	from, to := ed.SelRange()
	row, line := 0, ed.Top
	for row < h-2 {
		if line >= t.LineCount() {
			s.Put(0, row, "~", styleDim)
			row, line = row+1, line+1
			continue
		}
		base, mark := styleText, " "
		var hunk *core.Hunk
		for i := range hunks {
			if line >= hunks[i].First && line <= hunks[i].Last {
				hunk = &hunks[i]
			}
		}
		if hunk != nil && hunk.Removes {
			base, mark = styleRemoved, "-"
		}
		s.Put(0, row, mark, base)
		start, end := t.LineStart(line), ed.Buf.LineEnd(line)
		x := drawText(s, gutter, row, w, string(t.Slice(start, end)), start, func(off int) tcell.Style {
			switch {
			case off == cursor:
				return styleCursor
			case off >= from && off < to:
				return styleSel
			}
			return base
		})
		if end == cursor && x < w { // cursor on the newline or end of file
			s.Put(x, row, " ", styleCursor)
		}
		row++
		if hunk != nil && line == hunk.Last {
			for _, a := range hunk.Added {
				if row >= h-2 {
					break
				}
				fill(s, 0, row, w, styleAdded)
				s.Put(0, row, "+", styleAdded)
				drawText(s, gutter, row, w, a, 0, func(int) tcell.Style { return styleAdded })
				row++
			}
			for _, n := range hunk.Notes {
				if row >= h-2 {
					break
				}
				style := styleAgentNote
				if n.Mine {
					style = styleYourNote
				}
				s.Put(0, row, "┆", styleDim)
				putString(s, gutter, row, n.Text, style)
				row++
			}
		}
		line++
	}
	if ed.Splash != "" && ed.Buf.Path == "" && t.Len() == 0 {
		drawSplash(s, w, h-2, ed.Splash)
	}

	dirty := ""
	if ed.Buf.Dirty() {
		dirty = " [+]"
	}
	left := fmt.Sprintf(" %s  %s%s", ed.Mode, ed.Buf.Name, dirty)
	line, col := ed.Cursor()
	right := fmt.Sprintf("%d:%d ", line+1, col+1)
	if n := ed.PendingCount(); n > 0 {
		right = fmt.Sprintf("%d pending  ", n) + right
	}
	if a := agentStatus(ed); a != "" {
		right = a + "  " + right
	}
	fill(s, 0, h-2, w, styleStatus)
	putString(s, 0, h-2, left, styleStatus)
	putString(s, w-len(right), h-2, right, styleStatus)

	switch {
	case ed.Mode == core.Command:
		putString(s, 0, h-1, ":"+ed.Cmd, styleText)
	case ed.Msg != "":
		putString(s, 0, h-1, ed.Msg, styleText)
	default:
		if ss := ed.Sessions(); len(ss) > 0 && ss[0].State == core.Waiting && ss[0].Note != "" {
			putString(s, 0, h-1, "claude: "+ss[0].Note, styleDim)
		}
		if hk := ed.HunkAtCursor(); hk != nil {
			fill(s, 0, h-1, w, styleText)
			info := fmt.Sprintf("#%d %s", hk.P.ID, hk.P.Agent)
			if hk.P.Rationale != "" {
				info += ": " + hk.P.Rationale
			}
			putString(s, 0, h-1, info+"  (space a accept, space r reject, space c comment)", styleDim)
		}
	}
	if t := ed.ActiveToast(); t != nil {
		drawToast(s, w, t)
	}
	if ed.Menu != nil {
		drawMenu(s, w, h-2, ed.Menu)
	}
}

// agentStatus describes the most recently active agent session.
func agentStatus(ed *core.Editor) string {
	ss := ed.Sessions()
	if len(ss) == 0 {
		return ""
	}
	s := ss[0]
	state := string(s.State)
	if s.Awaiting(ed.Now()) {
		state = "awaiting your review"
	}
	out := fmt.Sprintf("claude %s · turn %d", state, s.Turn)
	if len(ss) > 1 {
		out += fmt.Sprintf(" (+%d)", len(ss)-1)
	}
	return out
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

// drawText draws str from column x until width, styling each character by its
// byte offset (str starts at off). It returns the column after the text.
func drawText(s Screen, x, row, width int, str string, off int, styleAt func(int) tcell.Style) int {
	col := 0 // column within the text, for tab stops
	for str != "" && x < width {
		style := styleAt(off)
		if str[0] == '\t' {
			n := tabWidth - col%tabWidth
			fill(s, x, row, min(n, width-x), style)
			str, off, x, col = str[1:], off+1, x+n, col+n
			continue
		}
		rest, cw := s.Put(x, row, str, style)
		if len(rest) == len(str) { // nothing consumed; skip a byte to make progress
			rest = str[1:]
		}
		off += len(str) - len(rest)
		str, x, col = rest, x+cw, col+cw
	}
	return x
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
