package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/core"
)

func TestToast(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(60, 8)
	if _, err := ed.WriteFile("claude", dir+"/g.go", "package g\n", nil, ""); err != nil {
		t.Fatal(err)
	}
	g := newGrid(60, 8)
	Draw(g, ed)
	if got := g.row(0); !strings.HasSuffix(got, "claude") {
		t.Errorf("toast title row = %q", got)
	}
	if got := g.row(1); !strings.HasSuffix(got, "proposed 1 hunk in g.go (]h to review)") {
		t.Errorf("toast text row = %q", got)
	}
	if g.styles[1][59] != styleMenu {
		t.Error("toast not drawn in menu style")
	}
	if n := ed.TakeNotices(); len(n) != 1 || n[0].Title != "claude" || len(ed.TakeNotices()) != 0 {
		t.Errorf("notices = %+v", n)
	}

	ed.Toast.Until = time.Time{} // expired
	g = newGrid(60, 8)
	Draw(g, ed)
	if strings.Contains(g.row(0), "claude") {
		t.Error("expired toast still drawn")
	}
}
