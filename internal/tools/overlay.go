package tools

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

// OverlayOut is what go_overlay wrote.
type OverlayOut struct {
	Path      string   `json:"path"`
	Files     []string `json:"files"`
	Proposals []int    `json:"proposals"`
}

func (o OverlayOut) Text() string {
	if len(o.Files) == 0 {
		return fmt.Sprintf("Nothing is pending or unsaved, so the overlay at %s changes nothing.", o.Path)
	}
	ids := make([]string, len(o.Proposals))
	for i, id := range o.Proposals {
		ids[i] = fmt.Sprintf("#%d", id)
	}
	applied := "unsaved changes only"
	switch {
	case len(ids) == 1:
		applied = "proposal " + ids[0]
	case len(ids) > 1:
		applied = "proposals " + strings.Join(ids, ", ")
	}
	return fmt.Sprintf("Wrote %s with %s applied, replacing %s.\nRun, for example: go vet -overlay=%s ./... && go test -overlay=%s ./...",
		o.Path, applied, strings.Join(o.Files, ", "), o.Path, o.Path)
}

var overlayTool = define(&mcp.Tool{
	Name: "go_overlay",
	Description: "Write the open files as they'd be with all pending proposals accepted (and the user's unsaved " +
		"changes), with an overlay file for Go's -overlay flag. Then run go build, vet or test with " +
		"-overlay=<path> yourself to check proposals before the user reviews them. It's a snapshot: call it " +
		"again after proposing more. Tests that read source files while running see the files on disk.",
}, func(ed *core.Editor, _ string, _ struct{}) (OverlayOut, error) {
	o, err := ed.WriteOverlay()
	return OverlayOut{Path: o.Path, Files: o.Files, Proposals: o.Proposals}, err
})
