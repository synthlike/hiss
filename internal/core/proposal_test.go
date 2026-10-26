package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/text"
)

func propose(t *testing.T, e *Editor, old, new string) *Proposal {
	t.Helper()
	ps, err := e.ProposeEdit("claude", e.Buf.Path, old, new, false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return ps[0]
}

func TestAcceptAppliesSavesAndAttributes(t *testing.T) {
	e := newEditor(t, "func a() {}\nfunc b() {}\n")
	p := propose(t, e, "b()", "bee()")
	if e.Buf.Text.String() != "func a() {}\nfunc b() {}\n" {
		t.Fatal("a pending proposal must not change the buffer")
	}
	keys(e, "] h +")
	if p.Status != Accepted || e.PendingCount() != 0 {
		t.Fatalf("status=%s pending=%d msg=%q", p.Status, e.PendingCount(), e.Msg)
	}
	got, _ := os.ReadFile(e.Buf.Path)
	if string(got) != "func a() {}\nfunc bee() {}\n" || e.Buf.Dirty() {
		t.Errorf("file = %q dirty=%v", got, e.Buf.Dirty())
	}
	want := text.Origin{Author: "claude", Proposal: p.ID}
	spans := e.Buf.Text.Spans(17, 20) // "bee"
	if len(spans) != 1 || spans[0].Origin != want {
		t.Errorf("spans = %v", spans)
	}
}

func TestRejectWithReason(t *testing.T) {
	e := newEditor(t, "x := 1\n")
	p := propose(t, e, "1", "2")
	keys(e, ": h r space n o enter")
	if p.Status != Rejected || p.Reason != "no" || e.Buf.Text.String() != "x := 1\n" {
		t.Errorf("status=%s reason=%q text=%q", p.Status, p.Reason, e.Buf.Text.String())
	}
}

func TestReviewKeys(t *testing.T) {
	e := newEditor(t, "one\ntwo\n")
	p := propose(t, e, "one", "1")
	keys(e, "j j #") // past the hunk's + line to plain text
	if e.Mode == Command || e.Msg != nothingToComment {
		t.Fatalf("# off a hunk: mode=%s msg=%q", e.Mode, e.Msg)
	}
	keys(e, "g g #")
	if e.Mode != Command || e.Cmd != "c " {
		t.Fatalf("# on a hunk: mode=%s cmd=%q", e.Mode, e.Cmd)
	}
	keys(e, "esc -")
	if e.Mode != Command || e.Cmd != "hr " || p.Status != Pending {
		t.Fatalf("- on a hunk: mode=%s cmd=%q status=%s", e.Mode, e.Cmd, p.Status)
	}
	keys(e, "enter") // no reason needed
	if p.Status != Rejected {
		t.Errorf("- on a hunk: status=%s msg=%q", p.Status, e.Msg)
	}
}

func TestReviewAll(t *testing.T) {
	e := newEditor(t, "a\nb\n")
	first, second := propose(t, e, "a", "A"), propose(t, e, "b", "B")
	e.execute("haa")
	if first.Status != Accepted || second.Status != Accepted || e.Msg != "accepted 2 hunks" {
		t.Fatalf("statuses %s %s, msg=%q", first.Status, second.Status, e.Msg)
	}
	if got, _ := os.ReadFile(e.Buf.Path); string(got) != "A\nB\n" {
		t.Errorf("file = %q", got)
	}
	third := propose(t, e, "A", "x")
	e.execute("hunk-reject-all too much")
	if third.Status != Rejected || third.Reason != "too much" || e.Msg != "rejected 1 hunk" {
		t.Errorf("status=%s reason=%q msg=%q", third.Status, third.Reason, e.Msg)
	}
	e.execute("hunk-accept-all")
	if e.Msg != "no pending hunks" {
		t.Errorf("msg = %q", e.Msg)
	}
}

func TestProposeEditMatching(t *testing.T) {
	e := newEditor(t, "a b a\n")
	cases := []struct {
		old, new string
		all      bool
		err      string
		n        int
	}{
		{"zzz", "y", false, "not found", 0},
		{"a", "y", false, "occurs 2 times", 0},
		{"a", "a", false, "the same", 0},
		{"", "y", false, "write_file", 0},
		{"a", "y", true, "", 2},
	}
	for _, c := range cases {
		ps, err := e.ProposeEdit("claude", e.Buf.Path, c.old, c.new, c.all, nil, "")
		if c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%q→%q: err = %v, want %q", c.old, c.new, err, c.err)
		}
		if len(ps) != c.n {
			t.Errorf("%q→%q: %d proposals, want %d", c.old, c.new, len(ps), c.n)
		}
	}
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "a b", "c", false, nil, ""); err == nil ||
		!strings.Contains(err.Error(), "overlaps pending proposal #1") {
		t.Errorf("overlap: err = %v", err)
	}
}

