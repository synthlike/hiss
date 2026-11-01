package tools

import (
	"strings"
	"testing"
)

func TestCommentsTextQuotesLine(t *testing.T) {
	got := CommentsText([]CommentInfo{
		{ID: 3, Path: "/p/a.go", Text: "typo", Line: "+ B1"},
		{ID: 4, Path: "/p/b.go", Text: "why?"},
		{Step: 2, Path: "/p/c.go", LineNo: 7, Text: "safe?", Review: "abc123 fix it"},
	})
	for _, want := range []string{
		"proposal #3 (/p/a.go) on the diff line\n    + B1\n> typo\n",
		"proposal #4 (/p/b.go):\n> why?\n",
		"step 2 of the review of abc123 fix it (/p/c.go:7):\n> safe?\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
