package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

// Go builds as if files had other contents when given -overlay: a JSON file
// mapping each path to a file to read instead. WriteOverlay writes the open
// files that differ from disk, as they'd be with their pending proposals
// accepted, so an agent can run go vet and go test on its proposals before
// the user reviews them. It's a snapshot: later proposals need a new one.

// overlayDir holds the overlay, under the project root. Go skips directories
// starting with a dot, so ./... doesn't find the copies.
const overlayDir = ".hiss/overlay"

// Overlay is what WriteOverlay wrote.
type Overlay struct {
	Path      string   // the file to pass to -overlay
	Files     []string // the files it replaces or adds, relative to the root when under it
	Proposals []int    // the pending proposals applied
}

// WriteOverlay writes buffers with pending proposals or unsaved changes, with
// the proposals applied, and an overlay file naming them.
func (e *Editor) WriteOverlay() (Overlay, error) {
	if e.Root == "" {
		return Overlay{}, errors.New("no project root to write the overlay under")
	}
	dir := filepath.Join(e.Root, overlayDir)
	if err := os.RemoveAll(dir); err != nil {
		return Overlay{}, err
	}
	o := Overlay{Path: filepath.Join(dir, "overlay.json")}
	replace := map[string]string{}
	for _, b := range e.Bufs {
		if b.Path == "" || b.output || len(b.pending) == 0 && !b.Dirty() {
			continue
		}
		name, ok := e.underRoot(b.Path)
		if !ok {
			name = filepath.Join("outside", b.Path)
		}
		backing := filepath.Join(dir, "files", name)
		if err := os.MkdirAll(filepath.Dir(backing), 0o755); err != nil {
			return Overlay{}, err
		}
		if err := os.WriteFile(backing, b.shadow().text.Bytes(), 0o600); err != nil {
			return Overlay{}, err
		}
		replace[b.Path] = backing
		o.Files = append(o.Files, name)
		for _, p := range b.pending {
			o.Proposals = append(o.Proposals, p.ID)
		}
	}
	slices.Sort(o.Files)
	slices.Sort(o.Proposals)
	data, err := json.MarshalIndent(map[string]any{"Replace": replace}, "", "\t")
	if err != nil {
		return Overlay{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Overlay{}, err
	}
	return o, os.WriteFile(o.Path, append(data, '\n'), 0o600)
}
