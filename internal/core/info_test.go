package core

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHunkInfoWraps(t *testing.T) {
	e := newEditor(t, "a\nb\n")
	e.Resize(40, 12)
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "a\n", "A\n", false, nil, "make it loud"); err != nil {
		t.Fatal(err)
	}
	keys(e, "g g")
	want := []string{"#1 claude: make it loud  (+ accept · -", "reject · # comment)"}
	if got := e.HunkInfo(); !slices.Equal(got, want) {
		t.Errorf("info %q, want %q", got, want)
	}
	if e.MessageRows() != 2 || e.Height != 9 {
		t.Errorf("message rows %d, text %d; want 2 and 9", e.MessageRows(), e.Height)
	}

	e.proposals[1].Rationale = strings.Repeat("very ", 30) + "long"
	got := e.HunkInfo()
	if len(got) != infoRows || !strings.HasSuffix(got[infoRows-1], "…") || len([]rune(got[infoRows-1])) > 40 {
		t.Errorf("long rationale: %q, want %d rows ending in an ellipsis", got, infoRows)
	}

	keys(e, "j j") // past its proposed line, off the hunk
	if e.HunkInfo() != nil || e.MessageRows() != 1 || e.Height != 10 {
		t.Errorf("off the hunk: info %q, text %d", e.HunkInfo(), e.Height)
	}
}

func TestFileStartAndEndOnProposedLines(t *testing.T) {
	e := newEditor(t, "")
	path := filepath.Join(filepath.Dir(e.Buf.Path), "new.txt")
	if _, err := e.WriteFile("claude", path, "one\ntwo\nthree\n", nil, ""); err != nil {
		t.Fatal(err)
	}
	e.SwitchTo(e.Find(path))
	keys(e, "g e")
	if e.DiffRow() != 3 {
		t.Errorf("ge on a new file: row %d, want the last proposed line, 3", e.DiffRow())
	}
	keys(e, "g g")
	if e.DiffRow() != 0 || e.line() != 0 {
		t.Errorf("gg: line %d row %d, want 0 0", e.line(), e.DiffRow())
	}
}
