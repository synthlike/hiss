package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// :pr <n> reviews a GitHub pull request in place: it fetches the PR's head and
// base from the remote that points at the PR's repository, checks the head out
// detached and walks merge-base..head as a review. The tree must be clean, so
// nothing of the user's is in the way; :pr-done goes back to where :pr started.
// gh finds the PR, so its login and default repository apply.

// pullRequest is what :pr knows about the PR it checked out.
type pullRequest struct {
	N     int
	Title string
	URL   string
	Head  string // the commit checked out
	Back  string // what :pr-done checks out: the branch, or commit, :pr started from
}

// prView is the part of gh pr view's JSON that :pr reads.
type prView struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	BaseRefName string `json:"baseRefName"`
	BaseRefOid  string `json:"baseRefOid"`
	HeadRefOid  string `json:"headRefOid"`
}

// fetchedPR is what the background fetch hands back to the editor.
type fetchedPR struct {
	view prView
	base string // the merge base of the PR's base and head
}

// openPR starts fetching PR arg in the background (:pr).
func (e *Editor) openPR(arg string) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	switch {
	case err != nil || n <= 0:
		e.Msg = "pr: give a PR number"
		return
	case e.prBusy:
		e.Msg = "pr: already fetching one"
		return
	}
	if err := e.prReady(); err != nil {
		e.Msg = "pr: " + err.Error()
		return
	}
	e.prBusy = true
	e.Msg = fmt.Sprintf("pr: fetching #%d…", n)
	root := e.Root
	go func() {
		f, err := fetchPR(root, n)
		e.Post(func(e *Editor) {
			e.prBusy = false
			if err != nil {
				e.Msg = fmt.Sprintf("pr #%d: %v", n, err)
				return
			}
			e.checkoutPR(f)
		})
	}()
}

// prReady says why the tree can't be switched to a PR, or nil. Untracked files
// don't count: git checkout refuses to overwrite one itself.
func (e *Editor) prReady() error {
	if e.Root == "" {
		return errors.New("no project")
	}
	for _, b := range e.Bufs {
		if b.Dirty() {
			return fmt.Errorf("unsaved changes in %s", b.Name)
		}
	}
	if n := e.PendingCount(); n > 0 {
		return errors.New(plural(n, "pending hunk") + " (review them first)")
	}
	s, err := git(e.Root, "status", "--porcelain", "--untracked-files=no")
	switch {
	case err != nil:
		return errors.New("not a git repository")
	case s != "":
		return errors.New("uncommitted changes: commit or stash them first")
	}
	return nil
}

// fetchPR asks gh about PR n and fetches its head and base. It runs off the
// editor goroutine: it touches only git and the network.
func fetchPR(root string, n int) (*fetchedPR, error) {
	out, err := runTool(root, "gh", "pr", "view", strconv.Itoa(n), "--json", "number,title,url,baseRefName,baseRefOid,headRefOid")
	if err != nil {
		return nil, err
	}
	var v prView
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.HeadRefOid == "" || v.BaseRefOid == "" {
		return nil, errors.New("gh pr view: unexpected output")
	}
	repo, err := prRepo(v.URL)
	if err != nil {
		return nil, err
	}
	remote := prRemote(root, repo)
	if _, err := runTool(root, "git", "fetch", "--quiet", "--no-tags", remote,
		fmt.Sprintf("refs/pull/%d/head", n), "refs/heads/"+v.BaseRefName); err != nil {
		return nil, err
	}
	for _, oid := range []string{v.HeadRefOid, v.BaseRefOid} {
		if _, err := git(root, "cat-file", "-e", oid+"^{commit}"); err != nil {
			return nil, errors.New("the PR changed while fetching; try again")
		}
	}
	base, err := git(root, "merge-base", v.BaseRefOid, v.HeadRefOid)
	if err != nil {
		return nil, errors.New("its head and base share no history")
	}
	return &fetchedPR{view: v, base: base}, nil
}

