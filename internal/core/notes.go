package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		e.Msg = err.Error()
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
