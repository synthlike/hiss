package core

import (
	"path/filepath"
	"testing"

	"github.com/synthlike/hiss/internal/syntax"
)

func TestJumpEntry(t *testing.T) {
	const doc = `[
  {
    "a": 1,
    "b": [2, 3]
  },
  {
    "a": 4
  }
]
`
	cases := []struct {
		start int // line
		keys  string
		line  int
		msg   string
	}{
		{1, "] e", 5, ""},
		{5, "] e", 5, "no next entry"}, // the closing ] is shallower: the end of the array
		{5, "[ e", 1, ""},
		{2, "] e", 3, ""},
		{3, "] e", 3, "no next entry"}, // stays inside the object
		{0, "] e", 0, "no next entry"},
	}
	for _, c := range cases {
		e := newEditor(t, doc)
		e.Sel = Selection{e.Buf.Text.LineStart(c.start), e.Buf.Text.LineStart(c.start)}
		keys(e, c.keys)
		if l, _ := e.Cursor(); l != c.line || e.Msg != c.msg {
			t.Errorf("line %d, %q: line %d %q, want %d %q", c.start, c.keys, l, e.Msg, c.line, c.msg)
		}
	}
}

const goSrc = `package p

// A does a.
func A() {
	x := 1
	_ = x
}

type T int

func (T) B() {}
`

func TestJumpFuncAndEntryInGo(t *testing.T) {
	cases := []struct {
		start int
		keys  string
		line  int
	}{
		{0, "] f", 3},
		{3, "] f", 10},
		{10, "[ f", 3},
		{5, "[ f", 3},
		{3, "] e", 8}, // past the body, the closing brace and the blank line
		{8, "] e", 10},
		{10, "[ e", 8},
		{4, "] e", 5},
	}
	for _, c := range cases {
		e := newEditor(t, goSrc)
		e.Sel = Selection{e.Buf.Text.LineStart(c.start), e.Buf.Text.LineStart(c.start)}
		keys(e, c.keys)
		if l, _ := e.Cursor(); l != c.line {
			t.Errorf("line %d, %q: line %d, want %d (%s)", c.start, c.keys, l, c.line, e.Msg)
		}
	}
}

func TestFuncLine(t *testing.T) {
	for s, want := range map[string]bool{
		"func A() {": true, "func (T) B() {}": true, "    async def f():": true, "pub(crate) async fn x() {": true,
		"export default function () {": true, "fun main() {": true, "define x": false, "x := func() {}": false,
		"// func in a comment": false,
	} {
		if got := funcLine.MatchString(s); got != want {
			t.Errorf("funcLine(%q) = %v", s, got)
		}
	}
}

func TestMatchBracket(t *testing.T) {
	src := `f("(", g(1))` + "\n"
	cases := []struct{ at, want int }{
		{1, 11}, {11, 1}, // the ( in the string doesn't count
		{8, 10}, {10, 8},
		{0, 0}, // not on a bracket
	}
	for _, c := range cases {
		e := newEditor(t, src)
		e.Buf.Path = filepath.Join(filepath.Dir(e.Buf.Path), "f.go")
		e.Sel = Selection{c.at, c.at}
		keys(e, "m m")
		if e.Sel.Head != c.want {
			t.Errorf("mm from %d: at %d, want %d (%s)", c.at, e.Sel.Head, c.want, e.Msg)
		}
	}
}

func TestLang(t *testing.T) {
	e := newEditor(t, "$ curl x\n# env dev\n\n  {\"a\": 1}\n")
	if e.Lang() != nil {
		t.Error("a .txt file has a language")
	}
	e.runs.out, e.runs.header = e.Buf, len("$ curl x\n# env dev\n")
	if e.Lang() != syntax.JSON {
		t.Error("JSON output isn't JSON")
	}
	e.Buf.Replace(e.runs.header, e.Buf.Text.Len(), []byte("id  body\n"), You)
	if e.Lang() != nil {
		t.Error("table output is highlighted")
	}
}
