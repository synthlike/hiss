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
	N       int // 1-based position in the trail
	Buf     *Buffer
	at      *anchor // start of the step's line
	Note    string
	Thread  []Comment
	Changed bool // loaded from a file whose saved line text no longer matches
}

// StepInput is a step as an agent describes it.
type StepInput struct {
	Path string // absolute
	Line int    // 1-based
	Note string
}

// Line returns the step's current 0-based line.
func (s *Step) Line() int { return s.Buf.Text.LineOf(s.at.off) }

// ShowTrail replaces the current trail with one from an agent.
func (e *Editor) ShowTrail(agent, title string, steps []StepInput) error {
	t, err := e.newTrail(agent, title, steps)
	if err != nil {
		return err
	}
	if s := e.Caller(); s != nil {
		t.Session = s.ID
	}
	e.setTrail(t)
	e.notify(agent, fmt.Sprintf("shared a trail: %s, %s (]s to walk it)", title, plural(len(steps), "step")))
	return nil
}

// newTrail builds a trail, opening the files it visits in the background.
func (e *Editor) newTrail(agent, title string, steps []StepInput) (*Trail, error) {
	t := &Trail{Agent: agent, Title: title, current: -1}
	for i, in := range steps {
		b, err := e.OpenBuffer(in.Path)
		if err != nil {
			t.release()
			return nil, err
		}
		line := min(max(in.Line-1, 0), b.Text.LineCount()-1)
		t.Steps = append(t.Steps, &Step{N: i + 1, Buf: b, at: b.anchor(b.Text.LineStart(line)), Note: in.Note})
	}
	return t, nil
}

func (e *Editor) setTrail(t *Trail) {
	if e.trail != nil {
		e.trail.release()
	}
	e.trail = t
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
		l, note := s.Line(), fmt.Sprintf("%d. %s", s.N, s.Note)
		if s.Changed {
			note += " (changed since saved)"
		}
		rows[l] = append(rows[l], NoteLine{Note: true, Text: note})
		rows[l] = append(rows[l], threadLines(s.Thread, e.TextWidth())...)
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
	e.centerOn(line)
	e.Msg = fmt.Sprintf("step %d/%d: %s", s.N, len(t.Steps), s.Note)
}

// centerOn scrolls so that line, with the rows drawn around it, sits in the
// middle of the screen, or as near as the start of the file allows.
func (e *Editor) centerOn(line int) { e.showLines(line, line) }

// toastRows is the height of the toast in the top-right corner.
const toastRows = 2

// showLines scrolls lines first..last, with the rows drawn around them, to
// the middle of the screen, clear of the toast when there is room. A block
// taller than the screen starts a quarter of the way down instead.
func (e *Editor) showLines(first, last int) {
	_, block := e.lineRows()
	rows := 0
	for l := first; l <= last; l++ {
		rows += block(l)
	}
	room := max((e.Height-rows)/2, min(toastRows, e.Height-rows))
	if rows > e.Height {
		room = e.Height / 4
	}
	e.Top, e.TopSub = first, 0
	for e.Top > 0 && block(e.Top-1) <= room {
		e.Top--
		room -= block(e.Top)
	}
}

// relName names a buffer relative to the project root when it's inside it.
func (e *Editor) relName(b *Buffer) string {
	if rel, ok := e.underRoot(b.Path); ok {
		return rel
	}
	return b.Name
}

// underRoot returns path relative to the project root, if it's inside it.
func (e *Editor) underRoot(path string) (string, bool) {
	rel, err := filepath.Rel(e.Root, path)
	return rel, err == nil && e.Root != "" && !strings.HasPrefix(rel, "..")
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
