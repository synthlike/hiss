//go:build shots

package ui

// The README's screenshots: each scene drives a real editor over a small
// fixture project and draws it, and the cells are written out as an HTML
// terminal window that docs/screenshots/shoot.sh turns into PNGs.
//
//	go test -tags shots -run TestShots ./internal/ui -args -out DIR -theme dark

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/synthlike/hiss/internal/core"
)

var (
	shotsOut   = flag.String("out", "", "directory for the HTML scenes")
	shotsTheme = flag.String("theme", "dark", "dark or light")
)

const shotW, shotH = 104, 32

// palette is a terminal's colours: its default foreground and background,
// then the 16 ANSI colours.
type palette struct {
	fg, bg, bar string
	ansi        [16]string
}

var palettes = map[string]palette{
	"dark": {fg: "#d5d8de", bg: "#1e2128", bar: "#2a2d35", ansi: [16]string{
		"#3b4048", "#be5046", "#98c379", "#e5c07b", "#2c4a7a", "#c678dd", "#56b6c2", "#abb2bf",
		"#7f848e", "#e06c75", "#b5e890", "#e5c07b", "#61afef", "#d58ce8", "#7fd8e0", "#ffffff"}},
	"light": {fg: "#24292f", bg: "#fafaf8", bar: "#ebebea", ansi: [16]string{
		"#24292f", "#a1260d", "#1a7f37", "#8a6100", "#0b3d91", "#8250df", "#0e7c86", "#6e7781",
		"#8c959f", "#cf222e", "#2da44e", "#9a6700", "#0550ae", "#bf3989", "#1b7c83", "#ffffff"}},
}

const greetGo = `package server

import (
	"net/http"
)

// greet welcomes a visitor by name.
func greet(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "world"
	}
	w.Write([]byte("Hello, " + name + "!"))
}

// Routes maps paths to handlers.
func Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", greet)
	return mux
}
`

const greetTestGo = `package server

import (
	"net/http/httptest"
	"testing"
)

func TestGreet(t *testing.T) {
	rec := httptest.NewRecorder()
	Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/hello?name=Ada", nil))
	if got := rec.Body.String(); got != "Hello, Ada!" {
		t.Errorf("body = %q", got)
	}
}

func TestGreetDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/hello", nil))
	if got := rec.Body.String(); got != "Hello, world!" {
		t.Errorf("body = %q", got)
	}
}
`

const mainGo = `package main

import (
	"log"
	"net/http"

	"example.com/greet/server"
)

func main() {
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", server.Routes()))
}
`

const readme = "# greet\n\nA tiny HTTP service that says hello.\n\n    curl localhost:8080/hello?name=Ada\n\nIt answers in plain text.\n"

// scene is one screenshot: set up the editor, then it's drawn.
type scene struct {
	name  string
	setup func(t *testing.T, s *studio)
}

// studio is a fixture project and the editor open on it.
type studio struct {
	root string
	ed   *core.Editor
}

func (s *studio) path(rel string) string { return filepath.Join(s.root, rel) }

func (s *studio) keys(ks ...string) {
	for _, k := range ks {
		s.ed.HandleKey(k)
	}
}

// typeText types s, as into the command line or a picker.
func (s *studio) typeText(text string) {
	for _, r := range text {
		k := string(r)
		if r == ' ' {
			k = "space"
		}
		s.ed.HandleKey(k)
	}
}

func (s *studio) open(t *testing.T, rel string) {
	t.Helper()
	b, err := s.ed.OpenBuffer(s.path(rel))
	if err != nil {
		t.Fatal(err)
	}
	s.ed.SwitchTo(b)
}

func (s *studio) propose(t *testing.T, rel, old, new, why string) *core.Proposal {
	t.Helper()
	ps, err := s.ed.ProposeEdit(core.HookAgent, s.path(rel), old, new, false, nil, why)
	if err != nil {
		t.Fatal(err)
	}
	return ps[0]
}

// turn starts an agent turn whose tool calls follow.
func (s *studio) turn() {
	s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__propose_edit"})
}

