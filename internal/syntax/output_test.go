package syntax

import "testing"

func TestOutput(t *testing.T) {
	cases := []struct {
		lang      *Lang
		src, want string
	}{
		{GoTest, "=== RUN   TestA\n    a_test.go:12: got 1\n--- FAIL: TestA (0.00s)\nFAIL\tx/a\t0.1s\n",
			"comment:=== RUN   TestA | keyword:a_test.go:12 | bad:--- FAIL: TestA (0.00s) | bad:FAIL\tx/a\t0.1s"},
		{GoTest, "--- PASS: TestB (0.00s)\nok  \tx/b\t0.1s\n", "good:--- PASS: TestB (0.00s) | good:ok  \tx/b\t0.1s"},
		{Table, "id  body\n--  ----\n1   NULL\n2   it's 3\n", "heading:id  body | comment:--  ---- | number:1 | keyword:NULL | number:2 | number:3"},
		{Table, " id | n\n----+---\n  7 | x\n(1 row)\n", "heading: id | n | comment:----+--- | number:7 | comment:(1 row)"},
		{Table, "Parse error near line 1: no such column: x\n", "bad:Parse error near line 1: no such column: x"},
		{ForOutput(JSON), "$ curl x\n# env dev\n{\"a\": 1}\n[ok, 0.2s]\n",
			`comment:$ curl x | comment:# env dev | string:"a" | number:1 | comment:[ok, 0.2s]`},
		{ForOutput(JSON), "$ jq\n[1]\n", "comment:$ jq | number:1"}, // still going: no status line yet
		{ForOutput(Table), "$ sqlite3\nid\n--\n1\n[exit status 1, 1m2s]\n",
			"comment:$ sqlite3 | heading:id | comment:-- | number:1 | comment:[exit status 1, 1m2s]"},
	}
	for _, c := range cases {
		if got := marks(c.src, c.lang.Lex([]byte(c.src))); got != c.want {
			t.Errorf("%s %q:\n got %s\nwant %s", c.lang.Name, c.src, got, c.want)
		}
	}
}
