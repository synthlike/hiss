package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/rpc"
	"github.com/synthlike/hiss/internal/tools"
)

func serveWatch(t *testing.T, reply func(n int) tools.WatchOut) {
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
	n := 0
	go rpc.Serve(ln, func(r rpc.Request) rpc.Response {
		n++
		raw, _ := json.Marshal(reply(n))
		return rpc.Response{Result: raw}
	})
	watchEvery = 5 * time.Millisecond
}

func TestWatchWakesWithComments(t *testing.T) {
	serveWatch(t, func(n int) tools.WatchOut {
		if n < 3 {
			return tools.WatchOut{}
		}
		return tools.WatchOut{Comments: []tools.CommentInfo{{ID: 4, Path: "/p/a.go", Text: "use a map"}}}
	})
	var stderr strings.Builder
	code := runWatch(strings.NewReader(`{"session_id":"s1"}`), &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "proposal #4") || !strings.Contains(stderr.String(), "> use a map") {
		t.Errorf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestWatchStandsDown(t *testing.T) {
	serveWatch(t, func(int) tools.WatchOut { return tools.WatchOut{Superseded: true} })
	if code := runWatch(strings.NewReader(`{"session_id":"s1"}`), &strings.Builder{}); code != 0 {
		t.Errorf("superseded: code=%d", code)
	}
	t.Setenv("HISS_SOCKET", "/tmp/hiss-test-missing.sock")
	if code := runWatch(strings.NewReader(`{"session_id":"s1"}`), &strings.Builder{}); code != 0 {
		t.Errorf("no editor: code=%d", code)
	}
	if code := runWatch(strings.NewReader(`{}`), &strings.Builder{}); code != 0 {
		t.Errorf("no session: code=%d", code)
	}
}
