package tools

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
)

func TestShowLocations(t *testing.T) {
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")
	res := call(t, cs, "show_locations", map[string]any{"title": "entry point", "items": []map[string]any{
		{"path": "main.go", "line": 3, "note": "main"},
		{"path": "main.go"},
	}})
	if res.IsError || !strings.Contains(text(res), "Shared 2 locations") {
		t.Fatalf("show_locations: %s", text(res))
	}
	onEditor(reqs, func(ed *core.Editor) {
		if ed.Toast.Text != "shared 2 locations: entry point (space l)" {
			t.Errorf("toast = %q", ed.Toast.Text)
		}
		ed.HandleKey("space")
		ed.HandleKey("l")
		p := ed.Picker
		if p == nil || p.Title != "test-agent: entry point" || len(p.Items) != 2 ||
			p.Items[0].Label != "main.go:3" || p.Items[0].Detail != "main" || p.Items[1].Label != "main.go:1" {
			t.Fatalf("picker = %+v", p)
		}
		ed.HandleKey("enter")
		if l, _ := ed.Cursor(); l != 2 {
			t.Errorf("jumped to line %d, want 2", l)
		}
	})

	res = call(t, cs, "show_locations", map[string]any{"title": "x", "items": []map[string]any{{"path": "nope.go"}}})
	if !res.IsError || !strings.Contains(text(res), "nope.go") {
		t.Errorf("missing path: IsError=%v %s", res.IsError, text(res))
	}
}
