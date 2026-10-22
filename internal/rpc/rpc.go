// Package rpc connects `hiss mcp` (and later hooks) to a running editor over a
// unix socket, one JSON request and response per line.
package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type Request struct {
	Agent string          `json:"agent,omitempty"` // MCP client name, e.g. "claude-code"
	Tool  string          `json:"tool"`
	Args  json.RawMessage `json:"args,omitempty"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ErrNotRunning means no editor is listening for the project.
var ErrNotRunning = errors.New("hiss is not running for this project")

// Root returns the project root for dir: the nearest ancestor with a .git entry,
// or dir itself.
func Root(dir string) string {
	dir, _ = filepath.Abs(dir)
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// SocketPath returns the socket for a project root, creating its directory.
// HISS_SOCKET overrides it. The directory is under /tmp rather than $TMPDIR
// because agents may run with a different TMPDIR than the editor.
func SocketPath(root string) (string, error) {
	if p := os.Getenv("HISS_SOCKET"); p != "" {
		return p, nil
	}
	dir := filepath.Join("/tmp", fmt.Sprintf("hiss-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil {
		return "", err
	} else if fi.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("%s must have mode 0700, has %v", dir, fi.Mode().Perm())
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".sock"), nil
}

// Listen listens on path, replacing a stale socket left by a crashed editor.
func Listen(path string) (net.Listener, error) {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, errors.New("another hiss is already serving this project")
	}
	os.Remove(path)
	return net.Listen("unix", path)
}

// Serve answers requests on ln with handle until ln is closed.
func Serve(ln net.Listener, handle func(Request) Response) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			dec, enc := json.NewDecoder(c), json.NewEncoder(c)
			for {
				var req Request
				if dec.Decode(&req) != nil || enc.Encode(handle(req)) != nil {
					return
				}
			}
		}()
	}
}

// Call sends one request to the editor at path and returns its result.
func Call(ctx context.Context, path string, req Request) (json.RawMessage, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotRunning, err)
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		c.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return nil, err
	}
	var resp Response
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("reading reply from hiss: %w", err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Result, nil
}
