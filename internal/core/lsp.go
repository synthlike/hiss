package core

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

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
		b.sent = append(b.sent, sentText{b.Version, b.Text})
		if len(b.sent) > maxSent { // the server is far behind; its diagnostics for these won't be placed
			b.sent = b.sent[len(b.sent)-maxSent:]
		}
	}
}

// maxSent caps the texts kept for placing diagnostics.
const maxSent = 32

type sentText struct {
	version int
	text    text.Text
}

// Diag is a diagnostic placed in a buffer. Its range moves with edits.
type Diag struct {
	start, end *anchor
	Severity   int // lsp.Error and so on; never 0
	Message    string
}

// Range returns the diagnostic's byte range in the current text.
func (d *Diag) Range() (start, end int) { return d.start.off, max(d.start.off, d.end.off) }

// Summary is the first line of the message.
func (d *Diag) Summary() string {
	s, _, _ := strings.Cut(d.Message, "\n")
	return s
}

// Diags returns the buffer's diagnostics, ordered by start.
func (b *Buffer) Diags() []*Diag { return b.diags }

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
		e.Post(func(e *Editor) { e.SetDiagnostics(d) })
	})
	if err != nil {
		e.Msg = srv.Cmd[0] + ": " + err.Error()
		e.servers[srv.Lang] = nil
		return nil
	}
	e.servers[srv.Lang] = c
	return c
}

// SetDiagnostics replaces a buffer's diagnostics with the server's. They are
// placed in the text the server saw, then mapped through later edits.
// Diagnostics for a text no longer kept are dropped; newer ones will follow.
func (e *Editor) SetDiagnostics(d lsp.Diagnostics) {
	b := e.Find(d.Path)
	if b == nil {
		return
	}
	t, version := b.Text, b.Version
	if d.Version >= 0 {
		i := slices.IndexFunc(b.sent, func(s sentText) bool { return s.version == d.Version })
		if i < 0 {
			return
		}
		t, version = b.sent[i].text, d.Version
		b.sent = b.sent[i:] // the server is done with older texts
	}
	for _, x := range b.diags {
		b.release(x.start)
		b.release(x.end)
	}
	b.diags = nil
	for _, x := range d.Diags {
		start := b.mapFrom(version, offset(t, x.Range.Start, d.UTF8), false)
		end := b.mapFrom(version, offset(t, x.Range.End, d.UTF8), true)
		sev := x.Severity
		if sev == 0 {
			sev = lsp.Error // the spec leaves it to the client
		}
		b.diags = append(b.diags, &Diag{start: b.anchor(start), end: b.anchor(end), Severity: sev, Message: x.Message})
	}
	slices.SortStableFunc(b.diags, func(x, y *Diag) int { return x.start.off - y.start.off })
}

// offset converts an LSP position in t to a byte offset, clamping it to the text.
func offset(t text.Text, p lsp.Position, utf8 bool) int {
	if p.Line >= t.LineCount() {
		return t.Len()
	}
	start := t.LineStart(p.Line)
	end := t.Len()
	if p.Line+1 < t.LineCount() {
		end = t.LineStart(p.Line+1) - 1
	}
	if utf8 {
		return start + min(max(p.Character, 0), end-start)
	}
	units := 0
	for i, r := range string(t.Slice(start, end)) {
		if units >= p.Character {
			return start + i
		}
		units += max(utf16.RuneLen(r), 1)
	}
	return end
}

// DiagAtCursor returns the diagnostic to show for the cursor: one whose range
// holds it, else the most severe one starting on its line.
func (e *Editor) DiagAtCursor() *Diag {
	var best *Diag
	line, head := e.line(), e.Sel.Head
	for _, d := range e.Buf.diags {
		start, end := d.Range()
		if start <= head && head < max(end, start+1) {
			return d
		}
		if e.Buf.Text.LineOf(start) == line && (best == nil || d.Severity < best.Severity) {
			best = d
		}
	}
	return best
}

// jumpDiag moves to the start of the next (dir > 0) or previous diagnostic
// in the buffer, wrapping around.
func (e *Editor) jumpDiag(dir int) {
	ds := e.Buf.diags
	if len(ds) == 0 {
		e.Msg = "no diagnostics"
		return
	}
	line := e.line()
	target := ds[0]
	if dir < 0 {
		target = ds[len(ds)-1]
	}
	for i := range ds {
		d := ds[i]
		if dir < 0 {
			d = ds[len(ds)-1-i]
		}
		l := e.Buf.Text.LineOf(d.start.off)
		if dir > 0 && l > line || dir < 0 && l < line {
			target = d
			break
		}
	}
	e.pushJump()
	e.setHead(target.start.off)
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
	for _, d := range b.diags {
		switch d.Severity {
		case lsp.Error:
			errors++
		case lsp.Warning:
			warnings++
		}
	}
	return errors, warnings
}
