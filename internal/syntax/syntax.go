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
	Heading
	Strong
	Emphasis
	Good // passing tests
	Bad  // failures and errors
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
	NoCase       bool // keywords match in any case, as in SQL

	lex func(src []byte) []Span // replaces the rules above, if set
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(s) {
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
	SQL  = &Lang{Name: "sql", LineComment: "--", BlockComment: [2]string{"/*", "*/"}, Quotes: "'\"", NoCase: true,
		Keywords: words(`select from where and or not in is null as join left right inner outer full cross on
			using group by order having limit offset union all distinct insert into values update set delete
			create table view index drop alter add column primary key foreign references unique default
			if exists case when then else end begin commit rollback with returning asc desc like between
			true false integer int text real blob boolean varchar timestamp date`)}
	// HTTP is for .http files of curl commands run with sh.
	HTTP = &Lang{Name: "http", LineComment: "#", Quotes: "\"'",
		Keywords: words(`curl jq GET POST PUT PATCH DELETE HEAD OPTIONS if then else fi for do done export`)}
	// HCL is Terraform's language; its // comments go unmarked.
	HCL = &Lang{Name: "hcl", LineComment: "#", BlockComment: [2]string{"/*", "*/"}, Quotes: "\"",
		Keywords: words(`resource data variable output module provider locals terraform backend
			required_providers for_each count depends_on lifecycle dynamic for in if true false null`)}
	Docker = &Lang{Name: "dockerfile", LineComment: "#", Quotes: "\"'", NoCase: true,
		Keywords: words(`from as run cmd copy add env arg workdir expose entrypoint user volume label
			healthcheck shell stopsignal onbuild`)}
)

var byExt = map[string]*Lang{
	".go": Go,
	".js": CLike, ".jsx": CLike, ".ts": CLike, ".tsx": CLike, ".mjs": CLike, ".rs": CLike, ".c": CLike,
	".h": CLike, ".cc": CLike, ".cpp": CLike, ".hpp": CLike, ".java": CLike, ".kt": CLike, ".swift": CLike,
	".cs": CLike, ".zig": CLike, ".dart": CLike, ".scala": CLike,
	".py": Hash, ".sh": Hash, ".bash": Hash, ".zsh": Hash, ".fish": Hash, ".ksh": Hash, ".rb": Hash, ".yml": Hash,
	".yaml": Hash, ".toml": Hash,
	// Dotfiles: filepath.Ext(".bashrc") is the whole name.
	".bashrc": Hash, ".zshrc": Hash, ".profile": Hash, ".envrc": Hash, ".env": Hash, ".hissrun": Hash,
	".json": JSON,
	".sql":  SQL, ".http": HTTP,
	".tf": HCL, ".tfvars": HCL, ".hcl": HCL,
	".dockerfile": Docker,
	".md":         Markdown, ".markdown": Markdown,
	".lisp": Lisp, ".lsp": Lisp, ".cl": Lisp, ".asd": Lisp, ".el": Lisp, ".emacs": Lisp, ".scm": Lisp, ".ss": Lisp,
	".sld": Lisp, ".rkt": Lisp, ".clj": Lisp, ".cljs": Lisp, ".cljc": Lisp, ".edn": Lisp, ".fnl": Lisp,
	".janet": Lisp, ".hy": Lisp, ".puff": Lisp,
}

// ForPath picks a language by file name or extension, or returns nil.
func ForPath(path string) *Lang {
	if strings.HasPrefix(filepath.Base(path), "Dockerfile") { // also Dockerfile.dev
		return Docker
	}
	return byExt[strings.ToLower(filepath.Ext(path))]
}

// ForShebang picks a language for a script without an extension by its
// first line, or returns nil.
func ForShebang(src []byte) *Lang {
	if !bytes.HasPrefix(src, []byte("#!")) {
		return nil
	}
	line := src[:lineEnd(src, 0)]
	for _, js := range []string{"node", "deno", "bun"} {
		if bytes.Contains(line, []byte(js)) {
			return CLike
		}
	}
	return Hash // sh, bash, python, ruby…
}

// Lex returns the highlighted spans of src in order.
func (l *Lang) Lex(src []byte) []Span {
	if l.lex != nil {
		return l.lex(src)
	}
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
			w := string(src[i:j])
			if l.NoCase {
				w = strings.ToLower(w)
			}
			if l.Keywords[w] {
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
