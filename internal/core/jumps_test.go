package core

import "testing"

func TestJumpBackAndForward(t *testing.T) {
	e := newEditor(t, "a\nb\nc\nd\n")
	e.execute("3")
	e.execute("4")
	steps := []struct {
		key  string
		line int
		msg  string
	}{
		{"C-o", 2, ""},
		{"C-o", 0, ""},
		{"C-o", 0, "no older position"},
		{"C-i", 2, ""},
		{"tab", 3, ""},
		{"C-i", 3, "no newer position"},
	}
	for i, s := range steps {
		keys(e, s.key)
		if l, _ := e.Cursor(); l != s.line || e.Msg != s.msg {
			t.Errorf("step %d (%s): line %d msg %q, want %d %q", i, s.key, l, e.Msg, s.line, s.msg)
		}
	}
}

func TestJumpsFollowEdits(t *testing.T) {
	e := newEditor(t, "a\nb\nc\nd\n")
	e.Sel = Selection{2, 2} // on "b"
	e.execute("4")
	e.Buf.Replace(0, 0, []byte("x\n"), You)
	keys(e, "C-o")
	if l, _ := e.Cursor(); l != 2 || e.Sel.Head != 4 {
		t.Errorf("C-o went to line %d (offset %d), want the moved \"b\"", l, e.Sel.Head)
	}
}

func TestNewJumpDropsNewerPositions(t *testing.T) {
	e := newEditor(t, "a\nb\nc\nd\n")
	e.execute("2")
	e.execute("3")
	keys(e, "C-o C-o") // back on line 0
	keys(e, "/ d enter")
	keys(e, "C-o")
	if l, _ := e.Cursor(); l != 0 {
		t.Errorf("C-o after a search went to line %d", l)
	}
	keys(e, "C-i C-i")
	if l, _ := e.Cursor(); l != 3 || e.Msg != "no newer position" {
		t.Errorf("forward history: line %d msg %q", l, e.Msg)
	}
}
