package core

import (
	"errors"
	"fmt"
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
	Err() error
}

// startServer starts a language server; replaced in tests.
var startServer = func(srv lsp.Server, root string, diags func(lsp.Diagnostics)) (langServer, error) {
	return lsp.Start(srv, root, diags)
}

// The language server sees each buffer as it would be with its pending
// proposals accepted, so diagnostics show what a proposal breaks before the
// user accepts it. Problems in a proposal's new text are drawn on its
// proposed lines; the rest are placed in the buffer.

// SyncLSP opens buffers with the language server for their file type and
// sends the text of those that changed since. The UI calls it once per event,
// so a burst of edits goes out as one change. It needs Post to deliver
// diagnostics, and does nothing without it.
func (e *Editor) SyncLSP() {
	if e.Post == nil {
		return
	}
	for _, b := range e.Bufs {
		if b.Path == "" || b.lspOpen && b.lspKey == b.syncKey() {
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
		s := b.shadow()
		b.lspSeq++
		s.seq = b.lspSeq
		if b.lspOpen {
			c.Change(b.Path, s.seq, s.text)
		} else {
			c.Open(b.Path, srv.Lang, s.seq, s.text)
			b.lspOpen = true
		}
		b.lspKey = b.syncKey()
		b.sent = append(b.sent, s)
		if len(b.sent) > maxSent { // the server is far behind; its diagnostics for these won't be placed
			b.sent = b.sent[len(b.sent)-maxSent:]
		}
	}
}

// maxSent caps the texts kept for placing diagnostics.
const maxSent = 32

// syncKey changes whenever the text the server should see does.
func (b *Buffer) syncKey() string {
	var sb strings.Builder
	fmt.Fprint(&sb, b.Version)
	for _, p := range b.pending {
		if !p.Tweaked { // a tweaked proposal is already in the text
			fmt.Fprint(&sb, " ", p.ID)
		}
	}
	return sb.String()
}

// sentText is a text sent to the server: the buffer at version with the
// pending proposals in spans applied.
type sentText struct {
	seq     int // the document version the server knows it by
	version int // Buffer.Version
	text    text.Text
	spans   []span
}

// span is a proposal applied in a sent text: [start, end) of the buffer
// became n bytes of the proposal's new text.
type span struct {
	p          *Proposal
	start, end int
	n          int
}

// shadow returns the buffer's text with its pending proposals applied.
func (b *Buffer) shadow() sentText {
	s := sentText{version: b.Version, text: b.Text}
	for _, p := range b.pending {
		if !p.Tweaked {
			s.spans = append(s.spans, span{p, p.Start, p.End, len(p.New)})
		}
	}
	for i := len(s.spans) - 1; i >= 0; i-- { // from the end, so earlier offsets hold
		sp := s.spans[i]
		s.text = s.text.Replace(sp.start, sp.end, []byte(sp.p.New), text.Origin{})
	}
	return s
}

// locate maps an offset in the sent text to the span whose new text holds it
// and the offset in that text, or else to nil and an offset in the buffer at
// the sent version. The offset just past a span belongs to the buffer.
func (s sentText) locate(off int) (sp *span, at int) {
	d := 0
	for i := range s.spans {
		sp := &s.spans[i]
		switch {
		case off < sp.start+d:
			return nil, off - d
		case off < sp.start+d+sp.n:
			return sp, off - sp.start - d
		}
		d += sp.n - (sp.end - sp.start)
	}
	return nil, off - d
}

// Problem is a diagnostic's severity and message.
type Problem struct {
	Severity int // lsp.Error and so on; never 0
	Message  string
}

// Summary is the first line of the message.
func (p Problem) Summary() string {
	s, _, _ := strings.Cut(p.Message, "\n")
	return s
}

// Diag is a diagnostic placed in a buffer. Its range moves with edits.
type Diag struct {
	Problem
	start, end *anchor
}

// Range returns the diagnostic's byte range in the current text.
func (d *Diag) Range() (start, end int) { return d.start.off, max(d.start.off, d.end.off) }

// hunkDiag is a diagnostic in a pending proposal's new text, at [start, end)
// of it. That text doesn't change while the proposal is pending.
type hunkDiag struct {
	Problem
	start, end int
}

// AddedDiag is a diagnostic on a hunk's Added line Row, at byte columns
// [Start, End) of it.
type AddedDiag struct {
	Problem
	Row, Start, End int
}

// Diags returns the buffer's diagnostics, ordered by start. Those in pending
// proposals are on the hunks.
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
	var s sentText
	switch i := slices.IndexFunc(b.sent, func(s sentText) bool { return s.seq == d.Version }); {
	case i >= 0:
		s = b.sent[i]
		b.sent = b.sent[i:] // the server is done with older texts
	case d.Version >= 0:
		return
	case len(b.sent) > 0: // the server didn't say: assume the latest
		s = b.sent[len(b.sent)-1]
	default:
		s = b.shadow()
	}
	b.diagSeq = s.seq
	for _, x := range b.diags {
		b.release(x.start)
		b.release(x.end)
	}
	b.diags = nil
	for _, p := range b.pending {
		p.diags = nil
	}
	for _, x := range d.Diags {
		pr := Problem{x.Severity, x.Message}
		if pr.Severity == 0 {
			pr.Severity = lsp.Error // the spec leaves it to the client
		}
		so := offset(s.text, x.Range.Start, d.UTF8)
		eo := max(offset(s.text, x.Range.End, d.UTF8), so)
		switch sp, at := s.locate(so); {
		case sp != nil && sp.p.Status == Pending && !sp.p.Tweaked:
			sp.p.diags = append(sp.p.diags, hunkDiag{pr, at, min(at+eo-so, sp.n)})
		case sp != nil: // the proposal was resolved since
		default:
			q, end := s.locate(eo)
			if q != nil { // runs into a proposal: stop where it starts
				end = q.start
			}
			start := b.mapFrom(s.version, at, false)
			end = max(b.mapFrom(s.version, end, true), start)
			b.diags = append(b.diags, &Diag{pr, b.anchor(start), b.anchor(end)})
		}
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

// addedDiags places p's diagnostics on its hunk's Added lines, which start
// with the first line's text before p (prefix bytes) and hold rows lines.
// A diagnostic running over several lines is marked on its first.
func addedDiags(p *Proposal, prefix, rows int) []AddedDiag {
	var out []AddedDiag
	for _, d := range p.diags {
		before := p.New[:d.start]
		row := strings.Count(before, "\n")
		col := d.start - (strings.LastIndex(before, "\n") + 1)
		if row == 0 {
			col += prefix
		}
		if row >= rows {
			row, col = rows-1, 0 // on a final newline the hunk doesn't draw
		}
		n := d.end - d.start
		if i := strings.IndexByte(p.New[d.start:d.end], '\n'); i >= 0 {
			n = i
		}
		out = append(out, AddedDiag{d.Problem, row, col, col + n})
	}
	return out
}

// DiagAtCursor returns the problem to show for the cursor. On a proposed
// line, it is the most severe one there. Otherwise it is one whose range
// holds the cursor, else the most severe one starting on its line.
func (e *Editor) DiagAtCursor() *Problem {
	var best *Problem
	if e.row > 0 {
		if h := e.HunkAtCursor(); h != nil {
			for i, d := range h.AddedDiags {
				if d.Row == e.row-1 && (best == nil || d.Severity < best.Severity) {
					best = &h.AddedDiags[i].Problem
				}
			}
		}
		return best
	}
	line, head := e.line(), e.Sel.Head
	for _, d := range e.Buf.diags {
		start, end := d.Range()
		if start <= head && head < max(end, start+1) {
			return &d.Problem
		}
		if e.Buf.Text.LineOf(start) == line && (best == nil || d.Severity < best.Severity) {
			best = &d.Problem
		}
	}
	return best
}

// jumpDiag moves to the next (dir > 0) or previous line with a diagnostic in
// the buffer, wrapping around. A hunk with problems in its proposed lines
// counts as its first line.
func (e *Editor) jumpDiag(dir int) {
	var offs []int
	for _, d := range e.Buf.diags {
		offs = append(offs, d.start.off)
	}
	for _, p := range e.Buf.pending {
		if len(p.diags) > 0 {
			offs = append(offs, e.Buf.Text.LineStart(e.Buf.Text.LineOf(p.Start)))
		}
	}
	if len(offs) == 0 {
		e.Msg = "no diagnostics"
		return
	}
	slices.Sort(offs)
	if dir < 0 {
		slices.Reverse(offs)
	}
	line, target := e.line(), offs[0]
	for _, off := range offs {
		l := e.Buf.Text.LineOf(off)
		if dir > 0 && l > line || dir < 0 && l < line {
			target = off
			break
		}
	}
	e.pushJump()
	e.setHead(target)
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

// DiagCounts returns the number of errors and warnings in the buffer,
// including its pending proposals.
func (b *Buffer) DiagCounts() (errors, warnings int) {
	count := func(sev int) {
		switch sev {
		case lsp.Error:
			errors++
		case lsp.Warning:
			warnings++
		}
	}
	for _, d := range b.diags {
		count(d.Severity)
	}
	for _, p := range b.pending {
		for _, d := range p.diags {
			count(d.Severity)
		}
	}
	return errors, warnings
}

// Diagnostic is a problem as reported to agents. Line and Column are 1-based
// and count lines and characters in the buffer. A problem in a pending
// proposal's new text has Proposal set instead, with Line and Column in that
// text.
type Diagnostic struct {
	Problem
	Proposal     int
	Line, Column int
}

// Diagnostics reports the problems in the file at path, opening it if
// needed. Fresh is false while the server has yet to answer for the text as
// it is now; the caller can ask again shortly.
func (e *Editor) Diagnostics(path string) (ds []Diagnostic, fresh bool, err error) {
	srv, ok := lsp.For(path)
	if !ok {
		return nil, false, fmt.Errorf("no language server for %s files", filepath.Ext(path))
	}
	b, err := e.OpenBuffer(path)
	if err != nil {
		return nil, false, err
	}
	if c, started := e.servers[srv.Lang]; started {
		if c == nil {
			return nil, false, fmt.Errorf("%s isn't available", srv.Cmd[0])
		}
		if err := c.Err(); err != nil {
			return nil, false, errors.New("the language server stopped: " + err.Error())
		}
	}
	for _, d := range b.diags {
		start, _ := d.Range()
		line, col := b.Text.LineOf(start), 0
		for range string(b.Text.Slice(b.Text.LineStart(line), start)) {
			col++
		}
		ds = append(ds, Diagnostic{d.Problem, 0, line + 1, col + 1})
	}
	for _, p := range b.pending {
		for _, d := range p.diags {
			before := p.New[:d.start]
			col := 0
			for range before[strings.LastIndex(before, "\n")+1:] {
				col++
			}
			ds = append(ds, Diagnostic{d.Problem, p.ID, strings.Count(before, "\n") + 1, col + 1})
		}
	}
	fresh = b.lspOpen && b.lspKey == b.syncKey() && b.diagSeq == b.lspSeq
	return ds, fresh, nil
}
