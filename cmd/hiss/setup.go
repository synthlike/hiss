package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// setupSettings returns the project settings for Claude Code: hooks that
// report agent state to hiss, permission for hiss's tools, and a ban on direct
// file edits. exe is how the hooks run hiss.
func setupSettings() (settings map[string]any, exe string, err error) {
	if exe, err = os.Executable(); err != nil {
		return nil, "", err
	}
	if strings.ContainsAny(exe, " '\"\\$") {
		exe = "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	command := exe + " hook"
	handler := func(async bool) []any {
		h := map[string]any{"type": "command", "command": command, "timeout": 5}
		if async {
			h["async"] = true
		}
		return []any{h}
	}
	group := func(matcher string, async bool) []any {
		g := map[string]any{"hooks": handler(async)}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return []any{g}
	}
	settings = map[string]any{
		"permissions": map[string]any{
			"allow": []any{"mcp__hiss"},
			"deny":  []any{"Edit", "Write", "NotebookEdit"},
		},
		"hooks": map[string]any{
			"SessionStart": group("", true),
			// Synchronous so new comments reach Claude with the prompt and right
			// after each tool call, not a turn later.
			"UserPromptSubmit": group("", false),
			// Synchronous, so hiss knows which session is calling before the call arrives.
			"PreToolUse":   group("mcp__hiss__.*", false),
			"PostToolUse":  group("", false),
			"Notification": group("", true),
			// The second Stop handler waits in the background for comments and
			// wakes Claude to answer them (exit 2 with asyncRewake).
			"Stop": []any{map[string]any{"hooks": append(handler(true), map[string]any{
				"type": "command", "command": exe + " hook watch", "asyncRewake": true, "timeout": 3600,
			})}},
			"SessionEnd": group("", true),
		},
	}
	return settings, exe, nil
}

// runSetup prints the settings for the user to merge.
func runSetup(w io.Writer) error {
	settings, exe, err := setupSettings()
	if err != nil {
		return err
	}
	out, err := encode(settings)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "# Merge into .claude/settings.json in your project (or run hiss setup --write),\n"+
		"# and register the MCP bridge:\n#   claude mcp add --scope user hiss -- %s mcp\n", exe)
	_, err = w.Write(out)
	return err
}

// runSetupWrite merges the settings into root/.claude/settings.json.
func runSetupWrite(root string, log io.Writer) error {
	settings, exe, err := setupSettings()
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".claude", "settings.json")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	cur := map[string]any{}
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &cur); err != nil {
			return fmt.Errorf("%s isn't valid JSON, so it was left alone: %w", path, err)
		}
	}
	if err := mergeSettings(cur, settings); err != nil {
		return fmt.Errorf("%s was left alone: %w", path, err)
	}
	out, err := encode(cur)
	if err != nil {
		return err
	}
	if bytes.Equal(out, old) {
		fmt.Fprintf(log, "%s is already set up for hiss\n", path)
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(log, "updated %s\n", path)
	}
	fmt.Fprintf(log, "register the MCP bridge once, if you haven't:\n  claude mcp add --scope user hiss -- %s mcp\n", exe)
	return nil
}

// encode formats settings as indented JSON, leaving & < > in commands as typed.
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return b.Bytes(), err
}

// hissHook matches the command of a hook that runs hiss, wherever it's
// installed and whatever the binary's suffix (hiss-dev, hiss.test).
var hissHook = regexp.MustCompile(`(^|/)hiss[^/ ']*'? hook( watch)?$`)

// mergeSettings adds hiss's permissions and hooks to cur, keeping everything
// else. hiss's earlier hooks are replaced, so running it again, or after
// moving the binary, leaves one copy.
func mergeSettings(cur, hiss map[string]any) error {
	perms, err := object(cur, "permissions")
	if err != nil {
		return err
	}
	for _, key := range []string{"allow", "deny"} {
		list, _ := perms[key].([]any)
		for _, p := range hiss["permissions"].(map[string]any)[key].([]any) {
			if !slices.Contains(list, p) {
				list = append(list, p)
			}
		}
		perms[key] = list
	}
	hooks, err := object(cur, "hooks")
	if err != nil {
		return err
	}
	for event, groups := range hiss["hooks"].(map[string]any) {
		existing, _ := hooks[event].([]any)
		kept := slices.DeleteFunc(slices.Clone(existing), isHissGroup)
		hooks[event] = append(kept, groups.([]any)...)
	}
	return nil
}

// object returns m[key] as a JSON object, creating it if missing.
func object(m map[string]any, key string) (map[string]any, error) {
	switch v := m[key].(type) {
	case nil:
		o := map[string]any{}
		m[key] = o
		return o, nil
	case map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("%q isn't an object", key)
}

// isHissGroup reports whether a hook group runs hiss.
func isHissGroup(g any) bool {
	m, _ := g.(map[string]any)
	hs, _ := m["hooks"].([]any)
	return slices.ContainsFunc(hs, func(h any) bool {
		m, _ := h.(map[string]any)
		cmd, _ := m["command"].(string)
		return hissHook.MatchString(cmd)
	})
}
