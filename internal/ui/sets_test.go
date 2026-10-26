package ui

import (
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/text"
)

func TestDrawChangeSet(t *testing.T) {
	dir := t.TempDir()
	b, err := core.Open(dir + "/f.go")
	if err != nil {
		t.Fatal(err)
	}
	b.Text = text.New([]byte("a\nb\n"), text.Origin{})
	ed := core.New(b)
	ed.Root = dir
	ed.Resize(100, 10)
	ed.Hook(core.HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	ed.Hook(core.HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__propose_edit"})
	for _, old := range []string{"a\n", "b\n"} {
		if _, err := ed.ProposeEdit("claude", b.Path, old, strings.ToUpper(old), false, nil, "shout"); err != nil {
			t.Fatal(err)
		}
	}
	ed.Toast = core.Toast{}
	ed.HandleKey("g") // on #1
	ed.HandleKey("g")
	g := newGrid(100, 10)
	Draw(g, ed)
	if got := g.row(9); !strings.HasPrefix(got, "#1 claude (set 0/2 reviewed): shout  (+ accept, - reject, # comment, :sa the set)") {
		t.Errorf("info = %q", got)
	}
	ed.HandleKey("+") // on to #2
	g = newGrid(100, 10)
	Draw(g, ed)
	if got := g.row(8); !strings.Contains(got, "1 pending, set 1/2 reviewed") {
		t.Errorf("status = %q", got)
	}
}
