package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFuzzy(t *testing.T) {
	cases := []struct {
		q, s string
		ok   bool
	}{
		{"", "anything", true},
		{"edg", "editor.go", true},
		{"EDG", "editor.go", true},
		{"gde", "editor.go", false},
	}
	for _, c := range cases {
		if _, ok := fuzzy(c.q, c.s); ok != c.ok {
			t.Errorf("fuzzy(%q, %q) ok = %v", c.q, c.s, ok)
		}
	}
	tight, _ := fuzzy("go", "go.mod")
	loose, _ := fuzzy("go", "a_g_x_o")
	if tight >= loose {
		t.Errorf("adjacent early match should score better: %d vs %d", tight, loose)
	}
}

func TestHunkPicker(t *testing.T) {
	e := newEditor(t, "alpha\nbeta\n")
	e.Root = filepath.Dir(e.Buf.Path)
	first := propose(t, e, "beta", "BETA")
	if _, err := e.WriteFile("claude", filepath.Join(e.Root, "other.go"), "package other\n", nil, "new package"); err != nil {
		t.Fatal(err)
	}

	keys(e, "space h")
	if e.Picker == nil || len(e.Picker.Items) != 2 {
		t.Fatalf("picker = %+v", e.Picker)
	}
	keys(e, "n e w")
	if ms := e.Picker.Matches(); len(ms) != 1 || !strings.Contains(ms[0].Detail, "new package") {
		t.Fatalf("matches for %q = %+v", e.Picker.Query, ms)
	}
	keys(e, "enter")
	if e.Picker != nil || e.Buf.Name != "other.go" {
		t.Errorf("after enter: picker=%v buf=%q", e.Picker, e.Buf.Name)
	}

	keys(e, "space h down down enter") // wraps around to the first item
	if l, _ := e.Cursor(); !strings.HasSuffix(e.Buf.Name, "f.txt") || l != 1 {
		t.Errorf("jumped to %s line %d", e.Buf.Name, l)
	}

	keys(e, "space h esc")
	if e.Picker != nil {
		t.Error("esc should close the picker")
	}

	keys(e, "space h")
	first.Status = Rejected // reviewed while the picker was open
	keys(e, "enter")
	if !strings.Contains(e.Msg, "is rejected") {
		t.Errorf("stale item: msg = %q", e.Msg)
	}
}

func TestHunkPickerEmpty(t *testing.T) {
	e := newEditor(t, "x\n")
	keys(e, "space h")
	if e.Picker != nil || e.Msg != "no pending hunks" {
		t.Errorf("picker=%v msg=%q", e.Picker, e.Msg)
	}
}
