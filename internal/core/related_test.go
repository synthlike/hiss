package core

import (
	"testing"

	"github.com/synthlike/hiss/internal/lsp"
)

func rng(line, from, to int) lsp.Range {
	return lsp.Range{Start: lsp.Position{Line: line, Character: from}, End: lsp.Position{Line: line, Character: to}}
}

// relatedLSP fakes the server with posted work queued, and no delay.
func relatedLSP(t *testing.T, e *Editor) (f *fakeServer, run func()) {
	f, run = lookupLSP(t, e)
	old := relatedDelay
	relatedDelay = 0
	t.Cleanup(func() { relatedDelay = old })
	return f, run
}

// settle syncs and asks for highlights, as the UI does after each event,
// then runs the delayed request and its answer.
func settle(e *Editor, run func()) {
	e.SyncLSP()
	e.UpdateRelated()
	run() // the delayed request
	run() // its answer
}

func TestRelated(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f(x int) int { return x }\n")
	f, run := relatedLSP(t, e)
	f.hls = []lsp.Range{rng(2, 7, 8), rng(2, 27, 28)}
	e.setHead(18) // the x parameter
	settle(e, run)
	if len(f.asked) != 1 || f.asked[0] != (lsp.Position{Line: 2, Character: 7}) {
		t.Errorf("asked about %+v", f.asked)
	}
	want := [][2]int{{18, 19}, {38, 39}}
	if got := e.Related(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Related() = %v, want %v", got, want)
	}

	e.setHead(38) // the other x: still highlighted while the new answer is on its way
	e.SyncLSP()
	e.UpdateRelated()
	if len(e.Related()) != 2 {
		t.Error("moving between highlights cleared them")
	}
	run()
	run()

	e.setHead(30) // a space
	e.UpdateRelated()
	if len(e.Related()) != 0 {
		t.Error("highlights stayed with the cursor off a word")
	}
	if len(f.asked) != 2 {
		t.Errorf("asked %d times, want no request for a space", len(f.asked))
	}
}

func TestRelatedClearedByEdits(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f(x int) int { return x }\n")
	f, run := relatedLSP(t, e)
	f.hls = []lsp.Range{rng(2, 7, 8)}
	e.setHead(18)
	settle(e, run)
	keys(e, "i y")
	if e.SyncLSP(); len(e.Related()) != 0 {
		t.Error("highlights outlived an edit")
	}
	e.UpdateRelated() // insert mode: nothing to ask
	if len(f.asked) != 1 {
		t.Errorf("asked %d times, want none in insert mode", len(f.asked))
	}
}

func TestRelatedInProposal(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f(x int) int {\n\treturn 0\n}\n")
	f, run := relatedLSP(t, e)
	p := propose(t, e, "\treturn 0", "\treturn x")
	f.hls = []lsp.Range{rng(2, 7, 8), rng(3, 8, 9)} // the parameter and its use in the proposal
	e.setHead(18)
	settle(e, run)
	if got := e.Related(); len(got) != 1 || got[0] != [2]int{18, 19} {
		t.Errorf("Related() = %v", got)
	}
	h := e.hunk(p)
	if len(h.Related) != 1 || h.Related[0] != (AddedSpan{0, 8, 9}) {
		t.Errorf("hunk highlights %+v, want the x on its proposed line", h.Related)
	}
}

func TestRelatedLateAnswerDropped(t *testing.T) {
	e := newGoEditor(t, "package a\n\nfunc f(x int) int { return x }\n")
	f, run := relatedLSP(t, e)
	f.hls = []lsp.Range{rng(2, 7, 8)}
	e.setHead(18)
	e.SyncLSP()
	e.UpdateRelated()
	run() // the request goes out
	e.setHead(30)
	e.UpdateRelated()
	run() // the answer arrives after the move
	if len(e.Related()) != 0 {
		t.Error("a late answer was drawn")
	}
}
