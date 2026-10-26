package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnclosingTest(t *testing.T) {
	src := `package x

func TestA(t *testing.T) {
	f := func() {
	}
	_ = f
}

func helper() {
}

func (s *suite) TestM() {
}
`
	cases := []struct {
		line int // 0-based
		want string
	}{
		{0, ""},      // package clause
		{2, "TestA"}, // the func line
		{4, "TestA"}, // a closure's closing brace, indented
		{6, "TestA"}, // the closing brace
		{7, ""},      // below it
		{9, ""},      // in a helper
		{12, ""},     // a method isn't a test
	}
	e := newEditor(t, src)
	for _, c := range cases {
		e.setHead(e.Buf.Text.LineStart(c.line))
		if got := e.enclosingTest(); got != c.want {
			t.Errorf("line %d: %q, want %q", c.line+1, got, c.want)
		}
	}
}

func TestRunNothingHere(t *testing.T) {
	e := newEditor(t, "a\n")
	keys(e, "enter")
	if e.Msg != "nothing to run here" {
		t.Errorf("msg %q", e.Msg)
	}
	keys(e, ": r r enter")
	if e.Msg != "nothing run yet" {
		t.Errorf(":rr msg %q", e.Msg)
	}
}

const runTests = `package x

import (
	"testing"
	"time"
)

func TestPass(t *testing.T) {
	t.Log("fine")
}

func TestFail(t *testing.T) {
	t.Error("broken")
}

func TestSlow(t *testing.T) {
	time.Sleep(time.Minute)
}
`

// runEditor opens x_test.go (runTests) in a new module, with a loop the test
// drives through pump.
func runEditor(t *testing.T) (*Editor, chan func(*Editor)) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	e := newEditor(t, "")
	e.Root = filepath.Dir(e.Buf.Path)
	files := map[string]string{"go.mod": "module x\n\ngo 1.21\n", "x_test.go": runTests}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(e.Root, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ch := make(chan func(*Editor))
	e.Post = func(f func(*Editor)) { ch <- f }
	t.Cleanup(e.StopRuns)
	e.openFile(filepath.Join(e.Root, "x_test.go"))
	return e, ch
}

// pump runs posted work until done reports true.
func pump(t *testing.T, e *Editor, ch chan func(*Editor), done func() bool) {
	t.Helper()
	timeout := time.After(60 * time.Second)
	for !done() {
		select {
		case f := <-ch:
			f(e)
		case <-timeout:
			t.Fatalf("timed out; *out*:\n%s", e.runs.out.Text.String())
		}
	}
}

func finished(e *Editor) func() bool { return func() bool { return e.runs.going == nil } }

func gotoText(t *testing.T, e *Editor, s string) {
	t.Helper()
	i := strings.Index(e.Buf.Text.String(), s)
	if i < 0 {
		t.Fatalf("%q not in %s:\n%s", s, e.Buf.Name, e.Buf.Text.String())
	}
	e.setHead(i)
}

