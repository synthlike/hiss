package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/synthlike/hiss/internal/syntax"
)

// Enter in a Foundry test file (.t.sol) runs the test under the cursor with
// forge test, compiled by solar, from the nearest foundry.toml up. A rule
// matching *.t.sol replaces this, for a project that needs solc.

var (
	solContract = regexp.MustCompile(`^(\s*)(abstract\s+)?contract\s+(\w+)`)
	solFunc     = regexp.MustCompile(`^(\s*)function\s+(\w+)\s*\(`)
	// forgeTestName matches the functions forge runs as tests.
	forgeTestName = regexp.MustCompile(`^(?:test|invariant|table)`)
)

// forgeTestAt returns the test function the cursor is in and its contract,
// or "" and "". The contract is "" for an abstract one: forge only runs its
// tests in the contracts that inherit them. Like enclosingTest it goes by
// lines: a function ends at a "}" indented as far as its "function".
func (e *Editor) forgeTestAt() (contract, test string) {
	t := e.Buf.Text
	cur := e.line()
	closed := map[int]bool{} // the indents of the closing braces above the cursor
	for l := cur; l >= 0; l-- {
		s := string(t.Line(l))
		if m := solContract.FindStringSubmatch(s); m != nil {
			switch {
			case test == "" || closed[len(m[1])]: // above the cursor's function, or closed before it
				return "", ""
			case m[2] != "":
				return "", test
			}
			return m[3], test
		}
		if m := solFunc.FindStringSubmatch(s); m != nil && test == "" {
			if closed[len(m[1])] || !forgeTestName.MatchString(m[2]) {
				return "", ""
			}
			test = m[2]
		} else if b := strings.TrimLeft(s, " \t"); l < cur && strings.HasPrefix(b, "}") {
			closed[len(s)-len(b)] = true
		}
	}
	return "", "" // a function outside any contract
}

// foundryRoot returns the nearest directory at or above path's that holds a
// foundry.toml, or "".
func foundryRoot(path string) string {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "foundry.toml")); err == nil {
			return dir
		}
		if dir == filepath.Dir(dir) {
			return ""
		}
	}
}

// runForgeTest runs the test under the cursor. forge matches --match-test
// against the whole signature, so the pattern stops at the parenthesis.
func (e *Editor) runForgeTest(c *runConfig) {
	contract, test := e.forgeTestAt()
	if test == "" {
		e.Msg = "not in a test function"
		return
	}
	root := foundryRoot(e.Buf.Path)
	if root == "" {
		e.Msg = "no foundry.toml above this file"
		return
	}
	argv := []string{"forge", "test", "--use", "solar", "--match-test", "^" + regexp.QuoteMeta(test) + `\(`}
	title := test
	if contract != "" {
		argv = append(argv, "--match-contract", "^"+regexp.QuoteMeta(contract)+"$")
		title = contract + "." + test
	}
	e.startRun(&run{title: title, what: title, dir: root, warnDir: root, warnAll: true,
		share: c.forgeShare, pin: c.forgeEnv, argv: append(argv, "-vvv"), output: syntax.ForgeTest})
}
