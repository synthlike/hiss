package core

import (
	"os"
	"strings"
	"testing"
)

func TestTweakThenAccept(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j i X esc") // insert on "+ B1"
	if got := e.Buf.Text.String(); got != "a\nXB1\nB2\nc\n" {
		t.Fatalf("buffer = %q", got)
	}
	if p.Status != Pending || !p.Tweaked {
		t.Fatalf("status %s, tweaked %v", p.Status, p.Tweaked)
	}
	h := e.HunkAtCursor()
	if h == nil || !h.Tweaked || h.First != 1 || h.Last != 2 || strings.Join(h.Removed, "|") != "b" || len(h.Added) != 0 {
		t.Fatalf("hunk = %+v", h)
	}
	keys(e, "+")
	if p.Status != Accepted || !p.Edited || p.Final != "XB1\nB2\n" {
		t.Fatalf("status %s, edited %v, final %q", p.Status, p.Edited, p.Final)
	}
	if got, _ := os.ReadFile(e.Buf.Path); string(got) != "a\nXB1\nB2\nc\n" {
		t.Errorf("file = %q", got)
	}
	if s := e.Buf.Text.Spans(2, 3); len(s) != 1 || s[0].Origin.Author != "you" {
		t.Errorf("typed text spans = %+v", s)
	}
	if s := e.Buf.Text.Spans(3, 9); len(s) != 1 || s[0].Origin.Author != "claude" || s[0].Origin.Proposal != p.ID {
		t.Errorf("proposed text spans = %+v", s)
	}
}

func TestTweakThenReject(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j d") // delete the "B" of "+ B1"
	if got := e.Buf.Text.String(); got != "a\n1\nB2\nc\n" {
		t.Fatalf("buffer = %q", got)
	}
	keys(e, "-")
	if got := e.Buf.Text.String(); got != "a\nb\nc\n" {
		t.Fatalf("after reject: %q", got)
	}
	if p.Status != Rejected || p.Tweaked || e.Buf.Dirty() {
		t.Errorf("status %s, tweaked %v, dirty %v", p.Status, p.Tweaked, e.Buf.Dirty())
	}
	if s := e.Buf.Text.Spans(0, e.Buf.Text.Len()); len(s) != 1 || s[0].Origin.Author != "" {
		t.Errorf("restored text should keep its origin: %+v", s)
	}
}

func TestUndoPastTweak(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j i X esc u")
	if got := e.Buf.Text.String(); got != "a\nB1\nB2\nc\n" || !p.Tweaked {
		t.Fatalf("after one undo: %q, tweaked %v", got, p.Tweaked)
	}
	keys(e, "u")
	if got := e.Buf.Text.String(); got != "a\nb\nc\n" || p.Tweaked || p.Status != Pending {
		t.Fatalf("after two undos: %q, tweaked %v, status %s", got, p.Tweaked, p.Status)
	}
	if h := e.HunkAtCursor(); h == nil || strings.Join(h.Added, "|") != "B1|B2" {
		t.Fatalf("hunk = %+v", h)
	}
	keys(e, "+")
	if got := e.Buf.Text.String(); got != "a\nB1\nB2\nc\n" || p.Edited {
		t.Errorf("accepted: %q, edited %v", got, p.Edited)
	}
}

func TestTweakedOldLinesAndSupersede(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j i X esc k") // onto the old "- b" above the hunk
	if e.DiffRow() != -1 {
		t.Fatalf("DiffRow = %d, want the old line above", e.DiffRow())
	}
	before := e.Buf.Text.String()
	keys(e, "d")
	if e.Buf.Text.String() != before || !strings.Contains(e.Msg, "old lines") {
		t.Errorf("editing an old line: %q, msg %q", e.Buf.Text.String(), e.Msg)
	}
	e.execute("comment why drop b")
	if c := p.Thread[len(p.Thread)-1]; c.At != "-2" || c.Line != "- b" {
		t.Errorf("comment on the old line: %+v", c)
	}
	keys(e, "k")
	if l, _ := e.Cursor(); l != 0 || e.DiffRow() != 0 {
		t.Errorf("k above the old lines: line %d row %d", l, e.DiffRow())
	}
	if _, err := e.Withdraw("claude", p.ID); err == nil {
		t.Error("superseding a proposal the user is editing should fail")
	}
}

func TestEditsAroundTweakedHunk(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B\n")
	keys(e, "j j i X esc g g i Y esc") // tweak, then type above the hunk
	if got := e.Buf.Text.String(); got != "Ya\nXB\nc\n" || p.Status != Pending {
		t.Fatalf("buffer %q, status %s", got, p.Status)
	}
	if got := string(e.Buf.Text.Slice(p.Start, p.End)); got != "XB\n" {
		t.Errorf("region = %q", got)
	}
	keys(e, "j j j g h i Z esc") // down past the old line and the hunk, then type at the start of "c"
	if got := e.Buf.Text.String(); got != "Ya\nXB\nZc\n" {
		t.Fatalf("buffer %q", got)
	}
	if got := string(e.Buf.Text.Slice(p.Start, p.End)); got != "XB\n" {
		t.Errorf("typing on the next line joined the hunk: region = %q", got)
	}
}

func TestRedoTweak(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	p := propose(t, e, "b\n", "B1\nB2\n")
	keys(e, "j j i X esc u u U")
	if got := e.Buf.Text.String(); got != "a\nB1\nB2\nc\n" || !p.Tweaked || p.Status != Pending {
		t.Fatalf("after redo: %q, tweaked %v, status %s", got, p.Tweaked, p.Status)
	}
	keys(e, "U +")
	if p.Status != Accepted || p.Final != "XB1\nB2\n" {
		t.Errorf("status %s, final %q", p.Status, p.Final)
	}
}
