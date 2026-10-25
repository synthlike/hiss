package core

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/synthlike/hiss/internal/lsp"
	"github.com/synthlike/hiss/internal/text"
)

// langServer is the part of *lsp.Client the editor uses; tests fake it.
type langServer interface {
	Open(path, lang string, version int, t text.Text)
	Change(path string, version int, t text.Text)
	Shutdown(timeout time.Duration)
}

// startServer starts a language server; replaced in tests.
var startServer = func(srv lsp.Server, root string, diags func(lsp.Diagnostics)) (langServer, error) {
	return lsp.Start(srv, root, diags)
}

// SyncLSP opens buffers with the language server for their file type and
// sends the text of those that changed since. The UI calls it once per event,
// so a burst of edits goes out as one change. It needs Post to deliver
// diagnostics, and does nothing without it.
func (e *Editor) SyncLSP() {
	if e.Post == nil {
		return
	}
	for _, b := range e.Bufs {
		if b.Path == "" || b.lspOpen && b.lspVersion == b.Version {
			continue
		}
		srv, ok := lsp.For(b.Path)
		if !ok {
			continue
		}
		c := e.langServer(srv, b)
		if c == nil {
			continue
		}
		if b.lspOpen {
			c.Change(b.Path, b.Version, b.Text)
		} else {
			c.Open(b.Path, srv.Lang, b.Version, b.Text)
			b.lspOpen = true
		}
		b.lspVersion = b.Version
	}
}

// langServer returns the running server for srv's language, starting it on
// first use. A server that fails to start is not retried.
func (e *Editor) langServer(srv lsp.Server, b *Buffer) langServer {
	if c, ok := e.servers[srv.Lang]; ok {
		return c
	}
	if e.servers == nil {
		e.servers = map[string]langServer{}
	}
	root := e.Root
	if root == "" {
		root = filepath.Dir(b.Path)
	}
	c, err := startServer(srv, root, func(d lsp.Diagnostics) {
		e.Post(func(e *Editor) { e.setDiagnostics(d) })
	})
	if err != nil {
		e.Msg = srv.Cmd[0] + ": " + err.Error()
		e.servers[srv.Lang] = nil
		return nil
	}
	e.servers[srv.Lang] = c
	return c
}

func (e *Editor) setDiagnostics(d lsp.Diagnostics) {
	if b := e.Find(d.Path); b != nil {
		b.Diags, b.DiagVersion = d.Diags, d.Version
	}
}

// StopLSP shuts the language servers down, together taking at most about a second.
func (e *Editor) StopLSP() {
	var wg sync.WaitGroup
	for _, c := range e.servers {
		if c != nil {
			wg.Go(func() { c.Shutdown(time.Second) })
		}
	}
	wg.Wait()
	e.servers = nil
}

// DiagCounts returns the number of errors and warnings in the buffer.
func (b *Buffer) DiagCounts() (errors, warnings int) {
	for _, d := range b.Diags {
		switch d.Severity {
		case lsp.Error:
			errors++
		case lsp.Warning:
			warnings++
		}
	}
	return errors, warnings
}
