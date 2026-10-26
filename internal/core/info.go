package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// On a hunk, the message line says what it is: its id, agent, change set and
// rationale, and the keys that review it. It wraps to up to infoRows rows,
// and the text area gives up the rows past the first.

const infoRows = 3

// HunkInfo returns the message area's rows for the hunk under the cursor, or
// nil when it shows something else: the command line, a message, or a
// problem on a proposed line.
func (e *Editor) HunkInfo() []string {
	if e.Mode == Command || e.Mode == Search || e.Msg != "" {
		return nil
	}
	hk := e.HunkAtCursor()
	if hk == nil || e.DiffRow() > 0 && e.DiagAtCursor() != nil {
		return nil
	}
	info := fmt.Sprintf("#%d %s", hk.P.ID, hk.P.Agent)
	r, total := e.SetProgress(hk.P)
	if total > 1 {
		info += fmt.Sprintf(" (set %d/%d reviewed)", r, total)
	}
	if hk.P.Rationale != "" {
		info += ": " + strings.Join(strings.Fields(hk.P.Rationale), " ")
	}
	switch {
	case hk.Tweaked:
		info += "  (editing: + accept as edited, - restore and reject)"
	case total > 1:
		info += "  (+ accept, - reject, # comment, :sa the set)"
	default:
		info += "  (+ accept, - reject, # comment)"
	}
	if e.Width <= 0 {
		return []string{info}
	}
	starts := wrapAt(info, e.Width)
	var rows []string
	for i, a := range starts {
		b := len(info)
		if i+1 < len(starts) {
			b = starts[i+1]
		}
		row := strings.TrimRight(info[a:b], " ")
		if i == infoRows-1 && i+1 < len(starts) { // more than fits: end with an ellipsis
			if utf8.RuneCountInString(row) >= e.Width {
				_, n := utf8.DecodeLastRuneInString(row)
				row = row[:len(row)-n]
			}
			rows = append(rows, row+"…")
			break
		}
		rows = append(rows, row)
	}
	return rows
}

// MessageRows returns the rows under the status line: one, or more while
// the hunk info wraps.
func (e *Editor) MessageRows() int { return max(len(e.HunkInfo()), 1) }
