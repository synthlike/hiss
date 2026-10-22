package ui

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawLocationNotes(t *testing.T) {
	b := &core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("one\ntwo\nthree\n"), text.Origin{})}
	ed := core.New(b)
	ed.Resize(40, 8)
	ed.ShowLocations("claude", "tour", []core.Location{
		{Path: "/p/a.go", Line: 2, Note: "2. the middle"},
		{Path: "/p/other.go", Line: 1, Note: "elsewhere"},
	})
	ed.Toast = core.Toast{}
	g := newGrid(40, 8)
	Draw(g, ed)
	for y, want := range []string{"  one", "  two", "▸ 2. the middle", "  three"} {
		if got := g.row(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}

	ed.HandleKey("space")
	ed.HandleKey("L")
	g = newGrid(40, 8)
	Draw(g, ed)
	if strings.Contains(g.row(2), "middle") {
		t.Error("notes still drawn after space L")
	}
}
