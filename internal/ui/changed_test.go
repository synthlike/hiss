package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
)

func TestDrawChangedLines(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f.go"
	os.WriteFile(path, []byte("a\nb\n"), 0o600)
	b, err := core.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(40, 10)
	ps, err := ed.ProposeEdit("claude", path, "b\n", "B\n", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ed.Settle("claude", ps, false)
	g := newGrid(40, 10)
	Draw(g, ed)
	if got := g.row(1); !strings.HasPrefix(got, "▏") {
		t.Errorf("changed line %q", got)
	}
	if got := g.row(0); strings.HasPrefix(got, "▏") {
		t.Errorf("unchanged line %q", got)
	}
}

func TestDrawRemovedRow(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f.go"
	os.WriteFile(path, []byte("a\nb\n"), 0o600)
	b, err := core.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(40, 10)
	if err := ed.ShowTrail("claude", "t", []core.StepInput{{Path: path, Line: 1, Note: "n"}}); err != nil {
		t.Fatal(err)
	}
	ed.Trail().Steps[0].Removed = []string{"old"}
	g := newGrid(40, 10)
	Draw(g, ed)
	if got := g.row(2); !strings.HasPrefix(got, "-") || !strings.Contains(got, "old") {
		t.Errorf("rows %q %q %q", g.row(0), g.row(1), g.row(2))
	}
}
