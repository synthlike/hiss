// Package core holds all editor state. It is owned by a single goroutine: other
// goroutines send it work as func(*Editor) values over a channel.
package core

import (
	"fmt"
	"unicode/utf8"
)

type Mode int

const (
	Normal Mode = iota
	Insert
	Command
)

func (m Mode) String() string { return [...]string{"NOR", "INS", "CMD"}[m] }

// Selection is a byte range from Anchor to Head; Head is where the cursor is.
// In Helix fashion it always covers at least the character under the head.
type Selection struct{ Anchor, Head int }

type Editor struct {
	Buf    *Buffer
	Sel    Selection
	Mode   Mode
	Top    int    // first visible line
	Height int    // rows available for text
	Cmd    string // command line contents in Command mode
	Msg    string // one-line message for the user
	Quit   bool

	goal    int    // preferred column for vertical moves, -1 if none
	pending string // first key of a two-key sequence such as "gg"
}

func New(b *Buffer) *Editor { return &Editor{Buf: b, goal: -1} }

// Resize records the text area height and keeps the cursor visible.
func (e *Editor) Resize(width, height int) {
	e.Height = max(height-2, 1) // status line and message line
	e.scroll()
}

// HandleKey applies one key, named like "j", "C-s", "A-x", "esc", "enter", "left".
func (e *Editor) HandleKey(k string) {
	e.Msg = ""
	switch e.Mode {
	case Normal:
		e.normalKey(k)
	case Command:
		e.commandKey(k)
	}
	e.scroll()
}

func (e *Editor) normalKey(k string) {
	if p := e.pending; p != "" {
		e.pending = ""
		switch p + k {
		case "gg":
			e.moveTo(0)
		case "ge":
			e.moveTo(e.Buf.Text.LineStart(e.Buf.Text.LineCount() - 1))
		case "gh":
			e.moveTo(e.Buf.Text.LineStart(e.line()))
		case "gl":
			e.moveTo(e.Buf.LineEnd(e.line()))
		}
		return
	}
	switch k {
	case "h", "left":
		e.moveTo(e.prevRune(e.Sel.Head))
	case "l", "right":
		e.moveTo(e.nextRune(e.Sel.Head))
	case "j", "down":
		e.moveLines(1)
	case "k", "up":
		e.moveLines(-1)
	case "C-d", "pgdn":
		e.moveLines(max(e.Height/2, 1))
	case "C-u", "pgup":
		e.moveLines(-max(e.Height/2, 1))
	case "g":
		e.pending = k
	case ":":
		e.Mode, e.Cmd = Command, ""
	}
}

func (e *Editor) commandKey(k string) {
	switch k {
	case "esc", "C-c":
		e.Mode = Normal
	case "enter":
		e.Mode = Normal
		e.execute(e.Cmd)
	case "backspace":
		if e.Cmd == "" {
			e.Mode = Normal
			return
		}
		_, n := utf8.DecodeLastRuneInString(e.Cmd)
		e.Cmd = e.Cmd[:len(e.Cmd)-n]
	default:
		if utf8.RuneCountInString(k) == 1 {
			e.Cmd += k
		}
	}
}

func (e *Editor) execute(cmd string) {
	switch cmd {
	case "w", "write":
		e.save()
	case "q", "quit":
		if e.Buf.Dirty() {
			e.Msg = "unsaved changes (:q! to discard, :wq to save)"
			return
		}
		e.Quit = true
	case "q!", "quit!":
		e.Quit = true
	case "wq", "x":
		if e.save() {
			e.Quit = true
		}
	case "":
	default:
		e.Msg = fmt.Sprintf("unknown command: %s", cmd)
	}
}

func (e *Editor) save() bool {
	if err := e.Buf.Save(); err != nil {
		e.Msg = "save failed: " + err.Error()
		return false
	}
	e.Msg = fmt.Sprintf("wrote %s (%d bytes)", e.Buf.Path, e.Buf.Text.Len())
	return true
}

// Cursor returns the 0-based line and rune column of the cursor.
func (e *Editor) Cursor() (line, col int) { return e.line(), e.column(e.Sel.Head) }

// line returns the line of the cursor.
func (e *Editor) line() int { return e.Buf.Text.LineOf(e.Sel.Head) }

// moveTo collapses the selection onto off and forgets the goal column.
func (e *Editor) moveTo(off int) {
	e.Sel = Selection{off, off}
	e.goal = -1
}

func (e *Editor) moveLines(n int) {
	line := e.line()
	if e.goal < 0 {
		e.goal = e.column(e.Sel.Head)
	}
	target := min(max(line+n, 0), e.Buf.Text.LineCount()-1)
	off := e.offsetAt(target, e.goal)
	e.Sel = Selection{off, off}
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
		return off
	}
	_, n := utf8.DecodeRune(t.Slice(off, min(off+utf8.UTFMax, t.Len())))
	return off + n
}

func (e *Editor) prevRune(off int) int {
	if off == 0 {
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
	} else if line >= e.Top+e.Height {
		e.Top = line - e.Height + 1
	}
}
