package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Location is a place an agent points the user at.
type Location struct {
	Path string // absolute
	Line int    // 1-based
	Note string
}

// LocationList is the latest list an agent shared; space l opens it.
type LocationList struct {
	Agent, Title string
	Items        []Location
}

// ShowLocations stores a list from an agent and tells the user about it.
func (e *Editor) ShowLocations(agent, title string, items []Location) {
	e.locations = &LocationList{agent, title, items}
	e.notify(agent, fmt.Sprintf("shared %d locations: %s (space l)", len(items), title))
}

// LocationNotes returns the shared list's notes for the visible buffer, by
// 0-based line, for drawing under the code they describe.
func (e *Editor) LocationNotes() map[int][]string {
	if e.locations == nil {
		return nil
	}
	var notes map[int][]string
	for _, loc := range e.locations.Items {
		if loc.Path != e.Buf.Path || loc.Note == "" {
			continue
		}
		if notes == nil {
			notes = map[int][]string{}
		}
		line := min(max(loc.Line-1, 0), e.Buf.Text.LineCount()-1)
		notes[line] = append(notes[line], loc.Note)
	}
	return notes
}

func (e *Editor) clearLocations() {
	e.locations = nil
	e.Msg = "cleared the agent's locations"
}

// openLocationPicker shows the shared list in its original order.
func (e *Editor) openLocationPicker() {
	l := e.locations
	if l == nil {
		e.Msg = "no locations shared yet"
		return
	}
	p := &Picker{Title: l.Agent + ": " + l.Title}
	for _, loc := range l.Items {
		name := loc.Path
		if rel, err := filepath.Rel(e.Root, loc.Path); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		p.Items = append(p.Items, PickItem{Label: fmt.Sprintf("%s:%d", name, loc.Line), Detail: loc.Note,
			Run: func(e *Editor) { e.goToLocation(loc) }})
	}
	e.Picker = p
}

func (e *Editor) goToLocation(loc Location) {
	e.openFile(loc.Path)
	if e.Buf.Path != loc.Path {
		return // opening failed; openFile said why
	}
	line := min(max(loc.Line-1, 0), e.Buf.Text.LineCount()-1)
	e.setHead(e.Buf.Text.LineStart(line))
	e.Top, e.TopSub = max(line-2, 0), 0
}
