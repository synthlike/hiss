package tools

import "testing"

func TestEditedProposalText(t *testing.T) {
	out := ProposalsOut{Proposals: []ProposalInfo{{ID: 3, Path: "/p/f.go", Status: "accepted", Edited: true, FinalText: "x\ny\n"}}}
	want := "#3 /p/f.go: accepted with the user's edits; the text as saved:\nx\ny\n"
	if got := out.Text(); got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}
