package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForgeTestAt(t *testing.T) {
	src := `pragma solidity ^0.8.13;

contract CounterTest is Test {
    Counter c;

    function setUp() public {
    }

    function test_A() public {
        c.increment();
    }

    function testFuzz_B(uint8 x) public {
        if (x > 0) {
        }
    }

    modifier m() {
        _;
    }
}

contract OtherTest is Test {
    uint256 n;
    function invariant_C() public {
    }
}

abstract contract Base is Test {
    function test_D() public {
    }
}

function test_E() {
}
`
	cases := []struct {
		line           int // 0-based
		contract, test string
	}{
		{0, "", ""},                       // the pragma
		{3, "", ""},                       // a state variable
		{5, "", ""},                       // setUp isn't a test
		{8, "CounterTest", "test_A"},      // the function line
		{9, "CounterTest", "test_A"},      // its body
		{10, "CounterTest", "test_A"},     // the closing brace
		{11, "", ""},                      // below it
		{14, "CounterTest", "testFuzz_B"}, // an indented brace doesn't end it
		{18, "", ""},                      // a modifier below a test
		{23, "", ""},                      // in the next contract, above its first function
		{25, "OtherTest", "invariant_C"},
		{30, "", "test_D"}, // abstract: its contract can't be matched
		{34, "", ""},       // outside any contract
	}
	e := newEditor(t, src)
	for _, c := range cases {
		e.setHead(e.Buf.Text.LineStart(c.line))
		if contract, test := e.forgeTestAt(); contract != c.contract || test != c.test {
			t.Errorf("line %d: %q %q, want %q %q", c.line+1, contract, test, c.contract, c.test)
		}
	}
}

// forgeEditor is a Foundry project below the editor's root, on a test file,
// with a forge that prints its arguments and directory.
func forgeEditor(t *testing.T) (*Editor, chan func(*Editor)) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"args: $*\"\necho \"dir: $PWD\"\necho \"[PASS] test_A() (gas: 1)\"\n"
	if err := os.WriteFile(filepath.Join(bin, "forge"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := newEditor(t, "")
	e.Root, _ = filepath.EvalSymlinks(filepath.Dir(e.Buf.Path))
	project := filepath.Join(e.Root, "contracts")
	os.MkdirAll(filepath.Join(project, "test"), 0o755)
	os.WriteFile(filepath.Join(project, "foundry.toml"), nil, 0o600)
	path := filepath.Join(project, "test", "C.t.sol")
	os.WriteFile(path, []byte("contract CT {\n    function test_A() public {\n        x();\n    }\n}\n"), 0o600)
	ch := make(chan func(*Editor))
	e.Post = func(f func(*Editor)) { ch <- f }
	t.Cleanup(e.StopRuns)
	e.openFile(path)
	return e, ch
}

func TestRunForgeTest(t *testing.T) {
	e, ch := forgeEditor(t)
	gotoText(t, e, "x();")
	keys(e, "enter")
	if e.Running() != "running CT.test_A (C-c)" {
		t.Fatalf("running %q, msg %q", e.Running(), e.Msg)
	}
	pump(t, e, ch, finished(e))
	out := e.runs.out.Text.String()
	for _, want := range []string{
		`args: test --use solar --match-test ^test_A\( --match-contract ^CT$ -vvv`,
		"dir: " + filepath.Join(e.Root, "contracts") + "\n",
		"[PASS] test_A()",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("*out* lacks %q:\n%s", want, out)
		}
	}
}

func TestRunForgeTestWarnsAboutUnsavedFiles(t *testing.T) {
	e, ch := forgeEditor(t)
	dir := filepath.Dir(e.Buf.Path)
	src := filepath.Join(dir, "..", "C.sol") // forge compiles the project, not one directory
	os.WriteFile(src, []byte("contract C {}\n"), 0o600)
	test := e.Buf.Path
	e.openFile(src)
	keys(e, "i x esc")
	e.openFile(test)
	gotoText(t, e, "x();")
	keys(e, "enter")
	pump(t, e, ch, finished(e))
	if out := e.runs.out.Text.String(); !strings.Contains(out, "# ran on disk, without 1 unsaved file\n") {
		t.Errorf("*out*:\n%s", out)
	}
}

func TestRunForgeTestProblems(t *testing.T) {
	e, _ := forgeEditor(t)
	keys(e, "enter") // on the contract line
	if e.Msg != "not in a test function" {
		t.Errorf("msg %q", e.Msg)
	}
	os.Remove(filepath.Join(filepath.Dir(e.Buf.Path), "..", "foundry.toml"))
	gotoText(t, e, "x();")
	keys(e, "enter")
	if e.Msg != "no foundry.toml above this file" {
		t.Errorf("msg %q", e.Msg)
	}
}

func TestForgeRuleReplacesRunner(t *testing.T) {
	e, ch := forgeEditor(t)
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[tests]\nmatch = *.t.sol\nfind = function (test\\w*)\ncmd = echo rule \"$1\"\n"), 0o600)
	gotoText(t, e, "x();")
	keys(e, "enter")
	pump(t, e, ch, finished(e))
	if out := e.runs.out.Text.String(); !strings.Contains(out, "rule test_A") {
		t.Errorf("*out*:\n%s", out)
	}
}
