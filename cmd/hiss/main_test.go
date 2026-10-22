package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/rpc"
	"github.com/synthlike/hiss/internal/tools"
)

// serveHook starts a fake editor that records hook requests and answers with out.
func serveHook(t *testing.T, out tools.HookOut) (dir string, got chan rpc.Request) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hiss-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("HISS_SOCKET", sock)
	ln, err := rpc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan rpc.Request, 1)
	raw, _ := json.Marshal(out)
	go rpc.Serve(ln, func(r rpc.Request) rpc.Response { got <- r; return rpc.Response{Result: raw} })
	return dir, got
}

func TestHookForwardsEvent(t *testing.T) {
	dir, got := serveHook(t, tools.HookOut{})
	var stdout strings.Builder
	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop","cwd":"`+dir+`"}`), &stdout)
	r := <-got
	if r.Tool != "hook" || !strings.Contains(string(r.Args), `"hook_event_name":"Stop"`) {
		t.Errorf("forwarded %+v", r)
	}
	if stdout.Len() != 0 {
		t.Errorf("printed %q with no comments", stdout.String())
	}
}

func TestHookHandsOverComments(t *testing.T) {
	serveHook(t, tools.HookOut{Comments: []tools.CommentInfo{{ID: 2, Path: "/p/a.go", Text: "why?"}}})
	var stdout strings.Builder
	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"PostToolUse"}`), &stdout)
	var out struct {
		HookSpecificOutput struct {
			HookEventName     string
			AdditionalContext string
		}
	}
	if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
		t.Fatalf("%q: %v", stdout.String(), err)
	}
	o := out.HookSpecificOutput
	if o.HookEventName != "PostToolUse" || !strings.Contains(o.AdditionalContext, "proposal #2") {
		t.Errorf("output = %+v", o)
	}
}

func TestHookWithoutEditorIsQuiet(t *testing.T) {
	t.Setenv("HISS_SOCKET", "/tmp/hiss-test-missing.sock")
	var stdout strings.Builder
	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`), &stdout) // must not panic or block
	runHook(strings.NewReader(`not json`), &stdout)
	if stdout.Len() != 0 {
		t.Errorf("printed %q", stdout.String())
	}
}

func TestSetupIsValidSettings(t *testing.T) {
	var sb strings.Builder
	if err := runSetup(&sb); err != nil {
		t.Fatal(err)
	}
	var s struct {
		Permissions struct{ Allow, Deny []string }
		Hooks       map[string][]struct {
			Matcher string
			Hooks   []struct {
				Type, Command string
				Async         bool
			}
		}
	}
	if err := json.Unmarshal([]byte(sb.String()), &s); err != nil {
		t.Fatal(err)
	}
	pre := s.Hooks["PreToolUse"]
	if len(pre) != 1 || pre[0].Matcher != "mcp__hiss__.*" || pre[0].Hooks[0].Async ||
		!strings.HasSuffix(pre[0].Hooks[0].Command, " hook") {
		t.Errorf("PreToolUse = %+v", pre)
	}
	if stop := s.Hooks["Stop"]; len(stop) != 1 || len(stop[0].Hooks) != 2 ||
		!strings.HasSuffix(stop[0].Hooks[1].Command, " hook watch") {
		t.Errorf("Stop = %+v", stop)
	}
	for _, ev := range []string{"PostToolUse", "UserPromptSubmit"} {
		if h := s.Hooks[ev]; len(h) != 1 || h[0].Hooks[0].Async {
			t.Errorf("%s must be synchronous to deliver comments: %+v", ev, h)
		}
	}
	if len(s.Hooks) != 7 || len(s.Permissions.Deny) != 4 {
		t.Errorf("hooks=%d deny=%v", len(s.Hooks), s.Permissions.Deny)
	}
}
