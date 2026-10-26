package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// proposalsFile keeps the pending proposals, relative to the project root,
// so they survive a crash or restart. Like the rest of .hiss/, it's not
// committed.
const proposalsFile = ".hiss/proposals.json"

// savedProposals is proposalsFile's content. A proposal is kept by its text
// rather than its offsets: unsaved edits are lost with the editor, so it is
// found again in the file as it is when hiss starts.
type savedProposals struct {
	NextID    int             `json:"next_id"`
	Proposals []savedProposal `json:"proposals"`
}

type savedProposal struct {
	ID        int            `json:"id"`
	Agent     string         `json:"agent"`
	Session   string         `json:"session,omitempty"`
	Turn      int            `json:"turn,omitempty"`
	Rationale string         `json:"rationale,omitempty"`
	Path      string         `json:"path"` // relative to the root when inside it
	Line      int            `json:"line"` // 1-based, where Old started: picks among repeats
	Old       string         `json:"old"`
	New       string         `json:"new"`
	Thread    []savedComment `json:"thread,omitempty"`
}

type savedComment struct {
	Author    string `json:"author"`
	Text      string `json:"text"`
	At        string `json:"at,omitempty"`
	Line      string `json:"line,omitempty"`
	Delivered bool   `json:"delivered,omitempty"`
}

// persist is the state of proposalsFile. Only the hiss serving the project
// keeps it, so a second one can't overwrite the first's proposals.
type persist struct {
	on   bool
	last []byte // what was last written, or read
}

// RestoreProposals reads the proposals kept by the last run and starts
// keeping them. Each goes back where its old text is in the file now; ones
// whose text has changed are dropped, and the message says how many.
func (e *Editor) RestoreProposals() {
	if e.Root == "" {
		return
	}
	e.persist.on = true
	data, err := os.ReadFile(filepath.Join(e.Root, proposalsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	var saved savedProposals
	if err == nil {
		err = json.Unmarshal(data, &saved)
	}
	if err != nil {
		e.Msg = failed("restoring "+proposalsFile, err)
		e.persist.on = false // keep the file for the user to look at
		return
	}
	e.persist.last = data
	e.nextID = max(e.nextID, saved.NextID)
	restored, dropped := 0, 0
	for _, sp := range saved.Proposals {
		if e.restoreProposal(sp) {
			restored++
		} else {
			dropped++
		}
	}
	switch {
	case restored > 0 && dropped > 0:
		e.Msg = fmt.Sprintf("restored %s (]h to review); dropped %d whose text changed", plural(restored, "pending hunk"), dropped)
	case restored > 0:
		e.Msg = fmt.Sprintf("restored %s (]h to review)", plural(restored, "pending hunk"))
	case dropped > 0:
		e.Msg = fmt.Sprintf("dropped %s whose text changed", plural(dropped, "pending hunk"))
	}
}

// restoreProposal puts sp back as a pending proposal and reports whether it could.
func (e *Editor) restoreProposal(sp savedProposal) bool {
	path := sp.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.Root, path)
	}
	b, err := e.OpenBuffer(path)
	if err != nil || e.proposals[sp.ID] != nil {
		return false
	}
	start := -1
	content := b.Text.Bytes()
	if sp.Old == "" {
		if len(content) == 0 {
			start = 0
		}
	} else {
		for i, best := 0, -1; ; {
			j := bytes.Index(content[i:], []byte(sp.Old))
			if j < 0 {
				break
			}
			at := i + j
			if d := abs(b.Text.LineOf(at) + 1 - sp.Line); best < 0 || d < best {
				start, best = at, d
			}
			i = at + len(sp.Old)
		}
	}
	if start < 0 || e.checkRegion(b, start, start+len(sp.Old), nil) != nil {
		return false
	}
	p := &Proposal{ID: sp.ID, Agent: sp.Agent, Session: sp.Session, Turn: sp.Turn, Rationale: sp.Rationale,
		Buf: b, Start: start, End: start + len(sp.Old), New: sp.New, Status: Pending}
	for _, c := range sp.Thread {
		p.Thread = append(p.Thread, Comment{Author: c.Author, Text: c.Text, At: c.At, Line: c.Line, delivered: c.Delivered})
	}
	e.track(p)
	e.nextID = max(e.nextID, p.ID)
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// SaveProposals writes the pending proposals to proposalsFile if they have
// changed since it was last written, and removes it once none are pending.
// The event loop calls it after each event.
func (e *Editor) SaveProposals() {
	if !e.persist.on {
		return
	}
	saved := savedProposals{NextID: e.nextID}
	for _, b := range e.Bufs {
		for _, p := range b.pending {
			saved.Proposals = append(saved.Proposals, e.saved(p))
		}
	}
	file := filepath.Join(e.Root, proposalsFile)
	if len(saved.Proposals) == 0 {
		if e.persist.last != nil {
			os.Remove(file)
			e.persist.last = nil
		}
		return
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil || bytes.Equal(data, e.persist.last) {
		return
	}
	if err := writeAtomic(file, append(data, '\n')); err != nil {
		e.Msg = failed("saving "+proposalsFile, err) + "; pending hunks won't survive a restart"
		e.persist.on = false // say it once, not after every key
		return
	}
	e.persist.last = data
}

// saved returns p as proposalsFile keeps it. A tweaked proposal is kept as
// the agent proposed it: the old text is what's on disk, and the user's
// edits to it are unsaved.
func (e *Editor) saved(p *Proposal) savedProposal {
	b := p.Buf
	old := string(b.Text.Slice(p.Start, p.End))
	if p.Tweaked {
		var sb strings.Builder
		for _, s := range p.old {
			sb.Write(s.text)
		}
		old = sb.String()
	}
	sp := savedProposal{ID: p.ID, Agent: p.Agent, Session: p.Session, Turn: p.Turn, Rationale: p.Rationale,
		Path: e.shortPath(b.Path), Line: b.Text.LineOf(p.Start) + 1, Old: old, New: p.New}
	for _, c := range p.Thread {
		sp.Thread = append(sp.Thread, savedComment{Author: c.Author, Text: c.Text, At: c.At, Line: c.Line, Delivered: c.delivered})
	}
	return sp
}

// ForgetProposals stops keeping proposals and removes the kept ones, as
// :q! discards them.
func (e *Editor) ForgetProposals() {
	if e.persist.on {
		os.Remove(filepath.Join(e.Root, proposalsFile))
	}
	e.persist = persist{}
}

// writeAtomic replaces file with data, so a crash can't leave half of it.
func writeAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".proposals-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // fails harmlessly once renamed
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}
