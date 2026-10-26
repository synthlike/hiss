package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/synthlike/hiss/internal/core"
)

// RunInfo is a shared run the user finished in hiss.
type RunInfo struct {
	Title   string `json:"title"`
	Command string `json:"command"`
	Status  string `json:"status"`
	Took    string `json:"took"`
	Tail    string `json:"tail"`
}

func runInfo(r *core.RunReport) *RunInfo {
	if r == nil {
		return nil
	}
	return &RunInfo{Title: r.Title, Command: r.Command, Status: r.Status,
		Took: r.Took.Round(100 * time.Millisecond).String(), Tail: r.Tail}
}

// RunText tells an agent about a run the user did.
func RunText(r *RunInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "The user ran %s in hiss: %s (%s).\n    $ %s\n", r.Title, r.Status, r.Took, r.Command)
	if r.Tail != "" {
		sb.WriteString("The output ended with:\n")
		for l := range strings.Lines(r.Tail) {
			sb.WriteString("    ")
			sb.WriteString(l)
		}
		if !strings.HasSuffix(r.Tail, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
