package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/core"
)

func TestDrawPanel(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{".hissrun": "[x]\nmatch = *.txt\ncmd = echo one; echo two\n", "f.txt": "a\nb\n"}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := core.Open(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	ed.Root = dir
	ch := make(chan func(*core.Editor), 16)
	ed.Post = func(f func(*core.Editor)) { ch <- f }
	t.Cleanup(ed.StopRuns)
	ed.Resize(40, 14) // a panel of four rows over the status line
	ed.HandleKey("enter")
	timeout := time.After(10 * time.Second)
	for ed.Running() != "" {
		select {
		case f := <-ch:
			f(ed)
		case <-timeout:
			t.Fatal("the run didn't finish")
		}
	}
	g := newGrid(40, 14)
	Draw(g, ed)
	want := []string{"   1 a", "   2 b", "   3", "~"}
	for y, w := range want {
		if got := g.row(y); got != w {
			t.Errorf("row %d = %q, want %q", y, got, w)
		}
	}
	if got := g.row(8); !strings.HasPrefix(got, " *out*") || !strings.HasSuffix(got, "space o opens, esc hides") {
		t.Errorf("title = %q", got)
	}
	if g.row(9) != " one" || g.row(10) != " two" || !strings.HasPrefix(g.row(11), " [ok, ") {
		t.Errorf("panel = %q %q %q", g.row(9), g.row(10), g.row(11))
	}
	if got := g.row(7); got != "~" {
		t.Errorf("row above the panel = %q, want the text area's", got)
	}

	ed.HandleKey("esc")
	g = newGrid(40, 14)
	Draw(g, ed)
	if strings.Contains(g.row(8), "*out*") {
		t.Errorf("after esc, row 8 = %q", g.row(8))
	}
}
