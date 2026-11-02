package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/synthlike/hiss/internal/text"
)

// A review walks a change that has already landed, as a trail with a step for
// each hunk: uncommitted changes, else the last commit, or a revision or range.
// An agent sends one with show_review, naming the files and adding notes, or
// the user starts one with :review. A step sits on the new text, with the
// lines it replaced below. Accepting a hunk leaves it; rejecting one puts the
// old text back in the working tree, whatever the change was, and tells the
// agent at once. Comments reach it the same way. The review finishes when every
// hunk is decided, or with :rf, and the agent gets a report. In a review of a
// PR the code is the author's, so accepting a hunk marks it viewed and
// rejecting one flags it, and neither touches the working tree.

const (
	maxReviewSteps = 500
	maxRemoved     = 12 // removed lines shown for a hunk
)

// diffHunk is a hunk of a diff, in terms of the new file.
type diffHunk struct {
	path    string
	line    int // 1-based: its first added line, or the one above what it removed
	lines   int // lines from there to its last added line; 0 if it adds none
	first   string
	added   int
	removed []string

	start    int      // 1-based: where the changed lines begin
	old, new []string // the changed lines, with the unchanged ones among them
	oldNoEOL bool     // the old text ended the file without a newline
	created  bool     // the diff creates the file
}

