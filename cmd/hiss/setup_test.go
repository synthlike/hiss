package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetupWriteMerges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := `{
  "model": "opus",
  "permissions": {"allow": ["Bash(go test:*)", "mcp__hiss"]},
  "hooks": {"Stop": [
    {"hooks": [{"type": "command", "command": "say done && true"}]},
    {"hooks": [{"type": "command", "command": "/old/place/hiss hook"}]}
  ]}
}`
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runSetupWrite(root, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	var s struct {
		Model       string
		Permissions struct{ Allow, Deny []string }
		Hooks       map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	if err := json.Unmarshal(got, &s); err != nil {
		t.Fatal(err)
	}
	if s.Model != "opus" {
		t.Errorf("other settings lost: %s", got)
	}
	if !slices.Equal(s.Permissions.Allow, []string{"Bash(go test:*)", "mcp__hiss"}) || len(s.Permissions.Deny) != 3 {
		t.Errorf("permissions = %+v", s.Permissions)
	}
	stop := s.Hooks["Stop"]
	if len(stop) != 2 || stop[0].Hooks[0].Command != "say done && true" ||
		!strings.HasSuffix(stop[1].Hooks[1].Command, " hook watch") {
		t.Errorf("Stop = %+v: want the user's hook kept, the old hiss one replaced", stop)
	}
	if !strings.Contains(string(got), "say done && true") {
		t.Error("& in the user's command was escaped")
	}
	if len(s.Hooks) != 7 {
		t.Errorf("%d hook events", len(s.Hooks))
	}

	if err := runSetupWrite(root, io.Discard); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != string(got) {
		t.Errorf("a second run changed the file:\n%s", again)
	}
}

func TestSetupWriteCreatesAndRefusesBadJSON(t *testing.T) {
	root := t.TempDir()
	if err := runSetupWrite(root, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".claude", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings not created: %v", err)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runSetupWrite(root, io.Discard); err == nil {
		t.Error("invalid JSON should be an error")
	}
	if got, _ := os.ReadFile(path); string(got) != "{oops" {
		t.Errorf("invalid file was rewritten: %q", got)
	}
}
