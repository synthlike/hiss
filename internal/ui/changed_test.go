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
