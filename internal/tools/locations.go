package tools

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

type LocationIn struct {
	Path string `json:"path" jsonschema:"absolute path, or relative to the project root"`
	Line int    `json:"line,omitempty" jsonschema:"1-based line (default 1)"`
	Note string `json:"note,omitempty" jsonschema:"what to look at here, in one line"`
}

type ShowLocationsIn struct {
	Title string       `json:"title" jsonschema:"what the list is, e.g. 'call sites of Save'"`
	Items []LocationIn `json:"items" jsonschema:"locations in the order the user should visit them"`
}

type ShowLocationsOut struct {
	Count int `json:"count"`
}

func (o ShowLocationsOut) Text() string {
	return fmt.Sprintf("Shared %d locations; the user opens them in the editor with space l.", o.Count)
}

const maxLocations = 500

func showLocations(ed *core.Editor, agent string, in ShowLocationsIn) (ShowLocationsOut, error) {
	switch {
	case len(in.Items) == 0:
		return ShowLocationsOut{}, errors.New("no items")
	case len(in.Items) > maxLocations:
		return ShowLocationsOut{}, fmt.Errorf("%d items; share at most %d", len(in.Items), maxLocations)
	}
	var locs []core.Location
	var missing []string
	for _, it := range in.Items {
		p := resolve(ed, it.Path)
		if _, err := os.Stat(p); err != nil && ed.Find(p) == nil {
			missing = append(missing, it.Path)
			continue
		}
		locs = append(locs, core.Location{Path: p, Line: max(it.Line, 1), Note: it.Note})
	}
	if len(missing) > 0 {
		return ShowLocationsOut{}, fmt.Errorf("these paths don't exist: %s", strings.Join(missing, ", "))
	}
	ed.ShowLocations(agent, in.Title, locs)
	return ShowLocationsOut{Count: len(locs)}, nil
}

var showLocationsTool = define(&mcp.Tool{
	Name: "show_locations",
	Description: "Share a list of code locations with the user: call sites, places a change affects, review " +
		"findings, or the steps of a flow. They open in the editor's picker (space l), where the user jumps " +
		"between them. Prefer this to listing file:line references in chat.",
}, showLocations)
