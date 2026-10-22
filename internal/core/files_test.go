package core

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFilePicker(t *testing.T) {
	e := newEditor(t, "x\n") // f.txt in a temp dir, which is not a git repository
	e.Root = filepath.Dir(e.Buf.Path)
	for _, f := range []string{"a.go", "sub/b.go", ".hidden/c.go"} {
		p := filepath.Join(e.Root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keys(e, "space f")
	if e.Picker == nil {
		t.Fatalf("no picker: %q", e.Msg)
	}
	var labels []string
	for _, it := range e.Picker.Items {
		labels = append(labels, it.Label)
	}
	slices.Sort(labels)
	if want := []string{"a.go", "f.txt", filepath.Join("sub", "b.go")}; !slices.Equal(labels, want) {
		t.Errorf("files = %v, want %v", labels, want)
	}
	keys(e, "s u b enter")
	if e.Buf.Name != filepath.Join("sub", "b.go") || e.Buf.Text.String() != "package x\n" {
		t.Errorf("opened %q: %q", e.Buf.Name, e.Buf.Text.String())
	}
}

func TestBufferPicker(t *testing.T) {
	e := newEditor(t, "x\n")
	e.Root = filepath.Dir(e.Buf.Path)
	first := e.Buf
	if _, err := e.WriteFile("claude", filepath.Join(e.Root, "new.go"), "package n\n", nil, ""); err != nil {
		t.Fatal(err)
	}
	keys(e, "space b")
	if e.Picker == nil || len(e.Picker.Items) != 2 || e.Picker.Items[1].Detail != "1 pending" {
		t.Fatalf("picker = %+v", e.Picker)
	}
	keys(e, "n e w enter")
	if e.Buf.Name != "new.go" {
		t.Errorf("switched to %q", e.Buf.Name)
	}
	keys(e, "space b enter") // first item: the original buffer
	if e.Buf != first {
		t.Errorf("switched to %q", e.Buf.Name)
	}
}

func TestPickerCachesMatches(t *testing.T) {
	p := &Picker{Items: []PickItem{{Label: "a"}, {Label: "b"}}}
	if len(p.Matches()) != 2 {
		t.Fatal("want all items for an empty query")
	}
	p.Query = "b"
	if ms := p.Matches(); len(ms) != 1 || ms[0].Label != "b" {
		t.Errorf("matches for b = %+v", ms)
	}
}
