package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/synthlike/hiss/internal/rpc"
	"github.com/synthlike/hiss/internal/tools"
)

// watchFor bounds how long a watcher waits; the hook's timeout must be longer.
var (
	watchFor   = 55 * time.Minute
	watchEvery = time.Second
)

// runWatch is a Stop hook that Claude Code runs in the background with
// asyncRewake. It waits for the user to comment on the session's proposals and
// then exits 2 with the comments on stderr, which wakes Claude to answer them.
// It exits 0 when hiss isn't running, a newer watcher takes over, or time runs out.
func runWatch(stdin io.Reader, stderr io.Writer) int {
	data, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return 0
	}
	var in struct {
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}
	if json.Unmarshal(data, &in) != nil || in.SessionID == "" {
		return 0
	}
	if in.Cwd == "" {
		in.Cwd, _ = os.Getwd()
	}
	sock, err := rpc.SocketPath(rpc.Root(in.Cwd))
	if err != nil {
		return 0
	}
	id := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	watchLog(sock, "start session %s", in.SessionID)
	register := true
	for deadline := time.Now().Add(watchFor); time.Now().Before(deadline); time.Sleep(watchEvery) {
		args, _ := json.Marshal(map[string]any{"session_id": in.SessionID, "watcher": id, "register": register})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		raw, err := rpc.Call(ctx, sock, rpc.Request{Tool: "watch_comments", Args: args})
		cancel()
		var out tools.WatchOut
		if err == nil {
			err = json.Unmarshal(raw, &out)
		}
		switch {
		case err != nil:
			watchLog(sock, "exit 0: %v", err)
			return 0
		case out.Superseded:
			watchLog(sock, "exit 0: superseded")
			return 0
		case len(out.Comments) > 0:
			watchLog(sock, "exit 2: waking claude with %d comment(s)", len(out.Comments))
			fmt.Fprint(stderr, tools.CommentsText(out.Comments))
			return 2
		}
		register = false
	}
	watchLog(sock, "exit 0: timed out")
	return 0
}

// watchLog appends a line to watch.log next to the socket, since Claude Code
// shows nothing of background hooks.
func watchLog(sock, format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(sock), "watch.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s pid %d: %s\n", time.Now().Format(time.TimeOnly), os.Getpid(), fmt.Sprintf(format, args...))
}
