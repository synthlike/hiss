package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// sockPath returns a short socket path; macOS limits them to 104 bytes.
func sockPath(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "hiss-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func TestCallRoundTrip(t *testing.T) {
	path := sockPath(t)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go Serve(ln, func(r Request) Response {
		if r.Tool == "fail" {
			return Response{Error: "nope"}
		}
		raw, _ := json.Marshal(r.Agent + ":" + r.Tool + ":" + string(r.Args))
		return Response{Result: raw}
	})

	got, err := Call(context.Background(), path, Request{Agent: "a", Tool: "t", Args: json.RawMessage(`{"x":1}`)})
	if err != nil || string(got) != `"a:t:{\"x\":1}"` {
		t.Errorf("got %s, %v", got, err)
	}
	if _, err := Call(context.Background(), path, Request{Tool: "fail"}); err == nil || err.Error() != "nope" {
		t.Errorf("error = %v", err)
	}
	if _, err := Listen(path); err == nil {
		t.Error("second Listen on a live socket should fail")
	}
}

func TestNotRunningAndStaleSocket(t *testing.T) {
	path := sockPath(t)
	if _, err := Call(context.Background(), path, Request{Tool: "t"}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("err = %v, want ErrNotRunning", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil { // leftover from a crash
		t.Fatal(err)
	}
	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen over stale socket: %v", err)
	}
	ln.Close()
}

func TestRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Root(sub); got != sub {
		t.Errorf("without .git: Root = %s, want %s", got, sub)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Root(sub); got != dir {
		t.Errorf("Root = %s, want %s", got, dir)
	}
}
