package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
)

func TestDrawTrail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := core.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(40, 8)
	if err := ed.ShowTrail("claude", "tour", []core.StepInput{{Path: path, Line: 2, Note: "the middle"}}); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReplyStep("claude", 1, "it's two"); err != nil {
		t.Fatal(err)
	}
	ed.Toast = core.Toast{}
	g := newGrid(40, 8)
	Draw(g, ed)
	for y, want := range []string{"  one", "  two", "▸ 1. the middle", "┆ claude: it's two", "  three"} {
		if got := g.row(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}

	ed.HandleKey("space")
	ed.HandleKey("T")
	g = newGrid(40, 8)
	Draw(g, ed)
	if strings.Contains(g.row(2), "middle") {
		t.Error("trail still drawn after space T")
	}
}
