package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
)

func TestHookDeliversSessionComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("x := 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := core.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ed := core.New(b)
	hook := func(ev string, extra string) rpc.Response {
		return Exec(ed, rpc.Request{Tool: "hook", Args: json.RawMessage(
			`{"session_id":"s1","hook_event_name":"` + ev + `"` + extra + `}`)})
	}
	hook("PreToolUse", `,"tool_name":"mcp__hiss__propose_edit"`)
	if _, err := ed.ProposeEdit("claude", path, "1", "2", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"]", "h", ":"} {
		ed.HandleKey(k)
	}
	ed.Cmd = "comment why"
	ed.HandleKey("enter")

	if r := hook("Notification", ""); strings.Contains(string(r.Result), "why") {
		t.Errorf("Notification should not take comments: %s", r.Result)
	}
	if r := hook("PostToolUse", ""); !strings.Contains(string(r.Result), `"text":"why"`) {
		t.Errorf("PostToolUse result = %s (%s)", r.Result, r.Error)
	}
	if r := hook("UserPromptSubmit", ""); strings.Contains(string(r.Result), "why") {
		t.Errorf("comment delivered twice: %s", r.Result)
	}
}

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
