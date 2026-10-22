package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/syntax"
	"github.com/synthlike/hiss/internal/text"
)

// grid is a fake Screen holding one rune per cell.
type grid struct {
	w, h   int
	cells  [][]rune
	styles [][]tcell.Style
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h}
	for range h {
		g.cells = append(g.cells, []rune(strings.Repeat(" ", w)))
		g.styles = append(g.styles, make([]tcell.Style, w))
	}
	return g
}

func (g *grid) Size() (int, int) { return g.w, g.h }

func (g *grid) Put(x, y int, str string, style tcell.Style) (string, int) {
	r, n := utf8.DecodeRuneInString(str)
	if x >= 0 && x < g.w && y >= 0 && y < g.h {
		g.cells[y][x], g.styles[y][x] = r, style
	}
	return str[n:], 1
}

func (g *grid) row(y int) string { return strings.TrimRight(string(g.cells[y]), " ") }

func TestDraw(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("a\tb\nxy\n"), text.Origin{})})
	ed.Resize(20, 6)
	ed.HandleKey("j")
	ed.HandleKey("l")
	g := newGrid(20, 6)
	Draw(g, ed)

	want := []string{"  a   b", "  xy", "", "~"}
	for y, w := range want {
		if got := g.row(y); got != w {
			t.Errorf("row %d = %q, want %q", y, got, w)
		}
	}
	if !g.styles[1][3].HasReverse() || g.styles[1][2].HasReverse() {
		t.Error("cursor not drawn on 'y'")
	}
	if got := g.row(4); !strings.HasPrefix(got, " NOR  a.go") || !strings.HasSuffix(got, "2:2") {
		t.Errorf("status = %q", got)
	}
}

func TestKeyName(t *testing.T) {
	cases := []struct {
		ev   *tcell.EventKey
		want string
	}{
		{tcell.NewEventKey(tcell.KeyRune, "j", tcell.ModNone), "j"},
		{tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModAlt), "A-x"},
		{tcell.NewEventKey(tcell.KeyRune, "d", tcell.ModCtrl), "C-d"},
		{tcell.NewEventKey(tcell.KeyEsc, "", tcell.ModNone), "esc"},
		{tcell.NewEventKey(tcell.KeyBackspace2, "", tcell.ModNone), "backspace"},
		{tcell.NewEventKey(tcell.KeyLeft, "", tcell.ModNone), "left"},
	}
	for _, c := range cases {
		if got := KeyName(c.ev); got != c.want {
			t.Errorf("KeyName(%s) = %q, want %q", c.ev.Name(), got, c.want)
		}
	}
}

func TestDrawHunk(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte("one\ntwo\nthree\n"), text.Origin{})
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(30, 9)
	if _, err := ed.ProposeEdit("claude", b.Path, "two\n", "2\nTWO\n", false, nil, "spell it out"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Reply("claude", 1, "spelled out"); err != nil {
		t.Fatal(err)
	}
	ed.Toast = core.Toast{} // the "replied" toast would cover the top rows
	ed.HandleKey("j")
	g := newGrid(30, 9)
	Draw(g, ed)
	want := []string{"  one", "- two", "+ 2", "+ TWO", "▸ #1 claude: spell it out", "┆ claude: spelled out", "  three"}
	for y, w := range want {
		if got := g.row(y); got != strings.TrimRight(w, " ") {
			t.Errorf("row %d = %q, want %q", y, got, w)
		}
	}
	if !strings.Contains(g.row(7), "1 pending") {
		t.Errorf("status = %q", g.row(7))
	}
	if got := g.row(8); !strings.HasPrefix(got, "#1 claude: spell it out") {
		t.Errorf("info = %q", got)
	}
}

func TestDrawCursorInTallHunk(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte("a\nb\nc\n"), text.Origin{})
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(30, 5) // three text rows
	if _, err := ed.ProposeEdit("claude", b.Path, "b\n", "1\n2\n3\n4\n5\n6\n", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	ed.Toast = core.Toast{}
	for range 5 {
		ed.HandleKey("j")
	}
	g := newGrid(30, 5)
	Draw(g, ed)
	for y, want := range []string{"+ 2", "+ 3", "+ 4"} {
		if got := g.row(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
	if !g.styles[2][0].HasReverse() || g.styles[1][0].HasReverse() {
		t.Error("cursor row should be the highlighted + line")
	}
}

func TestDrawHighlightsGo(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go",
		Text: text.New([]byte("x := `raw\nstill raw` // c\nreturn 1\n"), text.Origin{})})
	ed.Resize(40, 6)
	ed.HandleKey("j") // keep the cursor off the cells checked below
	ed.HandleKey("j")
	ed.HandleKey("l")
	g := newGrid(40, 6)
	Draw(g, ed)
	cases := []struct {
		x, y int
		want tcell.Style
	}{
		{2, 0, styleText},                     // x
		{7, 0, syntaxStyles[syntax.String]},   // `raw
		{2, 1, syntaxStyles[syntax.String]},   // still raw` (string continues)
		{14, 1, syntaxStyles[syntax.Comment]}, // // c
		{4, 2, syntaxStyles[syntax.Keyword]},  // return (the cursor is on its "e")
		{9, 2, syntaxStyles[syntax.Number]},   // 1
	}
	for _, c := range cases {
		if got := g.styles[c.y][c.x]; got != c.want {
			t.Errorf("(%d,%d) %q: style %v, want %v", c.x, c.y, g.cells[c.y][c.x], got, c.want)
		}
	}
}

func TestAgentStatus(t *testing.T) {
	ed := core.New(&core.Buffer{Path: "/p/a.go", Name: "a.go", Text: text.New([]byte("x\n"), text.Origin{})})
	ed.Resize(60, 4)
	ed.Hook(core.HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	ed.Hook(core.HookEvent{SessionID: "s1", Event: "Notification", NotificationType: "permission_prompt",
		Message: "Claude needs your permission to use Bash"})
	g := newGrid(60, 4)
	Draw(g, ed)
	if got := g.row(2); !strings.Contains(got, "claude waiting on you · turn 1") {
		t.Errorf("status = %q", got)
	}
	if got := g.row(3); got != "claude: Claude needs your permission to use Bash" {
		t.Errorf("message = %q", got)
	}
}

func TestSplash(t *testing.T) {
	ed := core.New(core.Scratch())
	ed.Splash = "Trust, but diff.\n— a reviewer"
	ed.Resize(40, 10)
	g := newGrid(40, 10)
	Draw(g, ed)
	var all string
	for y := range 8 {
		all += g.row(y) + "\n"
	}
	if !strings.Contains(all, "hiss") || !strings.Contains(all, "Trust, but diff.") {
		t.Errorf("no splash:\n%s", all)
	}
	ed.HandleKey("i")
	ed.HandleKey("x")
	g = newGrid(40, 10)
	Draw(g, ed)
	if strings.Contains(g.row(4), "Trust") {
		t.Error("splash should disappear once the buffer has text")
	}
	if !strings.Contains(g.row(8), "[scratch]") {
		t.Errorf("status = %q", g.row(8))
	}
}
