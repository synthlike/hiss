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

func TestShowTrailAndStepComments(t *testing.T) {
	cs, reqs := setup(t, "package main\n\nfunc main() {}\n")
	hook := func(ev, extra string) (out rpc.Response) {
		onEditor(reqs, func(ed *core.Editor) {
			out = Exec(ed, rpc.Request{Tool: "hook", Args: json.RawMessage(
				`{"session_id":"s1","hook_event_name":"` + ev + `"` + extra + `}`)})
		})
		return out
	}
	hook("PreToolUse", `,"tool_name":"mcp__hiss__show_trail"`) // the trail belongs to session s1
	res := call(t, cs, "show_trail", map[string]any{"title": "entry point", "steps": []map[string]any{
		{"path": "main.go", "line": 3, "note": "main"},
		{"path": "main.go", "note": "the package"},
	}})
	if res.IsError || !strings.Contains(text(res), "trail of 2 steps") {
		t.Fatalf("show_trail: %s", text(res))
	}
	onEditor(reqs, func(ed *core.Editor) {
		ed.Resize(80, 24) // threads wrap to the screen width
		for _, k := range []string{"space", "t"} {
			ed.HandleKey(k)
		}
		p := ed.Picker
		if p == nil || p.Title != "test-agent: entry point" || len(p.Items) != 2 ||
			p.Items[0].Label != "1. main.go:3" || p.Items[0].Detail != "main" || p.Items[1].Label != "2. main.go:1" {
			t.Fatalf("picker = %+v", p)
		}
		ed.HandleKey("enter")
		if l, _ := ed.Cursor(); l != 2 {
			t.Errorf("jumped to line %d, want 2", l)
		}
		ed.HandleKey(":")
		ed.Cmd = "comment why here?"
		ed.HandleKey("enter")
	})

	if r := hook("PostToolUse", ""); !strings.Contains(string(r.Result), `"step":1`) ||
		!strings.Contains(string(r.Result), `"text":"why here?"`) {
		t.Errorf("PostToolUse = %s %s", r.Result, r.Error)
	}
	if res = call(t, cs, "reply", map[string]any{"step": 1, "text": "it starts here"}); res.IsError {
		t.Errorf("reply: %s", text(res))
	}
	if res = call(t, cs, "reply", map[string]any{"step": 9, "text": "x"}); !res.IsError {
		t.Error("reply to a missing step should fail")
	}
	onEditor(reqs, func(ed *core.Editor) {
		rows := ed.TrailRows()[2]
		if len(rows) != 3 || rows[0].Text != "1. main" || rows[2].Text != "test-agent: it starts here" {
			t.Errorf("rows under line 3 = %+v", rows)
		}
	})

	res = call(t, cs, "show_trail", map[string]any{"title": "x", "steps": []map[string]any{{"path": "nope.go"}}})
	if !res.IsError || !strings.Contains(text(res), "nope.go") {
		t.Errorf("missing path: IsError=%v %s", res.IsError, text(res))
	}
}