// checkoutPR checks out a fetched PR and starts reviewing it.
func (e *Editor) checkoutPR(f *fetchedPR) {
	v := f.view
	if err := e.prReady(); err != nil { // things may have changed during the fetch
		e.Msg = fmt.Sprintf("pr #%d: fetched, but %v", v.Number, err)
		return
	}
	back := ""
	if e.pr != nil {
		back = e.pr.Back // switching PRs: still go back to the start
	} else if back, _ = git(e.Root, "symbolic-ref", "--quiet", "--short", "HEAD"); back == "" {
		back, _ = git(e.Root, "rev-parse", "HEAD")
	}
	if _, err := runTool(e.Root, "git", "checkout", "--quiet", "--detach", v.HeadRefOid); err != nil {
		e.Msg = fmt.Sprintf("pr #%d: %v", v.Number, err)
		return
	}
	e.pr = &pullRequest{N: v.Number, Title: v.Title, URL: v.URL, Head: v.HeadRefOid, Back: back}
	e.refreshBuffers()
	title := fmt.Sprintf("#%d %s", v.Number, v.Title)
	t, left, _, err := e.buildReview(&reviewScope{Agent: HookAgent, Title: title, Rev: f.base + ".." + v.HeadRefOid})
	if err != nil {
		e.Msg = fmt.Sprintf("pr #%d: checked out, but %v (:pr-done goes back)", v.Number, err)
		return
	}
	e.setTrail(t)
	e.goToStep(0)
	e.Msg = fmt.Sprintf("%s: %s (]s to walk it, :pr-done when finished)%s", title, plural(len(t.Steps), "hunk"), left)
}

// leavePR leaves the PR: it checks out what :pr started from (:pr-done).
func (e *Editor) leavePR() {
	p := e.pr
	if p == nil {
		e.Msg = "pr-done: no PR checked out here"
		return
	}
	if err := e.prReady(); err != nil {
		e.Msg = "pr-done: " + err.Error()
		return
	}
	if _, err := runTool(e.Root, "git", "checkout", "--quiet", p.Back); err != nil {
		e.Msg = "pr-done: " + err.Error()
		return
	}
	e.pr = nil
	if e.trail != nil && e.trail.review {
		e.clearTrail()
	}
	e.refreshBuffers()
	e.Msg = fmt.Sprintf("left #%d; back on %s", p.N, shortRev(p.Back))
}

// refreshBuffers rereads the open files a checkout changed.
func (e *Editor) refreshBuffers() {
	for _, b := range e.Bufs {
		b.Refresh()
	}
}

// prRepo returns "host/owner/name" from a PR's URL.
func prRepo(s string) (string, error) {
	u, err := url.Parse(s)
	if err == nil {
		if p := strings.Split(strings.Trim(u.Path, "/"), "/"); len(p) == 4 && p[2] == "pull" {
			return strings.ToLower(u.Host + "/" + p[0] + "/" + p[1]), nil
		}
	}
	return "", fmt.Errorf("can't read the repository from %q", s)
}

// prRemote returns the remote whose URL is repo ("host/owner/name"), or
// repo's https URL if no remote is.
func prRemote(root, repo string) string {
	out, _ := git(root, "remote", "-v")
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && remoteRepo(f[1]) == repo {
			return f[0]
		}
	}
	return "https://" + repo + ".git"
}

// remoteRepo returns "host/owner/name" for a remote URL in any of git's
// forms: https://host/o/n(.git), ssh://git@host/o/n, git@host:o/n.
func remoteRepo(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i] + "/" + s[i+1:]
	}
	if i := strings.Index(s, "@"); i >= 0 && i < strings.Index(s+"/", "/") {
		s = s[i+1:]
	}
	if host, path, ok := strings.Cut(s, "/"); ok {
		host, _, _ = strings.Cut(host, ":") // a port
		s = host + "/" + path
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git"))
}

// shortRev returns a branch name as is and a commit's first 7 characters.
func shortRev(s string) string {
	if len(s) == 40 && strings.Trim(s, "0123456789abcdef") == "" {
		return s[:7]
	}
	return s
}

// runTool runs a command in dir without a terminal to prompt on, and returns its
// output, or an error with the first line it printed to stderr.
func runTool(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	out, err := cmd.Output()
	if err != nil {
		var x *exec.ExitError
		if errors.As(err, &x) {
			if msg, _, _ := strings.Cut(strings.TrimSpace(string(x.Stderr)), "\n"); msg != "" {
				return "", fmt.Errorf("%s: %s", name, msg)
			}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s isn't installed", name)
		}
		return "", fmt.Errorf("%s: %v", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}
