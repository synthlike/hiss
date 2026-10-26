package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// subtestAt returns the subtest of test the cursor is in, as go test -v
// names it ("one_case", or "outer/inner" for nested t.Run calls), or "" for
// the whole test. A subtest is a t.Run call with a literal name, or a case
// in a table the test runs with t.Run: an element with a name, desc or title
// field, or a map entry with a string key. Duplicate names give "".
func (e *Editor) subtestAt(test string) string {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", e.Buf.Text.String(), parser.SkipObjectResolution)
	if f == nil || !f.Pos().IsValid() {
		return ""
	}
	pos := fset.File(f.Pos()).Pos(e.Sel.Head)
	inside := func(n ast.Node) bool { return n.Pos() <= pos && pos < n.End() }
	var body *ast.BlockStmt
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == test && fd.Body != nil && inside(fd.Body) {
			body = fd.Body
		}
	}
	if body == nil {
		return ""
	}
	var runs []string // the t.Run calls with literal names around the cursor, outermost first
	table, tabled := "", false
	looped := false // a t.Run whose name isn't a literal: a table's loop
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			name, ok := runCall(n)
			switch {
			case !ok:
			case name == "":
				looped = true
			case inside(n):
				runs = append(runs, rewriteSubtest(name))
			}
		case *ast.CompositeLit:
			if !tabled && inside(n) { // the outermost table: a case's own maps aren't one
				table, tabled = tableCase(n, inside)
			}
		}
		return true
	})
	switch {
	case len(runs) > 0:
		return strings.Join(runs, "/")
	case looped && table != "":
		return rewriteSubtest(table)
	}
	return ""
}

// runCall returns the name of a x.Run("name", func…) call, "" if it isn't a
// literal, or false if n isn't one.
func runCall(n *ast.CallExpr) (string, bool) {
	sel, ok := n.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" || len(n.Args) != 2 {
		return "", false
	}
	if _, ok := n.Args[1].(*ast.FuncLit); !ok {
		return "", false
	}
	name, _ := stringLit(n.Args[0])
	return name, true
}

// caseFields name a table case.
var caseFields = []string{"name", "desc", "title"}

// tableCase returns the name of the case in cl the cursor is in, and whether
// cl's elements have names at all. The name is "" between cases or when two
// share it.
func tableCase(cl *ast.CompositeLit, inside func(ast.Node) bool) (string, bool) {
	seen := map[string]int{}
	at := ""
	for _, el := range cl.Elts {
		name := ""
		switch el := el.(type) {
		case *ast.KeyValueExpr: // map[string]struct{…}{"case": {…}}
			if isLit(el.Value) {
				name, _ = stringLit(el.Key)
			}
		default:
			if c := lit(el); c != nil {
				for _, f := range c.Elts {
					if kv, ok := f.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && slices.Contains(caseFields, id.Name) {
							name, _ = stringLit(kv.Value)
						}
					}
				}
			}
		}
		if name == "" {
			continue
		}
		seen[name]++
		if inside(el) {
			at = name
		}
	}
	if seen[at] > 1 {
		at = "" // go test calls the second one "case#01"
	}
	return at, len(seen) > 0
}

// lit returns n as a composite literal, also through &, or nil.
func lit(n ast.Expr) *ast.CompositeLit {
	if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
		n = u.X
	}
	c, _ := n.(*ast.CompositeLit)
	return c
}

func isLit(n ast.Expr) bool { return lit(n) != nil }

func stringLit(n ast.Expr) (string, bool) {
	b, ok := n.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// rewriteSubtest names a subtest as the testing package does: spaces become
// underscores and unprintable runes are escaped.
func rewriteSubtest(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			b.WriteString(strings.Trim(strconv.QuoteRune(r), "'"))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// runPattern matches exactly test, given as go test -v prints it
// ("TestX/case"), for go test -run.
func runPattern(test string) string {
	parts := strings.Split(test, "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	return strings.Join(parts, "/")
}

// testLine matches the lines go test -v names a test on.
var testLine = regexp.MustCompile(`^\s*(?:--- (?:PASS|FAIL|SKIP):|=== RUN)\s+((?:Test|Fuzz|Example)\S*)`)
