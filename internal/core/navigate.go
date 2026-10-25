package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/synthlike/hiss/internal/lsp"
	"github.com/synthlike/hiss/internal/text"
)

// gd and gr ask the language server about the symbol under the cursor, which
// may be on a proposed line. The server sees buffers with their pending
// proposals applied (see SyncLSP), so positions are mapped into the sent text
// and the answers back out of it. Waiting happens on a goroutine; the answer
// is dropped if the user has moved on by the time it arrives.

// lookupTimeout bounds the wait; gopls can be slow while it loads a workspace.
const lookupTimeout = 10 * time.Second

func (e *Editor) gotoDefinition() { e.lookup("definition") }
func (e *Editor) findReferences() { e.lookup("references") }

func (e *Editor) lookup(what string) {
	b := e.Buf
	srv, ok := lsp.For(b.Path)
	if b.Path == "" || !ok {
		e.Msg = "no language server for this file"
		return
	}
	if e.Post == nil {
		return
	}
	e.SyncLSP()
	c := e.servers[srv.Lang]
	if c == nil || !b.lspOpen {
		e.Msg = srv.Cmd[0] + " isn't available"
		return
	}
	if err := c.Err(); err != nil {
		e.Msg = "the language server stopped: " + err.Error()
		return
	}
	s := b.sent[len(b.sent)-1] // the buffer as it is now, after SyncLSP
	off, ok := e.sentCursor(s)
	if !ok {
		e.Msg = "old lines aren't in the file the language server sees"
		return
	}
	utf8 := c.UTF8()
	pos := position(s.text, off, utf8)
	sent := map[string]sentText{}
	for _, x := range e.Bufs {
		if x.lspOpen && len(x.sent) > 0 {
			sent[x.Path] = x.sent[len(x.sent)-1]
		}
	}
	sel, row, mode := e.Sel, e.row, e.Mode
	go func() {
		var locs []lsp.Location
		var err error
		if what == "references" {
			locs, err = c.References(b.Path, pos, lookupTimeout)
		} else {
			locs, err = c.Definition(b.Path, pos, lookupTimeout)
		}
		e.Post(func(e *Editor) {
			if e.Buf != b || e.Sel != sel || e.row != row || e.Mode != mode || e.Picker != nil || e.Menu != nil || e.Asking != nil {
				return // the user moved on
			}
			e.showLocations(what, locs, err, sent, utf8)
		})
	}()
}

// sentCursor returns the cursor's offset in s, the current buffer's latest
// sent text. It is false on a tweaked hunk's old lines, which s doesn't have.
func (e *Editor) sentCursor(s sentText) (int, bool) {
	row := e.DiffRow()
	switch {
	case row < 0:
		return 0, false
	case row == 0:
		return s.toSent(e.Sel.Head), true
	}
	line := e.line()
	for _, h := range e.Hunks() {
		if h.Last != line || h.Tweaked {
			continue
		}
		// The hunk's Added lines are the sent text from its first line's start.
		off := s.toSent(e.Buf.Text.LineStart(h.First))
		for _, l := range h.Added[:row-1] {
			off += len(l) + 1
		}
		l := h.Added[row-1]
		i := 0
		for range max(e.goal, 0) {
			if i >= len(l) {
				break
			}
			_, n := utf8.DecodeRuneInString(l[i:])
			i += n
		}
		return off + i, true
	}
	return s.toSent(e.Sel.Head), true
}

// toSent maps an offset in the buffer at s.version to s. An offset in text a
// proposal replaces goes to the start of its new text.
func (s sentText) toSent(off int) int {
	d := 0
	for _, sp := range s.spans {
		if off < sp.start {
			break
		}
		if off < sp.end {
			return sp.start + d
		}
		d += sp.n - (sp.end - sp.start)
	}
	return off + d
}

// position converts a byte offset in t to an LSP position.
func position(t text.Text, off int, utf8 bool) lsp.Position {
	line := t.LineOf(off)
	start := t.LineStart(line)
	if utf8 {
		return lsp.Position{Line: line, Character: off - start}
	}
	units := 0
	for _, r := range string(t.Slice(start, off)) {
		units += max(utf16.RuneLen(r), 1)
	}
	return lsp.Position{Line: line, Character: units}
}

