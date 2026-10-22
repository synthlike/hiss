package core

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxFiles caps the file picker for very large trees.
const maxFiles = 50000

// projectFiles lists files under root, relative to it: git's view (tracked
// plus untracked, not ignored) in a repository, otherwise a walk that skips
// hidden directories.
func projectFiles(root string) []string {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		s := strings.TrimRight(string(out), "\n")
		if s == "" {
			return nil
		}
		files := strings.Split(s, "\n")
		return files[:min(len(files), maxFiles)]
	}
	var files []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir() && p != root && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.IsDir():
			return nil
		case len(files) >= maxFiles:
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, rel)
		return nil
	})
	return files
}

// openFilePicker lists the project's files; enter opens one.
func (e *Editor) openFilePicker() {
	if e.Root == "" {
		e.Msg = "no project root"
		return
	}
	p := &Picker{Title: "files"}
	for _, f := range projectFiles(e.Root) {
		p.Items = append(p.Items, PickItem{Label: f, Run: func(e *Editor) { e.openFile(filepath.Join(e.Root, f)) }})
	}
	if len(p.Items) == 0 {
		e.Msg = "no files under " + e.Root
		return
	}
	e.Picker = p
}

// openBufferPicker lists open buffers; enter switches to one.
func (e *Editor) openBufferPicker() {
	p := &Picker{Title: "buffers"}
	for _, b := range e.Bufs {
		var detail []string
		if b.Dirty() {
			detail = append(detail, "[+]")
		}
		if n := len(b.pending); n > 0 {
			detail = append(detail, fmt.Sprintf("%d pending", n))
		}
		p.Items = append(p.Items, PickItem{Label: b.Name, Detail: strings.Join(detail, " "),
			Run: func(e *Editor) { e.SwitchTo(b) }})
	}
	e.Picker = p
}
