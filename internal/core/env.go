package core

import (
	"fmt"
	"path/filepath"
)

// Environments are the [env] sections of the rules file. Runs source the
// active one's script first, unless their rule pins another; :env switches.
// The first is active at start, so a restart never leaves runs on staging.

// loadRunConfig reads the rules file, keeping its envs' names for the status
// line.
func (e *Editor) loadRunConfig() (*runConfig, error) {
	c, err := loadRules(e.Root)
	if err != nil {
		return nil, err
	}
	e.runs.envs, e.runs.loaded = c.envs, true
	return c, nil
}

// runEnv returns the env a run uses: pin, or the active one; nil when the
// rules file has none.
func (e *Editor) runEnv(c *runConfig, pin string) (*env, error) {
	name := pin
	if name == "" {
		name = e.runs.env
	}
	switch {
	case len(c.envs) == 0:
		return nil, nil
	case name == "":
		return &c.envs[0], nil
	}
	if en := c.env(name); en != nil {
		return en, nil
	}
	return nil, fmt.Errorf("no [env] is named %s any more (:env to pick one)", name)
}

// sourcePath returns en's script as an absolute path.
func (e *Editor) sourcePath(en *env) string {
	if filepath.IsAbs(en.source) {
		return en.source
	}
	return filepath.Join(e.Root, en.source)
}

// label names r for messages, with its env.
func (e *Editor) label(r *run) string {
	if r.envName == "" {
		return r.title
	}
	return r.title + " on " + r.envName
}

// EnvName returns the active env for the status line, and whether runs
// there ask first; "" with none.
func (e *Editor) EnvName() (string, bool) {
	if !e.runs.loaded {
		e.loadRunConfig() // a bad rules file shows when something runs
	}
	if len(e.runs.envs) == 0 {
		return "", false
	}
	for _, en := range e.runs.envs {
		if en.name == e.runs.env || e.runs.env == "" {
			return en.name, en.confirm
		}
	}
	return e.runs.env, false // gone from the rules file: the next run says so
}

// switchEnv makes name the active env; with no name, it lists them.
func (e *Editor) switchEnv(name string) {
	c, err := e.loadRunConfig()
	if err != nil {
		e.Msg = err.Error()
		return
	}
	if len(c.envs) == 0 {
		e.Msg = "no [env] in " + rulesFile
		return
	}
	if name == "" {
		p := &Picker{Title: "environments"}
		active, _ := e.EnvName()
		for _, en := range c.envs {
			detail := en.source
			if en.name == active {
				detail = "active  " + detail
			}
			p.Items = append(p.Items, PickItem{Label: en.name, Detail: detail,
				Run: func(e *Editor) { e.switchEnv(en.name) }})
		}
		e.Picker = p
		return
	}
	if c.env(name) == nil {
		e.Msg = "no [env] is named " + name
		return
	}
	e.runs.env = name
	e.Msg = "runs use " + name
}

// rulesChanged notes that path was written, so the status line rereads the
// envs if it's the rules file.
func (e *Editor) rulesChanged(path string) {
	if e.Root != "" && path == filepath.Join(e.Root, rulesFile) {
		e.runs.loaded = false
	}
}