// place is where a location is now.
type place struct {
	buf  *Buffer // nil for a file that isn't open
	path string
	off  int       // in buf, or in the file on disk
	prop *Proposal // set when the location is in this pending proposal's new text
	line string    // the location's line, for the picker
	err  error
}

// where finds a location given in the sent texts (or, for files that weren't
// open, on disk). files caches the files read.
func (e *Editor) where(l lsp.Location, sent map[string]sentText, utf8 bool, files map[string]text.Text) place {
	pl := place{path: l.Path, buf: e.Find(l.Path)}
	s, ok := sent[l.Path]
	switch {
	case pl.buf != nil && ok:
		so := offset(s.text, l.Range.Start, utf8)
		pl.line = lineText(s.text, so)
		sp, at := s.locate(so)
		switch {
		case sp != nil && sp.p.Status == Pending && !sp.p.Tweaked:
			pl.prop, pl.off = sp.p, sp.p.Start
			return pl
		case sp != nil: // resolved since
			at = sp.start
		}
		pl.off = min(pl.buf.mapFrom(s.version, at, false), pl.buf.Text.Len())
	case pl.buf != nil: // opened since: the server saw the file on disk
		pl.off = offset(pl.buf.Text, l.Range.Start, utf8)
		pl.line = lineText(pl.buf.Text, pl.off)
	default:
		t, ok := files[l.Path]
		if !ok {
			data, err := os.ReadFile(l.Path)
			if err != nil {
				pl.err = err
				return pl
			}
			t = text.New(data, text.Origin{})
			if files != nil {
				files[l.Path] = t
			}
		}
		pl.off = offset(t, l.Range.Start, utf8)
		pl.line = lineText(t, pl.off)
	}
	return pl
}

func lineText(t text.Text, off int) string {
	line := t.LineOf(off)
	end := t.Len()
	if line+1 < t.LineCount() {
		end = t.LineStart(line+1) - 1
	}
	return strings.TrimSpace(string(t.Slice(t.LineStart(line), end)))
}

// showLocations jumps to a lone definition and lists anything else in a picker.
func (e *Editor) showLocations(what string, locs []lsp.Location, err error, sent map[string]sentText, utf8 bool) {
	switch {
	case err != nil:
		e.Msg = what + ": " + err.Error()
		return
	case len(locs) == 0:
		e.Msg = "no " + what + " found"
		return
	case len(locs) == 1 && what == "definition":
		e.goPlace(e.where(locs[0], sent, utf8, nil))
		return
	}
	p := &Picker{Title: what}
	files := map[string]text.Text{}
	for _, l := range locs {
		pl := e.where(l, sent, utf8, files)
		if pl.err != nil {
			continue
		}
		name := pl.path
		if pl.buf != nil {
			name = pl.buf.Name
		} else if rel, err := filepath.Rel(e.Root, pl.path); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		line := l.Range.Start.Line + 1
		detail := pl.line
		switch {
		case pl.prop != nil:
			line = pl.buf.Text.LineOf(pl.prop.Start) + 1
			detail = fmt.Sprintf("#%d: %s", pl.prop.ID, pl.line)
		case pl.buf != nil:
			line = pl.buf.Text.LineOf(pl.off) + 1
		}
		p.Items = append(p.Items, PickItem{Label: fmt.Sprintf("%s:%d", name, line), Detail: detail,
			Run: func(e *Editor) { e.goPlace(e.where(l, sent, utf8, nil)) }})
	}
	if len(p.Items) == 0 {
		e.Msg = "no " + what + " found"
		return
	}
	e.Picker = p
}

func (e *Editor) goPlace(pl place) {
	switch {
	case pl.err != nil:
		e.Msg = pl.err.Error()
		return
	case pl.prop != nil:
		e.jumpToProposal(pl.prop)
		return
	case pl.buf == nil:
		e.openFile(pl.path)
		if e.Buf.Path != pl.path {
			return // it couldn't be opened; openFile said why
		}
	default:
		e.pushJump()
		e.SwitchTo(pl.buf)
	}
	e.setHead(min(pl.off, e.Buf.Text.Len()))
	e.revealLine(e.line())
}