// hunkHeader matches "@@ -old[,n] +new[,n] @@".
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseDiff reads git's unified diff, made with --no-prefix, and also returns
// how many files it deleted. Hunks are read by their line counts, because a
// removed "-- x" reads just like a file header.
func parseDiff(diff string) (hunks []diffHunk, deleted int) {
	lines := strings.Split(diff, "\n")
	count := func(s string) int {
		if s == "" {
			return 1
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	path, created := "", false
	for i := 0; i < len(lines); {
		l := lines[i]
		i++
		if name, ok := strings.CutPrefix(l, "--- "); ok {
			created = diffPath(name) == "/dev/null"
			continue
		}
		if name, ok := strings.CutPrefix(l, "+++ "); ok {
			path = diffPath(name)
			if path == "/dev/null" {
				path = ""
				deleted++
			}
			continue
		}
		m := hunkHeader.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		oldLeft, newLeft := count(m[1]), count(m[3])
		at, _ := strconv.Atoi(m[2])
		if newLeft == 0 {
			at++ // git gives the line before an empty range
		}
		h := diffHunk{path: path, created: created}
		firstAdd, lastAdd, removedAt := 0, 0, 0
		var between []string // unchanged lines since the last change
		changed := func() {
			if h.start == 0 {
				h.start = at
			}
			h.old, h.new = append(h.old, between...), append(h.new, between...)
			between = nil
		}
		for (oldLeft > 0 || newLeft > 0) && i < len(lines) {
			b := lines[i]
			i++
			switch {
			case strings.HasPrefix(b, "-"):
				if removedAt == 0 {
					removedAt = at
				}
				changed()
				h.removed = append(h.removed, b[1:])
				h.old = append(h.old, b[1:])
				oldLeft--
			case strings.HasPrefix(b, "+"):
				if firstAdd == 0 {
					firstAdd, h.first = at, b[1:]
				}
				changed()
				lastAdd = at
				h.added++
				h.new = append(h.new, b[1:])
				at++
				newLeft--
			case strings.HasPrefix(b, `\`): // no newline at end of file
				h.oldNoEOL = h.oldNoEOL || strings.HasPrefix(lines[i-2], "-") || strings.HasPrefix(lines[i-2], " ")
			default:
				between = append(between, strings.TrimPrefix(b, " "))
				at++
				oldLeft--
				newLeft--
			}
		}
		if firstAdd > 0 {
			h.line, h.lines = firstAdd, lastAdd-firstAdd+1
		} else {
			h.line = max(removedAt-1, 1)
		}
		if path != "" {
			hunks = append(hunks, h)
		}
	}
	return hunks, deleted
}

// diffPath reads a path from a "+++" line: git ends one holding spaces with a
// tab, and quotes one holding tabs, quotes or newlines.
func diffPath(s string) string {
	s = strings.TrimRight(s, "\t")
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// gitDiff returns the arguments that make git cmd print a diff of rev for
// parseDiff.
func gitDiff(rev string, cmd ...string) []string {
	args := append([]string{"-c", "core.quotePath=false"}, cmd...)
	return append(args, "-U0", "--inter-hunk-context=3", "--no-color", "--no-ext-diff", "--no-textconv",
		"--no-prefix", "--relative", rev, "--")
}

// revDiff returns the diff of rev: a range, or one commit against its first
// parent.
func revDiff(root, rev string) (string, error) {
	if strings.Contains(rev, "..") {
		return git(root, gitDiff(rev, "diff")...)
	}
	return git(root, gitDiff(rev, "show", "--format=", "--diff-merges=first-parent")...)
}

// reviewScope says what a review covers.
type reviewScope struct {
	Agent, Title string
	Rev          string      // "" for uncommitted changes, or the last commit if there are none
	Paths        []string    // absolute files or directories; none for all
	Notes        []StepInput // notes for the hunks holding these lines
}

func (r *reviewScope) covers(path string) bool {
	return len(r.Paths) == 0 || slices.ContainsFunc(r.Paths, func(p string) bool {
		return path == p || strings.HasPrefix(path, p+string(filepath.Separator))
	})
}

// noteFor returns the notes for hunk h of the file at path, and marks them used.
func (r *reviewScope) noteFor(path string, h diffHunk, used map[int]bool) string {
	var notes []string
	for i, n := range r.Notes {
		if n.Path == path && (n.Line == h.line || h.start <= n.Line && n.Line < h.start+len(h.new)) {
			notes = append(notes, n.Note)
			used[i] = true
		}
	}
	return strings.Join(notes, "; ")
}

// reviewDiffOf returns the diff of r.Rev and a title for it.
func (e *Editor) reviewDiffOf(r *reviewScope) (diff, title string, err error) {
	rev, title := r.Rev, r.Rev
	if rev == "" {
		title = "uncommitted changes"
		if diff, err = git(e.Root, gitDiff("HEAD", "diff")...); err != nil {
			return "", "", errors.New("no commits to compare with")
		}
		if diff != "" {
			return diff, title, nil
		}
		rev = "HEAD"
	}
	if diff, err = revDiff(e.Root, rev); err != nil {
		return "", "", errors.New("git can't show " + rev)
	}
	if s, err := git(e.Root, "log", "-1", "--format=%h %s", rev, "--"); err == nil && s != "" && !strings.Contains(rev, "..") {
		title = s
	}
	return diff, title, nil
}

// buildReview makes the trail for r, without showing it. left says what it
// couldn't include, and unmatched counts notes for no hunk.
func (e *Editor) buildReview(r *reviewScope) (t *Trail, left string, unmatched int, err error) {
	switch {
	case e.Root == "":
		return nil, "", 0, errors.New("no project")
	case strings.HasPrefix(r.Rev, "-"):
		return nil, "", 0, errors.New(r.Rev + " isn't a revision")
	}
	diff, title, err := e.reviewDiffOf(r)
	if err != nil {
		return nil, "", 0, err
	}
	hunks, deleted := parseDiff(diff)
	var steps []StepInput
	var kept []diffHunk
	used := map[int]bool{}
	missing, over := 0, 0
	for _, h := range hunks {
		path := filepath.Join(e.Root, h.path)
		if !r.covers(path) {
			continue
		}
		if _, err := os.Stat(path); err != nil && e.Find(path) == nil {
			missing++
			continue
		}
		if len(steps) == maxReviewSteps {
			over++
			continue
		}
		note := fmt.Sprintf("+%d -%d", h.added, len(h.removed))
		if n := r.noteFor(path, h, used); n != "" {
			note += ": " + n
		}
		steps = append(steps, StepInput{Path: path, Line: h.line, Note: note})
		kept = append(kept, h)
	}
	if len(steps) == 0 {
		if deleted > 0 {
			return nil, "", 0, fmt.Errorf("nothing to review in %s but %s", title, plural(deleted, "deleted file"))
		}
		return nil, "", 0, errors.New("nothing to review in " + title)
	}
	if r.Title != "" {
		title = r.Title
	}
	if t, err = e.newTrail(r.Agent, title, steps); err != nil {
		return nil, "", 0, err
	}
	t.review, t.rev = true, r.Rev
	t.commit, _ = git(e.Root, "rev-parse", "HEAD")
	s := e.Caller()
	if ss := e.Sessions(); s == nil && len(ss) > 0 {
		s = ss[0]
	}
	if s != nil {
		t.Session = s.ID
	}
	for i, s := range t.Steps {
		h := kept[i]
		s.Lines, s.Removed = h.lines, h.removed
		s.Offset, s.Old, s.New, s.OldNoEOL, s.Created = h.start-h.line, h.old, h.new, h.oldNoEOL, h.created
		s.Changed = h.lines > 0 && s.text() != h.first
		if n := len(s.Removed) - maxRemoved; n > 0 {
			s.Removed = append(s.Removed[:maxRemoved:maxRemoved], fmt.Sprintf("… %d more", n))
		}
	}
	if n := deleted + missing; n > 0 {
		left = fmt.Sprintf("; %d with no file left to show", n)
	}
	if over > 0 {
		left += fmt.Sprintf("; the first %d only", maxReviewSteps)
	}
	return t, left, len(r.Notes) - len(used), nil
}

// review starts the user's own review of rev (:review).
func (e *Editor) review(rev string) {
	t, left, _, err := e.buildReview(&reviewScope{Agent: HookAgent, Rev: rev})
	if err != nil {
		e.Msg = "review: " + err.Error()
		return
	}
	e.setTrail(t)
	e.goToStep(0)
	e.Msg = fmt.Sprintf("%s: %s (]s to walk it, + - # on a hunk)%s", t.Title, plural(len(t.Steps), "hunk"), left)
}

// ShowReview sends the user a review from an agent. It returns the number of
// hunks and of notes that matched none.
func (e *Editor) ShowReview(agent, title, rev string, paths []string, notes []StepInput) (int, int, error) {
	t, _, unmatched, err := e.buildReview(&reviewScope{Agent: agent, Title: title, Rev: rev, Paths: paths, Notes: notes})
	if err != nil {
		return 0, 0, err
	}
	e.setTrail(t)
	e.notify(agent, fmt.Sprintf("sent a review: %s, %s (]s to walk it)", t.Title, plural(len(t.Steps), "hunk")))
	return len(t.Steps), unmatched, nil
}

// reviewStep returns the review's hunk under the cursor, or nil.
func (e *Editor) reviewStep() *Step {
	if e.trail == nil || !e.trail.review {
		return nil
	}
	return e.stepAtCursor()
}

// decideStep accepts or rejects a review's hunk and goes to the next open one.
func (e *Editor) decideStep(s *Step, accept bool, reason string) {
	t := e.trail
	switch {
	case t.finished:
		e.Msg = "this review is finished"
		return
	case s.Verdict != "":
		e.Msg = fmt.Sprintf("hunk %d is already %s", s.N, s.Verdict)
		return
	}
	s.Lines = 0
	switch {
	case accept:
		s.Verdict = Accepted
		e.Msg = fmt.Sprintf("%s hunk %d", t.word(Accepted), s.N)
	case t.pr: // the author's code: flag it, leave it
		s.Verdict, s.Reason = Rejected, reason
		tell := "I flagged this hunk"
		e.Msg = fmt.Sprintf("flagged hunk %d", s.N)
		if reason != "" {
			tell += ": " + reason
		}
		s.Thread = append(s.Thread, Comment{Author: You.Author, Text: tell})
	default:
		s.Verdict, s.Reason = Rejected, reason
		s.Reverted = e.revertHunk(s)
		tell := "I rejected this hunk and the old text is back"
		e.Msg = fmt.Sprintf("rejected hunk %d and put the old text back", s.N)
		if s.Reverted {
			s.Removed = nil
		} else {
			tell = "I rejected this hunk, but it has changed since, so it's still in place"
			e.Msg = fmt.Sprintf("rejected hunk %d, but it has changed since, so it stays", s.N)
		}
		if reason != "" {
			tell += ": " + reason
		}
		s.Thread = append(s.Thread, Comment{Author: You.Author, Text: tell})
	}
	msg := e.Msg
	for i := range t.Steps {
		if n := (s.N + i) % len(t.Steps); t.Steps[n].Verdict == "" {
			e.goToStep(n)
			e.Msg = msg
			return
		}
	}
	e.finishReview("")
	e.Msg = msg + "; " + e.Msg
}

// revertHunk puts s's old lines back in place of its new ones, if those are
// still there, saving the file unless it has unsaved changes.
func (e *Editor) revertHunk(s *Step) bool {
	b := s.Buf
	b.Refresh()
	t := b.Text
	lineStart := func(l int) int {
		if l >= t.LineCount() {
			return t.Len()
		}
		return t.LineStart(l)
	}
	first := s.Line() + s.Offset
	if first < 0 {
		return false
	}
	start, end := lineStart(first), lineStart(first+len(s.New))
	region := string(t.Slice(start, end))
	if strings.TrimSuffix(region, "\n") != strings.Join(s.New, "\n") {
		return false
	}
	old := strings.Join(s.Old, "\n")
	if len(s.Old) > 0 && !s.OldNoEOL {
		old += "\n"
	}
	if len(s.Old) > 0 && start == t.Len() && start > 0 && t.Slice(start-1, start)[0] != '\n' {
		old = "\n" + old // after a last line with no newline
	}
	dirty := b.Dirty()
	e.checkpointBuf(b)
	e.moveCursor(b, b.Replace(start, end, []byte(old), text.Origin{}))
	switch {
	case dirty:
	case b.Text.Len() == 0 && s.Created:
		b.saved = b.Version
		return os.Remove(b.Path) == nil
	default:
		return b.Save() == nil
	}
	return true
}

// finishReview ends the review, leaving undecided hunks as they are, with an
// optional word for the agent (:rf).
func (e *Editor) finishReview(summary string) {
	t := e.trail
	switch {
	case t == nil || !t.review:
		e.Msg = "no review to finish"
		return
	case t.finished:
		e.Msg = "this review is finished"
		return
	}
	t.finished, t.summary = true, summary
	e.Msg = "finished the review: " + t.tally()
}

// tally counts the review's hunks by verdict.
func (t *Trail) tally() string {
	a, r := 0, 0
	for _, s := range t.Steps {
		switch s.Verdict {
		case Accepted:
			a++
		case Rejected:
			r++
		}
	}
	return fmt.Sprintf("%d %s, %d %s, %d not reviewed", a, t.word(Accepted), r, t.word(Rejected), len(t.Steps)-a-r)
}

// word names a verdict as the review uses it: in a PR's, accepting a hunk
// marks it viewed and rejecting flags it.
func (t *Trail) word(v Status) string {
	switch {
	case t.pr && v == Accepted:
		return "viewed"
	case t.pr && v == Rejected:
		return "flagged"
	}
	return string(v)
}

// report is what the agent is told when the review finishes.
func (t *Trail) report(e *Editor) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "The user finished the review of %s: %s.\n", t.Title, t.tally())
	for _, s := range t.Steps {
		if s.Verdict != Rejected {
			continue
		}
		fmt.Fprintf(&sb, "  %s %s:%d", t.word(Rejected), e.relName(s.Buf), s.Line()+1)
		if !s.Reverted && !t.pr {
			sb.WriteString(" (changed since, so still in place)")
		}
		if s.Reason != "" {
			sb.WriteString(": " + s.Reason)
		}
		sb.WriteString("\n")
	}
	if t.summary != "" {
		sb.WriteString("They said: " + t.summary + "\n")
	}
	return sb.String()
}

// TakeReview returns what the review has for the agent since it last asked,
// whichever session sent it, and whether the review is over.
func (e *Editor) TakeReview() ([]Delivery, bool) {
	if e.trail == nil || !e.trail.review {
		return nil, true
	}
	return e.trailDeliveries(), e.trail.finished
}
