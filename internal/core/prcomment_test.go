package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiffRanges(t *testing.T) {
	diff := "diff --git a.txt a.txt\n--- a.txt\n+++ a.txt\n" +
		"@@ -1,4 +1,5 @@\n one\n-two\n+TWO\n+++ not a header\n three\n four\n" +
		"@@ -20,3 +21,0 @@\n-x\n-y\n-z\n" +
		"diff --git new.txt new.txt\n--- /dev/null\n+++ new.txt\n@@ -0,0 +1 @@\n+hi\n\\ No newline at end of file\n" +
		"diff --git gone.txt gone.txt\n--- gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
	want := map[string][]lineRange{"a.txt": {{1, 5}}, "new.txt": {{1, 1}}}
	if got := diffRanges(diff); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func readDrafts(t *testing.T, e *Editor) prDrafts {
	t.Helper()
	var d prDrafts
	data, err := os.ReadFile(filepath.Join(e.Root, ".hiss", "pr", "7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPRDraftComments(t *testing.T) {
	e, posted, _, head := prProject(t)
	e.execute("gc too early")
	if e.Msg != "gh-comment: no PR checked out here (:pr <n>)" {
		t.Errorf("msg %q", e.Msg)
	}
	e.openPR("7")
	(<-posted)(e)

	// The PR changes line 2, so GitHub shows lines 1-5.
	e.execute("gc shout less")
	if !strings.HasPrefix(e.Msg, "drafted a comment on line 2 for #7 (1 draft;") {
		t.Fatalf("msg %q", e.Msg)
	}
	e.setHead(e.Buf.Text.LineStart(8))
	e.execute("gc out of reach")
	if e.Msg != "gh-comment: line 9 of a.txt isn't in the PR's diff; GitHub only takes comments there" {
		t.Errorf("msg %q", e.Msg)
	}
	e.Sel.Anchor, e.Sel.Head = e.Buf.Text.LineStart(2), e.Buf.Text.LineStart(4)
	e.execute("gc these three")
	if !strings.HasPrefix(e.Msg, "drafted a comment on lines 3-5") {
		t.Errorf("msg %q", e.Msg)
	}
	e.Sel.Anchor, e.Sel.Head = e.Buf.Text.LineStart(3), e.Buf.Text.LineStart(5)
	e.execute("gc past the hunk")
	if !strings.Contains(e.Msg, "lines 4-6 of a.txt aren't in one hunk") {
		t.Errorf("msg %q", e.Msg)
	}

	d := readDrafts(t, e)
	want := []ghComment{
		{Path: "a.txt", Line: 2, Side: "RIGHT", Body: "shout less"},
		{Path: "a.txt", Line: 5, Side: "RIGHT", StartLine: 3, StartSide: "RIGHT", Body: "these three"},
	}
	if d.Head != head || !reflect.DeepEqual(d.Comments, want) {
		t.Errorf("drafts %+v", d)
	}
	if rows := e.TrailRows()[4]; len(rows) != 1 || !rows[0].GitHub || rows[0].Text != "github draft (3-5): these three" {
		t.Errorf("rows %+v", rows)
	}

	// They're kept across leaving and coming back.
	e.leavePR()
	if e.TrailRows() != nil {
		t.Errorf("drafts drawn after :pr-done: %v", e.TrailRows())
	}
	e.openPR("7")
	(<-posted)(e)
	if !strings.HasSuffix(e.Msg, "; 2 draft comments") || len(e.pr.Drafts) != 2 {
		t.Errorf("msg %q, %d drafts", e.Msg, len(e.pr.Drafts))
	}

	e.setHead(e.Buf.Text.LineStart(3))
	e.execute("gcd")
	if e.Msg != "dropped 1 draft; 1 draft left" {
		t.Errorf("msg %q", e.Msg)
	}
	e.execute("gcd")
	if e.Msg != "gh-comment-drop: no draft on this line" {
		t.Errorf("msg %q", e.Msg)
	}
	e.setHead(e.Buf.Text.LineStart(1))
	e.execute("gcd")
	if _, err := os.Stat(filepath.Join(e.Root, ".hiss", "pr", "7.json")); !os.IsNotExist(err) {
		t.Errorf("drafts file left with no drafts: %v", err)
	}
}

func TestPRCommentNeedsSavedBuffer(t *testing.T) {
	e, posted, _, _ := prProject(t)
	e.openPR("7")
	(<-posted)(e)
	e.Buf.Version++ // an unsaved edit
	e.execute("gc hi")
	if !strings.HasPrefix(e.Msg, "gh-comment: save or undo") {
		t.Errorf("msg %q", e.Msg)
	}
}
