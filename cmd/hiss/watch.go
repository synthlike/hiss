package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	register := true
	for deadline := time.Now().Add(watchFor); time.Now().Before(deadline); time.Sleep(watchEvery) {
		args, _ := json.Marshal(map[string]any{"session_id": in.SessionID, "watcher": id, "register": register})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		raw, err := rpc.Call(ctx, sock, rpc.Request{Tool: "watch_comments", Args: args})
		cancel()
		var out tools.WatchOut
		if err != nil || json.Unmarshal(raw, &out) != nil || out.Superseded {
			return 0
		}
		if len(out.Comments) > 0 {
			fmt.Fprint(stderr, tools.CommentsText(out.Comments))
			return 2
		}
		register = false
	}
	return 0
}
