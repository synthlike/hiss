package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Trails and reviews save to JSON in one envelope: kind says which, version
// its format. A trail keeps each step's line, so loading can tell which steps
// the code has changed under. A review also keeps its diff and discussion, so
// the file stands on its own later. Files from before the envelope are trails.

const (
	kindTrail   = "trail"
	kindReview  = "review"
	fileVersion = 1
)

type trailFile struct {
	Kind    string     `json:"kind"`
	Version int        `json:"version"`
	Agent   string     `json:"agent,omitempty"`
	Title   string     `json:"title"`
	Steps   []stepFile `json:"steps"`

	// A review's:
	Rev      string `json:"rev,omitempty"`    // as it was asked for; "" for uncommitted changes
	Commit   string `json:"commit,omitempty"` // HEAD when it was made
	Finished bool   `json:"finished,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type stepFile struct {
	Path string `json:"path"` // relative to the project root when inside it
	Line int    `json:"line"` // 1-based
	Text string `json:"text"` // the line when saved
	Note string `json:"note,omitempty"`

	// A review's hunks:
	Lines    int      `json:"lines,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Offset   int      `json:"offset,omitempty"`
	Old      []string `json:"old,omitempty"`
	New      []string `json:"new,omitempty"`
	OldNoEOL bool     `json:"old_no_eol,omitempty"`
	Created  bool     `json:"created,omitempty"`
	Verdict  Status   `json:"verdict,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Reverted bool     `json:"reverted,omitempty"`

	Thread []threadFile `json:"thread,omitempty"`
}

type threadFile struct {
	Author    string `json:"author"`
	Text      string `json:"text"`
	Delivered bool   `json:"delivered,omitempty"` // the agent has had it, so loading won't send it again
}

// text returns the step's line, without its newline.
func (s *Step) text() string {
	l := s.Line()
	return string(s.Buf.Text.Slice(s.Buf.Text.LineStart(l), s.Buf.LineEnd(l)))
}

func (e *Editor) saveTrail(path string) {
	switch t := e.trail; {
	case t == nil:
		e.Msg = "no trail shared yet"
	case t.review:
		e.Msg = "this is a review: :rs saves it"
	default:
		e.saveWalk(path, kindTrail)
	}
}

func (e *Editor) saveReview(path string) {
	if e.trail == nil || !e.trail.review {
		e.Msg = "no review to save"
		return
	}
	e.saveWalk(path, kindReview)
}

// saveWalk writes the trail or review to path.
func (e *Editor) saveWalk(path, kind string) {
	t := e.trail
	f := trailFile{Kind: kind, Version: fileVersion, Agent: t.Agent, Title: t.Title,
		Rev: t.rev, Commit: t.commit, Finished: t.finished, Summary: t.summary}
	for _, s := range t.Steps {
		p := s.Buf.Path
		if rel, ok := e.underRoot(p); ok {
			p = rel
		}
		sf := stepFile{Path: p, Line: s.Line() + 1, Text: s.text(), Note: s.Note,
			Lines: s.Lines, Removed: s.Removed, Offset: s.Offset, Old: s.Old, New: s.New, OldNoEOL: s.OldNoEOL, Created: s.Created,
			Verdict: s.Verdict, Reason: s.Reason, Reverted: s.Reverted}
		if kind == kindReview {
			for _, c := range s.Thread {
				sf.Thread = append(sf.Thread, threadFile{c.Author, c.Text, c.delivered})
			}
		}
		f.Steps = append(f.Steps, sf)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o644)
	}
	if err != nil {
		e.Msg = failed("saving the "+kind+" to "+path, err)
		return
	}
	e.Msg = fmt.Sprintf("saved the %s to %s", kind, path)
}

func (e *Editor) loadTrail(path string)  { e.loadWalk(path, kindTrail) }
func (e *Editor) loadReview(path string) { e.loadWalk(path, kindReview) }

// loadWalk reads a trail or review from path, whichever kind is wanted.
func (e *Editor) loadWalk(path, kind string) {
	data, err := os.ReadFile(path)
	if err != nil {
		e.Msg = failed("loading "+path, err)
		return
	}
	var f trailFile
	if err := json.Unmarshal(data, &f); err != nil {
		e.Msg = failed("loading "+path, err)
		return
	}
	if f.Kind == "" {
		f.Kind = kindTrail
	}
	switch {
	case f.Version > fileVersion:
		e.Msg = fmt.Sprintf("%s: version %d is newer than this hiss reads", path, f.Version)
		return
	case f.Kind != kind:
		e.Msg = fmt.Sprintf("%s holds a %s: :%s loads it", path, f.Kind, map[string]string{kindTrail: "tl", kindReview: "rl"}[f.Kind])
		return
	case len(f.Steps) == 0:
		e.Msg = path + ": no steps"
		return
	}
	var steps []StepInput
	var saved []stepFile
	var missing []string
	for _, s := range f.Steps {
		p := s.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(e.Root, p)
		}
		p = filepath.Clean(p)
		if _, err := os.Stat(p); err != nil && e.Find(p) == nil {
			missing = append(missing, s.Path)
			continue
		}
		steps = append(steps, StepInput{Path: p, Line: s.Line, Note: s.Note})
		saved = append(saved, s)
	}
	if len(missing) > 0 && (kind == kindTrail || len(steps) == 0) {
		e.Msg = "these " + kind + " files don't exist: " + strings.Join(missing, ", ")
		return
	}
	t, err := e.newTrail(f.Agent, f.Title, steps)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	var changed []string
	for i, s := range t.Steps {
		if s.text() != saved[i].Text {
			s.Changed = true
			changed = append(changed, strconv.Itoa(s.N))
		}
		fs := saved[i]
		for _, c := range fs.Thread {
			s.Thread = append(s.Thread, Comment{Author: c.Author, Text: c.Text, delivered: c.Delivered})
		}
		s.Lines, s.Removed, s.Offset, s.Old, s.New, s.Created = fs.Lines, fs.Removed, fs.Offset, fs.Old, fs.New, fs.Created
		s.OldNoEOL, s.Verdict, s.Reason, s.Reverted = fs.OldNoEOL, fs.Verdict, fs.Reason, fs.Reverted
	}
	if kind == kindReview {
		t.review, t.rev, t.commit = true, f.Rev, f.Commit
		t.finished, t.told, t.summary = f.Finished, f.Finished, f.Summary
		if ss := e.Sessions(); len(ss) > 0 {
			t.Session = ss[0].ID
		}
	}
	e.setTrail(t)
	e.Msg = fmt.Sprintf("loaded %s, %s (]s to walk it)", f.Title, plural(len(t.Steps), "step"))
	if len(missing) > 0 {
		e.Msg += "; files gone: " + strings.Join(missing, ", ")
	}
	if len(changed) > 0 {
		noun := "steps "
		if len(changed) == 1 {
			noun = "step "
		}
		e.Msg += "; changed since saved: " + noun + strings.Join(changed, ", ")
	}
}
