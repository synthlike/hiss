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

// ghComment is a draft review comment, in the form GitHub's API takes.
type ghComment struct {
	Path      string `json:"path"` // from the repository's root, with slashes
	Line      int    `json:"line"` // 1-based, in the PR's head
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"` // a multi-line comment's first line
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
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

// prPath returns b's path as GitHub names it, from the repository's root.
func (e *Editor) prPath(b *Buffer) (string, bool) {
	if b.Path == "" {
		return "", false
	}
	rel, ok := e.underRoot(b.Path)
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

// ghComment drafts an official comment on the selected lines (:gc).
func (e *Editor) ghComment(text string) {
	p, b := e.pr, e.Buf
	from, to := e.selLines()
	first, last := from+1, to+1
	path, ok := "", false
	if p != nil {
		path, ok = e.prPath(b)
	}
	where := fmt.Sprintf("line %d", last)
	if first < last {
		where = fmt.Sprintf("lines %d-%d", first, last)
	}
	switch {
	case p == nil:
		e.Msg = "gh-comment: no PR checked out here (:pr <n>)"
		return
	case p.ranges == nil:
		e.Msg = "gh-comment: the PR's diff couldn't be read"
		return
	case !ok:
		e.Msg = "gh-comment: this file isn't in the PR's repository"
		return
	case b.Dirty():
		e.Msg = "gh-comment: save or undo your changes first, so lines match the PR's"
		return
	case !p.commentable(path, first, last):
		e.Msg = fmt.Sprintf("gh-comment: %s of %s aren't in one hunk of the PR's diff; GitHub only takes comments there", where, path)
		if first == last {
			e.Msg = fmt.Sprintf("gh-comment: %s of %s isn't in the PR's diff; GitHub only takes comments there", where, path)
		}
		return
	}
	c := ghComment{Path: path, Line: last, Side: "RIGHT", Body: text}
	if first < last {
		c.StartLine, c.StartSide = first, "RIGHT"
	}
	p.Drafts = append(p.Drafts, c)
	e.Mode = Normal
	e.Msg = fmt.Sprintf("drafted a comment on %s for #%d (%s; :gcd drops it)", where, p.N, plural(len(p.Drafts), "draft"))
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
	path, _ := e.prPath(e.Buf)
	line, n := e.line()+1, 0
	kept := p.Drafts[:0:0]
	for _, c := range p.Drafts {
		first := c.Line
		if c.StartLine > 0 {
			first = c.StartLine
		}
		if c.Path == path && first <= line && line <= c.Line {
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
	path, ok := e.prPath(e.Buf)
	if !ok {
		return
	}
	for _, c := range p.Drafts {
		l := c.Line - 1
		if c.Path != path || l >= e.Buf.Text.LineCount() {
			continue
		}
		label := "github draft:"
		if c.StartLine > 0 {
			label = fmt.Sprintf("github draft (%d-%d):", c.StartLine, c.Line)
		}
		for _, t := range labeledLines(label, c.Body, e.TextWidth()) {
			rows[l] = append(rows[l], NoteLine{GitHub: true, Text: t})
		}
	}
}
