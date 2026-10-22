package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/synthlike/hiss/internal/text"
)

// Buffer is a file's contents as edited in hiss, plus the view state that is
// restored when the user switches back to it.
type Buffer struct {
	Path    string // absolute
	Name    string // as shown to the user
	Text    text.Text
	Version int // bumped on every change
	saved   int // Version at the last load or save

	log     []logged    // every edit, for checking proposals against old versions
	pending []*Proposal // pending proposals, kept in step with edits

	sel        Selection
	top        int
	undo, redo []snapshot
}

type logged struct {
	version int // Version after the edit
	edit    text.Edit
}

// Scratch returns an empty buffer without a file; :w <path> gives it one.
func Scratch() *Buffer {
	return &Buffer{Name: "[scratch]", Text: text.New(nil, text.Origin{})}
}

// Open reads path into a buffer. A missing file gives an empty buffer.
func Open(path string) (*Buffer, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &Buffer{Path: abs, Name: path, Text: text.New(data, text.Origin{})}, nil
}

// PendingCount returns the number of pending proposals in the buffer.
func (b *Buffer) PendingCount() int { return len(b.pending) }

// Dirty reports whether the buffer has changes not written to disk.
func (b *Buffer) Dirty() bool { return b.Version != b.saved }

// Replace edits the buffer and returns the edit for mapping positions.
// Pending proposals whose text the edit touches become stale.
func (b *Buffer) Replace(start, end int, s []byte, o text.Origin) text.Edit {
	e := text.Edit{Start: start, End: end, Len: len(s)}
	if start == end && len(s) == 0 {
		return e
	}
	b.Text = b.Text.Replace(start, end, s, o)
	b.record(e)
	return e
}

// Restore replaces the whole text with t (for undo), recording the smallest
// edit that turns the old text into t.
func (b *Buffer) Restore(t text.Text) text.Edit {
	old, nw := b.Text.Bytes(), t.Bytes()
	p := 0
	for p < len(old) && p < len(nw) && old[p] == nw[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(nw)-p && old[len(old)-1-s] == nw[len(nw)-1-s] {
		s++
	}
	e := text.Edit{Start: p, End: len(old) - s, Len: len(nw) - p - s}
	b.Text = t
	b.record(e)
	return e
}

func (b *Buffer) record(e text.Edit) {
	b.Version++
	b.log = append(b.log, logged{b.Version, e})
	keep := b.pending[:0]
	for _, p := range b.pending {
		switch {
		case overlaps(e.Start, e.End, p.Start, p.End):
			p.Status, p.Reason = Stale, "the user edited this text before reviewing it"
			continue
		case e.End <= p.Start:
			d := e.Len - (e.End - e.Start)
			p.Start, p.End = p.Start+d, p.End+d
		}
		keep = append(keep, p)
	}
	clear(b.pending[len(keep):])
	b.pending = keep
}

// ChangedSince reports whether any edit after version touched [start, end) of
// the current text.
func (b *Buffer) ChangedSince(version, start, end int) bool {
	for i, l := range b.log {
		if l.version <= version {
			continue
		}
		a, z := l.edit.Start, l.edit.Start+l.edit.Len
		for _, later := range b.log[i+1:] {
			a, z = later.edit.Map(a, false), later.edit.Map(z, true)
		}
		if overlaps(a, z, start, end) {
			return true
		}
	}
	return false
}

// overlaps reports whether range [a, b) touches [s, e). An empty range is a
// point: it touches a range it falls strictly inside, or an equal point.
func overlaps(a, b, s, e int) bool {
	switch {
	case a == b && s == e:
		return a == s
	case a == b:
		return s < a && a < e
	case s == e:
		return a < s && s < b
	}
	return a < e && b > s
}

// Save writes the buffer atomically, keeping the file's permissions.
func (b *Buffer) Save() error {
	if b.Path == "" {
		return errors.New("no file name; use :w <path>")
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(b.Path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(b.Path), ".hiss-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(b.Text.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), b.Path); err != nil {
		return err
	}
	b.saved = b.Version
	return nil
}

// Lines counts lines the way agents and cat -n do: a trailing newline ends
// the last line rather than starting an empty one.
func (b *Buffer) Lines() int {
	n := b.Text.LineCount()
	if b.Text.Len() > 0 && b.Text.LineStart(n-1) == b.Text.Len() {
		n--
	}
	return n
}

// LineEnd returns the offset of line's newline, or Len() on the last line.
func (b *Buffer) LineEnd(line int) int {
	if line+1 < b.Text.LineCount() {
		return b.Text.LineStart(line+1) - 1
	}
	return b.Text.Len()
}
