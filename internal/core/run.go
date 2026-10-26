package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/synthlike/hiss/internal/text"
)

// Enter in normal mode runs the thing under the cursor: for now, the Go test
// it's in. The command runs on a worker goroutine against the files on disk,
// and its output streams into the *out* buffer, where Enter on a file:line
// goes there. One run at a time; C-c cancels it and :rerun repeats the last.

// outName names the buffer runs write to.
const outName = "*out*"

// run is a command Enter or :rerun starts.
type run struct {
	title string   // what the status line calls it, such as the test's name
	dir   string   // working directory
	argv  []string // the command; no shell
}

type runState struct {
	last   *run
	going  *run
	cancel context.CancelFunc // stops going
	start  time.Time
	out    *Buffer
}

// testFunc matches the line a test, fuzz test or example starts on.
var testFunc = regexp.MustCompile(`^func ((?:Test|Fuzz|Example)\w*)\(`)

func (e *Editor) runAtCursor() {
	if e.Buf == e.runs.out {
		e.openOutputLocation()
		return
	}
	if !strings.HasSuffix(e.Buf.Path, "_test.go") {
		e.Msg = "nothing to run here"
		return
	}
	name := e.enclosingTest()
	if name == "" {
		e.Msg = "not in a test function"
		return
	}
	e.startRun(&run{title: name, dir: filepath.Dir(e.Buf.Path),
		argv: []string{"go", "test", "-run", "^" + name + "$", "-v", "."}})
}

// enclosingTest returns the name of the top-level test function the cursor
// is in, or "". Go code starts functions at the start of a line with "func "
// and ends them with a "}" line, which is what it goes by.
func (e *Editor) enclosingTest() string {
	t := e.Buf.Text
	cur := e.line()
	for l := cur; l >= 0; l-- {
		s := string(t.Slice(t.LineStart(l), e.Buf.LineEnd(l)))
		switch {
		case strings.HasPrefix(s, "func "):
			if m := testFunc.FindStringSubmatch(s); m != nil {
				return m[1]
			}
			return ""
		case l < cur && strings.HasPrefix(s, "}"):
			return "" // below the end of a function
		}
	}
	return ""
}

func (e *Editor) rerun() {
	if e.runs.last == nil {
		e.Msg = "nothing run yet"
		return
	}
	e.startRun(e.runs.last)
}

// startRun shows *out* and runs r into it.
func (e *Editor) startRun(r *run) {
	if g := e.runs.going; g != nil {
		e.Msg = g.title + " is still running (C-c cancels)"
		return
	}
	if e.Post == nil {
		e.Msg = "can't run without the editor loop"
		return
	}
	out := e.outBuffer()
	header := "$ " + e.shellLine(r) + "\n"
	if w := e.diskWarning(r.dir); w != "" {
		header += "# " + w + "\n"
	}
	out.Replace(0, out.Text.Len(), []byte(header), text.Origin{})
	out.undo, out.redo = nil, nil
	if e.Buf != out {
		e.pushJump()
		e.SwitchTo(out)
	}
	e.setHead(out.Text.LineStart(out.Text.LineCount() - 1))
	e.Top, e.TopSub = 0, 0

	ctx, cancel := context.WithCancel(context.Background())
	e.runs.last, e.runs.going, e.runs.cancel, e.runs.start = r, r, cancel, e.now()
	cmd := exec.CommandContext(ctx, r.argv[0], r.argv[1:]...)
	cmd.Dir = r.dir
	// Its own process group, so cancelling also stops what it started (go test
	// runs the test binary as a child).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second // for grandchildren holding the output open
	w := outWriter{e.Post, r}
	cmd.Stdout, cmd.Stderr = w, w
	go func() {
		err := cmd.Run()
		cancelled := ctx.Err() != nil
		cancel()
		e.Post(func(e *Editor) { e.finishRun(r, err, cancelled) })
	}()
}

// outWriter hands output to the editor loop as it arrives.
type outWriter struct {
	post func(func(*Editor))
	r    *run
}

func (w outWriter) Write(p []byte) (int, error) {
	s := string(p)
	w.post(func(e *Editor) {
		if e.runs.going == w.r {
			e.appendOut(s)
		}
	})
	return len(p), nil
}

// appendOut adds s to *out*, keeping the cursor on the last line if it was
// there, so the output can be followed as it arrives.
func (e *Editor) appendOut(s string) {
	out := e.runs.out
	follow := e.Buf == out && e.line() == out.Text.LineCount()-1
	out.Replace(out.Text.Len(), out.Text.Len(), []byte(s), text.Origin{})
	if follow {
		e.setHead(out.Text.LineStart(out.Text.LineCount() - 1))
		e.scroll()
	}
}

func (e *Editor) finishRun(r *run, err error, cancelled bool) {
	if e.runs.going != r {
		return
	}
	e.runs.going, e.runs.cancel = nil, nil
	took := e.now().Sub(e.runs.start).Round(100 * time.Millisecond)
	var ee *exec.ExitError
	status := "ok"
	switch {
	case cancelled:
		status = "cancelled"
	case errors.As(err, &ee):
		status = ee.ProcessState.String() // "exit status 1"
	case err != nil:
		status = err.Error()
	}
	out := e.runs.out
	if n := out.Text.Len(); n > 0 && out.Text.Slice(n-1, n)[0] != '\n' {
		e.appendOut("\n")
	}
	e.appendOut(fmt.Sprintf("[%s, %s]\n", status, took))
	e.Msg = fmt.Sprintf("%s: %s (%s)", r.title, status, took)
	if w := e.diskWarning(r.dir); w != "" {
		e.Msg += "; " + w
	}
}

