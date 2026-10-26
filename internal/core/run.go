package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/synthlike/hiss/internal/text"
)

// Enter in normal mode runs the thing under the cursor: what a rule in
// .hissrun says (see rules.go), or else the Go test it's in. The command runs
// on a worker goroutine, and its output streams into the *out* buffer, where
// Enter on a file:line goes there. One run at a time; C-c cancels it and
// :rerun repeats the last. The cursor stays put: the end of *out* shows in a
// panel below the text until esc, and space o opens *out* itself.

// outName names the buffer runs write to.
const outName = "*out*"

// run is a command Enter or :rerun starts.
type run struct {
	title   string   // what the status line calls it, such as the test's name
	dir     string   // working directory
	argv    []string // the command
	env     []string // added to hiss's environment
	stdin   string
	show    string // the command as *out* shows it; set from argv if ""
	warnDir string // say if files here have changes that aren't on disk; "" not to
	share   bool   // the agent may see the output
	pin     string // the env it always runs in; "" for the active one
	envName string // the env it last ran in; "" with none
}

type runState struct {
	last   *run
	going  *run
	cancel context.CancelFunc // stops going
	start  time.Time
	out    *Buffer
	header int  // bytes of *out* before the output
	panel  bool // show the end of *out* below the text

	report *RunReport     // the latest shared run
	told   map[string]int // the Seq of the report each session was given

	env    string   // the env :env picked; "" for the first
	envs   []string // the envs' names, as the rules file last said
	loaded bool     // envs is up to date
}

// RunReport is a finished shared run, handed to each agent session once
// with the user's next prompt or the agent's next tool call.
type RunReport struct {
	Seq     int
	Title   string
	Env     string // the environment it ran in, or ""
	Command string
	Status  string // "ok", "exit status 1", "cancelled", …
	Took    time.Duration
	Tail    string // the output's last lines
}

// reportLines is how much output a report carries.
const reportLines = 20

// testFunc matches the line a test, fuzz test or example starts on.
var testFunc = regexp.MustCompile(`^func ((?:Test|Fuzz|Example)\w*)\(`)

