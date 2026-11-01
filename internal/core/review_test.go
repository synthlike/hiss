package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDiff(t *testing.T) {
	diff := `diff --git a.go a.go
--- a.go
+++ a.go
@@ -2,2 +2,3 @@ func a() {
-old one
-old two
+new one
+new two
+new three
@@ -9 +10,0 @@ func b() {
--- a removed comment, not a header
@@ -0,0 +11,2 @@
+++ an added line, not a header
+last
\ No newline at end of file
diff --git gone.go gone.go
deleted file mode 100644
--- gone.go
+++ /dev/null
@@ -1 +0,0 @@
-x
diff --git b.go b.go
--- b.go
+++ "with space\ttab.go"
@@ -1,2 +1,3 @@
-x
+y
 same
+z
diff --git c.go c.go
--- c.go
+++ all of it.go	
@@ -1 +0,0 @@
-x
`
	got, deleted := parseDiff(diff)
	want := []diffHunk{
		{path: "a.go", line: 2, lines: 3, first: "new one", added: 3, removed: []string{"old one", "old two"}, start: 2,
			old: []string{"old one", "old two"}, new: []string{"new one", "new two", "new three"}},
		{path: "a.go", line: 10, removed: []string{"-- a removed comment, not a header"}, start: 11,
			old: []string{"-- a removed comment, not a header"}},
		{path: "a.go", line: 11, lines: 2, first: "++ an added line, not a header", added: 2, start: 11,
			new: []string{"++ an added line, not a header", "last"}},
		{path: "with space\ttab.go", line: 1, lines: 3, first: "y", added: 2, removed: []string{"x"}, start: 1,
			old: []string{"x", "same"}, new: []string{"y", "same", "z"}},
		{path: "all of it.go", line: 1, removed: []string{"x"}, start: 1, old: []string{"x"}},
	}
	if !reflect.DeepEqual(got, want) || deleted != 1 {
		t.Errorf("deleted %d, hunks\n got %+v\nwant %+v", deleted, got, want)
	}
}

const tenLines = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"

