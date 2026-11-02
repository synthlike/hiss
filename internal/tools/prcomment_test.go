package tools

import (
	"strings"
	"testing"
)

// The drafting itself is tested in core, with a PR checked out.
func TestDraftPRCommentNeedsAPR(t *testing.T) {
	cs, _ := setup(t, "x\n")
	res := call(t, cs, "draft_pr_comment", map[string]any{"path": "main.go", "line": 1, "body": "hm"})
	if !res.IsError || !strings.Contains(text(res), "no PR checked out here") {
		t.Errorf("IsError=%v text=%q", res.IsError, text(res))
	}
}
