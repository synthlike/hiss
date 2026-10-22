package ui

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawTweakedHunk(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte("one\ntwo\nthree\n"), text.Origin{})
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(40, 9)
	if _, err := ed.ProposeEdit("claude", b.Path, "two\n", "2\nTWO\n", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	ed.Toast = core.Toast{}
	for _, k := range []string{"j", "j", "i", "X", "esc"} {
		ed.HandleKey(k)
	}
	g := newGrid(40, 9)
	Draw(g, ed)
	for y, want := range []string{"  one", "+ X2", "+ TWO", "- two", "  three"} {
		if got := g.row(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
	if got := g.row(8); !strings.Contains(got, "editing:") {
		t.Errorf("info = %q", got)
	}
}
