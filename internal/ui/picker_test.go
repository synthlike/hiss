package ui

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawPicker(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("x\n"), text.Origin{})})
	ed.Resize(50, 12)
	ed.Picker = &core.Picker{Title: "pending hunks", Items: []core.PickItem{
		{Label: "a.go:3", Detail: "#1 claude: rename"},
		{Label: "b.go:9", Detail: "#2 claude: add test"},
	}}
	ed.HandleKey("down")
	g := newGrid(50, 12)
	Draw(g, ed)
	if got := g.row(1); !strings.Contains(got, "pending hunks  2/2") {
		t.Errorf("title row = %q", got)
	}
	if got := g.row(2); !strings.Contains(got, "> ▏") {
		t.Errorf("query row = %q", got)
	}
	if got := g.row(4); !strings.Contains(got, "b.go:9  #2 claude: add test") {
		t.Errorf("item row = %q", got)
	}
	if g.styles[4][5].GetBackground() != styleSel.GetBackground() {
		t.Error("selected item not highlighted")
	}
	if g.styles[3][5].GetBackground() != styleMenu.GetBackground() {
		t.Error("unselected item should use the menu background")
	}
}
