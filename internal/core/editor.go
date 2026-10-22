// Package core holds all editor state. It is owned by a single goroutine: other
// goroutines send it work as func(*Editor) values over a channel.
package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	Root   string    // project root; agents' relative paths resolve against it
	Bufs   []*Buffer // open buffers, including ones agents opened
	Buf    *Buffer   // the buffer on screen
	Sel    Selection
	Mode   Mode
	Top    int    // first visible line
	TopSub int    // rows of the Top line's diff already scrolled past
	Width  int    // columns, for wrapping review threads
	Height int    // rows available for text
	Cmd    string // command line contents in Command mode
	Msg    string // one-line message for the user
	Menu   *Menu  // open prefix menu, if any
	Splash string // shown on an empty scratch buffer
	Toast  Toast  // notice in the top-right corner
	Quit   bool
	Picker *Picker // open fuzzy picker, if any

	goal      int  // preferred column for vertical moves, -1 if none
	row       int  // n>0: the cursor is on the n-th proposed line below its hunk
	rowSet    bool // a vertical move set row during the current key
	register  string
	proposals map[int]*Proposal
	nextID    int
	sessions  map[string]*Session
	caller    string           // session ID of the latest hiss tool call
	now       func() time.Time // replaced in tests
	notices   []Toast          // raised since the UI last took them
	locations *LocationList    // last list an agent shared
}

func New(b *Buffer) *Editor {
	return &Editor{Bufs: []*Buffer{b}, Buf: b, goal: -1, proposals: map[int]*Proposal{},
		sessions: map[string]*Session{}, now: time.Now}
}

// Resize records the text area height and keeps the cursor visible.
func (e *Editor) Resize(width, height int) {
	e.Width = width
	e.Height = max(height-2, 1) // status line and message line
	e.scroll()
}

// HandleKey applies one key, named like "j", "C-s", "A-x", "esc", "enter", "left".
func (e *Editor) HandleKey(k string) {
	e.Msg = ""
	sel, buf := e.Sel, e.Buf
	e.rowSet = false
	defer func() {
		if !e.rowSet && (e.Sel != sel || e.Buf != buf) {
			e.row = 0 // any other motion or edit leaves the proposed lines
		}
		e.scroll()
	}()
	switch {
	case e.Picker != nil:
		e.pickerKey(k)
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
	"]":     func(e *Editor) { e.Menu = &nextMenu },
	"[":     func(e *Editor) { e.Menu = &prevMenu },
	"space": func(e *Editor) { e.Menu = &spaceMenu },
	":":     func(e *Editor) { e.Mode, e.Cmd = Command, "" },
}

var gotoMenu = Menu{Title: "goto", Items: []MenuItem{
	{"g", "file start", func(e *Editor) { e.setHead(0) }},
	{"e", "file end", func(e *Editor) { e.setHead(e.Buf.Text.LineStart(e.Buf.Text.LineCount() - 1)) }},
	{"h", "line start", func(e *Editor) { e.setHead(e.Buf.Text.LineStart(e.line())) }},
	{"l", "line end", func(e *Editor) { e.setHead(e.Buf.LineEnd(e.line())) }},
}}

var nextMenu = Menu{Title: "next", Items: []MenuItem{
	{"h", "pending hunk", func(e *Editor) { e.jumpHunk(1) }},
}}

var prevMenu = Menu{Title: "previous", Items: []MenuItem{
	{"h", "pending hunk", func(e *Editor) { e.jumpHunk(-1) }},
}}

var spaceMenu = Menu{Title: "space", Items: []MenuItem{
	{"a", "accept hunk", func(e *Editor) { e.reviewHunk(true, "") }},
	{"r", "reject hunk", func(e *Editor) { e.reviewHunk(false, "") }},
	{"c", "comment on hunk", func(e *Editor) { e.Mode, e.Cmd = Command, "comment " }},
	{"h", "pick a hunk", (*Editor).openHunkPicker},
	{"l", "agent's locations", (*Editor).openLocationPicker},
	{"L", "clear agent's locations", (*Editor).clearLocations},
	{"f", "open file", (*Editor).openFilePicker},
	{"b", "buffers", (*Editor).openBufferPicker},
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
		for _, b := range e.Bufs {
			if b.Dirty() {
				e.Msg = fmt.Sprintf("unsaved changes in %s (:q! to discard)", b.Name)
				return
			}
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
		c := strings.TrimSpace(cmd)
		if text, ok := strings.CutPrefix(c, "comment"); ok {
			e.commentOnHunk(strings.TrimSpace(text))
			return
		}
		if reason, ok := strings.CutPrefix(c, "reject "); ok {
			e.reviewHunk(false, strings.TrimSpace(reason))
			return
		}
		if path, ok := strings.CutPrefix(c, "w "); ok {
			e.saveAs(strings.TrimSpace(path))
			return
		}
		if path, ok := strings.CutPrefix(c, "e "); ok {
			e.openFile(strings.TrimSpace(path))
			return
		}
		e.Msg = fmt.Sprintf("unknown command: %s", cmd)
	}
}

// saveAs gives the buffer a file name and writes it.
func (e *Editor) saveAs(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	if b := e.Find(abs); b != nil && b != e.Buf {
		e.Msg = path + " is already open"
		return
	}
	e.Buf.Path, e.Buf.Name = abs, path
	e.save()
}

// openFile shows the file at path, opening it if needed. An untouched
// scratch buffer is closed rather than kept around.
func (e *Editor) openFile(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	b, err := e.OpenBuffer(abs)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	old := e.Buf
	e.SwitchTo(b)
	if old != b && old.Path == "" && !old.Dirty() && old.Text.Len() == 0 {
		for i, x := range e.Bufs {
			if x == old {
				e.Bufs = append(e.Bufs[:i], e.Bufs[i+1:]...)
				break
			}
		}
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
	b := e.Buf
	b.undo = append(b.undo, snapshot{b.Text, e.Sel})
	b.redo = nil
}

func (e *Editor) undoOnce() {
	b := e.Buf
	if len(b.undo) == 0 {
		e.Msg = "nothing to undo"
		return
	}
	b.redo = append(b.redo, snapshot{b.Text, e.Sel})
	e.restore(&b.undo)
}

func (e *Editor) redoOnce() {
	b := e.Buf
	if len(b.redo) == 0 {
		e.Msg = "nothing to redo"
		return
	}
	b.undo = append(b.undo, snapshot{b.Text, e.Sel})
	e.restore(&b.redo)
}

func (e *Editor) restore(stack *[]snapshot) {
	s := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	e.Buf.Restore(s.text)
	e.Sel = s.sel
}

// SwitchTo shows b, saving the current buffer's view.
func (e *Editor) SwitchTo(b *Buffer) {
	if b == e.Buf {
		return
	}
	e.Buf.sel, e.Buf.top = e.Sel, e.Top
	e.Buf, e.Sel, e.Top, e.TopSub = b, b.sel, b.top, 0
	e.Mode, e.goal = Normal, -1
}

// Find returns the open buffer for an absolute path, or nil.
func (e *Editor) Find(path string) *Buffer {
	for _, b := range e.Bufs {
		if b.Path == path {
			return b
		}
	}
	return nil
}

// OpenBuffer returns the buffer for path, opening it in the background if needed.
func (e *Editor) OpenBuffer(path string) (*Buffer, error) {
	if b := e.Find(path); b != nil {
		return b, nil
	}
	b, err := Open(path)
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(e.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
		b.Name = rel
	}
	e.Bufs = append(e.Bufs, b)
	return b, nil
}
