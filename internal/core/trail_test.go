package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrail(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	e.Root = filepath.Dir(e.Buf.Path)
	f := e.Buf
	other := filepath.Join(e.Root, "g.txt")
	if err := os.WriteFile(other, []byte("g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Hook(HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__show_trail"})
	if err := e.ShowTrail("claude", "tour", []StepInput{
		{Path: f.Path, Line: 2, Note: "b matters"},
		{Path: other, Line: 1, Note: "then g"},
	}); err != nil {
		t.Fatal(err)
	}
	if rows := e.TrailRows(); len(rows[1]) != 1 || rows[1][0].Text != "1. b matters" || !rows[1][0].Note {
		t.Fatalf("rows = %+v", rows)
	}

	keys(e, "i z enter esc") // a new first line: step 1 must follow its text
	if l := e.trail.Steps[0].Line(); l != 2 {
		t.Fatalf("step 1 on line %d after an edit above, want 2", l)
	}

	keys(e, "] s")
	if l, _ := e.Cursor(); e.Buf != f || l != 2 || !strings.HasPrefix(e.Msg, "step 1/2") {
		t.Errorf("]s: %s line %d msg %q", e.Buf.Name, l, e.Msg)
	}
	keys(e, "] s")
	if e.Buf.Path != other {
		t.Errorf("second ]s went to %s", e.Buf.Name)
	}
	keys(e, "] s [ s") // wraps to step 1, then back to step 2
	if e.Buf.Path != other {
		t.Errorf("[s went to %s", e.Buf.Name)
	}

	keys(e, "[ s")
	e.execute("comment why b?")
	ds := e.SessionComments("s1")
	if len(ds) != 1 || ds[0].Step == nil || ds[0].Step.N != 1 || len(e.SessionComments("s1")) != 0 {
		t.Errorf("deliveries = %+v", ds)
	}
	if err := e.ReplyStep("claude", 1, "because"); err != nil {
		t.Fatal(err)
	}
	if rows := e.TrailRows()[2]; len(rows) != 3 || rows[2].Text != "claude: because" {
		t.Errorf("rows = %+v", rows)
	}

	e.execute("trail-clear")
	jumps := 0 // anchors the jump list keeps in f
	for _, p := range e.jumps.list {
		if p.buf == f {
			jumps++
		}
	}
	if e.trail != nil || len(f.anchors) != jumps {
		t.Errorf("clear: trail=%v anchors=%d, %d of them jumps", e.trail, len(f.anchors), jumps)
	}
	e.execute("comment anyone?")
	if !strings.HasPrefix(e.Msg, "nothing to comment on") {
		t.Errorf("msg = %q", e.Msg)
	}
}
