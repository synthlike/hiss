package syntax

import (
	"fmt"
	"strings"
	"testing"
)

// marks renders spans as "kind:text" for readable comparisons.
func marks(src string, spans []Span) string {
	names := []string{"plain", "comment", "string", "number", "keyword", "heading", "strong", "emphasis"}
	var out []string
	for _, s := range spans {
		out = append(out, fmt.Sprintf("%s:%s", names[s.Kind], src[s.Start:s.End]))
	}
	return strings.Join(out, " | ")
}

func TestLex(t *testing.T) {
	cases := []struct {
		lang      *Lang
		src, want string
	}{
		{Go, `func f() int { return 42 } // done`,
			"keyword:func | keyword:return | number:42 | comment:// done"},
		{Go, `s := "a \"q\" b" + 'x'`, `string:"a \"q\" b" | string:'x'`},
		{Go, "r := `one\ntwo` /* c\nd */ x2 := 0x1F",
			"string:`one\ntwo` | comment:/* c\nd */ | number:0x1F"},
		{Go, `"unterminated` + "\nreturn", `string:"unterminated | keyword:return`},
		{Go, `formatted iffy elsewhere`, ""}, // keywords only as whole words
		{CLike, `const x = 'a'; // js`, "keyword:const | string:'a' | comment:// js"},
		{Hash, `def f(): # py` + "\n" + `  return None`, "keyword:def | comment:# py | keyword:return | keyword:None"},
		{JSON, `{"a": [1, true, null]}`, `string:"a" | number:1 | keyword:true | keyword:null`},
		{Markdown, "# Title\n#hashtag\nSub\n===", "heading:# Title | heading:==="},
		{Markdown, "- a **b** and *c*, not snake_case_name", "number:- | strong:**b** | emphasis:*c*"},
		{Markdown, "1. see [docs](https://x.y) and `a*b*`", "number:1. | keyword:[docs] | comment:(https://x.y) | string:`a*b*`"},
		{Markdown, "```go\n# not a heading\n```\n> quote\n---", "string:```go | string:# not a heading | string:``` | comment:> quote | comment:---"},
		{Markdown, "<!-- a\nb --> after\n2 * 3 * 4", "comment:<!-- a\nb -->"},
	}
	for _, c := range cases {
		if got := marks(c.src, c.lang.Lex([]byte(c.src))); got != c.want {
			t.Errorf("%s %q:\n got %s\nwant %s", c.lang.Name, c.src, got, c.want)
		}
	}
}

func TestForPath(t *testing.T) {
	for path, want := range map[string]*Lang{
		"/x/main.go": Go, "a.TS": CLike, "run.sh": Hash, "go.mod": nil, "p.json": JSON, "README.md": Markdown, "Makefile": nil,
	} {
		if got := ForPath(path); got != want {
			t.Errorf("ForPath(%q) = %v", path, got)
		}
	}
}
