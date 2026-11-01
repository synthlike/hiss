package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/core"
)

func TestShowReview(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	pollInterval = 10 * time.Millisecond
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")
	var root string
	onEditor(reqs, func(ed *core.Editor) { root = ed.Root })
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	os.WriteFile(filepath.Join(root, "other.go"), []byte("package main\n"), 0o600)
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "first")
	call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "func main() {}", "new_string": "func main() { run() }"})
	call(t, cs, "propose_edit", map[string]any{"path": "other.go", "old_string": "package main", "new_string": "package other"})

	res := call(t, cs, "show_review", map[string]any{"title": "the run call", "paths": []string{"main.go"},
		"notes": []map[string]any{{"path": "main.go", "line": 3, "note": "is this safe?"}, {"path": "main.go", "line": 1, "note": "stray"}}})
	if got := text(res); !strings.HasPrefix(got, "Sent 1 hunk for review.") || !strings.HasSuffix(got, "1 note matched no hunk.") {
		t.Errorf("show_review: %q", got)
	}
	onEditor(reqs, func(ed *core.Editor) {
		if toast := ed.ActiveToast(); toast == nil || !strings.Contains(toast.Text, "sent a review: the run call, 1 hunk") {
			t.Errorf("toast %+v", toast)
		}
		s := ed.Trail().Steps[0]
		if s.Note != "+1 -1: is this safe?" || s.Buf.Path != filepath.Join(root, "main.go") {
			t.Errorf("step %+v", s)
		}
	})

	res = call(t, cs, "await_review", map[string]any{"review": true, "timeout_seconds": 1})
	if got := text(res); !strings.HasPrefix(got, "Still waiting") {
		t.Errorf("before the user acts: %q", got)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		onEditor(reqs, func(ed *core.Editor) {
			for _, k := range []string{"]", "s", "-", "n", "o", "enter"} {
				ed.HandleKey(k)
			}
		})
	}()
	res = call(t, cs, "await_review", map[string]any{"review": true})
	got := text(res)
	for _, want := range []string{"the old text is back: no", "The user finished the review of the run call: 0 accepted, 1 rejected, 0 not reviewed."} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q in %q", want, got)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(root, "main.go")); !strings.Contains(string(data), "func main() {}") {
		t.Errorf("not reverted: %s", data)
	}
	res = call(t, cs, "await_review", map[string]any{"review": true})
	if got := text(res); got != "The review is over.\n" {
		t.Errorf("after: %q", got)
	}
	res = call(t, cs, "show_review", map[string]any{"title": "x", "rev": "--output=y"})
	if !res.IsError || !strings.Contains(text(res), "isn't a revision") {
		t.Errorf("bad rev: %q", text(res))
	}
}
