package core

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// PickItem is one row of a picker.
type PickItem struct {
	Label  string // e.g. "internal/core/editor.go:42"
	Detail string // shown dimmed after the label; also matched
	Run    func(*Editor)
}

// Picker is a fuzzy-filtered list shown over the editor. Typing filters it,
// up/down move, enter runs the selected item, esc closes it.
type Picker struct {
	Title    string
	Items    []PickItem
	Query    string
	Selected int // index into Matches()

	matched  bool // matches holds the result for matchedQ
	matchedQ string
	matches  []PickItem
}

// Matches returns the items matching the query, best first.
func (p *Picker) Matches() []PickItem {
	if p.matched && p.matchedQ == p.Query {
		return p.matches
	}
	p.matched, p.matchedQ, p.matches = true, p.Query, p.match()
	return p.matches
}

func (p *Picker) match() []PickItem {
	type match struct {
		it    PickItem
		score int
	}
	var ms []match
	for _, it := range p.Items {
		if score, ok := fuzzy(p.Query, it.Label+" "+it.Detail); ok {
			ms = append(ms, match{it, score})
		}
	}
	slices.SortStableFunc(ms, func(a, b match) int { return a.score - b.score })
	out := make([]PickItem, len(ms))
	for i, m := range ms {
		out[i] = m.it
	}
	return out
}

// fuzzy reports whether the runes of query appear in s in order, ignoring
// case. Lower scores are better: matches that start early and stay together.
func fuzzy(query, s string) (score int, ok bool) {
	q := []rune(strings.ToLower(query))
	if len(q) == 0 {
		return 0, true
	}
	i, last := 0, -1
	for pos, r := range []rune(strings.ToLower(s)) {
		if r != q[i] {
			continue
		}
		if last < 0 {
			score += pos
		} else {
			score += pos - last - 1
		}
		last = pos
		if i++; i == len(q) {
			return score, true
		}
	}
	return 0, false
}

func (e *Editor) pickerKey(k string) {
	p := e.Picker
	n := len(p.Matches())
	switch k {
	case "esc", "C-c":
		e.Picker = nil
	case "enter":
		e.Picker = nil
		if ms := p.Matches(); len(ms) > 0 {
			ms[min(p.Selected, len(ms)-1)].Run(e)
		}
	case "down", "C-n", "tab":
		if n > 0 {
			p.Selected = (p.Selected + 1) % n
		}
	case "up", "C-p", "backtab":
		if n > 0 {
			p.Selected = (p.Selected - 1 + n) % n
		}
	case "backspace":
		_, size := utf8.DecodeLastRuneInString(p.Query)
		p.Query, p.Selected = p.Query[:len(p.Query)-size], 0
	case "space":
		p.Query, p.Selected = p.Query+" ", 0
	default:
		if utf8.RuneCountInString(k) == 1 {
			p.Query, p.Selected = p.Query+k, 0
		}
	}
}

// openHunkPicker lists pending hunks in all buffers; enter jumps to one.
func (e *Editor) openHunkPicker() {
	p := &Picker{Title: "hunks"}
	for _, b := range e.Bufs {
		for _, q := range b.pending {
			detail := fmt.Sprintf("#%d %s", q.ID, q.Agent)
			if q.Rationale != "" {
				detail += ": " + q.Rationale
			}
			p.Items = append(p.Items, PickItem{
				Label:  fmt.Sprintf("%s:%d", b.Name, b.Text.LineOf(q.Start)+1),
				Detail: detail,
				Run:    func(e *Editor) { e.jumpToProposal(q) },
			})
		}
	}
	if len(p.Items) == 0 {
		e.Msg = "no pending hunks"
		return
	}
	e.Picker = p
}

func (e *Editor) jumpToProposal(p *Proposal) {
	if p.Status != Pending {
		e.Msg = fmt.Sprintf("#%d is %s", p.ID, p.Status)
		return
	}
	e.pushJump()
	e.SwitchTo(p.Buf)
	e.showHunk(p)
}

// showHunk puts the cursor on p's first line and scrolls its hunk into view
// (see showLines).
func (e *Editor) showHunk(p *Proposal) {
	t := p.Buf.Text
	first := t.LineOf(p.Start)
	e.setHead(t.LineStart(first))
	e.showLines(first, t.LineOf(max(p.End-1, p.Start)))
}
