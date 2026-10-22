package core

import (
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	e := newEditor(t, "foo bar\nbaz foo\nFoo\n")
	keys(e, "/ f o o") // searches as you type, from the cursor
	if e.Mode != Search || e.Sel != (Selection{12, 14}) {
		t.Fatalf("mode %s, sel %v", e.Mode, e.Sel)
	}
	keys(e, "enter")
	if e.Mode != Normal || e.Sel != (Selection{12, 14}) || e.Msg != "/foo  2/3" {
		t.Fatalf("after enter: mode %s, sel %v, msg %q", e.Mode, e.Sel, e.Msg)
	}
	steps := []struct {
		key   string
		start int
		msg   string
	}{
		{"n", 16, "/foo  3/3"}, // lower-case pattern: "Foo" matches too
		{"n", 0, "/foo  1/3 (wrapped)"},
		{"N", 16, "/foo  3/3 (wrapped)"},
		{"N", 12, "/foo  2/3"},
	}
	for _, s := range steps {
		keys(e, s.key)
		if e.Sel.Anchor != s.start || e.Msg != s.msg {
			t.Errorf("%s: sel %v msg %q, want start %d msg %q", s.key, e.Sel, e.Msg, s.start, s.msg)
		}
	}
	if ms := e.Matches(0, e.Buf.Text.Len()); len(ms) != 3 {
		t.Errorf("highlighted %v", ms)
	}
	keys(e, "esc")
	if ms := e.Matches(0, e.Buf.Text.Len()); ms != nil {
		t.Errorf("esc should clear highlighting: %v", ms)
	}
	keys(e, "n")
	if e.Sel.Anchor != 16 {
		t.Errorf("n after esc should still search: sel %v", e.Sel)
	}
}

func TestSearchCaseAndLiterals(t *testing.T) {
	e := newEditor(t, "foo\nFoo (x\n")
	keys(e, "/ F o o enter")
	if e.Sel != (Selection{4, 6}) || !strings.HasSuffix(e.Msg, "1/1") {
		t.Errorf("upper-case pattern: sel %v msg %q", e.Sel, e.Msg)
	}
	keys(e, "/ ( x enter") // not a valid regexp: searched literally
	if e.Sel != (Selection{8, 9}) {
		t.Errorf("literal: sel %v msg %q", e.Sel, e.Msg)
	}
	keys(e, "/ z z enter")
	if e.Msg != "no match: zz" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestSearchEscRestores(t *testing.T) {
	e := newEditor(t, "foo\nbar\nfoo\n")
	keys(e, "/ f o o enter") // the previous search
	keys(e, "/ b a")
	if e.Sel.Anchor != 4 {
		t.Fatalf("incremental: sel %v", e.Sel)
	}
	keys(e, "esc")
	if e.Mode != Normal || e.Sel != (Selection{8, 10}) {
		t.Errorf("esc: mode %s sel %v", e.Mode, e.Sel)
	}
	keys(e, "n") // still the previous search
	if e.Sel.Anchor != 0 {
		t.Errorf("n after esc: sel %v", e.Sel)
	}
	keys(e, "/ enter") // an empty search repeats the last one
	if e.Sel.Anchor != 8 {
		t.Errorf("empty search: sel %v", e.Sel)
	}
}

func TestSearchWordUnderCursor(t *testing.T) {
	e := newEditor(t, "foo foobar\nfoo\n")
	keys(e, "*")
	if e.Sel != (Selection{11, 13}) {
		t.Errorf("* should skip foobar: sel %v msg %q", e.Sel, e.Msg)
	}
}

func TestGotoLine(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	e.execute("2")
	if l, c := e.Cursor(); l != 1 || c != 0 {
		t.Errorf(":2 went to %d:%d", l, c)
	}
	e.execute("99")
	if l, _ := e.Cursor(); l != 2 {
		t.Errorf(":99 went to line %d", l)
	}
	e.Cmd = "12"
	if m := e.CommandHints(); m == nil || len(m.Items) != 1 || m.Items[0].Desc != "go to line 12" {
		t.Errorf("hints = %+v", m)
	}
}
