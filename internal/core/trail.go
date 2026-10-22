package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Trail is an ordered walk through code that an agent shares: each step points
// at a line and explains it. Steps are anchored to the text, so they move with
// edits, and the user can comment on a step to ask for more.
type Trail struct {
	Agent, Session, Title string
	Steps                 []*Step
	current               int // index of the step last visited, -1 before the first
}

// Step is one stop on a trail.
type Step struct {
	N      int // 1-based position in the trail
	Buf    *Buffer
	at     *anchor // start of the step's line
	Note   string
	Thread []Comment
}

// StepInput is a step as an agent describes it.
type StepInput struct {
	Path string // absolute
	Line int    // 1-based
	Note string
}

// Line returns the step's current 0-based line.
func (s *Step) Line() int { return s.Buf.Text.LineOf(s.at.off) }

// ShowTrail replaces the current trail with one from an agent, opening the
// files it visits in the background.
func (e *Editor) ShowTrail(agent, title string, steps []StepInput) error {
	t := &Trail{Agent: agent, Title: title, current: -1}
	if s := e.Caller(); s != nil {
		t.Session = s.ID
	}
	for i, in := range steps {
		b, err := e.OpenBuffer(in.Path)
		if err != nil {
			t.release()
			return err
		}
		line := min(max(in.Line-1, 0), b.Text.LineCount()-1)
		t.Steps = append(t.Steps, &Step{N: i + 1, Buf: b, at: b.anchor(b.Text.LineStart(line)), Note: in.Note})
	}
	if e.trail != nil {
		e.trail.release()
	}
	e.trail = t
	e.notify(agent, fmt.Sprintf("shared a trail: %s, %d steps (]s to walk it)", title, len(steps)))
	return nil
}

func (t *Trail) release() {
	for _, s := range t.Steps {
		s.Buf.release(s.at)
	}
}

func (e *Editor) clearTrail() {
	if e.trail != nil {
		e.trail.release()
		e.trail = nil
	}
	e.Msg = "cleared the trail"
}

// TrailRows returns the rows drawn under lines of the visible buffer for the
// trail: each step's note, then its comment thread.
func (e *Editor) TrailRows() map[int][]NoteLine {
	if e.trail == nil {
		return nil
	}
	var rows map[int][]NoteLine
	for _, s := range e.trail.Steps {
		if s.Buf != e.Buf {
			continue
		}
		if rows == nil {
			rows = map[int][]NoteLine{}
		}
		l := s.Line()
		rows[l] = append(rows[l], NoteLine{Note: true, Text: fmt.Sprintf("%d. %s", s.N, s.Note)})
		rows[l] = append(rows[l], threadLines(s.Thread, e.Width-2)...)
	}
	return rows
}

func (e *Editor) openTrailPicker() {
	t := e.trail
	if t == nil {
		e.Msg = "no trail shared yet"
		return
	}
	p := &Picker{Title: t.Agent + ": " + t.Title}
	for i, s := range t.Steps {
		p.Items = append(p.Items, PickItem{
			Label: fmt.Sprintf("%d. %s:%d", s.N, e.relName(s.Buf), s.Line()+1), Detail: s.Note,
			Run: func(e *Editor) { e.goToStep(i) },
		})
	}
	e.Picker = p
}

// stepTrail visits the next (dir 1) or previous (dir -1) step, wrapping around.
func (e *Editor) stepTrail(dir int) {
	t := e.trail
	if t == nil || len(t.Steps) == 0 {
		e.Msg = "no trail shared yet"
		return
	}
	n, i := len(t.Steps), t.current+dir
	if t.current < 0 && dir < 0 {
		i = n - 1
	}
	e.goToStep((i%n + n) % n)
}

func (e *Editor) goToStep(i int) {
	t := e.trail
	t.current = i
	s := t.Steps[i]
	e.pushJump()
	e.SwitchTo(s.Buf)
	line := s.Line()
	e.setHead(s.Buf.Text.LineStart(line))
	e.Top, e.TopSub = max(line-2, 0), 0
	e.Msg = fmt.Sprintf("step %d/%d: %s", s.N, len(t.Steps), s.Note)
}

// relName names a buffer relative to the project root when it's inside it.
func (e *Editor) relName(b *Buffer) string {
	if rel, err := filepath.Rel(e.Root, b.Path); err == nil && e.Root != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return b.Name
}

func (e *Editor) stepAtCursor() *Step {
	if e.trail == nil {
		return nil
	}
	for _, s := range e.trail.Steps {
		if s.Buf == e.Buf && s.Line() == e.line() {
			return s
		}
	}
	return nil
}

// ReplyStep adds an agent's answer to a trail step's thread.
func (e *Editor) ReplyStep(agent string, n int, text string) error {
	if e.trail == nil || n < 1 || n > len(e.trail.Steps) {
		return fmt.Errorf("no trail step %d", n)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("empty reply")
	}
	s := e.trail.Steps[n-1]
	s.Thread = append(s.Thread, Comment{Author: agent, Text: text})
	e.notify(agent, fmt.Sprintf("replied on step %d: %s", n, text))
	return nil
}

// takeTrailComments returns the user's new comments on the session's trail
// and marks them delivered.
func (e *Editor) takeTrailComments(session string) []Delivery {
	t := e.trail
	if t == nil || t.Session != session {
		return nil
	}
	var out []Delivery
	for _, s := range t.Steps {
		for i := range s.Thread {
			if c := &s.Thread[i]; c.Author == You.Author && !c.delivered {
				c.delivered = true
				out = append(out, Delivery{Step: s, Text: c.Text})
			}
		}
	}
	return out
}