// jsonChange proposes switching the greeting to JSON: three hunks in two
// files, one change set.
func (s *studio) jsonChange(t *testing.T) (imp, body, test *core.Proposal) {
	s.turn()
	imp = s.propose(t, "server/hello.go", "\t\"net/http\"\n", "\t\"encoding/json\"\n\t\"net/http\"\n",
		"Import encoding/json for the response")
	body = s.propose(t, "server/hello.go", "\tw.Write([]byte(\"Hello, \" + name + \"!\"))\n",
		"\tw.Header().Set(\"Content-Type\", \"application/json\")\n"+
			"\tjson.NewEncoder(w).Encode(map[string]string{\n"+
			"\t\t\"message\": \"Hello, \" + name + \"!\",\n"+
			"\t})\n",
		"Answer in JSON, so clients can parse the greeting")
	test = s.propose(t, "server/hello_test.go", "got != \"Hello, Ada!\"",
		"got != `{\"message\":\"Hello, Ada!\"}`+\"\\n\"", "Expect the JSON body")
	return imp, body, test
}

var scenes = []scene{
	{"review", func(t *testing.T, s *studio) {
		s.open(t, "server/hello.go")
		s.jsonChange(t)
		s.keys("g", "g", "]", "h", "+", "esc") // accept the import; the cursor moves on to the body
		s.keys("#")
		s.typeText("Should errors be JSON too?")
		s.keys("enter", "esc")
		if err := s.ed.Reply(core.HookAgent, 2, "Yes: I'll add a small helper so every response has the same shape."); err != nil {
			t.Fatal(err)
		}
		s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__await_review"})
		s.ed.MarkAwaiting()
	}},
	{"code-tour", func(t *testing.T, s *studio) {
		s.open(t, "server/hello.go")
		s.turn()
		err := s.ed.ShowTrail(core.HookAgent, "How a request becomes a response", []core.StepInput{
			{Path: s.path("server/hello.go"), Line: 19, Note: "The router matches GET /hello and calls greet."},
			{Path: s.path("server/hello.go"), Line: 9, Note: "Read the visitor's name; fall back to a friendly default."},
			{Path: s.path("server/hello.go"), Line: 13, Note: "Write the greeting as plain text."},
		})
		if err != nil {
			t.Fatal(err)
		}
		s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "Stop"})
		s.keys("]", "s", "]", "s", "]", "s", "#")
		s.typeText("Could this be JSON?")
		s.keys("enter")
		if err := s.ed.ReplyStep(core.HookAgent, 3, "Yes, with json.NewEncoder(w). Want me to propose it?"); err != nil {
			t.Fatal(err)
		}
		s.keys("[", "s", "]", "s")
	}},
	{"fuzzy-finder", func(t *testing.T, s *studio) {
		s.open(t, "server/hello.go")
		s.jsonChange(t)
		s.propose(t, "README.md", "It answers in plain text.\n", "It answers in JSON:\n\n    {\"message\":\"Hello, Ada!\"}\n",
			"Document the JSON response")
		s.propose(t, "cmd/greet/main.go", "server.Routes()", "server.Routes(server.WithLogging())",
			"Log each request")
		s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "Stop"})
		s.keys("space", "h")
		s.typeText("json")
	}},
	{"ask", func(t *testing.T, s *studio) {
		s.open(t, "server/hello.go")
		s.jsonChange(t)
		s.keys("g", "g", "]", "h")
		s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__ask"})
		s.ed.Ask(core.HookAgent, "Old clients read the plain-text greeting. Keep it for them?",
			[]string{"Yes, under GET /hello.txt", "Yes, when Accept is text/plain", "No, JSON only"})
		s.keys("down")
	}},
	{"run", func(t *testing.T, s *studio) {
		s.open(t, "server/hello_test.go")
		_, _, test := s.jsonChange(t)
		s.ed.Hook(core.HookEvent{SessionID: "s1", Event: "Stop"})
		s.ed.SwitchTo(test.Buf)
		s.keys("g", "g", "]", "h", "+", "esc") // accept the test; the handler is still pending
		s.open(t, "server/hello_test.go")
		s.keys("g", "g", "j", "j", "j", "j", "j", "j", "j", "j", "j")
		ch := make(chan func(*core.Editor), 64)
		s.ed.Post = func(f func(*core.Editor)) { ch <- f }
		s.keys("enter")
		timeout := time.After(60 * time.Second)
		for s.ed.Running() != "" {
			select {
			case f := <-ch:
				f(s.ed)
			case <-timeout:
				t.Fatal("run timed out")
			}
		}
	}},
}

