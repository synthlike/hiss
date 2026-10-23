package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// trailFile is a trail as :trail-save writes it. Each step keeps its line's
// text, so loading can tell which steps the code has changed under.
type trailFile struct {
	Agent string     `json:"agent,omitempty"`
	Title string     `json:"title"`
	Steps []stepFile `json:"steps"`
}

type stepFile struct {
	Path string `json:"path"` // relative to the project root when inside it
	Line int    `json:"line"` // 1-based
	Text string `json:"text"` // the line when saved
	Note string `json:"note,omitempty"`
}

// text returns the step's line, without its newline.
func (s *Step) text() string {
	l := s.Line()
	return string(s.Buf.Text.Slice(s.Buf.Text.LineStart(l), s.Buf.LineEnd(l)))
}

func (e *Editor) saveTrail(path string) {
	t := e.trail
	if t == nil {
		e.Msg = "no trail shared yet"
		return
	}
	f := trailFile{Agent: t.Agent, Title: t.Title}
	for _, s := range t.Steps {
		p := s.Buf.Path
		if rel, ok := e.underRoot(p); ok {
			p = rel
		}
		f.Steps = append(f.Steps, stepFile{Path: p, Line: s.Line() + 1, Text: s.text(), Note: s.Note})
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o644)
	}
	if err != nil {
		e.Msg = err.Error()
		return
	}
	e.Msg = fmt.Sprintf("saved the trail to %s", path)
}

func (e *Editor) loadTrail(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	var f trailFile
	if err := json.Unmarshal(data, &f); err != nil {
		e.Msg = fmt.Sprintf("%s: %v", path, err)
		return
	}
	if len(f.Steps) == 0 {
		e.Msg = path + ": no steps"
		return
	}
	var steps []StepInput
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
	}
	if len(missing) > 0 {
		e.Msg = "these trail files don't exist: " + strings.Join(missing, ", ")
		return
	}
	t, err := e.newTrail(f.Agent, f.Title, steps)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	var changed []string
	for i, s := range t.Steps {
		if s.text() != f.Steps[i].Text {
			s.Changed = true
			changed = append(changed, strconv.Itoa(s.N))
		}
	}
	e.setTrail(t)
	e.Msg = fmt.Sprintf("loaded %s, %d steps (]s to walk it)", f.Title, len(t.Steps))
	if len(changed) > 0 {
		e.Msg += "; changed since saved: steps " + strings.Join(changed, ", ")
	}
}
