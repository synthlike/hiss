package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// notesFile is where :note appends, relative to the project root.
const notesFile = ".hiss/notes.md"

// note appends a line to the notes file: the date, the commit, the cursor's
// path:line and text. The commit is HEAD's short hash, with "+" when the file
// differs from it, so the line can be found again with git show.
func (e *Editor) note(text string) {
	if e.Root == "" {
		e.Msg = "no project root"
		return
	}
	fields := []string{e.now().Format("2006-01-02")}
	path := e.Buf.Path
	if rel, ok := e.underRoot(path); ok {
		path = rel
	}
	if c := noteCommit(e.Root, path, e.Buf.Dirty()); c != "" {
		fields = append(fields, c)
	}
	if path != "" {
		fields = append(fields, fmt.Sprintf("%s:%d", path, e.line()+1))
	}
	fields = append(fields, text)
	file := filepath.Join(e.Root, notesFile)
	err := os.MkdirAll(filepath.Dir(file), 0o755)
	if err == nil {
		err = appendLine(file, strings.Join(fields, "  "))
	}
	if err != nil {
		e.Msg = failed("writing "+notesFile, err)
		return
	}
	e.Msg = "noted in " + notesFile
}

func appendLine(file, line string) error {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// noteCommit returns HEAD's short hash in root's repository, with "+" when
// path (relative to root, or "") is unsaved, changed, staged or untracked.
// It returns "" outside a repository or before the first commit.
func noteCommit(root, path string, dirty bool) string {
	hash, err := git(root, "rev-parse", "--short", "HEAD")
	if err != nil || hash == "" {
		return ""
	}
	if path == "" {
		return hash
	}
	if dirty {
		return hash + "+"
	}
	if st, err := git(root, "status", "--porcelain", "--", path); err != nil || st != "" {
		return hash + "+"
	}
	return hash
}

// git runs git in dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// noteLine is one line of the notes file, split into the fields :note writes.
// Lines edited by hand keep whatever fields still parse; the rest is text.
type noteLine struct {
	date, commit, path string
	line               int // 1-based; 0 without a location
	text               string
}

func parseNote(s string) noteLine {
	fs := strings.Split(s, "  ")
	var n noteLine
	if len(fs) > 1 {
		n.date, fs = fs[0], fs[1:]
	}
	if len(fs) > 1 && isCommit(fs[0]) {
		n.commit, fs = fs[0], fs[1:]
	}
	if len(fs) > 1 {
		if i := strings.LastIndexByte(fs[0], ':'); i > 0 {
			if l, err := strconv.Atoi(fs[0][i+1:]); err == nil && l > 0 {
				n.path, n.line, fs = fs[0][:i], l, fs[1:]
			}
		}
	}
	n.text = strings.Join(fs, "  ")
	return n
}

// isCommit reports whether s looks like a short hash, optionally with "+".
func isCommit(s string) bool {
	s = strings.TrimSuffix(s, "+")
	return len(s) >= 4 && strings.Trim(s, "0123456789abcdef") == ""
}

// openNotePicker lists the notes file, newest first; enter goes to a note's
// line as the file is now, which may have drifted since the note's commit.
func (e *Editor) openNotePicker() {
	if e.Root == "" {
		e.Msg = "no project root"
		return
	}
	data, err := os.ReadFile(filepath.Join(e.Root, notesFile))
	if err != nil && !os.IsNotExist(err) {
		e.Msg = failed("reading "+notesFile, err)
		return
	}
	p := &Picker{Title: "notes"}
	for _, s := range slices.Backward(strings.Split(string(data), "\n")) {
		if strings.TrimSpace(s) == "" {
			continue
		}
		n := parseNote(s)
		label := n.date
		if n.line > 0 {
			label = fmt.Sprintf("%s:%d", n.path, n.line)
		}
		p.Items = append(p.Items, PickItem{Label: label, Detail: n.text, Run: func(e *Editor) { e.goToNote(n) }})
	}
	if len(p.Items) == 0 {
		e.Msg = "no notes yet (:n <text> to add one)"
		return
	}
	e.Picker = p
}

func (e *Editor) goToNote(n noteLine) {
	if n.line == 0 {
		e.Msg = "this note has no location"
		return
	}
	path := n.path
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.Root, path)
	}
	if _, err := os.Stat(path); err != nil && e.Find(path) == nil {
		e.Msg = n.path + " doesn't exist any more"
		return
	}
	e.openAt(path, n.line, 0)
}
