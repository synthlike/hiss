// Package ui draws the editor with tcell and feeds it terminal events.
package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/lsp"
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
	styleSel       = tcell.StyleDefault.Background(color.Navy)
	styleMatch     = tcell.StyleDefault.Background(color.Olive).Foreground(color.Black)
	styleRelated   = tcell.StyleDefault.Background(tcell.NewRGBColor(0x44, 0x44, 0x44)) // only its background is used
	styleMenu      = tcell.StyleDefault.Background(color.DarkSlateGray)
	styleMenuKey   = styleMenu.Bold(true).Foreground(color.Yellow)
	styleRemoved   = tcell.StyleDefault.Foreground(color.Red)
	styleAdded     = tcell.StyleDefault.Foreground(color.Green)
	styleYourNote  = tcell.StyleDefault.Foreground(color.Yellow)
	styleAgentNote = tcell.StyleDefault.Foreground(color.Teal)
	styleError     = tcell.StyleDefault.Foreground(color.Red)
	styleWarning   = tcell.StyleDefault.Foreground(color.Yellow)
	styleInfo      = tcell.StyleDefault.Foreground(color.Teal)
)

func diagStyle(severity int) tcell.Style {
	switch severity {
	case lsp.Error:
		return styleError
	case lsp.Warning:
		return styleWarning
	}
	return styleInfo
}

// underline marks text in a diagnostic: curly for errors and warnings,
// dotted for the rest.
func underline(st tcell.Style, severity int) tcell.Style {
	u := tcell.UnderlineStyleCurly
	if severity > lsp.Warning {
		u = tcell.UnderlineStyleDotted
	}
	return st.Underline(u, diagStyle(severity).GetForeground())
}