func TestRunGoTest(t *testing.T) {
	e, ch := runEditor(t)
	gotoText(t, e, `t.Log("fine")`)
	keys(e, "enter")
	if e.Buf.Name != outName || e.Running() != "running TestPass (C-c)" {
		t.Fatalf("after enter: on %s, running %q, msg %q", e.Buf.Name, e.Running(), e.Msg)
	}
	pump(t, e, ch, finished(e))
	out := e.Buf.Text.String()
	for _, want := range []string{"$ go test -run '^TestPass$' -v .\n", "--- PASS: TestPass", "[ok, "} {
		if !strings.Contains(out, want) {
			t.Errorf("*out* lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(e.Msg, "TestPass: ok (") {
		t.Errorf("msg %q", e.Msg)
	}
	if e.Buf.Dirty() {
		t.Error("*out* is dirty")
	}

	keys(e, "C-o") // back to the test file
	gotoText(t, e, `t.Error("broken")`)
	keys(e, "enter")
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(e.Msg, "TestFail: exit status 1 (") {
		t.Errorf("msg %q", e.Msg)
	}
	gotoText(t, e, "x_test.go:13:")
	keys(e, "enter")
	if e.Buf.Name != "x_test.go" || e.line() != 12 {
		t.Errorf("from the failure: on %s line %d, want x_test.go line 12", e.Buf.Name, e.line())
	}

	keys(e, ": r r enter") // from anywhere
	if e.Buf.Name != outName || e.Running() == "" {
		t.Fatalf(":rr: on %s, running %q, msg %q", e.Buf.Name, e.Running(), e.Msg)
	}
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(e.Msg, "TestFail: exit status 1") || strings.Count(e.Buf.Text.String(), "$ go test") != 1 {
		t.Errorf("rerun: msg %q, *out*:\n%s", e.Msg, e.Buf.Text.String())
	}
}

func TestRunCancel(t *testing.T) {
	e, ch := runEditor(t)
	gotoText(t, e, "time.Sleep(time.Minute)")
	keys(e, "enter")
	pump(t, e, ch, func() bool { return strings.Contains(e.runs.out.Text.String(), "=== RUN   TestSlow") })
	keys(e, ": r r enter")
	if e.Msg != "TestSlow is still running (C-c cancels)" {
		t.Errorf(":rr while running: msg %q", e.Msg)
	}
	start := time.Now()
	keys(e, "C-c")
	pump(t, e, ch, finished(e))
	if !strings.HasPrefix(e.Msg, "TestSlow: cancelled") || time.Since(start) > 10*time.Second {
		t.Errorf("msg %q after %s", e.Msg, time.Since(start))
	}
}

func TestRunWarnsAboutUnsavedFiles(t *testing.T) {
	e, ch := runEditor(t)
	gotoText(t, e, `t.Log("fine")`)
	keys(e, "i x esc") // unsaved, and still in TestPass
	keys(e, "enter")
	pump(t, e, ch, finished(e))
	const w = "ran on disk, without 1 unsaved file"
	if !strings.Contains(e.Buf.Text.String(), "# "+w+"\n") || !strings.HasSuffix(e.Msg, "; "+w) {
		t.Errorf("msg %q, *out*:\n%s", e.Msg, e.Buf.Text.String())
	}
}

func TestOpenOutputLocation(t *testing.T) {
	e := newEditor(t, "1\n2\n3\n")
	e.Root = filepath.Dir(e.Buf.Path)
	out := e.outBuffer()
	e.SwitchTo(out)
	e.appendOut("no location here\n" +
		"\tgone.go:3 then f.txt:2:1 and f.txt:3\n")
	f := e.Bufs[0]
	e.setHead(0)
	keys(e, "enter")
	if e.Msg != "no file:line on this line" {
		t.Errorf("msg %q", e.Msg)
	}
	keys(e, "j enter") // the cursor isn't on one: the first that exists
	if e.Buf != f || e.line() != 1 {
		t.Errorf("first: on %s line %d", e.Buf.Name, e.line())
	}
	keys(e, "C-o")
	gotoText(t, e, "f.txt:3")
	keys(e, "l enter") // under the cursor
	if e.Buf != f || e.line() != 2 {
		t.Errorf("under cursor: on %s line %d", e.Buf.Name, e.line())
	}
}

func TestRunReportsSharedRuns(t *testing.T) {
	e, ch := runEditor(t)
	gotoText(t, e, `t.Error("broken")`)
	keys(e, "enter")
	pump(t, e, ch, finished(e))
	r := e.TakeRun("s1")
	if r == nil {
		t.Fatal("no report for a Go test")
	}
	if r.Title != "TestFail" || r.Status != "exit status 1" || r.Command != "go test -run '^TestFail$' -v ." ||
		!strings.Contains(r.Tail, "x_test.go:13: broken") || strings.Contains(r.Tail, "[exit status 1") {
		t.Errorf("report %+v", r)
	}
	if e.TakeRun("s1") != nil {
		t.Error("s1 was told twice")
	}
	if e.TakeRun("s2") == nil {
		t.Error("s2 wasn't told")
	}
}

func TestRunReportTail(t *testing.T) {
	e := newEditor(t, "")
	var out strings.Builder
	for i := range 30 {
		fmt.Fprintf(&out, "line %d\n", i)
	}
	e.report(&run{title: "x", show: "x"}, "ok", 0, out.String())
	r := e.TakeRun("s")
	if !strings.HasPrefix(r.Tail, "line 10\n") || !strings.HasSuffix(r.Tail, "line 29\n") {
		t.Errorf("tail %q", r.Tail)
	}
}