// gitRepo is a project with a.txt and gone.txt committed, and a function to
// commit a.txt with new content.
func gitRepo(t *testing.T) (e *Editor, git func(args ...string), commit func(content, msg string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	e = projectEditor(t, "")
	git = func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = e.Root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(e.Root, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commit = func(content, msg string) {
		t.Helper()
		write("a.txt", content)
		git("add", "-A")
		git("commit", "-qm", msg)
	}
	git("init", "-q")
	write("gone.txt", "bye\n")
	commit(tenLines, "first")
	return e, git, commit
}

func TestReviewCommit(t *testing.T) {
	e, _, commit := gitRepo(t)
	commit("one\nTWO\n2b\nthree\nfour\nfive\nsix\nseven\neight\nten\n", "second")
	keys(e, ": r v enter")
	if !strings.HasSuffix(e.Buf.Path, "a.txt") || e.line() != 1 {
		t.Fatalf("on %s line %d, msg %q", e.Buf.Path, e.line(), e.Msg)
	}
	if !strings.HasSuffix(e.Msg, " second: 2 hunks (]s to walk it, + - # on a hunk)") {
		t.Errorf("msg %q", e.Msg)
	}
	rows := e.TrailRows()[1]
	if len(rows) != 2 || rows[0].Text != "1. +2 -1" || !rows[1].Removed || rows[1].Text != "two" {
		t.Errorf("rows %+v", rows)
	}
	if got := e.ChangedLines(); len(got) != 2 || !got[1] || !got[2] {
		t.Errorf("changed lines %v", got)
	}
	keys(e, "] s")
	if e.line() != 8 { // "nine" went: the step is on the line above
		t.Errorf("second step on line %d", e.line())
	}
	if rows := e.TrailRows()[8]; len(rows) != 2 || rows[1].Text != "nine" {
		t.Errorf("rows %+v", rows)
	}
	if e.trail.Steps[0].Changed {
		t.Error("marked as changed since the commit")
	}
}

func TestReviewUncommittedFirst(t *testing.T) {
	e, _, _ := gitRepo(t)
	os.WriteFile(filepath.Join(e.Root, "a.txt"), []byte("ONE\n"+tenLines[4:]), 0o600)
	keys(e, ": r v enter")
	if !strings.HasPrefix(e.Msg, "uncommitted changes: 1 hunk") {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewRangeAndRevision(t *testing.T) {
	e, _, commit := gitRepo(t)
	second := "ONE\n" + tenLines[4:]
	commit(second, "second")
	commit(strings.Replace(second, "ten", "TEN", 1), "third")
	e.review("HEAD~2..HEAD")
	if !strings.HasPrefix(e.Msg, "HEAD~2..HEAD: 2 hunks") {
		t.Errorf("msg %q", e.Msg)
	}
	e.review("HEAD~")
	if !strings.Contains(e.Msg, " second: 1 hunk") || e.line() != 0 {
		t.Errorf("msg %q, line %d", e.Msg, e.line())
	}
}

func TestReviewMergeAgainstFirstParent(t *testing.T) {
	e, git, commit := gitRepo(t)
	git("checkout", "-qb", "fix")
	commit(strings.Replace(tenLines, "ten", "TEN", 1), "on the branch")
	git("checkout", "-q", "-")
	git("merge", "-q", "--no-ff", "-m", "merge it", "fix")
	e.review("HEAD")
	if !strings.Contains(e.Msg, " merge it: 1 hunk") {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewMarksDrift(t *testing.T) {
	e, _, commit := gitRepo(t)
	second := "ONE\n" + tenLines[4:]
	commit(second, "second")
	commit("zero\n"+second, "third") // the commit's line 1 isn't line 1 any more
	e.review("HEAD~")
	if !e.trail.Steps[0].Changed {
		t.Error("not marked as changed since the commit")
	}
}

func TestReviewCapsRemovedLines(t *testing.T) {
	e, _, commit := gitRepo(t)
	commit(strings.Repeat("x\n", 20), "first of many")
	commit("y\n", "replace them")
	e.review("")
	rows := e.TrailRows()[0]
	if len(rows) != maxRemoved+2 || rows[len(rows)-1].Text != "… 8 more" {
		t.Errorf("%d rows, last %+v", len(rows), rows[len(rows)-1])
	}
}

func TestReviewProblems(t *testing.T) {
	e, git, _ := gitRepo(t)
	for rev, want := range map[string]string{
		"--output=x": "review: --output=x isn't a revision",
		"nosuchrev":  "review: git can't show nosuchrev",
	} {
		e.review(rev)
		if e.Msg != want {
			t.Errorf("%s: msg %q", rev, e.Msg)
		}
	}
	if _, err := os.Stat(filepath.Join(e.Root, "x")); !os.IsNotExist(err) {
		t.Errorf("--output wrote a file: %v", err)
	}
	git("rm", "-q", "gone.txt")
	git("commit", "-qm", "only a deletion")
	e.review("")
	if !strings.HasSuffix(e.Msg, " only a deletion but 1 deleted file") {
		t.Errorf("msg %q", e.Msg)
	}
	e.Root = ""
	e.review("")
	if e.Msg != "review: no project" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewPathsWithin(t *testing.T) {
	e, git, _ := gitRepo(t)
	sub := filepath.Join(e.Root, "my project")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "b c.txt"), []byte("a\n"), 0o600)
	git("add", "-A")
	git("commit", "-qm", "add it")
	os.WriteFile(filepath.Join(sub, "b c.txt"), []byte("b\n"), 0o600)
	os.WriteFile(filepath.Join(e.Root, "a.txt"), []byte("outside the project\n"), 0o600)
	e.Root = sub
	e.review("")
	if !strings.HasPrefix(e.Msg, "uncommitted changes: 1 hunk") || e.Buf.Path != filepath.Join(sub, "b c.txt") {
		t.Errorf("on %s, msg %q", e.Buf.Path, e.Msg)
	}
}

func TestReviewCommentsReachTheAgent(t *testing.T) {
	e, _, _ := gitRepo(t)
	os.WriteFile(filepath.Join(e.Root, "a.txt"), []byte("ONE\n"+tenLines[4:]), 0o600)
	e.review("")
	e.comment("why?")
	if e.Msg != "commented on step 1; no agent is here to see it" {
		t.Errorf("msg %q", e.Msg)
	}
	turn(e, "s")
	e.review("")
	e.comment("why?")
	ds := e.SessionComments("s")
	if len(ds) != 1 || ds[0].Review != "uncommitted changes" || ds[0].Text != "why?" {
		t.Errorf("delivered %+v", ds)
	}
}

// changed is the repository with two hunks uncommitted in a.txt, far apart,
// and a review of them on the first.
func changed(t *testing.T) (*Editor, string) {
	t.Helper()
	e, _, _ := gitRepo(t)
	path := filepath.Join(e.Root, "a.txt")
	os.WriteFile(path, []byte(strings.Replace(strings.Replace(tenLines, "two\n", "TWO\nmore\n", 1), "nine\n", "", 1)), 0o600)
	turn(e, "s")
	e.review("")
	if e.Msg == "" || e.trail == nil || len(e.trail.Steps) != 2 {
		t.Fatalf("msg %q, trail %+v", e.Msg, e.trail)
	}
	return e, path
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReviewAcceptRejectFinish(t *testing.T) {
	e, path := changed(t)
	before := fileText(t, path)
	keys(e, "+") // accepted, nothing changes, on to the next
	if fileText(t, path) != before || e.trail.Steps[0].Verdict != Accepted || e.line() != e.trail.Steps[1].Line() {
		t.Errorf("accept: line %d, verdict %q", e.line(), e.trail.Steps[0].Verdict)
	}
	keys(e, "- n o p e enter") // rejected: the deleted line is back, and that was the last
	if got := fileText(t, path); !strings.Contains(got, "nine\n") || !strings.Contains(got, "TWO\n") {
		t.Errorf("reject: %q", got)
	}
	if !e.trail.finished {
		t.Error("deciding the last hunk doesn't finish the review")
	}
	ds := e.SessionComments("s")
	if len(ds) != 2 || !strings.HasSuffix(ds[0].Text, "the old text is back: nope") {
		t.Fatalf("delivered %+v", ds)
	}
	r := ds[1].Report
	if !strings.Contains(r, "1 accepted, 1 rejected, 0 not reviewed") || !strings.Contains(r, "rejected a.txt:") || !strings.HasSuffix(r, ": nope\n") {
		t.Errorf("report %q", r)
	}
	if len(e.SessionComments("s")) != 0 {
		t.Error("told twice")
	}
	keys(e, ": r f enter")
	if e.Msg != "this review is finished" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewRejectRestoresExactly(t *testing.T) {
	noFinal := strings.TrimSuffix(tenLines, "\n")
	for name, c := range map[string][2]string{"gains a newline": {noFinal, tenLines}} {
		e, _, commit := gitRepo(t)
		commit(c[0], "without")
		path := filepath.Join(e.Root, "a.txt")
		os.WriteFile(path, []byte(c[1]), 0o600)
		e.review("")
		keys(e, "- enter")
		if got := fileText(t, path); got != c[0] {
			t.Errorf("%s: %q", name, got)
		}
	}
	for name, src := range map[string]string{
		"replace":  strings.Replace(tenLines, "five\n", "FIVE\nmore\n", 1),
		"insert":   strings.Replace(tenLines, "five\n", "five\nmore\n", 1),
		"delete":   strings.Replace(tenLines, "five\n", "", 1),
		"top":      strings.TrimPrefix(tenLines, "one\n"),
		"bottom":   strings.TrimSuffix(tenLines, "ten\n"),
		"no final": strings.TrimSuffix(tenLines, "\n"),
		"merged":   strings.Replace(strings.Replace(tenLines, "two\n", "", 1), "four\n", "FOUR\n", 1),
	} {
		e, _, _ := gitRepo(t)
		path := filepath.Join(e.Root, "a.txt")
		os.WriteFile(path, []byte(src), 0o600)
		e.review("")
		for e.trail != nil && !e.trail.finished {
			keys(e, "- enter")
		}
		if got := fileText(t, path); got != tenLines {
			t.Errorf("%s: %q; %s", name, got, e.Msg)
		}
	}
}

func TestReviewRejectLeavesChangedText(t *testing.T) {
	e, path := changed(t)
	keys(e, "i x esc : w enter")
	keys(e, "- enter")
	s := e.trail.Steps[0]
	if s.Reverted || !strings.Contains(e.Msg, "changed since") || !strings.Contains(fileText(t, path), "xTWO") {
		t.Errorf("reverted %v, msg %q", s.Reverted, e.Msg)
	}
	if !strings.Contains(s.Thread[0].Text, "still in place") {
		t.Errorf("told %q", s.Thread[0].Text)
	}
}

func TestReviewCreatedFileRejected(t *testing.T) {
	e, git, _ := gitRepo(t)
	path := filepath.Join(e.Root, "new.txt")
	os.WriteFile(path, []byte("a\nb\n"), 0o600)
	git("add", "-A")
	git("commit", "-qm", "add it")
	e.review("")
	keys(e, "- enter")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat: %v, msg %q", err, e.Msg)
	}
}

func TestReviewRefreshesFromDisk(t *testing.T) {
	e, _, commit := gitRepo(t)
	path := filepath.Join(e.Root, "a.txt")
	e.openFile(path) // open, then changed behind hiss's back
	commit("ONE\n"+tenLines[4:], "second")
	e.review("")
	if e.trail == nil || e.trail.Steps[0].Changed || e.Buf.Dirty() {
		t.Fatalf("msg %q, trail %+v", e.Msg, e.trail)
	}
	os.WriteFile(path, []byte("ONE\n"+strings.Replace(tenLines[4:], "ten", "outside", 1)), 0o600) // and again
	keys(e, "- enter")
	if got := fileText(t, path); !strings.HasPrefix(got, "one\n") || !strings.Contains(got, "outside") {
		t.Errorf("reject lost the change on disk: %q", got)
	}
}

func TestReviewFinishEarly(t *testing.T) {
	e, _ := changed(t)
	keys(e, "+")
	keys(e, ": r f space o k enter")
	if e.Msg != "finished the review: 1 accepted, 0 rejected, 1 not reviewed" {
		t.Errorf("msg %q", e.Msg)
	}
	ds, done := e.TakeReview()
	if !done || len(ds) != 1 || !strings.HasSuffix(ds[0].Report, "They said: ok\n") {
		t.Errorf("done %v, %+v", done, ds)
	}
	keys(e, "+")
	if e.Msg != "this review is finished" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewSaveAndLoad(t *testing.T) {
	e, _ := changed(t)
	keys(e, "- n o p e enter") // a verdict and a comment, delivered
	e.SessionComments("s")
	e.comment("and this?") // not delivered yet
	file := filepath.Join(t.TempDir(), "r.json")
	keys(e, ": r s space")
	e.Cmd = "rs " + file
	keys(e, "enter")
	data := fileText(t, file)
	for _, want := range []string{`"kind": "review"`, `"version": 1`, `"old": [`, `"delivered": true`, `"commit": "`} {
		if !strings.Contains(data, want) {
			t.Errorf("file lacks %s:\n%s", want, data)
		}
	}
	e.clearTrail()
	e.loadTrail(file)
	if !strings.Contains(e.Msg, "holds a review: :rl loads it") {
		t.Errorf(":tl: %q", e.Msg)
	}
	e.loadReview(file)
	tr := e.trail
	if tr == nil || !tr.review || tr.Steps[0].Verdict != Rejected || len(tr.Steps[1].Thread) != 1 {
		t.Fatalf("loaded %+v, msg %q", tr, e.Msg)
	}
	ds := e.SessionComments("s")
	if len(ds) != 1 || ds[0].Text != "and this?" {
		t.Errorf("after loading, delivered %+v", ds)
	}
	e.goToStep(1) // the rest of the review still works
	keys(e, "+")
	if !tr.finished {
		t.Error("not finished")
	}
}

func TestReviewLoadWithFileGone(t *testing.T) {
	e, _, _ := gitRepo(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "r.json")
	os.WriteFile(file, []byte(`{"kind":"review","version":1,"title":"old","steps":[
		{"path":"a.txt","line":1,"text":"one","old":["x"],"new":["one"]},
		{"path":"nope.txt","line":1,"text":"x"}]}`), 0o600)
	e.loadReview(file)
	if !strings.Contains(e.Msg, "files gone: nope.txt") || len(e.trail.Steps) != 1 {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestTrailFileEnvelope(t *testing.T) {
	e := projectEditor(t, "a\n")
	file := filepath.Join(t.TempDir(), "t.json")
	os.WriteFile(file, []byte(`{"title":"from before","steps":[{"path":"f.txt","line":1,"text":"a"}]}`), 0o600)
	e.loadTrail(file)
	if e.trail == nil || !strings.HasPrefix(e.Msg, "loaded from before") {
		t.Fatalf("msg %q", e.Msg)
	}
	e.saveWalk(file, kindTrail)
	if data := fileText(t, file); !strings.Contains(data, `"kind": "trail"`) {
		t.Errorf("saved %s", data)
	}
	os.WriteFile(file, []byte(`{"kind":"trail","version":2,"title":"t","steps":[{"path":"f.txt"}]}`), 0o600)
	e.loadTrail(file)
	if !strings.Contains(e.Msg, "version 2 is newer") {
		t.Errorf("msg %q", e.Msg)
	}
	e.saveReview(file)
	if e.Msg != "no review to save" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewDecidesAHunkOnce(t *testing.T) {
	e, path := changed(t)
	keys(e, "+")
	e.goToStep(0)
	before := fileText(t, path)
	keys(e, "- enter")
	if e.Msg != "hunk 1 is already accepted" || fileText(t, path) != before {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestReviewNotesLandOnTheirHunk(t *testing.T) {
	e, _, _ := gitRepo(t)
	path := filepath.Join(e.Root, "a.txt")
	os.WriteFile(path, []byte(strings.Replace(strings.Replace(tenLines, "two\n", "TWO\nmore\n", 1), "nine\n", "", 1)), 0o600)
	n, unmatched, err := e.ShowReview("claude", "t", "", nil, []StepInput{
		{Path: path, Line: 3, Note: "mid-hunk"}, // the hunk's second line
		{Path: path, Line: 6, Note: "between hunks"},
		{Path: filepath.Join(e.Root, "other.txt"), Line: 2, Note: "another file"},
	})
	if err != nil || n != 2 || unmatched != 2 {
		t.Fatalf("%d hunks, %d unmatched, %v", n, unmatched, err)
	}
	if got := e.trail.Steps[0].Note; got != "+2 -1: mid-hunk" {
		t.Errorf("note %q", got)
	}
}

func TestLoadedFinishedReviewIsNotReportedAgain(t *testing.T) {
	e, _ := changed(t)
	keys(e, ": r f enter")
	e.SessionComments("s")
	file := filepath.Join(t.TempDir(), "r.json")
	e.saveReview(file)
	e.loadReview(file)
	if ds := e.SessionComments("s"); len(ds) != 0 || !e.trail.finished {
		t.Errorf("finished %v, delivered %+v", e.trail.finished, ds)
	}
}
