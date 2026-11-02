package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prProject makes a project whose origin, https://github.com/o/r, is a local bare
// repository holding PR 7: it changes a.txt from the base branch, which has
// since moved on in b.txt. A fake gh describes the PR. It returns the editor,
// the posted callbacks, the base branch's name and the PR's head.
func prProject(t *testing.T) (e *Editor, posted chan func(*Editor), branch, head string) {
	t.Helper()
	e, git, commit := gitRepo(t)
	out := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = e.Root
		b, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(b))
	}
	branch = out("symbolic-ref", "--short", "HEAD")
	bare := filepath.Join(t.TempDir(), "r.git")
	git("init", "-q", "--bare", bare)
	git("config", "url."+bare+".insteadOf", "https://github.com/o/r.git")
	git("remote", "add", "origin", "https://github.com/o/r.git")

	commit("one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "the PR")
	head = out("rev-parse", "HEAD")
	git("push", "-q", "origin", "HEAD:refs/pull/7/head")
	git("reset", "-q", "--hard", "HEAD~")
	os.WriteFile(filepath.Join(e.Root, "b.txt"), []byte("later\n"), 0o600)
	git("add", "b.txt")
	git("commit", "-qm", "base moves on")
	git("push", "-q", "origin", branch)
	base := out("rev-parse", "HEAD")
	git("checkout", "-q", "-b", "mine")

	bin := t.TempDir()
	js := fmt.Sprintf(`{"number":7,"title":"Shout two","url":"https://github.com/o/r/pull/7","baseRefName":%q,"baseRefOid":%q,"headRefOid":%q}`, branch, base, head)
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = 'pr view 7' ] || { echo 'no such PR' >&2; exit 1; }\necho '" + js + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	posted = make(chan func(*Editor), 1)
	e.Post = func(f func(*Editor)) { posted <- f }
	return e, posted, branch, head
}

func headOf(t *testing.T, e *Editor) string {
	t.Helper()
	s, _ := git(e.Root, "rev-parse", "HEAD")
	return s
}

func TestPRCheckoutAndLeave(t *testing.T) {
	e, posted, _, head := prProject(t)
	keys(e, ": p r space 7 enter")
	if e.Msg != "pr: fetching #7…" {
		t.Fatalf("msg %q", e.Msg)
	}
	(<-posted)(e)
	if got := headOf(t, e); got != head {
		t.Fatalf("HEAD %s, want the PR's %s; msg %q", got, head, e.Msg)
	}
	if want := "#7 Shout two: 1 hunk (]s to walk it, :pr-done when finished)"; e.Msg != want {
		t.Errorf("msg %q, want %q", e.Msg, want)
	}
	// Only the PR's change: b.txt came later on the base, past the merge base.
	if len(e.trail.Steps) != 1 || !strings.HasSuffix(e.Buf.Path, "a.txt") || e.line() != 1 {
		t.Errorf("%d steps, on %s line %d", len(e.trail.Steps), e.Buf.Path, e.line())
	}
	if e.pr.Back != "mine" {
		t.Errorf("back to %q", e.pr.Back)
	}
	if got := e.Buf.Text.String(); !strings.HasPrefix(got, "one\nTWO\n") {
		t.Errorf("buffer %q", got)
	}

	keys(e, ": p r d enter")
	if b, _ := git(e.Root, "symbolic-ref", "--short", "HEAD"); b != "mine" {
		t.Errorf("on %q after :pr-done; msg %q", b, e.Msg)
	}
	if e.Msg != "left #7; back on mine" || e.trail != nil || e.pr != nil {
		t.Errorf("msg %q, trail %v, pr %v", e.Msg, e.trail, e.pr)
	}
	if got := e.Buf.Text.String(); got != tenLines {
		t.Errorf("buffer not reread: %q", got)
	}
}

func TestPRFlagLeavesTheCode(t *testing.T) {
	e, posted, _, _ := prProject(t)
	e.openPR("7")
	(<-posted)(e)
	before := e.Buf.Text.String()
	keys(e, "- t o o space l o u d enter")
	s := e.trail.Steps[0]
	if got := fileText(t, e.Buf.Path); got != before || e.Buf.Text.String() != before || s.Reverted {
		t.Errorf("flagging changed the code: %q", got)
	}
	if s.Verdict != Rejected || e.Msg != "flagged hunk 1; finished the review: 0 viewed, 1 flagged, 0 not reviewed" {
		t.Errorf("verdict %q, msg %q", s.Verdict, e.Msg)
	}
	if r := e.trail.report(e); !strings.Contains(r, "  flagged a.txt:2: too loud\n") || strings.Contains(r, "still in place") {
		t.Errorf("report %q", r)
	}
	if err := e.prReady(); err != nil { // so :pr-done can go back
		t.Errorf("tree not clean: %v", err)
	}
}

func TestPRNeedsACleanTree(t *testing.T) {
	e, _, _, _ := prProject(t)
	os.WriteFile(filepath.Join(e.Root, "a.txt"), []byte("mine\n"), 0o600)
	e.openPR("7")
	if e.Msg != "pr: uncommitted changes: commit or stash them first" || e.prBusy {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestPRTreeDirtiedDuringFetch(t *testing.T) {
	e, posted, _, _ := prProject(t)
	before := headOf(t, e)
	e.openPR("#7")
	os.WriteFile(filepath.Join(e.Root, "a.txt"), []byte("mine\n"), 0o600)
	(<-posted)(e)
	if headOf(t, e) != before || !strings.HasPrefix(e.Msg, "pr #7: fetched, but uncommitted changes") {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestPRErrors(t *testing.T) {
	e, posted, _, _ := prProject(t)
	e.openPR("x")
	if e.Msg != "pr: give a PR number" {
		t.Errorf("msg %q", e.Msg)
	}
	e.openPR("8")
	(<-posted)(e)
	if e.Msg != "pr #8: gh: no such PR" || e.prBusy || e.pr != nil {
		t.Errorf("msg %q", e.Msg)
	}
	e.leavePR()
	if e.Msg != "pr-done: no PR checked out here" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestRemoteRepo(t *testing.T) {
	for _, s := range []string{
		"https://github.com/O/r.git",
		"https://user@github.com/o/r/",
		"ssh://git@github.com:22/o/r.git",
		"git@github.com:o/r.git",
		"github.com:o/r",
	} {
		if got := remoteRepo(s); got != "github.com/o/r" {
			t.Errorf("remoteRepo(%q) = %q", s, got)
		}
	}
	if got, err := prRepo("https://GitHub.com/o/r/pull/7"); got != "github.com/o/r" || err != nil {
		t.Errorf("prRepo = %q, %v", got, err)
	}
}
