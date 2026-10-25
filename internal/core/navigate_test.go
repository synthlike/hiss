package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/synthlike/hiss/internal/lsp"
)

// lookupLSP fakes the server like fakeLSP, but queues posted work so the test
// runs it on its own goroutine with run.
func lookupLSP(t *testing.T, e *Editor) (f *fakeServer, run func()) {
	f, _ = fakeLSP(t, e)
	posted := make(chan func(*Editor), 1)
	e.Post = func(fn func(*Editor)) { posted <- fn }
	return f, func() { t.Helper(); (<-posted)(e) }
}

func loc(path string, line, char int) lsp.Location {
	p := lsp.Position{Line: line, Character: char}
	return lsp.Location{Path: path, Range: lsp.Range{Start: p, End: p}}
}

func TestGotoDefinition(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f() { g() }\n\nfunc g() {}\n")
	f, run := lookupLSP(t, e)
	f.locs = []lsp.Location{loc(e.Buf.Path, 4, 5)}
	e.setHead(22) // the g of g()
	keys(e, "g d")
	run()
	if len(f.asked) != 1 || f.asked[0] != (lsp.Position{Line: 2, Character: 11}) {
		t.Errorf("asked about %+v, want 2:11", f.asked)
	}
	if e.Sel.Head != 34 {
		t.Errorf("cursor at %d, want 34 (the g of func g)", e.Sel.Head)
	}
	keys(e, "C-o")
	if e.Sel.Head != 22 {
		t.Errorf("C-o went to %d, want 22", e.Sel.Head)
	}
}

func TestGotoDefinitionThroughProposals(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f() { g() }\n\nfunc g() {}\n")
	f, run := lookupLSP(t, e)
	propose(t, e, "package a\n", "package a\n\nvar x = 1\n") // two lines above everything
	// The server sees the proposal applied: g() is on line 4, func g on 6.
	f.locs = []lsp.Location{loc(e.Buf.Path, 6, 5)}
	e.setHead(22) // the g of g()
	keys(e, "g d")
	run()
	if len(f.asked) != 1 || f.asked[0] != (lsp.Position{Line: 4, Character: 11}) {
		t.Errorf("asked about %+v, want 4:11 in the text with the proposal", f.asked)
	}
	if e.Sel.Head != 34 {
		t.Errorf("cursor at %d, want 34 in the buffer", e.Sel.Head)
	}
}

func TestGotoDefinitionFromProposedLine(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f() {}\n\nfunc g() {}\n")
	f, run := lookupLSP(t, e)
	p := propose(t, e, "func f() {}", "func f() {\n\tg()\n}")
	f.locs = []lsp.Location{loc(e.Buf.Path, 2, 5)} // f itself, inside the proposal
	e.setHead(11)                                  // line 2
	keys(e, "j j")                                 // the second proposed line
	e.goal = 1                                     // on the g after the tab
	keys(e, "g d")
	run()
	if len(f.asked) != 1 || f.asked[0] != (lsp.Position{Line: 3, Character: 1}) {
		t.Errorf("asked about %+v, want 3:1 on the proposed line", f.asked)
	}
	if e.line() != e.Buf.Text.LineOf(p.Start) {
		t.Errorf("on line %d, want the proposal's", e.line())
	}
}

func TestFindReferences(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc g() {}\n")
	e.Root = filepath.Dir(e.Buf.Path)
	other := filepath.Join(e.Root, "b.go")
	os.WriteFile(other, []byte("package a\n\nfunc h() {\n\tg()\n}\n"), 0o600)
	f, run := lookupLSP(t, e)
	f.locs = []lsp.Location{loc(e.Buf.Path, 2, 5), loc(other, 3, 1)}
	keys(e, "g r")
	run()
	if e.Picker == nil {
		t.Fatalf("no picker; msg %q", e.Msg)
	}
	ms := e.Picker.Matches()
	if len(ms) != 2 || ms[0].Label != e.Buf.Name+":3" || ms[1].Label != "b.go:4" || ms[1].Detail != "g()" {
		t.Fatalf("items %+v", ms)
	}
	keys(e, "down enter")
	if e.Buf.Path != other || e.Sel.Head != 23 {
		t.Errorf("at %s:%d, want b.go:23", e.Buf.Path, e.Sel.Head)
	}
}

func TestLookupDroppedWhenMovedOn(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc g() {}\n")
	f, run := lookupLSP(t, e)
	f.locs = []lsp.Location{loc(e.Buf.Path, 2, 5), loc(e.Buf.Path, 0, 0)}
	keys(e, "g r j")
	run()
	if e.Picker != nil {
		t.Error("a late answer opened a picker after the cursor moved")
	}
}

func TestLookupWithoutServer(t *testing.T) {
	e := newEditor(t, "hello\n")
	keys(e, "g d")
	if e.Msg != "no language server for this file" {
		t.Errorf("msg %q", e.Msg)
	}
}