func (e *Editor) runAtCursor() {
	if e.Buf == e.runs.out {
		e.openOutputLocation()
		return
	}
	c, err := e.loadRunConfig()
	if err != nil {
		e.Msg = err.Error()
		return
	}
	if e.runByRule(c.rules) {
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
	dir := filepath.Dir(e.Buf.Path)
	e.startRun(&run{title: name, dir: dir, warnDir: dir, share: c.goShare, pin: c.goEnv,
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

// startRun runs r into *out* in its environment, asking first if that has
// confirm set.
func (e *Editor) startRun(r *run) {
	if g := e.runs.going; g != nil {
		e.Msg = e.label(g) + " is still running (C-c to cancel)"
		return
	}
	if e.Post == nil {
		e.Msg = "can't run without the editor loop"
		return
	}
	c, err := e.loadRunConfig()
	if err != nil {
		e.Msg = err.Error()
		return
	}
	en, err := e.runEnv(c, r.pin)
	if err != nil {
		e.Msg = err.Error()
		return
	}
	r.envName = ""
	if en != nil {
		r.envName = en.name
		if en.source != "" {
			if _, err := os.Stat(e.sourcePath(en)); err != nil {
				e.Msg = failed("env "+en.name+": sourcing "+en.source, err)
				return
			}
		}
	}
	if en == nil || !en.confirm {
		e.launch(r, en)
		return
	}
	e.Menu = &Menu{Title: "run " + e.label(r) + "?", Items: []MenuItem{
		{"y", "run it", func(e *Editor) { e.launch(r, en) }},
	}}
}

// launch runs r into *out*, showing the panel unless *out* is on screen.
func (e *Editor) launch(r *run, en *env) {
	out := e.outBuffer()
	if r.show == "" {
		r.show = e.shellLine(r)
	}
	header := "$ " + r.show + "\n"
	argv, environ := r.argv, r.env
	if en != nil {
		environ = append(slices.Clip(environ), "env="+en.name)
		header += "# env " + en.name
		if en.source != "" {
			// Sourced from the root, where the rules file's paths start.
			argv = append([]string{"sh", "-c", "cd " + shellQuote(e.Root) +
				" || exit; set -a; . " + shellQuote(e.sourcePath(en)) + "; set +a; cd " +
				shellQuote(r.dir) + ` || exit; exec "$@"`, "sh"}, r.argv...)
			header += ": " + en.source
		}
		header += "\n"
	}
	if w := e.diskWarning(r.warnDir); w != "" {
		header += "# " + w + "\n"
	}
	out.Replace(0, out.Text.Len(), []byte(header), text.Origin{})
	e.runs.header = len(header)
	out.undo, out.redo = nil, nil
	if e.Buf == out {
		e.setHead(out.Text.LineStart(out.Text.LineCount() - 1))
		e.Top, e.TopSub = 0, 0
	} else {
		e.runs.panel = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.runs.last, e.runs.going, e.runs.cancel, e.runs.start = r, r, cancel, e.now()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = r.dir
	if environ != nil {
		cmd.Env = append(os.Environ(), environ...)
	}
	if r.stdin != "" {
		cmd.Stdin = strings.NewReader(r.stdin)
	}
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
	s := strings.ReplaceAll(string(p), "\r", "") // CRLF from HTTP headers; progress bars run together
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
	if r.share {
		e.report(r, status, took, string(out.Text.Slice(min(e.runs.header, out.Text.Len()), out.Text.Len())))
	}
	e.appendOut(fmt.Sprintf("[%s, %s]\n", status, took))
	if !e.runs.panel && e.Buf != out { // otherwise the line above is on screen
		e.Msg = fmt.Sprintf("%s: %s (%s)", e.label(r), status, took)
		if w := e.diskWarning(r.warnDir); w != "" {
			e.Msg += "; " + w
		}
	}
}

// report keeps a shared run's result, with the last lines of its output, as
// the latest for agents.
func (e *Editor) report(r *run, status string, took time.Duration, output string) {
	lines := strings.SplitAfter(output, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	tail := strings.Join(lines[max(len(lines)-reportLines, 0):], "")
	seq := 1
	if p := e.runs.report; p != nil {
		seq = p.Seq + 1
	}
	e.runs.report = &RunReport{Seq: seq, Title: r.title, Env: r.envName, Command: r.show, Status: status, Took: took, Tail: tail}
}

// TakeRun returns the latest shared run if session hasn't been given it yet.
func (e *Editor) TakeRun(session string) *RunReport {
	r := e.runs.report
	if r == nil || e.runs.told[session] >= r.Seq {
		return nil
	}
	if e.runs.told == nil {
		e.runs.told = map[string]int{}
	}
	e.runs.told[session] = r.Seq
	return r
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
		return "running " + e.label(g) + " (C-c)"
	}
	return ""
}

// openOutput shows *out* in place of the panel, at its end.
func (e *Editor) openOutput() {
	out := e.runs.out
	if out == nil {
		e.Msg = "nothing run yet"
		return
	}
	e.runs.panel = false
	if e.Buf != out {
		e.pushJump()
		e.SwitchTo(e.outBuffer())
	}
	e.setHead(out.Text.LineStart(out.Text.LineCount() - 1))
}

// minText is the fewest text rows the panel leaves.
const minText = 3

// PanelRows returns the rows of the output panel, its title included, or 0
// when it's hidden or *out* is on screen.
func (e *Editor) PanelRows() int {
	if !e.runs.panel || e.Buf == e.runs.out {
		return 0
	}
	rows := min(max((e.screen-2)/3, 4), e.screen-2-minText)
	if rows < 2 {
		return 0
	}
	return rows
}

// OutTail returns *out*'s last n lines.
func (e *Editor) OutTail(n int) []string {
	if e.runs.out == nil || n <= 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(e.runs.out.Text.String(), "\n"), "\n")
	return lines[max(len(lines)-n, 0):]
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
	if dir == "" {
		return ""
	}
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