// Run draws and handles events until the editor quits. Work sent on reqs runs on
// this goroutine, which owns the editor.
func Run(ed *core.Editor, scr tcell.Screen, reqs <-chan func(*core.Editor)) {
	tick := time.NewTicker(time.Second) // redraw time-based state such as agent status
	defer tick.Stop()
	scr.EnableFocus()
	for !ed.Quit {
		if notices := ed.TakeNotices(); len(notices) > 0 && ed.Unfocused { // the toast is enough when the user is looking
			for _, n := range notices {
				scr.ShowNotification("hiss: "+n.Title, n.Text)
			}
			// The bell is what terminals make noticeable: Ghostty bounces the
			// dock icon and marks the tab with 🔔 until it is focused again.
			scr.Beep()
		}
		ed.SyncLSP()
		ed.UpdateRelated()
		scr.Clear()
		Draw(scr, ed)
		scr.Show()
		select {
		case <-tick.C:
		case ev := <-scr.EventQ():
			switch ev := ev.(type) {
			case *tcell.EventFocus:
				ed.SetFocused(ev.Focused)
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
	trailRows := ed.TrailRows()
	syn := highlight(ed.Buf.Path, t, ed.Top, h)
	cursor := ed.Sel.Head
	curLine, _ := ed.Cursor()
	diffRow := ed.DiffRow()
	if diffRow != 0 {
		cursor = -1 // the cursor is on a diff line drawn below or above the buffer line
	}
	from, to := ed.SelRange()
	matches := ed.Matches(t.LineStart(ed.Top), ed.Buf.LineEnd(min(ed.Top+h, t.LineCount()-1)))
	inMatch := func(off int) bool { return inSpans(matches, off) }
	related := ed.Related()
	diags := ed.Buf.Diags()
	lineSeverity := map[int]int{} // the most severe diagnostic starting on each line
	for _, d := range diags {
		start, _ := d.Range()
		if l := t.LineOf(start); lineSeverity[l] == 0 || d.Severity < lineSeverity[l] {
			lineSeverity[l] = d.Severity
		}
	}
	// diagAt returns the most severe diagnostic over off, or 0.
	diagAt := func(off int) int {
		sev := 0
		for _, d := range diags {
			start, end := d.Range()
			if start > off {
				break
			}
			if off < max(end, start+1) && (sev == 0 || d.Severity < sev) {
				sev = d.Severity
			}
		}
		return sev
	}
	g := ed.Gutter() // a mark column, the line number and a space
	panel := ed.PanelRows()
	textH := h - 2 - panel // rows for text, above the panel and the status and message lines
	row, line := 0, ed.Top
	for row < textH {
		if line >= t.LineCount() {
			s.Put(0, row, "~", styleDim)
			row, line = row+1, line+1
			continue
		}
		skip := 0 // rows of this line's block scrolled past: notes above it, the line, then its diff
		if line == ed.Top {
			skip = ed.TopSub
		}
		k := 0 // rows of this line's block so far
		// visible reports whether to draw the block's next row.
		visible := func() bool {
			k++
			return k > skip && row < textH
		}
		for _, hk := range hunks {
			if hk.First == line {
				for _, n := range hk.Above {
					if visible() {
						drawHunkNote(s, row, g, n)
						row++
					}
				}
				for i, a := range hk.Removed { // a tweaked hunk's old lines
					onIt := line == curLine && i-len(hk.Removed) == diffRow
					for j, segs := 0, ed.Wrap(a); j < len(segs); j++ {
						if visible() {
							drawDiffLine(s, row, w, g, "-", a, segs, j, styleRemoved, onIt, nil, nil)
							row++
						}
					}
				}
			}
		}
		base, mark := styleText, " "
		var hunk *core.Hunk
		for i := range hunks {
			if line >= hunks[i].First && line <= hunks[i].Last {
				hunk = &hunks[i]
			}
		}
		switch {
		case hunk != nil && hunk.Removes:
			base, mark = styleRemoved, "-"
		case hunk != nil && hunk.Tweaked:
			base, mark = styleAdded, "+"
		}
		start, end := t.LineStart(line), ed.Buf.LineEnd(line)
		str := string(t.Slice(start, end))
		segs := ed.Wrap(str)
		for i, a := range segs {
			if !visible() {
				continue
			}
			if i == 0 {
				if sev := lineSeverity[line]; sev != 0 && mark == " " {
					s.Put(0, row, diagMark(sev), diagStyle(sev))
				} else {
					s.Put(0, row, mark, base)
				}
				num := styleDim
				if line == curLine {
					num = styleText
				}
				putString(s, 1, row, fmt.Sprintf("%*d", g-2, line+1), num)
			}
			b := len(str)
			if i+1 < len(segs) {
				b = segs[i+1]
			}
			x := drawText(s, g, row, w, str[a:b], start+a, func(off int) tcell.Style {
				st := base
				switch {
				case off == cursor:
					st = styleCursor
				case off >= from && off < to:
					st = styleSel
				case inMatch(off):
					st = styleMatch
				default:
					if base == styleText {
						st = syn(off) // plain lines get syntax colours; diff lines keep theirs
					}
					if inSpans(related, off) {
						st = st.Background(styleRelated.GetBackground())
					}
				}
				if sev := diagAt(off); sev != 0 {
					st = underline(st, sev)
				}
				return st
			})
			if i == len(segs)-1 && end == cursor && x < w { // cursor on the newline or end of file
				s.Put(x, row, " ", styleCursor)
			}
			row++
		}
		if hunk != nil && line == hunk.Last {
			for i, a := range hunk.Added {
				onIt := line == curLine && i+1 == diffRow
				diags, related := rowDiags(hunk.AddedDiags, i), rowSpans(hunk.Related, i)
				for j, segs := 0, ed.Wrap(a); j < len(segs); j++ {
					if visible() {
						drawDiffLine(s, row, w, g, "+", a, segs, j, styleAdded, onIt, diags, related)
						row++
					}
				}
			}
			for _, n := range hunk.Notes {
				if visible() {
					drawHunkNote(s, row, g, n)
					row++
				}
			}
		}
		for _, n := range trailRows[line] {
			if !visible() {
				continue
			}
			mark, st := "┆", styleAgentNote
			switch {
			case n.Note:
				mark = "▸"
			case n.Mine:
				st = styleYourNote
			}
			s.Put(0, row, mark, st)
			drawText(s, g, row, w, n.Text, 0, style(st))
			row++
		}
		line++
	}
	if panel > 0 {
		drawPanel(s, w, textH, panel, ed)
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
	if errs, warns := ed.Buf.DiagCounts(); errs+warns > 0 {
		right = diagSummary(errs, warns) + "  " + right
	}
	if n := ed.PendingCount(); n > 0 {
		right = fmt.Sprintf("%d pending  ", n) + right
	}
	if n := ed.QuestionCount(); n > 0 && ed.Asking == nil {
		right = fmt.Sprintf("%d asked (:answer)  ", n) + right
	}
	if r := ed.Running(); r != "" {
		right = r + "  " + right
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
	case ed.Mode == core.Search:
		putString(s, 0, h-1, "/"+ed.Cmd, styleText)
	case ed.Msg != "":
		putString(s, 0, h-1, ed.Msg, styleText)
	default:
		if ss := ed.Sessions(); len(ss) > 0 && ss[0].State == core.Waiting && ss[0].Note != "" {
			putString(s, 0, h-1, "claude: "+ss[0].Note, styleDim)
		}
		d, hk := ed.DiagAtCursor(), ed.HunkAtCursor()
		if hk != nil && d != nil && ed.DiffRow() <= 0 {
			d = nil // on a hunk's own lines, its info wins; on its proposed lines, their problems
		}
		switch {
		case d != nil:
			fill(s, 0, h-1, w, styleText)
			putString(s, 0, h-1, d.Summary(), diagStyle(d.Severity))
		case hk != nil:
			fill(s, 0, h-1, w, styleText)
			info := fmt.Sprintf("#%d %s", hk.P.ID, hk.P.Agent)
			if hk.P.Rationale != "" {
				info += ": " + hk.P.Rationale
			}
			keys := "  (+ accept, - reject, # comment)"
			if hk.Tweaked {
				keys = "  (editing: + accept as edited, - restore and reject)"
			}
			putString(s, 0, h-1, info+keys, styleDim)
		}
	}
	if t := ed.ActiveToast(); t != nil {
		drawToast(s, w, t)
	}
	if ed.Menu != nil {
		drawMenu(s, w, h-2, ed.Menu, false)
	}
	if ed.Mode == core.Command {
		if m := ed.CommandHints(); m != nil {
			drawMenu(s, w, h-2, m, true) // by the command line it completes
		}
	}
	if ed.Picker != nil {
		drawPicker(s, w, h, ed.Picker)
	}
	if ed.Asking != nil {
		drawAsk(s, w, h, ed.Asking)
	}
}

// drawDiffLine draws row j of a diff line that isn't in the buffer, reversed
// when the cursor is on it; segs is where its rows start, from Wrap.
// diags are the line's problems, marked after mark and underlined; related
// are the columns of its related highlights.
func drawDiffLine(s Screen, row, w, g int, mark, text string, segs []int, j int, st tcell.Style, cursor bool, diags []core.AddedDiag, related [][2]int) {
	if cursor {
		st = st.Reverse(true)
	}
	fill(s, 0, row, w, st)
	if j == 0 {
		s.Put(0, row, mark, st)
	}
	sevAt := func(col int) int {
		sev := 0
		for _, d := range diags {
			if col >= d.Start && col < max(d.End, d.Start+1) && (sev == 0 || d.Severity < sev) {
				sev = d.Severity
			}
		}
		return sev
	}
	if len(diags) > 0 && j == 0 {
		sev := diags[0].Severity
		for _, d := range diags[1:] {
			sev = min(sev, d.Severity)
		}
		s.Put(1, row, diagMark(sev), diagStyle(sev)) // the mark column holds the + already
	}
	from, to := segs[j], len(text)
	if j+1 < len(segs) {
		to = segs[j+1]
	}
	drawText(s, g, row, w, text[from:to], from, func(col int) tcell.Style {
		st := st
		if !cursor && inSpans(related, col) {
			st = st.Background(styleRelated.GetBackground())
		}
		if sev := sevAt(col); sev != 0 {
			return underline(st, sev)
		}
		return st
	})
}

// inSpans reports whether off is in one of the sorted [start, end) ranges.
func inSpans(spans [][2]int, off int) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i][1] > off })
	return i < len(spans) && spans[i][0] <= off
}