// cancelRun stops the run that's going, if any.
func (e *Editor) cancelRun() {
	if e.runs.cancel == nil {
		return
	}
	e.runs.cancel()
	e.Msg = "cancelling " + e.runs.going.title
}

// StopRuns cancels a run that's going; hiss calls it on the way out.
func (e *Editor) StopRuns() {
	if e.runs.cancel != nil {
		e.runs.cancel()
	}
}

// Running describes the run that's going for the status line, or "".
func (e *Editor) Running() string {
	if g := e.runs.going; g != nil {
		return "running " + g.title + " (C-c)"
	}
	return ""
}

// outBuffer returns *out*, creating it or putting it back in the buffer list.
func (e *Editor) outBuffer() *Buffer {
	if e.runs.out == nil {
		e.runs.out = &Buffer{Name: outName, Text: text.New(nil, text.Origin{}), output: true}
	}
	for _, b := range e.Bufs {
		if b == e.runs.out {
			return b
		}
	}
	e.Bufs = append(e.Bufs, e.runs.out)
	return e.runs.out
}

// diskWarning says what the run doesn't see: pending hunks and unsaved
// changes in files under dir, since commands read the files on disk.
func (e *Editor) diskWarning(dir string) string {
	hunks, unsaved := 0, 0
	for _, b := range e.Bufs {
		if b.Path == "" || filepath.Dir(b.Path) != dir {
			continue
		}
		hunks += len(b.pending)
		if b.Dirty() {
			unsaved++
		}
	}
	var not []string
	if hunks > 0 {
		not = append(not, plural(hunks, "pending hunk"))
	}
	if unsaved > 0 {
		not = append(not, plural(unsaved, "unsaved file"))
	}
	if len(not) == 0 {
		return ""
	}
	return "ran on disk, without " + strings.Join(not, " and ")
}

func plural(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}

// shellLine shows r as a command to paste into a shell at the project root.
func (e *Editor) shellLine(r *run) string {
	var b strings.Builder
	dir := r.dir
	if rel, ok := e.underRoot(dir); ok {
		dir = rel
	}
	if dir != "." {
		b.WriteString("cd ")
		b.WriteString(shellQuote(dir))
		b.WriteString(" && ")
	}
	for i, a := range r.argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// location matches a file:line or file:line:col, as compilers, tests and
// stack traces print them.
var location = regexp.MustCompile(`([^\s:()"'\x60]+\.\w+):(\d+)(?::(\d+))?`)

// openOutputLocation goes to the file:line under the cursor in *out*, or the
// first one on the line that exists. Relative paths are tried against the
// run's directory and then the project root.
func (e *Editor) openOutputLocation() {
	t, line := e.Buf.Text, e.line()
	start := t.LineStart(line)
	s := string(t.Slice(start, e.Buf.LineEnd(line)))
	col := e.Sel.Head - start
	var dirs []string
	if r := e.runs.last; r != nil {
		dirs = append(dirs, r.dir)
	}
	dirs = append(dirs, e.Root)
	type loc struct {
		path      string
		line, col int
	}
	var found []loc
	under := -1
	for _, m := range location.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		l, _ := strconv.Atoi(s[m[4]:m[5]])
		c := 0
		if m[6] >= 0 {
			c, _ = strconv.Atoi(s[m[6]:m[7]])
		}
		path := e.existing(name, dirs)
		if path == "" || l == 0 {
			continue
		}
		if m[0] <= col && col < m[1] {
			under = len(found)
		}
		found = append(found, loc{path, l, c})
	}
	switch {
	case len(found) == 0:
		e.Msg = "no file:line on this line"
		return
	case under < 0:
		under = 0
	}
	f := found[under]
	e.openAt(f.path, f.line, f.col)
}

// existing returns name as an absolute path to a file that exists or is
// open, trying it against each of dirs when it's relative, or "".
func (e *Editor) existing(name string, dirs []string) string {
	cands := []string{name}
	if !filepath.IsAbs(name) {
		cands = nil
		for _, d := range dirs {
			if d != "" {
				cands = append(cands, filepath.Join(d, name))
			}
		}
	}
	for _, p := range cands {
		if e.Find(p) != nil {
			return p
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// openAt opens path at a 1-based line and, if col > 0, a 1-based byte column,
// clamped to the file.
func (e *Editor) openAt(path string, line, col int) {
	e.openFile(path)
	if e.Buf.Path != path {
		return // it couldn't be opened; openFile said why
	}
	l := min(max(line-1, 0), max(e.Buf.Lines()-1, 0))
	off := e.Buf.Text.LineStart(l)
	if col > 0 {
		off = min(off+col-1, e.Buf.LineEnd(l))
	}
	e.setHead(off)
	e.revealLine(l)
}
