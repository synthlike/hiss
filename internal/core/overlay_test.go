package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWriteOverlay(t *testing.T) {
	e := newEditor(t, "package m\n\nfunc F() int { return 1 }\n")
	e.Root = filepath.Dir(e.Buf.Path)
	f := filepath.Join(e.Root, "f.go")
	if err := os.Rename(e.Buf.Path, f); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":    "module m\n\ngo 1.21\n",
		"f_test.go": "package m\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n\tif F() != 2 {\n\t\tt.Fatal(\"F isn't 2\")\n\t}\n}\n",
	}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(e.Root, name), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.openFile(f)
	if _, err := e.ProposeEdit("claude", f, "return 1", "return G()", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(e.Root, "g.go") // a new file
	if _, err := e.WriteFile("claude", g, "package m\n\nfunc G() int { return 2 }\n", nil, ""); err != nil {
		t.Fatal(err)
	}

	o, err := e.WriteOverlay()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.Files, []string{"f.go", "g.go"}) || !slices.Equal(o.Proposals, []int{1, 2}) {
		t.Errorf("overlay files %q, proposals %v", o.Files, o.Proposals)
	}
	data, err := os.ReadFile(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(cfg.Replace[f]); string(got) != "package m\n\nfunc F() int { return G() }\n" {
		t.Errorf("f.go in the overlay: %q", got)
	}
	if got, _ := os.ReadFile(f); !strings.Contains(string(got), "return 1") {
		t.Errorf("f.go on disk changed: %q", got)
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	test := func(args ...string) error {
		cmd := exec.Command("go", append([]string{"test"}, args...)...)
		cmd.Dir = e.Root
		return cmd.Run()
	}
	if err := test("./..."); err == nil {
		t.Error("go test passed on disk, where F is still 1")
	}
	if err := test("-overlay="+o.Path, "./..."); err != nil {
		t.Errorf("go test with the overlay: %v", err)
	}

	keys(e, ": h r a enter") // nothing pending: the overlay is empty again
	if o, err := e.WriteOverlay(); err != nil || len(o.Files) != 0 {
		t.Errorf("after rejecting all: files %q, err %v", o.Files, err)
	}
	if _, err := os.Stat(filepath.Dir(cfg.Replace[g])); err == nil {
		t.Error("the old copies are still there")
	}
}
