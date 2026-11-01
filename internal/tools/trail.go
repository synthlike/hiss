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
	return fmt.Sprintf("Shared a trail of %d steps; the user walks it with ]s and may comment on steps.", o.Steps)
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

type ShowReviewIn struct {
	Title string   `json:"title" jsonschema:"what the user is reviewing, e.g. 'retry logic for the importer'"`
	Rev   string   `json:"rev,omitempty" jsonschema:"a commit or a range such as main..HEAD; by default uncommitted changes, or the last commit if there are none"`
	Paths []string `json:"paths,omitempty" jsonschema:"files or directories to include; all of the change if empty"`
	Notes []StepIn `json:"notes,omitempty" jsonschema:"what to look at in particular: each note goes on the hunk holding its line, in the file as it is now"`
}

type ShowReviewOut struct {
	Hunks     int `json:"hunks"`
	Unmatched int `json:"unmatched_notes,omitempty"`
}

func (o ShowReviewOut) Text() string {
	s := fmt.Sprintf("Sent %s for review. The user walks them with ]s; comments and rejections reach you as they "+
		"happen (a rejection has already restored the old text), and you get a report when the review ends. "+
		"Wait with await_review and review set, or end your turn and be woken.", plural(o.Hunks, "hunk"))
	if o.Unmatched > 0 {
		s += fmt.Sprintf(" %s matched no hunk.", plural(o.Unmatched, "note"))
	}
	return s
}

func plural(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}

func showReview(ed *core.Editor, agent string, in ShowReviewIn) (ShowReviewOut, error) {
	var paths []string
	for _, p := range in.Paths {
		paths = append(paths, resolve(ed, p))
	}
	var notes []core.StepInput
	for _, n := range in.Notes {
		notes = append(notes, core.StepInput{Path: resolve(ed, n.Path), Line: max(n.Line, 1), Note: n.Note})
	}
	hunks, unmatched, err := ed.ShowReview(agent, in.Title, in.Rev, paths, notes)
	if err != nil {
		return ShowReviewOut{}, err
	}
	return ShowReviewOut{Hunks: hunks, Unmatched: unmatched}, nil
}

var showReviewTool = define(&mcp.Tool{
	Name: "show_review",
	Description: "Send the user changes that have landed to review, hunk by hunk: uncommitted changes, a commit or " +
		"a range, narrowed to paths, with notes on what to look at. They accept, reject or comment on each hunk, and " +
		"can finish early. A rejected hunk is reverted in the working tree. Answer comments with reply (step). " +
		"Replaces any trail being shown.",
}, showReview)

var showTrailTool = define(&mcp.Tool{
	Name: "show_trail",
	Description: "Walk the user through code: a titled, ordered list of steps (file, line, short note), " +
		"such as a flow, call sites, or places a change affects. The user steps through it in the editor " +
		"(]s, space t), sees each note under its line, and can comment on a step; answer with reply (step). " +
		"Prefer this to listing file:line references in chat. A new trail replaces the previous one.",
}, showTrail)
