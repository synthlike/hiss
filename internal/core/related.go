package core

import (
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/synthlike/hiss/internal/lsp"
)

// Related highlights: with the cursor on a word, the language server's
// document highlights for it are drawn, such as the other uses of an
// identifier or, on func and return, the function's returns. They are asked
// for once the cursor has rested for relatedDelay, through the sent texts as
// for gd, so they reach into pending proposals. A move off them or any change
// to the text clears them.

// relatedDelay is how long the cursor rests before highlights are asked for.
var relatedDelay = 150 * time.Millisecond

type relatedState struct {
	want   relatedKey
	buf    *Buffer
	seq    int      // the sent text they were placed in
	shadow [][2]int // their ranges in that text, to tell whether the cursor is still on one
	spans  [][2]int // in the buffer, sorted
	added  map[*Proposal][][2]int
}

// relatedKey identifies a request: the cursor at off in a sent text.
type relatedKey struct {
	buf      *Buffer
	seq, off int
}

func (r *relatedState) clear() {
	*r = relatedState{want: r.want}
}

// UpdateRelated asks for highlights when the cursor has moved to another
// word, and clears them when they no longer apply. The UI calls it once per
// event, after SyncLSP.
func (e *Editor) UpdateRelated() {
	r := &e.related
	if r.buf != nil && (r.buf != e.Buf || r.seq != r.buf.lspSeq) {
		r.clear() // the text changed under them
	}
	key, ok := e.relatedKey()
	if key == r.want {
		return
	}
	r.want = key
	if !ok {
		r.clear()
		return
	}
	if !slices.ContainsFunc(r.shadow, func(h [2]int) bool { return h[0] <= key.off && key.off < h[1] }) {
		r.clear()
	}
	time.AfterFunc(relatedDelay, func() {
		e.Post(func(e *Editor) {
			if e.related.want == key {
				e.askRelated(key)
			}
		})
	})
}

// relatedKey returns the request for the cursor, or false when there is
// nothing to ask: not a word, not Normal or Select mode, or no server.
func (e *Editor) relatedKey() (relatedKey, bool) {
	b := e.Buf
	if e.Post == nil || e.Mode != Normal && e.Mode != Select || !b.lspOpen || b.lspKey != b.syncKey() {
		return relatedKey{}, false
	}
	s := b.sent[len(b.sent)-1]
	off, ok := e.sentCursor(s)
	if !ok {
		return relatedKey{}, false
	}
	r, _ := utf8.DecodeRune(s.text.Slice(off, min(off+utf8.UTFMax, s.text.Len())))
	if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return relatedKey{}, false
	}
	return relatedKey{b, s.seq, off}, true
}

func (e *Editor) askRelated(key relatedKey) {
	b := key.buf
	srv, _ := lsp.For(b.Path)
	c := e.servers[srv.Lang]
	if c == nil || c.Err() != nil {
		return
	}
	s := b.sent[len(b.sent)-1] // the key still matching means this is the text it was made in
	utf8 := c.UTF8()
	pos := position(s.text, key.off, utf8)
	go func() {
		hs, err := c.Highlights(b.Path, pos, lookupTimeout)
		if err != nil {
			return // not worth a message: the next move asks again
		}
		e.Post(func(e *Editor) {
			if e.related.want == key {
				e.placeRelated(s, hs, utf8)
			}
		})
	}()
}

// placeRelated keeps the highlights hs, given in s, the buffer's latest sent text.
func (e *Editor) placeRelated(s sentText, hs []lsp.Range, utf8 bool) {
	r := &e.related
	r.clear()
	r.buf, r.seq, r.added = e.Buf, s.seq, map[*Proposal][][2]int{}
	for _, h := range hs {
		so := offset(s.text, h.Start, utf8)
		eo := max(offset(s.text, h.End, utf8), so)
		r.shadow = append(r.shadow, [2]int{so, eo})
		switch sp, at := s.locate(so); {
		case sp != nil && sp.p.Status == Pending && !sp.p.Tweaked:
			r.added[sp.p] = append(r.added[sp.p], [2]int{at, min(at+eo-so, sp.n)})
		case sp != nil:
		default:
			q, end := s.locate(eo)
			if q != nil { // runs into a proposal: stop where it starts
				end = q.start
			}
			r.spans = append(r.spans, [2]int{at, max(end, at)})
		}
	}
	slices.SortFunc(r.spans, func(x, y [2]int) int { return x[0] - y[0] })
}

// Related returns the highlighted ranges in the buffer on screen, sorted.
func (e *Editor) Related() [][2]int {
	if r := &e.related; r.buf == e.Buf && r.seq == e.Buf.lspSeq {
		return r.spans
	}
	return nil
}

// relatedIn returns the highlighted ranges in p's new text.
func (e *Editor) relatedIn(p *Proposal) [][2]int {
	if r := &e.related; r.buf == p.Buf && r.seq == p.Buf.lspSeq {
		return r.added[p]
	}
	return nil
}
