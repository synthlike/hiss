// Command hiss is a modal terminal editor for reviewing AI agents' edits.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
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
  hiss <file>   edit a file
  hiss mcp      serve MCP on stdio for the hiss running in this project`

func main() {
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 1 && args[0] == "mcp":
		err = runMCP()
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
	buf, err := core.Open(path)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ed := core.New(buf)
	ed.Root = rpc.Root(cwd)

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
