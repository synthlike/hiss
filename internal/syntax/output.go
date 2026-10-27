package syntax

import (
	"bytes"
	"regexp"
)

// Lexers for what runs print: go test's output and query results. ForOutput
// wraps any of them, or JSON, for *out* as hiss writes it.

var (
	// GoTest is go test -v's output.
	GoTest = &Lang{Name: "go test", lex: lexGoTest}
	// Table is a query's result as a table, as sqlite3 -column or psql print it.
	Table = &Lang{Name: "table", lex: lexTable}
)

// statusLine is the line hiss ends a run's output with: [ok, 1.2s].
var statusLine = regexp.MustCompile(`^\[[a-z][^\]"]*, [0-9][^\]]*\]$`)

// ForOutput lexes a run's output in *out* with l, except for the command and
// env lines hiss writes first and the status line it writes last, which are
// comments.
func ForOutput(l *Lang) *Lang {
	return &Lang{Name: l.Name + " output", lex: func(src []byte) []Span {
		var out []Span
		start := 0
		for start < len(src) && (bytes.HasPrefix(src[start:], []byte("$ ")) || bytes.HasPrefix(src[start:], []byte("# "))) {
			end := lineEnd(src, start)
			out = append(out, Span{start, end, Comment})
			start = min(end+1, len(src))
		}
		end, last := len(src), bytes.TrimRight(src, "\n")
		if i := bytes.LastIndexByte(last, '\n') + 1; i >= start && statusLine.Match(last[i:]) {
			end = i
		}
		for _, s := range l.Lex(src[start:end]) {
			out = append(out, Span{s.Start + start, s.End + start, s.Kind})
		}
		if end < len(last) {
			out = append(out, Span{end, len(last), Comment})
		}
		return out
	}}
}

// lines calls f with the start and end of each line in src.
func lines(src []byte, f func(start, end int)) {
	for start := 0; start < len(src); {
		end := lineEnd(src, start)
		f(start, end)
		start = end + 1
	}
}

func hasPrefix(b []byte, prefixes ...string) bool {
	for _, p := range prefixes {
		if bytes.HasPrefix(b, []byte(p)) {
			return true
		}
	}
	return false
}

// goFileLine is a location go test prints, which Enter in *out* jumps to.
var goFileLine = regexp.MustCompile(`[\w./-]+\.go:\d+(?::\d+)?`)

// lexGoTest marks failures Bad and passes Good, dims the progress lines and
// marks file:line locations as keywords.
func lexGoTest(src []byte) []Span {
	var out []Span
	lines(src, func(start, end int) {
		t := bytes.TrimLeft(src[start:end], " \t")
		switch {
		case hasPrefix(t, "--- FAIL", "FAIL", "panic:"):
			out = append(out, Span{start, end, Bad})
		case hasPrefix(t, "--- PASS", "PASS", "ok ", "ok\t"):
			out = append(out, Span{start, end, Good})
		case hasPrefix(t, "=== ", "--- SKIP", "?   "):
			out = append(out, Span{start, end, Comment})
		default:
			if m := goFileLine.FindIndex(src[start:end]); m != nil {
				out = append(out, Span{start + m[0], start + m[1], Keyword})
			}
		}
	})
	return out
}

var (
	rowCount = regexp.MustCompile(`^\(\d+ rows?\)$`)
	cells    = &Lang{Keywords: words("NULL null")}
)

// lexTable marks the first row as the header, rules and row counts as
// comments, errors as Bad, and numbers and NULLs in the cells.
func lexTable(src []byte) []Span {
	var out []Span
	header := true
	lines(src, func(start, end int) {
		t := bytes.TrimSpace(src[start:end])
		switch {
		case len(t) == 0:
		case len(bytes.Trim(t, "-+|= ")) == 0, rowCount.Match(t):
			out = append(out, Span{start, end, Comment})
		case hasPrefix(t, "Error", "ERROR", "Parse error", "Runtime error"):
			out = append(out, Span{start, end, Bad})
		case header:
			out = append(out, Span{start, end, Heading})
			header = false
		default:
			for _, s := range cells.Lex(src[start:end]) {
				out = append(out, Span{s.Start + start, s.End + start, s.Kind})
			}
		}
	})
	return out
}