func TestShots(t *testing.T) {
	if *shotsOut == "" {
		t.Skip("no -out")
	}
	pal, ok := palettes[*shotsTheme]
	if !ok {
		t.Fatalf("unknown theme %q", *shotsTheme)
	}
	if *shotsTheme == "light" {
		UseLightTheme()
	}
	for _, sc := range scenes {
		t.Run(sc.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, content := range map[string]string{
				"go.mod":               "module example.com/greet\n\ngo 1.27\n",
				"server/hello.go":      greetGo,
				"server/hello_test.go": greetTestGo,
				"cmd/greet/main.go":    mainGo,
				"README.md":            readme,
			} {
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			ed := core.New(core.Scratch())
			ed.Root = root
			ed.Resize(shotW, shotH)
			s := &studio{root: root, ed: ed}
			sc.setup(t, s)
			ed.Toast = core.Toast{}
			g := newGrid(shotW, shotH)
			Draw(g, ed)
			title := "hiss — " + ed.Buf.Name
			file := filepath.Join(*shotsOut, fmt.Sprintf("%s-%s.html", sc.name, *shotsTheme))
			if err := os.WriteFile(file, []byte(windowHTML(g, pal, title)), 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (p palette) colour(c color.Color, def string) string {
	switch {
	case c == color.Default:
		return def
	case c >= color.XTerm0 && c <= color.XTerm15:
		return p.ansi[c-color.XTerm0]
	}
	return fmt.Sprintf("#%06x", c.Hex())
}

// css renders a cell's style.
func (p palette) css(st tcell.Style) string {
	fg, bg := p.colour(st.GetForeground(), p.fg), p.colour(st.GetBackground(), "")
	if st.HasReverse() {
		if bg == "" {
			bg = p.bg
		}
		fg, bg = bg, fg
	}
	var b strings.Builder
	if st.HasDim() {
		fmt.Fprintf(&b, "color:color-mix(in srgb,%s 55%%,%s);", fg, p.bg)
	} else {
		fmt.Fprintf(&b, "color:%s;", fg)
	}
	if bg != "" {
		fmt.Fprintf(&b, "background:%s;", bg)
	}
	if st.HasBold() {
		b.WriteString("font-weight:700;")
	}
	if st.HasItalic() {
		b.WriteString("font-style:italic;")
	}
	if u := st.GetUnderlineStyle(); u != tcell.UnderlineStyleNone {
		kind := "solid"
		switch u {
		case tcell.UnderlineStyleCurly:
			kind = "wavy"
		case tcell.UnderlineStyleDotted:
			kind = "dotted"
		}
		fmt.Fprintf(&b, "text-decoration:underline %s %s;", kind, p.colour(st.GetUnderlineColor(), fg))
	}
	return b.String()
}

func windowHTML(g *grid, p palette, title string) string {
	var rows strings.Builder
	for y := range g.h {
		rows.WriteString(`<div class="r">`)
		for x := 0; x < g.w; {
			css := p.css(g.styles[y][x])
			end := x
			for end < g.w && p.css(g.styles[y][end]) == css {
				end++
			}
			fmt.Fprintf(&rows, `<span style="%s">%s</span>`, css, html.EscapeString(string(g.cells[y][x:end])))
			x = end
		}
		rows.WriteString("</div>\n")
	}
	return fmt.Sprintf(`<!doctype html>
<meta charset="utf-8">
<style>
:root { --cw: 9.6px; --lh: 22px; }
html, body { margin: 0; background: %[2]s; }
.win { width: calc(%[4]d * var(--cw) + 40px); background: %[2]s; }
.bar { height: 40px; background: %[3]s; position: relative; font: 500 13px -apple-system, "Helvetica Neue", sans-serif; color: color-mix(in srgb, %[1]s 70%%, %[3]s); display: flex; align-items: center; justify-content: center; }
.dots { position: absolute; left: 16px; top: 14px; display: flex; gap: 8px; }
.dots i { width: 12px; height: 12px; border-radius: 50%%; display: block; }
.term { padding: 14px 20px 16px; font: 16px Menlo, monospace; }
.r { height: var(--lh); white-space: pre; }
.r span { display: inline-block; height: var(--lh); line-height: var(--lh); vertical-align: top; }
</style>
<div class="win">
<div class="bar"><div class="dots"><i style="background:#ff5f57"></i><i style="background:#febc2e"></i><i style="background:#28c840"></i></div>%[5]s</div>
<div class="term">
%[6]s</div>
</div>
`, p.fg, p.bg, p.bar, g.w, html.EscapeString(title), rows.String())
}
