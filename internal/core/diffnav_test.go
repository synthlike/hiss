package core

import "testing"

func TestJKThroughProposedLines(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	propose(t, e, "b\n", "B1\nB2\n")
	type pos struct{ line, row int }
	steps := []struct {
		key  string
		want pos
	}{
		{"j", pos{1, 0}}, // - b
		{"j", pos{1, 1}}, // + B1
		{"j", pos{1, 2}}, // + B2
		{"j", pos{2, 0}}, // c
		{"k", pos{1, 2}},
		{"k", pos{1, 1}},
	}
	for i, s := range steps {
		keys(e, s.key)
		if l, _ := e.Cursor(); (pos{l, e.DiffRow()}) != s.want {
			t.Fatalf("step %d (%s): at %d/+%d, want %v", i, s.key, l, e.DiffRow(), s.want)
		}
	}
	keys(e, "l")
	if e.DiffRow() != 0 {
		t.Error("a horizontal move should leave the proposed lines")
	}
}

func TestLineComments(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j") // on "+ B1"
	e.execute("comment typo")
	keys(e, "k") // on "- b"
	e.execute("comment why remove this")
	if len(p.Thread) != 2 || p.Thread[0].At != "+1" || p.Thread[0].Line != "+ B1" ||
		p.Thread[1].At != "-2" || p.Thread[1].Line != "- b" {
		t.Fatalf("thread = %+v", p.Thread)
	}
	if ds := e.TakeComments([]*Proposal{p}); len(ds) != 2 || ds[0].Line != "+ B1" {
		t.Errorf("deliveries = %+v", ds)
	}
	if h := e.HunkAtCursor(); h == nil || h.Notes[0].Text != "you (+1): typo" {
		t.Errorf("notes = %+v", h)
	}
}

func TestScrollIntoTallHunk(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	e.Resize(80, 5) // three text rows
	propose(t, e, "b\n", "1\n2\n3\n4\n5\n6\n")
	keys(e, "j j j j j") // line 1, fourth proposed line
	if e.DiffRow() != 4 {
		t.Fatalf("DiffRow = %d", e.DiffRow())
	}
	// Window of three rows ending at the cursor: +2, +3, +4.
	if e.Top != 1 || e.TopSub != 2 {
		t.Errorf("Top=%d TopSub=%d, want 1 2", e.Top, e.TopSub)
	}
	keys(e, "g g")
	if e.Top != 0 || e.TopSub != 0 || e.DiffRow() != 0 {
		t.Errorf("after gg: Top=%d TopSub=%d row=%d", e.Top, e.TopSub, e.DiffRow())
	}
}

func TestScrollKeepsRationaleAboveHunk(t *testing.T) {
	e := newEditor(t, "a\nb\nc\nd\n")
	e.Resize(80, 4) // two text rows
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "b", "B", false, nil, "why"); err != nil {
		t.Fatal(err)
	}
	type view struct{ top, sub int }
	steps := []struct {
		key  string
		want view
	}{
		{"j", view{1, 0}}, // "▸ why", "- b"
		{"j", view{1, 1}}, // "- b", "+ B"
		{"k", view{1, 0}}, // back on "- b": the rationale shows again
	}
	for i, s := range steps {
		keys(e, s.key)
		if got := (view{e.Top, e.TopSub}); got != s.want {
			t.Errorf("step %d (%s): view %v, want %v", i, s.key, got, s.want)
		}
	}
}
