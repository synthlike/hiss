package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// prWithDraft checks out PR 7 with a draft on line 2 and returns where the
// fake gh api writes.
func prWithDraft(t *testing.T) (e *Editor, posted chan func(*Editor), head, apiDir string) {
	t.Helper()
	e, posted, _, head = prProject(t)
	apiDir = t.TempDir()
	t.Setenv("GH_API_DIR", apiDir)
	e.openPR("7")
	(<-posted)(e)
	e.execute("gc shout less")
	return e, posted, head, apiDir
}

func TestPRSubmit(t *testing.T) {
	e, posted, head, apiDir := prWithDraft(t)
	e.execute("prs changes")
	if e.Msg != "pr-submit: say what to change: :prs changes <text>" {
		t.Errorf("msg %q", e.Msg)
	}

	e.execute("prs approve nice work")
	if e.Msg != "pr-submit: checking #7 for new pushes…" {
		t.Fatalf("msg %q", e.Msg)
	}
	e.openPR("7")
	if !strings.Contains(e.Msg, "still busy") {
		t.Errorf("msg %q", e.Msg)
	}
	(<-posted)(e)
	if e.Menu == nil || e.Menu.Title != "send the review to #7: approve, 1 comment, with a note?" {
		t.Fatalf("menu %+v", e.Menu)
	}
	keys(e, "y")
	(<-posted)(e)
	if want := "approved on #7 with 1 comment: https://github.com/o/r/pull/7#pullrequestreview-1"; e.Msg != want {
		t.Errorf("msg %q, want %q", e.Msg, want)
	}

	args, _ := os.ReadFile(filepath.Join(apiDir, "args"))
	if got := strings.TrimSpace(string(args)); got != "api --method POST repos/o/r/pulls/7/reviews --input -" {
		t.Errorf("gh %s", got)
	}
	var r ghReview
	data, _ := os.ReadFile(filepath.Join(apiDir, "input"))
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	want := ghReview{CommitID: head, Event: "APPROVE", Body: "nice work",
		Comments: []ghComment{{Path: "a.txt", Line: 2, Side: "RIGHT", Body: "shout less"}}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("sent %+v", r)
	}
	if len(e.pr.Drafts) != 0 || e.prBusy {
		t.Errorf("%d drafts left, busy %v", len(e.pr.Drafts), e.prBusy)
	}
	if _, err := os.Stat(filepath.Join(e.Root, ".hiss", "pr", "7.json")); !os.IsNotExist(err) {
		t.Errorf("drafts file kept: %v", err)
	}
	e.execute("prs")
	if !strings.HasPrefix(e.Msg, "pr-submit: nothing to send") {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestPRSubmitFailureKeepsDrafts(t *testing.T) {
	e, posted, _, apiDir := prWithDraft(t)
	os.WriteFile(filepath.Join(apiDir, "fail"), nil, 0o600)
	e.execute("prs")
	(<-posted)(e)
	keys(e, "y")
	(<-posted)(e)
	want := "pr-submit: sending the review to #7 failed: gh: Validation Failed (HTTP 422): Line could not be resolved; the drafts are kept"
	if e.Msg != want {
		t.Errorf("msg %q, want %q", e.Msg, want)
	}
	if len(e.pr.Drafts) != 1 || e.prBusy {
		t.Errorf("%d drafts, busy %v", len(e.pr.Drafts), e.prBusy)
	}
}

func TestPRSubmitDeclinedOrMoved(t *testing.T) {
	e, posted, _, apiDir := prWithDraft(t)
	e.pr.Head = strings.Repeat("ab", 20) // as if the author pushed after the checkout
	e.execute("prs looks fine")
	(<-posted)(e)
	if want := "#7 has new pushes since abababa, which you reviewed; send anyway: comment, 1 comment, with a note?"; e.Menu == nil || e.Menu.Title != want {
		t.Fatalf("menu %+v", e.Menu)
	}
	keys(e, "n")
	if e.Msg != "pr-submit: not sent; the drafts are kept" || e.prBusy || len(e.pr.Drafts) != 1 {
		t.Errorf("msg %q, busy %v", e.Msg, e.prBusy)
	}
	if _, err := os.Stat(filepath.Join(apiDir, "args")); !os.IsNotExist(err) {
		t.Error("sent after declining")
	}
}
