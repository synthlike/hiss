package core

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"slices"

	"github.com/synthlike/hiss/internal/text"
)

// Tweaking lets the user edit a pending proposal before accepting it. An
// editing key on a proposed (+) line puts the proposal's text into the buffer,
// attributed to the agent; the user's edits there then reshape the proposal
// instead of making it stale. + accepts the text as it stands, - puts the old
// text back, and undoing past the tweak makes it a plain proposal again.

// tweakKeys are the normal-mode keys that edit, or select for editing, at the
// cursor. On a proposed line they first put the proposal into the buffer.
var tweakKeys = map[string]bool{"d": true, "c": true, "p": true, "P": true,
	"i": true, "a": true, "o": true, "O": true, "x": true}

const userEdited = "the user edited this text before reviewing it"

// oldSpan is a run of the text a tweaked proposal replaced, kept with its
// origin so rejecting restores who wrote it as well as what.
type oldSpan struct {
	text   []byte
	origin text.Origin
}

// beforeKey prepares normal-mode key k and reports whether it should run.
func (e *Editor) beforeKey(k string) bool {
	r := e.DiffRow()
	if !tweakKeys[k] || r == 0 {
		return true
	}
	h := e.HunkAtCursor()
	if h.Tweaked {
		e.Msg = "these are the old lines; edit the + lines above them"
		return false
	}
	col := e.goal
	if col < 0 {
		col = e.column(e.Sel.Head)
	}
	ed := e.tweak(h.P)
	head := e.offsetAt(h.First+r-1, col) // proposed line r is now buffer line First+r-1
	e.Sel = Selection{head, head}
	if e.Mode == Select {
		e.Sel.Anchor = ed.Map(e.Sel.Anchor, false)
	}
	e.goal, e.row = col, 0
	return true
}

// tweak puts p's proposed text into its buffer for the user to edit.
func (e *Editor) tweak(p *Proposal) text.Edit {
	b := p.Buf
	e.checkpointBuf(b)
	p.tweakUndo = len(b.undo)
	for _, s := range b.Text.Spans(p.Start, p.End) {
		p.old = append(p.old, oldSpan{b.Text.Slice(s.Start, s.End), s.Origin})
	}
	p.Tweaked = true // before the edit, so the buffer keeps p pending
	return b.Replace(p.Start, p.End, []byte(p.New), p.origin())
}

// untweak puts back the text a tweaked proposal replaced. p must already be
// out of its buffer's pending list.
func (e *Editor) untweak(p *Proposal) {
	b := p.Buf
	e.checkpointBuf(b)
	b.Replace(p.Start, p.End, nil, You)
	at := p.Start
	for _, s := range p.old {
		b.Replace(at, at, s.text, s.origin)
		at += len(s.text)
	}
	p.Tweaked, p.old = false, nil
	b.markSavedIfOnDisk()
}

// oldText returns the text a tweaked proposal replaced.
func (p *Proposal) oldText() string {
	var s []byte
	for _, o := range p.old {
		s = append(s, o.text...)
	}
	return string(s)
}

// untweakUndone turns tweaked proposals whose tweak was just undone back
// into plain proposals over the restored old text.
func (e *Editor) untweakUndone() {
	b := e.Buf
	b.pending = slices.DeleteFunc(b.pending, func(p *Proposal) bool {
		if !p.Tweaked || len(b.undo) >= p.tweakUndo {
			return false
		}
		old := p.oldText()
		p.Tweaked, p.old = false, nil
		if end := p.Start + len(old); end <= b.Text.Len() && string(b.Text.Slice(p.Start, end)) == old {
			p.End = end
			return false
		}
		p.Status, p.Reason = Stale, userEdited
		return true
	})
}

// markSavedIfOnDisk marks b saved if its text is what the file holds.
func (b *Buffer) markSavedIfOnDisk() {
	data, err := os.ReadFile(b.Path)
	if err == nil && bytes.Equal(data, b.Text.Bytes()) || errors.Is(err, fs.ErrNotExist) && b.Text.Len() == 0 {
		b.saved = b.Version
	}
}
