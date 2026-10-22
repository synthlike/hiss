// Package core holds all editor state. It is owned by a single goroutine: other
// goroutines send it work as func(*Editor) values over a channel.
package core

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/synthlike/hiss/internal/text"
)

type Mode int

const (
	Normal Mode = iota
	Insert
	Select
	Command
)

func (m Mode) String() string { return [...]string{"NOR", "INS", "SEL", "CMD"}[m] }

// You is the origin of text typed by the user.
var You = text.Origin{Author: "you"}

// Selection runs from Anchor to Head; Head is where the cursor is. Outside
// Insert mode it includes the character under the head, as in Helix.
type Selection struct{ Anchor, Head int }

// Menu is a prefix key's table of follow-up keys, shown as hints while open.
type Menu struct {
	Title string
	Items []MenuItem
}

type MenuItem struct {
	Key, Desc string
	Run       func(*Editor)
}

type snapshot struct {
	text text.Text
	sel  Selection
}

type Editor struct {
	Root   string // project root; agents' relative paths resolve against it
	Buf    *Buffer
	Sel    Selection
	Mode   Mode
	Top    int    // first visible line
	Height int    // rows available for text
	Cmd    string // command line contents in Command mode
	Msg    string // one-line message for the user
	Menu   *Menu  // open prefix menu, if any
	Quit   bool

	goal       int // preferred column for vertical moves, -1 if none
	undo, redo []snapshot
	register   string
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
	switch {
	case e.Menu != nil:
		m := e.Menu
		e.Menu = nil
		for _, it := range m.Items {
			if it.Key == k {
				it.Run(e)
			}
		}
	case e.Mode == Insert:
		e.insertKey(k)
	case e.Mode == Command:
		e.commandKey(k)
	default:
		if f, ok := normalKeys[k]; ok {
			f(e)
		}
	}
	e.scroll()
}

var normalKeys = map[string]func(*Editor){
	"h": (*Editor).left, "left": (*Editor).left,
	"l": (*Editor).right, "right": (*Editor).right,
	"j": func(e *Editor) { e.moveLines(1) }, "down": func(e *Editor) { e.moveLines(1) },
	"k": func(e *Editor) { e.moveLines(-1) }, "up": func(e *Editor) { e.moveLines(-1) },
	"C-d": func(e *Editor) { e.moveLines(max(e.Height/2, 1)) },
	"C-u": func(e *Editor) { e.moveLines(-max(e.Height/2, 1)) },
	"w":   (*Editor).wordNext, "b": (*Editor).wordPrev, "e": (*Editor).wordEnd,
	"x":   (*Editor).selectLine,
	";":   func(e *Editor) { e.Sel.Anchor = e.Sel.Head },
	"v":   (*Editor).toggleSelect,
	"esc": func(e *Editor) { e.Mode = Normal },

	"d": func(e *Editor) { e.checkpoint(); e.deleteSelection() },
	"c": func(e *Editor) { e.checkpoint(); e.deleteSelection(); e.Mode = Insert },
	"y": func(e *Editor) { e.yank(); e.Msg = "yanked" },
	"p": func(e *Editor) { e.paste(true) },
	"P": func(e *Editor) { e.paste(false) },
	"i": func(e *Editor) { from, _ := e.selRange(); e.startInsert(from) },
	"a": func(e *Editor) { _, to := e.selRange(); e.startInsert(to) },
	"o": (*Editor).openBelow,
	"O": (*Editor).openAbove,
	"u": (*Editor).undoOnce,
	"U": (*Editor).redoOnce,

	"g":     func(e *Editor) { e.Menu = &gotoMenu },
	"space": func(e *Editor) { e.Menu = &spaceMenu },
	":":     func(e *Editor) { e.Mode, e.Cmd = Command, "" },
}

var gotoMenu = Menu{Title: "goto", Items: []MenuItem{
	{"g", "file start", func(e *Editor) { e.setHead(0) }},
	{"e", "file end", func(e *Editor) { e.setHead(e.Buf.Text.LineStart(e.Buf.Text.LineCount() - 1)) }},
	{"h", "line start", func(e *Editor) { e.setHead(e.Buf.Text.LineStart(e.line())) }},
	{"l", "line end", func(e *Editor) { e.setHead(e.Buf.LineEnd(e.line())) }},
}}

