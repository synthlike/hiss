package tools

import (
	"strings"
	"testing"
)

func TestRunTextNamesEnv(t *testing.T) {
	got := RunText(&RunInfo{Title: "sql", Env: "staging", Command: "psql", Status: "ok", Took: "0.1s"})
	if want := "The user ran sql on staging in hiss: ok (0.1s).\n"; !strings.HasPrefix(got, want) {
		t.Errorf("got %q, want prefix %q", got, want)
	}
}
