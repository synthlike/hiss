// Command hiss is a modal terminal editor for reviewing AI agents' edits.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/rpc"
	"github.com/synthlike/hiss/internal/tools"
	"github.com/synthlike/hiss/internal/ui"
)

const version = "0.1.0-dev"

const usage = `usage:
  hiss [file]   edit a file, or start with an empty buffer
  hiss mcp      serve MCP on stdio for the hiss running in this project
  hiss hook     forward a Claude Code hook event (JSON on stdin) to hiss
  hiss setup    print Claude Code settings that route edits through hiss`

func main() {
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 1 && args[0] == "mcp":
		err = runMCP()
	case len(args) == 1 && args[0] == "hook":
		runHook(os.Stdin)
	case len(args) == 2 && args[0] == "hook" && args[1] == "watch":
		os.Exit(runWatch(os.Stdin, os.Stderr))
	case len(args) == 1 && args[0] == "setup":
		err = runSetup(os.Stdout)
	case len(args) == 0:
		err = runEditor("")
	case len(args) == 1 && args[0] != "-h" && args[0] != "--help":
		err = runEditor(args[0])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hiss:", err)
		os.Exit(1)
	}
}

func runEditor(path string) error {
	buf := core.Scratch()
	if path != "" {
		var err error
		if buf, err = core.Open(path); err != nil {
			return err
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ed := core.New(buf)
	ed.Root = rpc.Root(cwd)
	ed.Splash = ui.RandomQuote()

	reqs := make(chan func(*core.Editor))
	done := make(chan struct{})
	defer close(done)
	if sock, err := rpc.SocketPath(ed.Root); err != nil {
		ed.Msg = "agents disabled: " + err.Error()
	} else if ln, err := rpc.Listen(sock); err != nil {
		ed.Msg = "agents disabled: " + err.Error()
	} else {
		defer ln.Close()
		go rpc.Serve(ln, func(req rpc.Request) rpc.Response {
			return onLoop(reqs, done, func(ed *core.Editor) rpc.Response { return tools.Exec(ed, req) })
		})
	}

	scr, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := scr.Init(); err != nil {
		return err
	}
	defer scr.Fini()
	ui.Run(ed, scr, reqs)
	return nil
}

// onLoop runs f on the editor goroutine and returns its result. It gives up
// if the loop is stuck, for example while hiss is suspended.
func onLoop(reqs chan<- func(*core.Editor), done <-chan struct{}, f func(*core.Editor) rpc.Response) rpc.Response {
	busy := rpc.Response{Error: "hiss is busy or suspended; ask the user to check the editor"}
	timeout := time.After(10 * time.Second)
	reply := make(chan rpc.Response, 1)
	select {
	case reqs <- func(ed *core.Editor) { reply <- f(ed) }:
	case <-done:
		return rpc.Response{Error: "hiss is shutting down"}
	case <-timeout:
		return busy
	}
	select {
	case r := <-reply:
		return r
	case <-done:
		return rpc.Response{Error: "hiss is shutting down"}
	case <-timeout:
		return busy
	}
}

func runMCP() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sock, err := rpc.SocketPath(rpc.Root(cwd))
	if err != nil {
		return err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "hiss", Version: version},
		&mcp.ServerOptions{Instructions: tools.Instructions})
	tools.Register(s, func(ctx context.Context, agent, tool string, args any) (json.RawMessage, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return rpc.Call(ctx, sock, rpc.Request{Agent: agent, Tool: tool, Args: raw})
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return s.Run(ctx, &mcp.StdioTransport{})
}

// runHook forwards a hook event to the editor. It never fails: hooks must not
// get in Claude Code's way, and hiss may simply not be running.
func runHook(stdin io.Reader) {
	data, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return
	}
	var in struct {
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(data, &in) != nil {
		return
	}
	if in.Cwd == "" {
		in.Cwd, _ = os.Getwd()
	}
	sock, err := rpc.SocketPath(rpc.Root(in.Cwd))
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rpc.Call(ctx, sock, rpc.Request{Tool: "hook", Args: data})
}

// runSetup prints project settings for Claude Code: hooks that report agent
// state to hiss, permission for hiss's tools, and a ban on direct file edits.
func runSetup(w io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, " '\"\\$") {
		exe = "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	command := exe + " hook"
	handler := func(async bool) []map[string]any {
		h := map[string]any{"type": "command", "command": command, "timeout": 5}
		if async {
			h["async"] = true
		}
		return []map[string]any{h}
	}
	group := func(matcher string, async bool) []map[string]any {
		g := map[string]any{"hooks": handler(async)}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return []map[string]any{g}
	}
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{"mcp__hiss"},
			"deny":  []string{"Edit", "Write", "MultiEdit", "NotebookEdit"},
		},
		"hooks": map[string]any{
			"SessionStart":     group("", true),
			"UserPromptSubmit": group("", true),
			// Synchronous, so hiss knows which session is calling before the call arrives.
			"PreToolUse":   group("mcp__hiss__.*", false),
			"PostToolUse":  group("", true),
			"Notification": group("", true),
			// The second Stop handler waits in the background for comments and
			// wakes Claude to answer them (exit 2 with asyncRewake).
			"Stop": []map[string]any{{"hooks": append(handler(true), map[string]any{
				"type": "command", "command": exe + " hook watch", "asyncRewake": true, "timeout": 3600,
			})}},
			"SessionEnd": group("", true),
		},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	fmt.Fprintf(os.Stderr, "# Merge into .claude/settings.json in your project, and register the MCP bridge:\n"+
		"#   claude mcp add --scope user hiss -- %s mcp\n", exe)
	return enc.Encode(settings)
}
