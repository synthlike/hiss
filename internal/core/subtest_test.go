package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const subtests = `package x

import "testing"

func TestTable(t *testing.T) {
	tests := []struct {
		name string
		in   int
	}{
		{name: "one case", in: 1},
		{
			in:   2,
			name: "two",
		},
		{name: "dup", in: 3},
		{name: "dup", in: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log("ran", tt.in)
		})
	}
}

func TestMap(t *testing.T) {
	for name, c := range map[string]struct{ h map[string]string }{
		"plain": {h: map[string]string{"inner": "x"}},
	} {
		t.Run(name, func(t *testing.T) { _ = c })
	}
}

func TestNested(t *testing.T) {
	t.Run("outer", func(t *testing.T) {
		t.Run("in.ner", func(t *testing.T) {
			t.Log("deep")
		})
		t.Log("shallow")
	})
	t.Log("top")
}

func TestNoRun(t *testing.T) {
	cases := []struct{ name string }{{name: "lonely"}}
	_ = cases
}
`

func TestSubtestAt(t *testing.T) {
	e := newEditor(t, subtests)
	for at, want := range map[string]string{
		"in: 1}":           "one_case",
		"in:   2":          "two", // above its name
		"in: 3":            "",    // two cases share the name
		`t.Log("ran"`:      "",    // the loop runs them all
		"tests :=":         "",
		`"inner"`:          "plain", // the case, not the map inside it
		`t.Log("deep")`:    "outer/in.ner",
		`t.Log("shallow")`: "outer",
		`t.Log("top")`:     "",
		`"lonely"`:         "", // nothing runs the table
	} {
		gotoText(t, e, at)
		if got := e.subtestAt(e.enclosingTest()); got != want {
			t.Errorf("at %q: %q, want %q", at, got, want)
		}
	}

	e = newEditor(t, "package x\n\nfunc TestA(t *testing.T) {\n\tt.Run(\"a\", func(t *testing.T) {\n")
	gotoText(t, e, "t.Run")
	e.subtestAt("TestA") // a broken file: anything but a panic
}

func TestRunPattern(t *testing.T) {
	for test, want := range map[string]string{
		"TestA":            "^TestA$",
		"TestA/one_case":   "^TestA$/^one_case$",
		"TestA/a.b/c(d)+e": `^TestA$/^a\.b$/^c\(d\)\+e$`,
	} {
		if got := runPattern(test); got != want {
			t.Errorf("%s: %s, want %s", test, got, want)
		}
	}
	if got := rewriteSubtest("a b\tc\x01"); got != `a_b_c\x01` {
		t.Errorf("rewrite: %q", got)
	}
}

func TestRunSubtest(t *testing.T) {
	e, ch := runEditor(t)
	if err := os.WriteFile(filepath.Join(e.Root, "sub_test.go"), []byte(subtests), 0o600); err != nil {
		t.Fatal(err)
	}
	e.openFile(filepath.Join(e.Root, "sub_test.go"))
	gotoText(t, e, "in: 1}")
	keys(e, "enter")
	if got := e.Running(); got != "running TestTable/one_case (C-c)" {
		t.Fatalf("running %q, msg %q", got, e.Msg)
	}
	pump(t, e, ch, finished(e))
	out := e.runs.out.Text.String()
	if !strings.HasPrefix(out, "$ go test -run '^TestTable$/^one_case$' -v .\n") ||
		!strings.Contains(out, "--- PASS: TestTable/one_case") || strings.Contains(out, "TestTable/two") {
		t.Fatalf("*out*:\n%s", out)
	}

	keys(e, "space o")
	gotoText(t, e, "--- PASS: TestTable/one_case")
	keys(e, "enter") // runs it again
	if got := e.Running(); got != "running TestTable/one_case (C-c)" {
		t.Fatalf("from *out*: running %q, msg %q", got, e.Msg)
	}
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(lastOut(e), "[ok, ") {
		t.Errorf("status %q", lastOut(e))
	}
	gotoText(t, e, "[ok, ")
	keys(e, "enter")
	if e.Msg != "no file:line on this line" {
		t.Errorf("on the status: msg %q", e.Msg)
	}
}
