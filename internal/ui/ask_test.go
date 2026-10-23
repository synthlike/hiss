package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawAsk(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("x\n"), text.Origin{})})
	ed.Resize(50, 14)
	ed.Ask("claude", "Commit the bell change?", []string{"yes", "no"})
	ed.HandleKey("down")
	g := newGrid(50, 14)
	Draw(g, ed)
	// 7 rows centred in the 12-row text area start at row 2, below the toast.
	want := []string{"claude asks", "Commit the bell change?", "", "yes", "no", "> or type an answer", "↑↓ choose"}
	for i, w := range want {
		if got := g.row(2 + i); !strings.Contains(got, w) {
			t.Errorf("row %d = %q, want %q", 2+i, got, w)
		}
	}
	if g.styles[6][5].GetBackground() != styleSel.GetBackground() {
		t.Error("selected option not highlighted")
	}
}

func TestDrawAskWrapsTypedAnswer(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("x\n"), text.Origin{})})
	ed.Resize(30, 14)
	ed.Ask("claude", "Why?", nil)
	for _, k := range strings.Repeat("ab", 20) {
		ed.HandleKey(string(k))
	}
	g := newGrid(30, 14)
	Draw(g, ed)
	var all string
	for y := range 12 {
		all += strings.Trim(g.row(y), " ~")
	}
	if !strings.Contains(all, "> "+strings.Repeat("ab", 20)+"▏") {
		t.Errorf("typed answer cut off: %q", all)
	}
}

func TestWrap(t *testing.T) {
	got := wrap("one two three\nfour", 8)
	if want := []string{"one two", "three", "four"}; !slices.Equal(got, want) {
		t.Errorf("wrap = %q", got)
	}
	if got := chunk("abcdé fg", 3); !slices.Equal(got, []string{"abc", "dé ", "fg"}) {
		t.Errorf("chunk = %q", got)
	}
}
