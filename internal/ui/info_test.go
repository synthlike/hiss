package ui

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawWrappedHunkInfo(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte("a\nb\nc\nd\ne\nf\n"), text.Origin{})
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(40, 10)
	if _, err := ed.ProposeEdit("claude", b.Path, "a\n", "A\n", false, nil, "make it loud"); err != nil {
		t.Fatal(err)
	}
	ed.Toast = core.Toast{}
	g := newGrid(40, 10)
	Draw(g, ed)
	if g.row(8) != "#1 claude: make it loud  (+ accept · -" || g.row(9) != "reject · # comment)" {
		t.Errorf("message rows %q %q", g.row(8), g.row(9))
	}
	if got := g.row(7); !strings.HasPrefix(got, " NOR") {
		t.Errorf("status = %q, want it above the message rows", got)
	}
	if got := g.row(6); got != "   5 e" { // after the rationale and the proposed line
		t.Errorf("last text row = %q, want line 5", got)
	}
}