var spaceMenu = Menu{Title: "space", Items: []MenuItem{
	{"w", "write", func(e *Editor) { e.save() }},
	{"q", "quit", func(e *Editor) { e.execute("q") }},
}}

func (e *Editor) toggleSelect() {
	if e.Mode == Select {
		e.Mode = Normal
	} else {
		e.Mode = Select
	}
}

func (e *Editor) insertKey(k string) {
	t := e.Buf.Text
	at := e.Sel.Head
	switch k {
	case "esc":
		e.Mode = Normal
	case "enter":
		e.insert("\n" + e.indent(e.line()))
	case "tab":
		e.insert("\t")
	case "backspace":
		if at > 0 {
			e.edit(e.prevRune(at), at, "")
		}
	case "del":
		if at < t.Len() {
			e.edit(at, e.nextRune(at), "")
		}
	case "left", "right", "up", "down":
		normalKeys[k](e)
	default:
		if utf8.RuneCountInString(k) == 1 {
			e.insert(k)
		} else if k == "space" {
			e.insert(" ")
		}
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
	case "space":
		e.Cmd += " "
	default:
		if utf8.RuneCountInString(k) == 1 {
			e.Cmd += k
		}
	}
}

func (e *Editor) execute(cmd string) {
	switch strings.TrimSpace(cmd) {
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
	e.Msg = fmt.Sprintf("wrote %s (%d bytes)", e.Buf.Name, e.Buf.Text.Len())
	return true
}

// edit replaces [start, end) with s as the user and leaves the cursor after s.
func (e *Editor) edit(start, end int, s string) {
	e.Buf.Replace(start, end, []byte(s), You)
	e.Sel = Selection{start + len(s), start + len(s)}
	e.goal = -1
}

func (e *Editor) insert(s string) { e.edit(e.Sel.Head, e.Sel.Head, s) }

func (e *Editor) startInsert(at int) {
	e.checkpoint()
	e.Sel = Selection{at, at}
	e.Mode = Insert
}

func (e *Editor) deleteSelection() {
	from, to := e.selRange()
	e.yank()
	e.edit(from, to, "")
	if e.Mode == Select {
		e.Mode = Normal
	}
}

func (e *Editor) yank() {
	from, to := e.selRange()
	e.register = string(e.Buf.Text.Slice(from, to))
}

func (e *Editor) paste(after bool) {
	if e.register == "" {
		return
	}
	e.checkpoint()
	from, to := e.selRange()
	at := from
	if after {
		at = to
	}
	e.Buf.Replace(at, at, []byte(e.register), You)
	e.Sel = Selection{at, e.prevRune(at + len(e.register))}
}

func (e *Editor) openBelow() {
	line := e.line()
	e.startInsert(e.Buf.LineEnd(line))
	e.insert("\n" + e.indent(line))
}

func (e *Editor) openAbove() {
	line := e.line()
	ind := e.indent(line)
	e.startInsert(e.Buf.Text.LineStart(line))
	e.insert(ind + "\n")
	e.Sel = Selection{e.Sel.Head - 1, e.Sel.Head - 1}
}

// indent returns the leading spaces and tabs of line.
func (e *Editor) indent(line int) string {
	b := e.Buf.Text.Slice(e.Buf.Text.LineStart(line), e.Buf.LineEnd(line))
	return string(b[:len(b)-len(strings.TrimLeft(string(b), " \t"))])
}

// checkpoint records the state before a change for undo.
func (e *Editor) checkpoint() {
	e.undo = append(e.undo, snapshot{e.Buf.Text, e.Sel})
	e.redo = nil
}

func (e *Editor) undoOnce() {
	if len(e.undo) == 0 {
		e.Msg = "nothing to undo"
		return
	}
	e.redo = append(e.redo, snapshot{e.Buf.Text, e.Sel})
	e.restore(&e.undo)
}

func (e *Editor) redoOnce() {
	if len(e.redo) == 0 {
		e.Msg = "nothing to redo"
		return
	}
	e.undo = append(e.undo, snapshot{e.Buf.Text, e.Sel})
	e.restore(&e.redo)
}

func (e *Editor) restore(stack *[]snapshot) {
	s := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	e.Buf.Text = s.text
	e.Buf.Version++
	e.Sel = s.sel
}
