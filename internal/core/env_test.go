package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvs(t *testing.T) {
	c, err := parseRules(`[env]
name   = dev
source = env/dev.sh

[env]
name    = staging
source  = env/staging.sh
confirm = true

[go]
env   = dev
share = false

[sql]
match = *.sql
cmd   = psql
env   = staging
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.envs) != 2 || c.envs[0] != (env{"dev", "env/dev.sh", false}) || c.envs[1] != (env{"staging", "env/staging.sh", true}) {
		t.Errorf("envs %+v", c.envs)
	}
	if c.goEnv != "dev" || c.goShare {
		t.Errorf("go: env %q share %v", c.goEnv, c.goShare)
	}
	if len(c.rules) != 1 || c.rules[0].env != "staging" {
		t.Errorf("rules %+v", c.rules)
	}
	if c, _ := parseRules(""); !c.goShare {
		t.Error("go tests don't share by default")
	}
}

func TestParseEnvErrors(t *testing.T) {
	cases := map[string]string{
		"[env]\nsource = a.sh\n":                    ".hissrun:1: [env] has no name",
		"[env]\nname = a\n[env]\nname = a\n":        ".hissrun:3: a second [env] named a",
		"[env]\nname = a\nmatch = *.sql\n":          ".hissrun:3: unknown key \"match\" in [env] (name, source, confirm)",
		"[env]\nname = a\nconfirm = yes\n":          ".hissrun:3: confirm is true or false, not \"yes\"",
		"[go]\nmatch = *.go\n":                      ".hissrun:2: unknown key \"match\" in [go] (env, share)",
		"[go]\nenv = prod\n[env]\nname = dev\n":     ".hissrun:2: no [env] is named prod",
		"[a]\nmatch = *.sql\ncmd = x\nenv = prod\n": ".hissrun:4: no [env] is named prod",
	}
	for src, want := range cases {
		if _, err := parseRules(src); err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %s", src, err, want)
		}
	}
}

const envRules = `[env]
name   = dev
source = env/dev.sh

[env]
name    = staging
source  = env/staging.sh
confirm = true

[env]
name = shell

[greet]
match = *.txt
cmd   = echo "$env says ${GREETING:-nothing}"

[pinned]
match = *.pin
env   = dev
cmd   = echo "$env says $GREETING"
`

// envEditor is rulesEditor with envRules and their scripts.
func envEditor(t *testing.T, name string) (*Editor, chan func(*Editor)) {
	t.Helper()
	e, ch := rulesEditor(t, name, "x\n")
	os.MkdirAll(filepath.Join(e.Root, "env"), 0o755)
	for f, s := range map[string]string{
		rulesFile:        envRules,
		"env/dev.sh":     "GREETING=hi\n",
		"env/staging.sh": "GREETING=yo\nfalse\n", // a failing last line doesn't stop the run
	} {
		if err := os.WriteFile(filepath.Join(e.Root, f), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.rulesChanged(filepath.Join(e.Root, rulesFile))
	return e, ch
}

func TestRunInEnv(t *testing.T) {
	e, ch := envEditor(t, "a.txt")
	if got := e.EnvName(); got != "dev" {
		t.Errorf("active at start: %q", got)
	}
	if got := runOutput(t, e, ch, "enter"); got != "# env dev: env/dev.sh\ndev says hi" {
		t.Errorf("dev: %q", got)
	}

	e.execute("env shell")
	if e.EnvName() != "shell" || e.Msg != "runs use shell" {
		t.Errorf("switched: %q, msg %q", e.EnvName(), e.Msg)
	}
	if got := runOutput(t, e, ch, "enter"); got != "# env shell\nshell says nothing" {
		t.Errorf("no source: %q", got)
	}
	keys(e, "enter")
	if got := e.Running(); got != "running greet on shell (C-c)" {
		t.Errorf("running: %q", got)
	}
	pump(t, e, ch, finished(e))
	e.execute("env nope")
	if e.EnvName() != "shell" || e.Msg != "no [env] is named nope" {
		t.Errorf("unknown env: %q, msg %q", e.EnvName(), e.Msg)
	}
}

func TestRunPinnedEnv(t *testing.T) {
	e, ch := envEditor(t, "a.pin")
	e.execute("env shell")
	if got := runOutput(t, e, ch, "enter"); got != "# env dev: env/dev.sh\ndev says hi" {
		t.Errorf("pinned: %q", got)
	}
}

func TestRunConfirmsEnv(t *testing.T) {
	e, ch := envEditor(t, "a.txt")
	e.execute("env staging")
	keys(e, "enter")
	if e.Menu == nil || e.Menu.Title != "run greet on staging?" || e.runs.going != nil {
		t.Fatalf("no confirm: menu %v, running %v", e.Menu, e.runs.going != nil)
	}
	keys(e, "esc")
	if e.Menu != nil || e.runs.going != nil {
		t.Fatal("ran after esc")
	}
	keys(e, "enter")
	if got := runOutput(t, e, ch, "y"); got != "# env staging: env/staging.sh\nstaging says yo" {
		t.Errorf("staging: %q", got)
	}
	if got := e.Running(); got != "" {
		t.Errorf("still running %q", got)
	}
	e.execute("rerun") // asks again
	if e.Menu == nil || e.runs.going != nil {
		t.Error(":rerun on staging didn't ask")
	}
}

func TestRunEnvProblems(t *testing.T) {
	e, _ := envEditor(t, "a.txt")
	os.Remove(filepath.Join(e.Root, "env/dev.sh"))
	keys(e, "enter")
	if e.Msg != "env dev: sourcing env/dev.sh failed: no such file or directory" || e.runs.going != nil {
		t.Errorf("missing script: msg %q", e.Msg)
	}

	e.execute("env shell")
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[env]\nname = dev\n[greet]\nmatch = *.txt\ncmd = true\n"), 0o600)
	keys(e, "enter")
	if e.Msg != "no [env] is named shell any more (:env to pick one)" || e.runs.going != nil {
		t.Errorf("env gone: msg %q", e.Msg)
	}
}

func TestEnvList(t *testing.T) {
	e, _ := envEditor(t, "a.txt")
	e.execute("env")
	if e.Picker == nil {
		t.Fatalf("no picker: %q", e.Msg)
	}
	var got []string
	for _, it := range e.Picker.Items {
		got = append(got, it.Label+" "+it.Detail)
	}
	if want := []string{"dev active  env/dev.sh", "staging env/staging.sh", "shell "}; !equal(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
	keys(e, "s t a g enter")
	if e.EnvName() != "staging" {
		t.Errorf("picked %q", e.EnvName())
	}
}

func TestEnvNameFollowsRulesFile(t *testing.T) {
	e, _ := rulesEditor(t, "a.txt", "x\n")
	if got := e.EnvName(); got != "" {
		t.Errorf("no envs: %q", got)
	}
	e.openFile(filepath.Join(e.Root, rulesFile))
	e.Buf.Replace(0, 0, []byte("[env]\nname = local\n"), You)
	e.save()
	if got := e.EnvName(); got != "local" {
		t.Errorf("after saving %s: %q", rulesFile, got)
	}
}

func TestRunReportsEnv(t *testing.T) {
	e, ch := envEditor(t, "a.txt")
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte(envRules+"share = true\n"), 0o600) // on [pinned]
	os.WriteFile(filepath.Join(e.Root, "a.pin"), []byte("x\n"), 0o600)
	e.openFile(filepath.Join(e.Root, "a.pin"))
	runOutput(t, e, ch, "enter")
	if r := e.TakeRun("s1"); r == nil || r.Title != "pinned" || r.Env != "dev" {
		t.Errorf("report %+v", r)
	}
}

func TestGoTestInEnv(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	e, ch := runEditor(t)
	os.WriteFile(filepath.Join(e.Root, "env_test.go"), []byte(`package x

import (
	"os"
	"testing"
)

func TestEnv(t *testing.T) {
	t.Logf("db=%s env=%s", os.Getenv("DB"), os.Getenv("env"))
}
`), 0o600)
	os.MkdirAll(filepath.Join(e.Root, "env"), 0o755)
	os.WriteFile(filepath.Join(e.Root, "env/test.sh"), []byte("DB=test.db\n"), 0o600)
	os.WriteFile(filepath.Join(e.Root, rulesFile), []byte("[env]\nname = dev\n[env]\nname = test\nsource = env/test.sh\n[go]\nenv = test\nshare = false\n"), 0o600)
	e.openFile(filepath.Join(e.Root, "env_test.go"))
	gotoText(t, e, "t.Logf")
	keys(e, "enter")
	if e.runs.going == nil {
		t.Fatalf("not running: %q", e.Msg)
	}
	pump(t, e, ch, finished(e))
	if out := e.runs.out.Text.String(); !strings.Contains(out, "db=test.db env=test") {
		t.Errorf("output:\n%s", out)
	}
	if r := e.TakeRun("s1"); r != nil {
		t.Errorf("shared with share = false: %+v", r)
	}
}
