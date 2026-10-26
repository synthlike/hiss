package ui

import (
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawSearch(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.txt", Name: "a.txt", Text: text.New([]byte("one two\ntwo\n"), text.Origin{})})
	ed.Resize(20, 6)
	for _, k := range []string{"/", "t", "w", "o"} {
		ed.HandleKey(k)
	}
	g := newGrid(20, 6)
	Draw(g, ed)
	if got := g.row(5); got != "/two" {
		t.Errorf("prompt = %q", got)
	}
	if g.styles[0][9] != styleSel { // the current match is selected
		t.Errorf("current match style = %v", g.styles[0][9])
	}
	if g.styles[1][5] != styleMatch || g.styles[1][7] != styleMatch {
		t.Errorf("other match not highlighted: %v", g.styles[1][5])
	}
	if g.styles[0][5] == styleMatch {
		t.Error("non-matching text highlighted")
	}
}