// rowSpans returns the columns of the spans on Added line row, sorted.
func rowSpans(ss []core.AddedSpan, row int) [][2]int {
	var out [][2]int
	for _, s := range ss {
		if s.Row == row {
			out = append(out, [2]int{s.Start, s.End})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// rowDiags returns the problems on Added line row.
func rowDiags(ds []core.AddedDiag, row int) []core.AddedDiag {
	var out []core.AddedDiag
	for _, d := range ds {
		if d.Row == row {
			out = append(out, d)
		}
	}
	return out
}

// diagMark is the gutter mark for a line's most severe problem.
func diagMark(severity int) string {
	if severity <= lsp.Warning {
		return "●"
	}
	return "•"
}

// drawPanel draws the output panel's title on row y and the end of *out*
// on the rows below it, rows in all.
func drawPanel(s Screen, w, y, rows int, ed *core.Editor) {
	title := " *out*"
	if r := ed.Running(); r != "" {
		title += "  " + r
	}
	keys := "space o opens, esc hides "
	fill(s, 0, y, w, styleMenu)
	putString(s, 0, y, title, styleMenu.Bold(true))
	if len(title)+len(keys)+2 <= w {
		putString(s, w-len(keys), y, keys, styleMenu)
	}
	for i, l := range ed.OutTail(rows - 1) {
		fill(s, 0, y+1+i, w, styleText)
		drawText(s, 1, y+1+i, w, l, 0, style(styleText))
	}
}

// drawHunkNote draws a hunk's rationale or a comment in its review thread.
func drawHunkNote(s Screen, row, g int, n core.NoteLine) {
	mark, style := "┆", styleAgentNote
	switch {
	case n.Note:
		mark = "▸" // the proposal's rationale
	case n.Mine:
		style = styleYourNote
	}
	s.Put(0, row, mark, styleDim)
	putString(s, g, row, n.Text, style)
}

// diagSummary reads like "2 errors, 1 warning".
func diagSummary(errs, warns int) string {
	var parts []string
	for _, c := range []struct {
		n    int
		name string
	}{{errs, "error"}, {warns, "warning"}} {
		switch {
		case c.n == 1:
			parts = append(parts, "1 "+c.name)
		case c.n > 1:
			parts = append(parts, fmt.Sprintf("%d %ss", c.n, c.name))
		}
	}
	return strings.Join(parts, ", ")
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

// drawMenu draws a menu's key hints in a box ending just above row bottom, at
// the right edge or, if left is set, the left.
func drawMenu(s Screen, w, bottom int, m *core.Menu, left bool) {
	keyWidth := 0
	for _, it := range m.Items {
		keyWidth = max(keyWidth, len(it.Key))
	}
	width := len(m.Title) + 2
	for _, it := range m.Items {
		width = max(width, keyWidth+len(it.Desc)+4)
	}
	x, y := max(w-width-1, 0), max(bottom-len(m.Items)-1, 0)
	if left {
		x = 0
	}
	fill(s, x, y, width, styleMenu)
	putString(s, x+1, y, m.Title, styleMenu.Bold(true))
	for i, it := range m.Items {
		if y+1+i >= bottom {
			break
		}
		fill(s, x, y+1+i, width, styleMenu)
		putString(s, x+1, y+1+i, it.Key, styleMenuKey)
		putString(s, x+3+keyWidth, y+1+i, it.Desc, styleMenu)
	}
}

// drawText draws str from column x until width, styling each character by its
// byte offset (str starts at off). It returns the column after the text.
func drawText(s Screen, x, row, width int, str string, off int, styleAt func(int) tcell.Style) int {
	col := 0 // column within the text, for tab stops
	for str != "" && x < width {
		style := styleAt(off)
		if str[0] == '\t' {
			n := core.TabWidth - col%core.TabWidth
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
