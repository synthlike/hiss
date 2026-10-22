package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/rpc"
)

func TestHookForwardsEvent(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hiss-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("HISS_SOCKET", sock)
	ln, err := rpc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan rpc.Request, 1)
	go rpc.Serve(ln, func(r rpc.Request) rpc.Response { got <- r; return rpc.Response{Result: json.RawMessage(`{}`)} })

	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop","cwd":"` + dir + `"}`))
	r := <-got
	if r.Tool != "hook" || !strings.Contains(string(r.Args), `"hook_event_name":"Stop"`) {
		t.Errorf("forwarded %+v", r)
	}
}

func TestHookWithoutEditorIsQuiet(t *testing.T) {
	t.Setenv("HISS_SOCKET", "/tmp/hiss-test-missing.sock")
	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`)) // must not panic or block
	runHook(strings.NewReader(`not json`))
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
	if len(s.Hooks) != 7 || len(s.Permissions.Deny) != 4 {
		t.Errorf("hooks=%d deny=%v", len(s.Hooks), s.Permissions.Deny)
	}
}
