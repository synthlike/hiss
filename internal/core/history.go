package core

import (
	"fmt"
	"time"

	"github.com/synthlike/hiss/internal/text"
)

// Finished runs' output is kept in memory, the newest last, and [r / ]r
// put an older one back in *out*.

// keptRuns is how many finished runs' output is kept.
const keptRuns = 20

// pastRun is a finished run and *out* as it ended.
type pastRun struct {
	r      *run
	out    string
	header int // bytes of out before the output
	status string
	env    string
	ended  time.Time
}

// keep adds r, which just finished with status, to the history and shows it.
func (e *Editor) keep(r *run, status string) {
	h := &e.runs.history
	*h = append(*h, pastRun{r: r, out: e.runs.out.Text.String(), header: e.runs.header,
		status: status, env: r.envName, ended: e.now()})
	if len(*h) > keptRuns {
		*h = (*h)[len(*h)-keptRuns:]
	}
	e.runs.at = len(*h) - 1
}

// shownRun returns the run *out* shows: the one going, the one picked with
// [r / ]r, or the last; nil before any.
func (e *Editor) shownRun() *run {
	if h := e.runs.history; e.runs.going == nil && e.runs.at < len(h) {
		return h[e.runs.at].r
	}
	return e.runs.last
}

// stepRun shows the output of the run dir runs before (-1) or after (1) the
// one shown, first showing the panel if neither it nor *out* is on screen.
func (e *Editor) stepRun(dir int) {
	h := e.runs.history
	switch {
	case e.runs.going != nil:
		e.Msg = e.label(e.runs.going) + " is still running (C-c to cancel)"
		return
	case len(h) == 0:
		e.Msg = "nothing run yet"
		return
	case e.Buf != e.runs.out && !e.runs.panel:
		e.runs.panel = true
		return
	}
	at := e.runs.at + dir
	switch {
	case at < 0:
		e.Msg = "no older runs (" + plural(len(h), "run") + " kept)"
		return
	case at >= len(h):
		e.Msg = "no newer runs"
		return
	}
	e.runs.at = at
	p := h[at]
	out := e.runs.out
	out.Replace(0, out.Text.Len(), []byte(p.out), text.Origin{})
	out.undo, out.redo = nil, nil
	e.runs.header = p.header
	if e.Buf == out {
		e.setHead(out.Text.LineStart(out.Text.LineCount() - 1))
		e.Top, e.TopSub = 0, 0
		e.Msg = e.OutInfo()
	}
}

// OutInfo describes what *out* shows for its title, such as
// "run 3/7 · orders.sql · dev · exit status 1 · 2m ago", or "".
func (e *Editor) OutInfo() string {
	if r := e.Running(); r != "" {
		return r
	}
	h := e.runs.history
	if e.runs.at >= len(h) {
		return ""
	}
	p := h[e.runs.at]
	s := fmt.Sprintf("run %d/%d · %s", e.runs.at+1, len(h), p.r.what)
	if p.env != "" {
		s += " · " + p.env
	}
	return s + " · " + p.status + " · " + ago(e.now().Sub(p.ended))
}

// ago reads like "just now", "40s ago" or "2h ago".
func ago(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
