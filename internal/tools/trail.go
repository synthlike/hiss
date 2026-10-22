package tools

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

type StepIn struct {
	Path string `json:"path" jsonschema:"absolute path, or relative to the project root"`
	Line int    `json:"line,omitempty" jsonschema:"1-based line (default 1)"`
	Note string `json:"note,omitempty" jsonschema:"what to look at here, in one line; hiss numbers the steps"`
}

type ShowTrailIn struct {
	Title string   `json:"title" jsonschema:"what the trail shows, e.g. 'how a proposal reaches the screen'"`
	Steps []StepIn `json:"steps" jsonschema:"the steps in the order to visit them"`
}

type ShowTrailOut struct {
	Steps int `json:"steps"`
}

func (o ShowTrailOut) Text() string {
	return fmt.Sprintf("Shared a trail of %d steps; the user walks it with ]t and may comment on steps.", o.Steps)
}

const maxSteps = 500

func showTrail(ed *core.Editor, agent string, in ShowTrailIn) (ShowTrailOut, error) {
	switch {
	case len(in.Steps) == 0:
		return ShowTrailOut{}, errors.New("no steps")
	case len(in.Steps) > maxSteps:
		return ShowTrailOut{}, fmt.Errorf("%d steps; share at most %d", len(in.Steps), maxSteps)
	}
	var steps []core.StepInput
	var missing []string
	for _, s := range in.Steps {
		p := resolve(ed, s.Path)
		if _, err := os.Stat(p); err != nil && ed.Find(p) == nil {
			missing = append(missing, s.Path)
			continue
		}
		steps = append(steps, core.StepInput{Path: p, Line: max(s.Line, 1), Note: s.Note})
	}
	if len(missing) > 0 {
		return ShowTrailOut{}, fmt.Errorf("these paths don't exist: %s", strings.Join(missing, ", "))
	}
	if err := ed.ShowTrail(agent, in.Title, steps); err != nil {
		return ShowTrailOut{}, err
	}
	return ShowTrailOut{Steps: len(steps)}, nil
}

var showTrailTool = define(&mcp.Tool{
	Name: "show_trail",
	Description: "Walk the user through code: a titled, ordered list of steps (file, line, short note), " +
		"such as a flow, call sites, or places a change affects. The user steps through it in the editor " +
		"(]t, space t), sees each note under its line, and can comment on a step; answer with reply (step). " +
		"Prefer this to listing file:line references in chat. A new trail replaces the previous one.",
}, showTrail)
