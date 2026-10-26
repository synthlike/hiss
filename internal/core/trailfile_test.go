package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synthlike/hiss/internal/text"
)

func TestTrailSaveLoad(t *testing.T) {
	e := newEditor(t, "a\nb\nc\n")
	e.Root = filepath.Dir(e.Buf.Path)
	if err := e.ShowTrail("claude", "tour", []StepInput{
		{Path: e.Buf.Path, Line: 2, Note: "b"},
		{Path: e.Buf.Path, Line: 3, Note: "c"},
	}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "trail.json")
	e.execute("trail-save " + file)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (msg %q)", err, e.Msg)
	}
	if s := string(data); !strings.Contains(s, `"path": "f.txt"`) || !strings.Contains(s, `"text": "b"`) {
		t.Errorf("saved:\n%s", s)
	}

	e.execute("trail-clear")
	e.Buf.Replace(4, 5, []byte("C"), text.Origin{}) // line 3: c -> C
	e.execute("trail-load " + file)
	tr := e.trail
	if tr == nil || len(tr.Steps) != 2 || tr.Title != "tour" || tr.Session != "" {
		t.Fatalf("trail = %+v (msg %q)", tr, e.Msg)
	}
	if tr.Steps[0].Changed || !tr.Steps[1].Changed || !strings.HasSuffix(e.Msg, "changed since saved: step 2") {
		t.Errorf("changed = %v %v, msg %q", tr.Steps[0].Changed, tr.Steps[1].Changed, e.Msg)
	}
	if rows := e.TrailRows()[2]; len(rows) != 1 || rows[0].Text != "2. c (changed since saved)" {
		t.Errorf("rows = %+v", rows)
	}

	e.execute("trail-load " + filepath.Join(t.TempDir(), "none.json"))
	if e.trail != tr {
		t.Errorf("a failed load replaced the trail")
	}
}

func TestTrailCentersStep(t *testing.T) {
	e := newEditor(t, strings.Repeat("x\n", 100))
	if err := e.ShowTrail("claude", "tour", []StepInput{
		{Path: e.Buf.Path, Line: 50, Note: "here"},
		{Path: e.Buf.Path, Line: 2, Note: "top"},
	}); err != nil {
		t.Fatal(err)
	}
	keys(e, "] s") // height 8: the step and its note take 2 rows, 3 lines above
	if e.Top != 46 || e.TopSub != 0 {
		t.Errorf("top = %d/%d, want 46/0", e.Top, e.TopSub)
	}
	keys(e, "] s")
	if e.Top != 0 {
		t.Errorf("near the start: top = %d, want 0", e.Top)
	}
}
