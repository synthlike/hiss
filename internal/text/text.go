// Package text implements an immutable piece table that records who wrote each byte.
//
// A Text is a value: editing returns a new Text and leaves the old one valid, so
// snapshots are free and can be handed to other goroutines. Only the goroutine
// that owns a buffer may call Replace (and Insert/Delete) on its Texts; any
// goroutine may read them. All offsets are byte offsets.
package text

import (
	"bytes"
	"fmt"
)

// Origin says where a run of text came from.
type Origin struct {
	Author   string // "" for text loaded from disk, otherwise "you" or an agent name
	Session  string // agent session, if any
	Turn     int    // agent turn within the session
	Proposal int    // proposal that introduced the text, 0 if none
}

type source uint8

const (
	fromOrig source = iota
	fromAdd
)

type piece struct {
	src    source
	start  int
	len    int
	lines  int // newlines in the piece
	origin Origin
}

// store is the append-only add buffer shared by every Text derived from one New.
// Bytes are only ever appended, so a slice header taken at snapshot time stays valid.
type store struct{ data []byte }

// Text is an immutable document. The zero value is an empty document.
type Text struct {
	orig   []byte
	add    []byte // view of store.data when this Text was made
	store  *store
	pieces []piece
	size   int
	lines  int // newlines in the document
}

// New returns a document containing content, attributed to o.
// The caller must not modify content afterwards.
func New(content []byte, o Origin) Text {
	t := Text{orig: content, store: &store{}, size: len(content)}
	if len(content) > 0 {
		n := bytes.Count(content, nl)
		t.pieces = []piece{{src: fromOrig, len: len(content), lines: n, origin: o}}
		t.lines = n
	}
	return t
}

var nl = []byte{'\n'}

// Len returns the document size in bytes.
func (t Text) Len() int { return t.size }

// LineCount returns the number of lines; a trailing newline starts an empty last line.
func (t Text) LineCount() int { return t.lines + 1 }

func (t Text) bytesOf(p piece) []byte {
	if p.src == fromOrig {
		return t.orig[p.start : p.start+p.len]
	}
	return t.add[p.start : p.start+p.len]
}

// Bytes returns a copy of the whole document.
func (t Text) Bytes() []byte { return t.Slice(0, t.size) }

func (t Text) String() string { return string(t.Bytes()) }

// Slice returns a copy of the bytes in [start, end).
func (t Text) Slice(start, end int) []byte {
	t.check(start, end)
	out := make([]byte, 0, end-start)
	pos := 0
	for _, p := range t.pieces {
		if pos >= end {
			break
		}
		if pos+p.len > start {
			b := t.bytesOf(p)
			lo, hi := max(start-pos, 0), min(end-pos, p.len)
			out = append(out, b[lo:hi]...)
		}
		pos += p.len
	}
	return out
}

// Insert inserts s at off, attributed to o.
func (t Text) Insert(off int, s []byte, o Origin) Text { return t.Replace(off, off, s, o) }

// Delete removes [start, end).
func (t Text) Delete(start, end int) Text { return t.Replace(start, end, nil, Origin{}) }

// Replace replaces [start, end) with s, attributed to o.
func (t Text) Replace(start, end int, s []byte, o Origin) Text {
	t.check(start, end)
	if start == end && len(s) == 0 {
		return t
	}
	nt := Text{orig: t.orig, store: t.store, size: t.size - (end - start) + len(s), lines: t.lines}
	if nt.store == nil {
		nt.store = &store{}
	}
	var ins piece
	if len(s) > 0 {
		ins = piece{src: fromAdd, start: len(nt.store.data), len: len(s), lines: bytes.Count(s, nl), origin: o}
		nt.store.data = append(nt.store.data, s...)
		nt.lines += ins.lines
	}
	nt.add = nt.store.data

	pieces := make([]piece, 0, len(t.pieces)+2)
	pos, i := 0, 0
	for ; i < len(t.pieces) && pos+t.pieces[i].len <= start; i++ {
		pieces = append(pieces, t.pieces[i])
		pos += t.pieces[i].len
	}
	if i < len(t.pieces) && pos < start {
		pieces = append(pieces, t.cut(t.pieces[i], 0, start-pos))
	}
	if ins.len > 0 {
		pieces = appendMerged(pieces, ins)
	}
	for ; i < len(t.pieces) && pos+t.pieces[i].len <= end; i++ {
		pos += t.pieces[i].len
	}
	if i < len(t.pieces) {
		pieces = append(pieces, t.cut(t.pieces[i], end-pos, t.pieces[i].len))
		pieces = append(pieces, t.pieces[i+1:]...)
	}
	nt.lines -= bytes.Count(t.Slice(start, end), nl)
	nt.pieces = pieces
	return nt
}

