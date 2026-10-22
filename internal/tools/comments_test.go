package tools

import (
	"strings"
	"testing"
)

func TestCommentsTextQuotesLine(t *testing.T) {
	got := CommentsText([]CommentInfo{
		{ID: 3, Path: "/p/a.go", Text: "typo", Line: "+ B1"},
		{ID: 4, Path: "/p/b.go", Text: "why?"},
	})
	for _, want := range []string{
		"proposal #3 (/p/a.go) on the diff line\n    + B1\n> typo\n",
		"proposal #4 (/p/b.go):\n> why?\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
