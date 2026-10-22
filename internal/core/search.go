package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Search: / opens a prompt that searches as you type; enter keeps the match
// selected and esc goes back to where the search started. n and N repeat the
// last search, * searches for the selection. Patterns are Go regular
// expressions, case-insensitive unless they contain an upper-case letter; one
// that doesn't compile is searched for literally. Matches stay highlighted
// until esc in normal mode.

type searchState struct {
	re      *regexp.Regexp // the last search, for n, N and highlighting
	pattern string         // as typed, for messages
	shown   bool           // highlight its matches

	// While the prompt is open: what esc restores.
	from        Selection
	mode        Mode
	top, topSub int
	prev        *regexp.Regexp
	prevPattern string
}

// compile turns a typed pattern into a regexp, with smart case.
func compile(p string) *regexp.Regexp {
	if p == "" {
		return nil
	}
	flags := ""
	if strings.ToLower(p) == p {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + p)
	if err != nil {
		re = regexp.MustCompile(flags + regexp.QuoteMeta(p))
	}
	return re
}

func (e *Editor) startSearch() {
	s := &e.search
	s.from, s.mode, s.top, s.topSub = e.Sel, e.Mode, e.Top, e.TopSub
	s.prev, s.prevPattern = s.re, s.pattern
	e.Mode, e.Cmd = Search, ""
}

func (e *Editor) cancelSearch() {
	s := &e.search
	e.Sel, e.Mode, e.Top, e.TopSub = s.from, s.mode, s.top, s.topSub
	s.re, s.pattern = s.prev, s.prevPattern
}

func (e *Editor) searchKey(k string) {
	s := &e.search
	switch k {
	case "esc", "C-c":
		e.cancelSearch()
		return
	case "enter":
		e.Mode = s.mode
		switch {
		case e.Cmd != "": // search again, for the match count the key cleared
			e.Sel = s.from
			e.findMatch(1, s.mode)
		case s.prev != nil: // like Vim, an empty search repeats the last one
			s.re, s.pattern = s.prev, s.prevPattern
			e.searchNext(1)
		}
		return
	case "tab", "backtab": // the : line completes commands; there is nothing to complete here
		return
	case "backspace":
		if e.Cmd == "" {
			e.cancelSearch()
			return
		}
	}
	e.commandKey(k) // edits e.Cmd like the : line
	s.re, s.pattern, s.shown = compile(e.Cmd), e.Cmd, true
	e.Sel = s.from
	if s.re != nil {
		e.findMatch(1, s.mode)
	}
}

// searchNext moves to the next (dir 1) or previous (dir -1) match of the last search.
func (e *Editor) searchNext(dir int) {
	if e.search.re == nil {
		e.Msg = "no search yet: / to search"
		return
	}
	e.search.shown = true
	e.findMatch(dir, e.Mode)
}

// searchSelection searches for the selected text, or the word under the
// cursor when only one character is selected.
func (e *Editor) searchSelection() {
	from, to := e.selRange()
	if to-from <= 1 && e.classAt(from) == word {
		for from > 0 && e.classAt(e.prevRune(from)) == word {
			from = e.prevRune(from)
		}
		for to < e.Buf.Text.Len() && e.classAt(to) == word {
			to = e.nextRune(to)
		}
	}
	text := string(e.Buf.Text.Slice(from, to))
	if text == "" {
		return
	}
	p := regexp.QuoteMeta(text)
	if e.classAt(from) == word && e.classAt(e.prevRune(to)) == word {
		p = `\b` + p + `\b`
	}
	e.search.re, e.search.pattern = regexp.MustCompile(p), text
	e.searchNext(1)
}

// findMatch selects the first match after the selection's start (dir 1) or
// the last one before it (dir -1), wrapping around the buffer. In Select
// mode (mode is the mode the search applies in) the anchor stays put.
func (e *Editor) findMatch(dir int, mode Mode) {
	ms := e.matches(0, e.Buf.Text.Len())
	if len(ms) == 0 {
		e.Msg = "no match: " + e.search.pattern
		return
	}
	cur, _ := e.selRange()
	i, wrapped := 0, false
	if dir > 0 {
		i = sort.Search(len(ms), func(i int) bool { return ms[i][0] > cur })
		if i == len(ms) {
			i, wrapped = 0, true
		}
	} else {
		i = sort.Search(len(ms), func(i int) bool { return ms[i][0] >= cur }) - 1
		if i < 0 {
			i, wrapped = len(ms)-1, true
		}
	}
	m := ms[i]
	head := e.prevRune(m[1])
	if mode == Select {
		e.Sel.Head = head
	} else {
		e.Sel = Selection{m[0], head}
	}
	e.goal = -1
	e.revealLine(e.Buf.Text.LineOf(m[0]))
	e.Msg = fmt.Sprintf("/%s  %d/%d", e.search.pattern, i+1, len(ms))
	if wrapped {
		e.Msg += " (wrapped)"
	}
}

// Matches returns the highlighted search matches overlapping [from, to), as
// sorted [start, end) pairs.
func (e *Editor) Matches(from, to int) [][2]int {
	if !e.search.shown {
		return nil
	}
	return e.matches(from, to)
}

func (e *Editor) matches(from, to int) [][2]int {
	if e.search.re == nil {
		return nil
	}
	var out [][2]int
	for _, m := range e.search.re.FindAllIndex(e.Buf.Text.Slice(from, to), -1) {
		if m[1] > m[0] { // an empty match has nothing to select
			out = append(out, [2]int{from + m[0], from + m[1]})
		}
	}
	return out
}

// gotoLine moves to the start of 1-based line n, clamped to the buffer.
func (e *Editor) gotoLine(n int) {
	line := min(max(n-1, 0), max(e.Buf.Lines()-1, 0))
	e.setHead(e.Buf.Text.LineStart(line))
	e.revealLine(line)
}

// revealLine scrolls so line is a third of the way down the screen, unless
// it is already visible.
func (e *Editor) revealLine(line int) {
	if line < e.Top || line >= e.Top+e.Height {
		e.Top, e.TopSub = max(line-e.Height/3, 0), 0
	}
}
