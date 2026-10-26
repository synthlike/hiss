package core

import (
	"fmt"
	"strings"
)

// An agent can propose a query, request or script with run set: accepting
// it saves the file and runs the block at the change, as Enter there would,
// in the env active then. The agent hears how the run went.

// CanRun returns why a proposal to the file at path couldn't run, or nil.
func (e *Editor) CanRun(path string) error {
	c, err := loadRules(e.Root)
	if err != nil {
		return err
	}
	if e.ruleFor(c, path) == nil && !isMarkdown(path) {
		return fmt.Errorf("no rule in %s matches %s, so it can't run; write it where one does, such as %s/",
			rulesFile, e.shortPath(path), savedRuns)
	}
	return nil
}

// ruleFor returns the rule for the file at path, or nil.
func (e *Editor) ruleFor(c *runConfig, path string) *rule {
	rel := path
	if r, ok := e.underRoot(path); ok {
		rel = r
	}
	return findRule(c.rules, rel)
}

// RunOnAccept makes p run once accepted, from the first line it changes.
func (e *Editor) RunOnAccept(p *Proposal) {
	old := string(p.Buf.Text.Slice(p.Start, p.End))
	n := 0
	for n < len(old) && n < len(p.New) && old[n] == p.New[n] {
		n++
	}
	p.Run, p.runAt = true, strings.LastIndexByte(p.New[:n], '\n')+1
}

// runAccepted runs p, which was just accepted, and returns what happened if
// it couldn't start.
func (e *Editor) runAccepted(p *Proposal) string {
	b := p.Buf
	end := p.Start + len(p.New)
	if p.Edited {
		end = p.Start + len(p.Final)
	}
	at := min(p.Start+p.runAt, end)
	for l := b.Text.LineOf(at); l+1 < b.Lines() && b.Text.LineStart(l+1) < end &&
		strings.TrimSpace(string(b.Text.Slice(b.Text.LineStart(l), b.LineEnd(l)))) == ""; l++ {
		at = b.Text.LineStart(l + 1) // the change starts with blank lines: run what follows
	}

	buf, sel, mode, msg := e.Buf, e.Sel, e.Mode, e.Msg
	e.Buf, e.Sel, e.Mode, e.Msg = b, Selection{at, at}, Normal, ""
	e.runs.proposal = p
	c, err := e.loadRunConfig()
	switch {
	case err != nil:
		e.Msg = err.Error()
	case !e.runByRule(c.rules):
		e.Msg = "no rule in " + rulesFile + " runs it"
	}
	e.runs.proposal = nil
	why := e.Msg
	e.Buf, e.Sel, e.Mode, e.Msg = buf, sel, mode, msg
	if p.runGoing {
		return ""
	}
	p.Ran = "not run: " + why
	return p.Ran
}

// runNote says how p will run once accepted, for its hunk info.
func (e *Editor) runNote(p *Proposal) string {
	c, err := loadRules(e.Root)
	if err != nil {
		return "won't run: " + err.Error()
	}
	r := e.ruleFor(c, p.Buf.Path)
	if r == nil {
		return "runs the block there"
	}
	en, err := e.runEnv(c, r.env)
	if err != nil {
		return "won't run: " + err.Error()
	}
	s := "runs [" + r.name + "]"
	if en != nil {
		s += " on " + en.name
		if en.confirm {
			s += ", asks first"
		}
	}
	return s
}

// Running reports whether p was accepted and its run hasn't finished.
func (p *Proposal) Running() bool { return p.runGoing }
