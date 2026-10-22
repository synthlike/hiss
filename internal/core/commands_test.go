package core

import "testing"

func hintKeys(e *Editor) []string {
	m := e.CommandHints()
	if m == nil {
		return nil
	}
	var ks []string
	for _, it := range m.Items {
		ks = append(ks, it.Key)
	}
	return ks
}

func TestCommandHints(t *testing.T) {
	e := newEditor(t, "a\n")
	cases := []struct {
		cmd  string
		want []string
	}{
		{"w", []string{"w / write [path]", "wq / write-quit"}},
		{"write-", []string{"wq / write-quit"}},
		{"hunk-r", []string{"hr / hunk-reject [reason]", "hra / hunk-reject-all [reason]"}},
		{"hr why", []string{"hr / hunk-reject [reason]"}},
		{"comment why", []string{"c / comment <text>"}},
		{"zz", nil},
	}
	for _, c := range cases {
		e.Cmd = c.cmd
		if got := hintKeys(e); !equal(got, c.want) {
			t.Errorf("%q: hints %q, want %q", c.cmd, got, c.want)
		}
	}
	e.Cmd = ""
	if got := hintKeys(e); len(got) != len(commands) {
		t.Errorf("empty line: %d hints, want all %d", len(got), len(commands))
	}
}

func TestCompleteCommand(t *testing.T) {
	e := newEditor(t, "a\n")
	cases := []struct{ typed, want string }{
		{"hunk-acc", "hunk-accept"}, // hunk-accept or hunk-accept-all
		{"co", "comment "},          // takes an argument
		{"trail", "trail-clear"},
		{"h", "h"},
		{"zz", "zz"},
	}
	for _, c := range cases {
		e.Mode, e.Cmd = Command, c.typed
		keys(e, "tab")
		if e.Cmd != c.want {
			t.Errorf("%q: completed to %q, want %q", c.typed, e.Cmd, c.want)
		}
	}
}

func TestCommandUsage(t *testing.T) {
	e := newEditor(t, "a\n")
	e.execute("e")
	if e.Msg != "usage: :e <path>" {
		t.Errorf("msg = %q", e.Msg)
	}
	e.execute("so")
	if e.Msg != "unknown command: so" {
		t.Errorf("msg = %q", e.Msg)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
