package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Official comments on a PR: :gc drafts one on the cursor's line, or on the
// selected lines, for GitHub rather than the agent. GitHub only takes them on
// lines its diff shows, the changed lines and three around each hunk, so others
// are refused up front. Drafts are kept in .hiss/pr/<n>.json as they're
// written, so a crash or :pr-done doesn't lose them; :pr <n> picks them up.
// For now they're on the new side only: removed lines aren't cursor lines.
//
// An agent drafts them too, with draft_pr_comment. They publish in the user's
// name, so an agent's draft is pending until the user accepts it (:gca);
// :pr-submit leaves pending ones behind.

// ghComment is a draft review comment, in the form GitHub's API takes, plus
// who drafted it.
type ghComment struct {
	Path      string `json:"path"` // from the repository's root, with slashes
	Line      int    `json:"line"` // 1-based, in the PR's head
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"` // a multi-line comment's first line
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`  // the agent that drafted it; "" for the user
	Pending   bool   `json:"pending,omitempty"` // an agent's, not yet accepted
}

// covers reports whether c is on line of path.
func (c ghComment) covers(path string, line int) bool {
	first := c.Line
	if c.StartLine > 0 {
		first = c.StartLine
	}
	return c.Path == path && first <= line && line <= c.Line
}

// prDrafts is what .hiss/pr/<n>.json holds.
type prDrafts struct {
	Head     string      `json:"head"` // the PR's head when they were written
	Comments []ghComment `json:"comments"`
}

// lineRange is lines first..last of a file, 1-based.
type lineRange struct{ first, last int }

// diffRanges returns the new-side lines each file's hunks show in a unified
// diff made with --no-prefix. Hunks are read by their line counts, as in
// parseDiff, so a "+++ " line in a hunk isn't taken for a file header.
func diffRanges(diff string) map[string][]lineRange {
	out := map[string][]lineRange{}
	count := func(s string) int {
		if s == "" {
			return 1
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	lines := strings.Split(diff, "\n")
	path := ""
	for i := 0; i < len(lines); i++ {
		if name, ok := strings.CutPrefix(lines[i], "+++ "); ok {
			path = diffPath(name)
			continue
		}
		m := hunkHeader.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[2])
		oldLeft, newLeft := count(m[1]), count(m[3])
		if newLeft > 0 && path != "/dev/null" {
			out[path] = append(out[path], lineRange{start, start + newLeft - 1})
		}
		for (oldLeft > 0 || newLeft > 0) && i+1 < len(lines) {
			i++
			switch l := lines[i]; {
			case strings.HasPrefix(l, "-"):
				oldLeft--
			case strings.HasPrefix(l, "+"):
				newLeft--
			case strings.HasPrefix(l, `\`): // no newline at end of file
			default:
				oldLeft--
				newLeft--
			}
		}
	}
	return out
}

// loadPRDiff reads which lines of the PR GitHub takes comments on, and the
// drafts kept for it, and says what's worth knowing about them.
func (e *Editor) loadPRDiff(base string) string {
	p := e.pr
	diff, err := git(e.Root, "-c", "core.quotePath=false", "diff", "-U3", "--no-color", "--no-ext-diff",
		"--no-textconv", "--no-prefix", "--no-relative", base, p.Head, "--")
	if err != nil {
		return "; can't read its diff, so no :gc"
	}
	p.ranges = diffRanges(diff)
	p.prefix, _ = git(e.Root, "rev-parse", "--show-prefix")
	data, err := os.ReadFile(e.draftsFile(p))
	if err != nil {
		return ""
	}
	var d prDrafts
	if err := json.Unmarshal(data, &d); err != nil {
		return "; " + e.relPath(e.draftsFile(p)) + " is unreadable"
	}
	p.Drafts = d.Comments
	msg := "; " + plural(len(p.Drafts), "draft comment")
	if d.Head != p.Head && len(p.Drafts) > 0 {
		msg += " from an earlier push, maybe on moved lines"
	}
	return msg
}

// draftsFile is where the drafts for p are kept.
func (e *Editor) draftsFile(p *pullRequest) string {
	return filepath.Join(e.Root, ".hiss", "pr", strconv.Itoa(p.N)+".json")
}

// relPath returns path relative to the project root, if it's inside it.
func (e *Editor) relPath(path string) string {
	if rel, ok := e.underRoot(path); ok {
		return rel
	}
	return path
}

// saveDrafts writes p's drafts, or removes the file when there are none.
func (e *Editor) saveDrafts(p *pullRequest) error {
	if len(p.Drafts) == 0 {
		if err := os.Remove(e.draftsFile(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(prDrafts{Head: p.Head, Comments: p.Drafts}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(e.draftsFile(p), append(data, '\n'))
}

// prPath returns path as GitHub names it, from the repository's root.
func (e *Editor) prPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	rel, ok := e.underRoot(path)
	if !ok {
		return "", false
	}
	return e.pr.prefix + filepath.ToSlash(rel), true
}

// commentable reports whether one hunk of path's diff shows lines first..last.
func (p *pullRequest) commentable(path string, first, last int) bool {
	for _, r := range p.ranges[path] {
		if r.first <= first && last <= r.last {
			return true
		}
	}
	return false
}

// addDraft adds a draft on lines first..last of the file at abs, by agent, or
// by the user if agent is "". It returns where the draft is, as "line 3" or
// "lines 3-5", and a note if saving the drafts failed; or why GitHub
// wouldn't take a comment there.
func (e *Editor) addDraft(abs string, first, last int, body, agent string) (where, note string, err error) {
	p := e.pr
	if p == nil {
		return "", "", errors.New("no PR checked out here (:pr <n>)")
	}
	path, ok := e.prPath(abs)
	where = fmt.Sprintf("line %d", last)
	if first < last {
		where = fmt.Sprintf("lines %d-%d", first, last)
	}
	switch b := e.Find(abs); {
	case p.ranges == nil:
		return "", "", errors.New("the PR's diff couldn't be read")
	case !ok:
		return "", "", errors.New("this file isn't in the PR's repository")
	case b != nil && b.Dirty():
		return "", "", errors.New("the file has unsaved changes, so its lines may not match the PR's: save or undo them first")
	case strings.TrimSpace(body) == "":
		return "", "", errors.New("the comment is empty")
	case first < 1 || last < first:
		return "", "", fmt.Errorf("no such lines: %d-%d", first, last)
	case !p.commentable(path, first, last) && first < last:
		return "", "", fmt.Errorf("%s of %s aren't in one hunk of the PR's diff; GitHub only takes comments there", where, path)
	case !p.commentable(path, first, last):
		return "", "", fmt.Errorf("%s of %s isn't in the PR's diff; GitHub only takes comments there", where, path)
	}
	c := ghComment{Path: path, Line: last, Side: "RIGHT", Body: body, Author: agent, Pending: agent != ""}
	if first < last {
		c.StartLine, c.StartSide = first, "RIGHT"
	}
	p.Drafts = append(p.Drafts, c)
	if err := e.saveDrafts(p); err != nil {
		note = "; saving drafts failed: " + err.Error()
	}
	return where, note, nil
}

// ghComment drafts an official comment on the selected lines (:gc).
func (e *Editor) ghComment(text string) {
	from, to := e.selLines()
	where, note, err := e.addDraft(e.Buf.Path, from+1, to+1, text, "")
	if err != nil {
		e.Msg = "gh-comment: " + err.Error()
		return
	}
	e.Mode = Normal
	e.Msg = fmt.Sprintf("drafted a comment on %s for #%d (%s; :gcd drops it)%s", where, e.pr.N, plural(len(e.pr.Drafts), "draft"), note)
}

// DraftPRComment adds an agent's draft on lines first..last of the file at
// abs, pending until the user accepts it, and returns where it is and the
// PR's number.
func (e *Editor) DraftPRComment(agent, abs string, first, last int, body string) (string, int, error) {
	where, note, err := e.addDraft(abs, first, last, body, agent)
	if err != nil {
		return "", 0, err
	}
	path, _ := e.prPath(abs)
	e.notify(agent+" drafted a PR comment", fmt.Sprintf("on %s of %s; :gca accepts it, :gcd drops it%s", where, path, note))
	return where, e.pr.N, nil
}

// acceptGHComment accepts the agents' pending drafts on the cursor's line
// (:gca), so :pr-submit sends them.
func (e *Editor) acceptGHComment() {
	p := e.pr
	if p == nil {
		e.Msg = "gh-comment-accept: no PR checked out here"
		return
	}
	path, _ := e.prPath(e.Buf.Path)
	n := 0
	for i, c := range p.Drafts {
		if c.Pending && c.covers(path, e.line()+1) {
			p.Drafts[i].Pending = false
			n++
		}
	}
	if n == 0 {
		e.Msg = "gh-comment-accept: no pending draft on this line"
		return
	}
	e.Msg = fmt.Sprintf("accepted %s; :prs sends it with the rest", plural(n, "draft"))
	if err := e.saveDrafts(p); err != nil {
		e.Msg += "; saving drafts failed: " + err.Error()
	}
}

// dropGHComment drops the drafts on the cursor's line (:gcd).
func (e *Editor) dropGHComment() {
	p := e.pr
	if p == nil {
		e.Msg = "gh-comment-drop: no PR checked out here"
		return
	}
	path, _ := e.prPath(e.Buf.Path)
	line, n := e.line()+1, 0
	kept := p.Drafts[:0:0]
	for _, c := range p.Drafts {
		if c.covers(path, line) {
			n++
			continue
		}
		kept = append(kept, c)
	}
	if n == 0 {
		e.Msg = "gh-comment-drop: no draft on this line"
		return
	}
	p.Drafts = kept
	e.Msg = fmt.Sprintf("dropped %s; %s left", plural(n, "draft"), plural(len(kept), "draft"))
	if err := e.saveDrafts(p); err != nil {
		e.Msg += "; saving drafts failed: " + err.Error()
	}
}

// draftRows adds the PR's draft comments on the visible buffer to rows, each
// under the last line it's on.
func (e *Editor) draftRows(rows map[int][]NoteLine) {
	p := e.pr
	if p == nil || len(p.Drafts) == 0 {
		return
	}
	path, ok := e.prPath(e.Buf.Path)
	if !ok {
		return
	}
	for _, c := range p.Drafts {
		l := c.Line - 1
		if c.Path != path || l >= e.Buf.Text.LineCount() {
			continue
		}
		var tags []string
		if c.StartLine > 0 {
			tags = append(tags, fmt.Sprintf("%d-%d", c.StartLine, c.Line))
		}
		if c.Author != "" {
			tags = append(tags, "by "+c.Author)
		}
		if c.Pending {
			tags = append(tags, "pending, :gca accepts")
		}
		label := "github draft:"
		if len(tags) > 0 {
			label = "github draft (" + strings.Join(tags, ", ") + "):"
		}
		for _, t := range labeledLines(label, c.Body, e.TextWidth()) {
			rows[l] = append(rows[l], NoteLine{GitHub: true, Text: t})
		}
	}
}
