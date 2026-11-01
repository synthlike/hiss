package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
)

func TestEditsApplyUnlessHeld(t *testing.T) {
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")
	var root string
	done := make(chan bool)
	reqs <- func(ed *core.Editor) { root = ed.Root; done <- true }
	<-done

	res := call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "func main", "new_string": "func Main"})
	if got := text(res); got != "Applied [1] in "+filepath.Join(root, "main.go")+" and saved it." {
		t.Errorf("applied: %q", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "main.go")); !strings.Contains(string(data), "func Main") {
		t.Errorf("on disk: %s", data)
	}
	if got := text(call(t, cs, "await_review", map[string]any{"ids": []int{1}})); !strings.HasPrefix(got, "#1 ") || !strings.Contains(got, ": applied") {
		t.Errorf("await_review doesn't wait for an applied edit: %q", got)
	}

	res = call(t, cs, "write_file", map[string]any{"path": "new.txt", "content": "hello\n"})
	if !strings.HasPrefix(text(res), "Applied [2]") {
		t.Errorf("write_file: %q", text(res))
	}

	res = call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "package main", "new_string": "package x", "review": true, "rationale": "is this right?"})
	if !strings.HasPrefix(text(res), "Proposed [3]") {
		t.Errorf("review: %q", text(res))
	}
	res = call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "package main", "new_string": "package y", "supersedes": 3})
	if !strings.HasPrefix(text(res), "Proposed [4]") {
		t.Errorf("a revision stays in review: %q", text(res))
	}

	os.WriteFile(filepath.Join(root, ".hissrun"), []byte("[review]\nmatch = *.lock\n[sql]\nmatch = *.sql\ncmd = sh\n"), 0o600)
	res = call(t, cs, "write_file", map[string]any{"path": "a.lock", "content": "x\n"})
	if got := text(res); !strings.HasPrefix(got, "Held [5]") || !strings.Contains(got, "because a [review] rule in .hissrun matches it") {
		t.Errorf("held: %q", got)
	}
	res = call(t, cs, "write_file", map[string]any{"path": "q.sql", "content": "select 1\n", "run": true})
	if !strings.HasPrefix(text(res), "Proposed [6]") {
		t.Errorf("a run needs the user: %q", text(res))
	}
	if _, err := os.Stat(filepath.Join(root, "q.sql")); !os.IsNotExist(err) {
		t.Errorf("q.sql written before review: %v", err)
	}
}

func TestReadBufferSeesChangesOnDisk(t *testing.T) {
	cs, reqs := setup(t, "package main\n")
	var path string
	onEditor(reqs, func(ed *core.Editor) { path = ed.Buf.Path })
	os.WriteFile(path, []byte("package outside\n"), 0o600)
	if got := text(call(t, cs, "read_buffer", map[string]any{"path": "main.go"})); !strings.Contains(got, "package outside") {
		t.Errorf("read_buffer: %q", got)
	}
}
