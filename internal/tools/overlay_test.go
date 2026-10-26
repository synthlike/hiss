package tools

import (
	"strings"
	"testing"
)

func TestGoOverlay(t *testing.T) {
	cs, _ := setup(t, "package main\n")
	res := call(t, cs, "go_overlay", map[string]any{})
	if got := text(res); !strings.HasPrefix(got, "Nothing is pending or unsaved") {
		t.Errorf("before proposing: %q", got)
	}
	call(t, cs, "propose_edit", map[string]any{"path": "main.go", "old_string": "package main\n", "new_string": "package main\n\nfunc main() {}\n"})
	res = call(t, cs, "go_overlay", map[string]any{})
	out := structured[OverlayOut](t, res)
	if len(out.Files) != 1 || out.Files[0] != "main.go" || len(out.Proposals) != 1 {
		t.Errorf("overlay %+v", out)
	}
	if got := text(res); !strings.Contains(got, "proposal #1 applied, replacing main.go") || !strings.Contains(got, "go test -overlay="+out.Path) {
		t.Errorf("text %q", got)
	}
}