// cut returns the part [from, to) of p, counting newlines in the shorter side.
func (t Text) cut(p piece, from, to int) piece {
	b := t.bytesOf(p)
	q := piece{src: p.src, start: p.start + from, len: to - from, origin: p.origin}
	if q.len <= p.len/2 {
		q.lines = bytes.Count(b[from:to], nl)
	} else {
		q.lines = p.lines - bytes.Count(b[:from], nl) - bytes.Count(b[to:], nl)
	}
	return q
}

// appendMerged appends q, extending the last piece when q continues it (typing).
func appendMerged(ps []piece, q piece) []piece {
	if n := len(ps); n > 0 {
		last := &ps[n-1]
		if last.src == q.src && last.start+last.len == q.start && last.origin == q.origin {
			last.len += q.len
			last.lines += q.lines
			return ps
		}
	}
	return append(ps, q)
}

func (t Text) check(start, end int) {
	if start < 0 || end < start || end > t.size {
		panic(fmt.Sprintf("text: range [%d,%d) out of bounds [0,%d]", start, end, t.size))
	}
}

// LineStart returns the offset of the first byte of line (0-based).
// Line must be in [0, LineCount()).
func (t Text) LineStart(line int) int {
	if line < 0 || line > t.lines {
		panic(fmt.Sprintf("text: line %d out of range [0,%d]", line, t.lines))
	}
	if line == 0 {
		return 0
	}
	pos, seen := 0, 0
	for _, p := range t.pieces {
		if seen+p.lines >= line {
			b := t.bytesOf(p)
			for j, c := range b {
				if c == '\n' {
					if seen++; seen == line {
						return pos + j + 1
					}
				}
			}
		}
		seen += p.lines
		pos += p.len
	}
	panic("text: line index out of sync")
}

// LineOf returns the 0-based line containing off; off == Len() is on the last line.
func (t Text) LineOf(off int) int {
	t.check(off, off)
	pos, line := 0, 0
	for _, p := range t.pieces {
		if pos+p.len > off {
			return line + bytes.Count(t.bytesOf(p)[:off-pos], nl)
		}
		line += p.lines
		pos += p.len
	}
	return line
}

// Span is a run of bytes [Start, End) with one origin.
type Span struct {
	Start, End int
	Origin     Origin
}

// Spans returns the origins of [start, end), merging adjacent runs with equal origins.
func (t Text) Spans(start, end int) []Span {
	t.check(start, end)
	var out []Span
	pos := 0
	for _, p := range t.pieces {
		if pos >= end {
			break
		}
		if pos+p.len > start {
			s := Span{max(start, pos), min(end, pos+p.len), p.origin}
			if n := len(out); n > 0 && out[n-1].End == s.Start && out[n-1].Origin == s.Origin {
				out[n-1].End = s.End
			} else {
				out = append(out, s)
			}
		}
		pos += p.len
	}
	return out
}

// Edit describes replacing [Start, End) with Len bytes.
type Edit struct {
	Start, End, Len int
}

// Map moves an offset in the old text to the new text. Offsets inside the edited
// range, or on its boundary, go to the end of the new text if after is true and to
// its start otherwise. Range starts should map with after=false and range ends with
// after=true, so a range grows to cover text inserted at its edges.
func (e Edit) Map(off int, after bool) int {
	switch {
	case off < e.Start:
		return off
	case off > e.End:
		return off + e.Len - (e.End - e.Start)
	case after:
		return e.Start + e.Len
	default:
		return e.Start
	}
}
