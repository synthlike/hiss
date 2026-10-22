// Package syntax finds comments, strings, numbers and keywords for light
// highlighting. It is a small lexer, not a parser: good enough to colour code,
// not to understand it.
package syntax

import (
	"bytes"
	"path/filepath"
	"strings"
)

type Kind uint8

const (
	Plain Kind = iota
	Comment
	String
	Number
	Keyword
)

// Span is a highlighted byte range [Start, End).
type Span struct {
	Start, End int
	Kind       Kind
}

// Lang holds a language's lexing rules.
type Lang struct {
	Name         string
	LineComment  string    // e.g. "//" or "#"
	BlockComment [2]string // e.g. "/*", "*/"
	Quotes       string    // characters that open strings
	RawQuotes    string    // quotes without escapes that may span lines, e.g. "`" in Go
	Keywords     map[string]bool
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var (
	Go = &Lang{Name: "go", LineComment: "//", BlockComment: [2]string{"/*", "*/"}, Quotes: "\"'`", RawQuotes: "`",
		Keywords: words(`break case chan const continue default defer else fallthrough for func go goto if
			import interface map package range return select struct switch type var true false nil`)}
	CLike = &Lang{Name: "c-like", LineComment: "//", BlockComment: [2]string{"/*", "*/"}, Quotes: "\"'`", RawQuotes: "`",
		Keywords: words(`if else for while do return break continue switch case default function fn let
			const var class struct enum impl trait pub use mod import export from new this self super
			true false null nil undefined try catch finally throw async await type interface static
			void match loop where as in of yield extends implements package public private protected`)}
	Hash = &Lang{Name: "hash", LineComment: "#", Quotes: "\"'",
		Keywords: words(`def class if elif else for while return import from as with try except finally
			raise lambda pass break continue in not and or is None True False then fi do done case esac
			function local export end true false null`)}
	JSON = &Lang{Name: "json", Quotes: "\"", Keywords: words("true false null")}
)

var byExt = map[string]*Lang{
	".go": Go,
	".js": CLike, ".jsx": CLike, ".ts": CLike, ".tsx": CLike, ".mjs": CLike, ".rs": CLike, ".c": CLike,
	".h": CLike, ".cc": CLike, ".cpp": CLike, ".hpp": CLike, ".java": CLike, ".kt": CLike, ".swift": CLike,
	".cs": CLike, ".zig": CLike, ".dart": CLike, ".scala": CLike,
	".py": Hash, ".sh": Hash, ".bash": Hash, ".zsh": Hash, ".fish": Hash, ".rb": Hash, ".yml": Hash,
	".yaml": Hash, ".toml": Hash,
	".json": JSON,
}

// ForPath picks a language by file extension, or returns nil.
func ForPath(path string) *Lang { return byExt[strings.ToLower(filepath.Ext(path))] }

// Lex returns the highlighted spans of src in order.
func (l *Lang) Lex(src []byte) []Span {
	var out []Span
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case l.LineComment != "" && bytes.HasPrefix(src[i:], []byte(l.LineComment)):
			end := lineEnd(src, i)
			out = append(out, Span{i, end, Comment})
			i = end
		case l.BlockComment[0] != "" && bytes.HasPrefix(src[i:], []byte(l.BlockComment[0])):
			end := len(src)
			if j := bytes.Index(src[i+len(l.BlockComment[0]):], []byte(l.BlockComment[1])); j >= 0 {
				end = i + len(l.BlockComment[0]) + j + len(l.BlockComment[1])
			}
			out = append(out, Span{i, end, Comment})
			i = end
		case strings.IndexByte(l.Quotes, c) >= 0:
			end := l.stringEnd(src, i)
			out = append(out, Span{i, end, String})
			i = end
		case isDigit(c) && (i == 0 || !isIdent(src[i-1])):
			j := i
			for j < len(src) && (isIdent(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, Span{i, j, Number})
			i = j
		case isIdent(c):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			if l.Keywords[string(src[i:j])] {
				out = append(out, Span{i, j, Keyword})
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// stringEnd returns the end of the string opened at i: after the closing
// quote, or at the end of the line if the string isn't closed there (raw
// strings may span lines).
func (l *Lang) stringEnd(src []byte, i int) int {
	q := src[i]
	raw := strings.IndexByte(l.RawQuotes, q) >= 0
	for j := i + 1; j < len(src); j++ {
		switch {
		case src[j] == q:
			return j + 1
		case src[j] == '\\' && !raw:
			j++
		case src[j] == '\n' && !raw:
			return j
		}
	}
	return len(src)
}

func lineEnd(src []byte, i int) int {
	if j := bytes.IndexByte(src[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(src)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isIdent reports identifier bytes; bytes of multi-byte UTF-8 runes count, so
// names in any script stay whole.
func isIdent(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
