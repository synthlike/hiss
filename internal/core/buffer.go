package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/synthlike/hiss/internal/text"
)

// Buffer is a file's contents as edited in hiss.
type Buffer struct {
	Path    string
	Text    text.Text
	Version int // bumped on every change
	saved   int // Version at the last load or save
}

// Open reads path into a buffer. A missing file gives an empty buffer.
func Open(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &Buffer{Path: path, Text: text.New(data, text.Origin{})}, nil
}

// Dirty reports whether the buffer has changes not written to disk.
func (b *Buffer) Dirty() bool { return b.Version != b.saved }

// Replace edits the buffer and returns the edit for mapping positions.
func (b *Buffer) Replace(start, end int, s []byte, o text.Origin) text.Edit {
	if start == end && len(s) == 0 {
		return text.Edit{Start: start, End: end}
	}
	b.Text = b.Text.Replace(start, end, s, o)
	b.Version++
	return text.Edit{Start: start, End: end, Len: len(s)}
}

// Save writes the buffer atomically, keeping the file's permissions.
func (b *Buffer) Save() error {
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

// LineEnd returns the offset of line's newline, or Len() on the last line.
func (b *Buffer) LineEnd(line int) int {
	if line+1 < b.Text.LineCount() {
		return b.Text.LineStart(line+1) - 1
	}
	return b.Text.Len()
}