func TestUserEditsMoveOrStaleProposals(t *testing.T) {
	e := newEditor(t, "one\ntwo\nthree\n")
	p := propose(t, e, "three", "3")
	keys(e, "i X X esc") // before the region: shifts it
	if p.Status != Pending || string(e.Buf.Text.Slice(p.Start, p.End)) != "three" {
		t.Fatalf("after edit before region: status=%s region=%q", p.Status, e.Buf.Text.Slice(p.Start, p.End))
	}
	keys(e, "g e k l i Y esc") // inside "three"
	if p.Status != Stale || e.PendingCount() != 0 {
		t.Errorf("status = %s, pending = %d", p.Status, e.PendingCount())
	}
}

func TestUndoKeepsUnrelatedProposals(t *testing.T) {
	e := newEditor(t, "alpha\nbeta\n")
	p := propose(t, e, "beta", "BETA")
	keys(e, "i X esc u")
	if p.Status != Pending || string(e.Buf.Text.Slice(p.Start, p.End)) != "beta" {
		t.Errorf("status=%s region=%q", p.Status, e.Buf.Text.Slice(p.Start, p.End))
	}
}

func TestBaseVersion(t *testing.T) {
	e := newEditor(t, "alpha\nbeta\n")
	base := e.Buf.Version
	keys(e, "i X esc") // user edits line 1 after the agent read the buffer
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "beta", "B", false, &base, ""); err != nil {
		t.Errorf("edit elsewhere should rebase: %v", err)
	}
	keys(e, "g g i Y esc")
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "YXalpha", "A", false, &base, ""); err == nil ||
		!strings.Contains(err.Error(), "changed this text") {
		t.Errorf("edit inside region: err = %v", err)
	}
	future := e.Buf.Version + 5
	if _, err := e.ProposeEdit("claude", e.Buf.Path, "YXalpha", "A", false, &future, ""); err == nil {
		t.Error("unknown version accepted")
	}
}

func TestWriteFileAndJumpAcrossBuffers(t *testing.T) {
	e := newEditor(t, "main\n")
	e.Root = filepath.Dir(e.Buf.Path)
	newPath := filepath.Join(e.Root, "new.go")
	p, err := e.WriteFile("claude", newPath, "package x\n\nfunc X() {}\n", nil, "add X")
	if err != nil {
		t.Fatal(err)
	}
	h := e.hunk(p)
	if h.Removes || strings.Join(h.Added, "|") != "package x||func X() {}" {
		t.Errorf("hunk = %+v", h)
	}
	keys(e, "] h")
	if e.Buf.Path != newPath || e.Buf.Name != "new.go" {
		t.Fatalf("jumped to %s", e.Buf.Path)
	}
	keys(e, "+")
	got, _ := os.ReadFile(newPath)
	if string(got) != "package x\n\nfunc X() {}\n" {
		t.Errorf("new file = %q", got)
	}
}

func TestHunkLines(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	cases := []struct {
		old, new string
		added    string
		first    int
		last     int
	}{
		{"b", "B", "B", 1, 1},
		{"b\n", "", "", 1, 1},
		{"b\nc", "x\ny\nz", "x|y|z", 1, 2},
		{"c\n", "c\nd\n", "c|d", 2, 2},
	}
	for _, c := range cases {
		ps, err := e.ProposeEdit("claude", e.Buf.Path, c.old, c.new, false, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		h := e.hunk(ps[0])
		if strings.Join(h.Added, "|") != c.added || h.First != c.first || h.Last != c.last {
			t.Errorf("%q→%q: added=%q lines %d-%d", c.old, c.new, h.Added, h.First, h.Last)
		}
		keys(e, "] h - enter")
	}
}

func TestAcceptShrinkingBelowTop(t *testing.T) {
	var sb strings.Builder
	for i := range 40 {
		sb.WriteString("l" + strconv.Itoa(i) + "\n")
	}
	e := newEditor(t, sb.String())
	old := strings.Join(strings.SplitAfter(sb.String(), "\n")[5:38], "")
	propose(t, e, old, "x\n")
	keys(e, "g e")
	if e.Top < 30 {
		t.Fatalf("top %d: the test needs it past the accepted text's end", e.Top)
	}
	keys(e, ": h a a enter") // panicked: Top stayed past the shrunk buffer's last line
	if n := e.Buf.Text.LineCount(); e.Top >= n || e.line() < e.Top {
		t.Errorf("top %d, cursor line %d, %d lines", e.Top, e.line(), n)
	}
}
