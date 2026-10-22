package core

import (
	"fmt"
	"slices"
	"strings"
)

// command is a command for the : line.
type command struct {
	names []string // the first is the usual one; the rest are aliases
	args  string   // argument summary for hints: "<x>" required, "[x]" optional
	desc  string
	run   func(e *Editor, arg string)
}

var commands = []command{
	{[]string{"w", "write"}, "[path]", "write the buffer, or save it as path", func(e *Editor, arg string) {
		if arg == "" {
			e.save()
		} else {
			e.saveAs(arg)
		}
	}},
	{[]string{"e", "edit"}, "<path>", "open a file", (*Editor).openFile},
	{[]string{"q", "quit"}, "", "quit", func(e *Editor, _ string) { e.quit() }},
	{[]string{"q!", "quit!"}, "", "quit, discarding unsaved changes", func(e *Editor, _ string) { e.Quit = true }},
	{[]string{"wq", "write-quit"}, "", "write and quit", func(e *Editor, _ string) {
		if e.save() {
			e.Quit = true
		}
	}},
	{[]string{"ha", "hunk-accept"}, "", "accept the hunk under the cursor", func(e *Editor, _ string) { e.reviewHunk(true, "") }},
	{[]string{"hr", "hunk-reject"}, "[reason]", "reject the hunk, telling the agent why", func(e *Editor, arg string) { e.reviewHunk(false, arg) }},
	{[]string{"haa", "hunk-accept-all"}, "", "accept every pending hunk", func(e *Editor, _ string) { e.reviewAll(true, "") }},
	{[]string{"hra", "hunk-reject-all"}, "[reason]", "reject every pending hunk", func(e *Editor, arg string) { e.reviewAll(false, arg) }},
	{[]string{"c", "comment"}, "<text>", "comment on the hunk, line or trail step", (*Editor).comment},
	{[]string{"tc", "trail-clear"}, "", "stop showing the trail", func(e *Editor, _ string) { e.clearTrail() }},
}

func findCommand(name string) *command {
	for i := range commands {
		if slices.Contains(commands[i].names, name) {
			return &commands[i]
		}
	}
	return nil
}

// matching returns the first of c's names that starts with prefix, or "".
func (c *command) matching(prefix string) string {
	for _, n := range c.names {
		if strings.HasPrefix(n, prefix) {
			return n
		}
	}
	return ""
}

func (e *Editor) execute(cmd string) {
	name, arg, _ := strings.Cut(strings.TrimSpace(cmd), " ")
	if name == "" {
		return
	}
	c := findCommand(name)
	arg = strings.TrimSpace(arg)
	switch {
	case c == nil:
		e.Msg = fmt.Sprintf("unknown command: %s", name)
	case arg == "" && strings.HasPrefix(c.args, "<"):
		e.Msg = fmt.Sprintf("usage: :%s %s", name, c.args)
	default:
		c.run(e, arg)
	}
}

// CommandHints lists the commands matching the command line: those starting
// with the name being typed, or the named command once its arguments start.
func (e *Editor) CommandHints() *Menu {
	name, _, inArgs := strings.Cut(strings.TrimLeft(e.Cmd, " "), " ")
	m := &Menu{Title: "commands"}
	for i := range commands {
		c := &commands[i]
		if inArgs && !slices.Contains(c.names, name) || !inArgs && c.matching(name) == "" {
			continue
		}
		key := strings.TrimSpace(strings.Join(c.names, " / ") + " " + c.args)
		m.Items = append(m.Items, MenuItem{Key: key, Desc: c.desc})
	}
	if len(m.Items) == 0 {
		return nil
	}
	return m
}

// completeCommand extends the command name being typed as far as the
// matching commands agree, adding a space once it is complete and takes
// arguments.
func (e *Editor) completeCommand() {
	if strings.Contains(e.Cmd, " ") {
		return
	}
	var names []string
	var last *command
	for i := range commands {
		if n := commands[i].matching(e.Cmd); n != "" {
			names, last = append(names, n), &commands[i]
		}
	}
	switch len(names) {
	case 0:
		return
	case 1:
		e.Cmd = names[0]
		if last.args != "" {
			e.Cmd += " "
		}
		return
	}
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	e.Cmd = common
}

func (e *Editor) quit() {
	for _, b := range e.Bufs {
		if b.Dirty() {
			e.Msg = fmt.Sprintf("unsaved changes in %s (:q! to discard)", b.Name)
			return
		}
	}
	e.Quit = true
}
