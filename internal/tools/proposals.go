package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

type ProposeEditIn struct {
	Path        string `json:"path" jsonschema:"absolute path, or relative to the project root; the file need not be open"`
	OldString   string `json:"old_string" jsonschema:"exact text to replace; must occur exactly once unless replace_all is set"`
	NewString   string `json:"new_string" jsonschema:"replacement text"`
	ReplaceAll  bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence"`
	BaseVersion *int   `json:"base_version,omitempty" jsonschema:"buffer version you read; the edit is refused if the user changed this text since"`
	Rationale   string `json:"rationale,omitempty" jsonschema:"one line shown to the user next to the change; with review, say what you want them to look at"`
	Review      bool   `json:"review,omitempty" jsonschema:"hold this for the user's review instead of applying it"`
	Supersedes  int    `json:"supersedes,omitempty" jsonschema:"id of your pending proposal this revises; it is replaced and its comment thread carries over; the revision stays pending"`
	Run         bool   `json:"run,omitempty" jsonschema:"run the block you changed once the user accepts, as Enter there would: for a query, request or script in a file a .hissrun rule matches; always held for review"`
}

type WriteFileIn struct {
	Path        string `json:"path" jsonschema:"absolute path, or relative to the project root"`
	Content     string `json:"content" jsonschema:"the complete new file content"`
	BaseVersion *int   `json:"base_version,omitempty" jsonschema:"buffer version you read, for an existing file"`
	Rationale   string `json:"rationale,omitempty" jsonschema:"one line shown to the user next to the change; with review, say what you want them to look at"`
	Review      bool   `json:"review,omitempty" jsonschema:"hold this for the user's review instead of applying it"`
	Supersedes  int    `json:"supersedes,omitempty" jsonschema:"id of your pending proposal this revises; it is replaced and its comment thread carries over; the revision stays pending"`
	Run         bool   `json:"run,omitempty" jsonschema:"run the first block you changed once the user accepts, as Enter there would: for a query, request or script in a file a .hissrun rule matches; always held for review"`
}

type ProposeOut struct {
	IDs     []int  `json:"ids"`
	Path    string `json:"path"`
	Applied bool   `json:"applied" jsonschema:"applied and saved; otherwise pending the user's review"`
	Held    string `json:"held,omitempty" jsonschema:"why it was held for review though you didn't ask"`
	Problem string `json:"problem,omitempty"`
}

func (o ProposeOut) Text() string {
	switch {
	case o.Problem != "":
		return fmt.Sprintf("Applied %v in %s, but: %s", o.IDs, o.Path, o.Problem)
	case o.Applied:
		return fmt.Sprintf("Applied %v in %s and saved it.", o.IDs, o.Path)
	case o.Held != "":
		return fmt.Sprintf("Held %v in %s for the user's review, because %s. Call await_review with these ids "+
			"before relying on the change.", o.IDs, o.Path, o.Held)
	}
	return fmt.Sprintf("Proposed %v in %s; pending the user's review. Call await_review with these ids "+
		"before relying on the change.", o.IDs, o.Path)
}

type ProposalInfo struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Session   string `json:"session,omitempty"`
	Turn      int    `json:"turn,omitempty"`
	Status    string `json:"status" jsonschema:"applied, pending, accepted, rejected, stale or undone"`
	Reason    string `json:"reason,omitempty" jsonschema:"why it was rejected or went stale"`
	Rationale string `json:"rationale,omitempty"`
	Edited    bool   `json:"edited,omitempty" jsonschema:"the user changed your text before accepting it"`
	FinalText string `json:"final_text,omitempty" jsonschema:"the text as accepted, when edited; build on this rather than your proposal"`
	Running   bool   `json:"running,omitempty" jsonschema:"accepted, and its run hasn't finished"`
	Ran       string `json:"ran,omitempty" jsonschema:"how its run went, for a proposal made with run"`
	RanOutput string `json:"ran_output,omitempty" jsonschema:"the end of the run's output, when its rule shares it"`
}

type GetProposalsIn struct {
	IDs    []int `json:"ids,omitempty" jsonschema:"proposals to report; all of yours if empty"`
	Review bool  `json:"review,omitempty" jsonschema:"report on the review you sent with show_review instead"`
}

type ProposalsOut struct {
	Proposals []ProposalInfo `json:"proposals"`
	Comments  []CommentInfo  `json:"new_comments,omitempty" jsonschema:"comments the user made since you last checked"`
	TimedOut  bool           `json:"timed_out,omitempty"`
	Review    bool           `json:"review,omitempty" jsonschema:"this is about a review, not proposals"`
	Finished  bool           `json:"finished,omitempty" jsonschema:"the review is over"`
}

func (o ProposalsOut) Text() string {
	if o.Review {
		switch {
		case len(o.Comments) > 0:
			return CommentsText(o.Comments)
		case o.Finished:
			return "The review is over.\n"
		}
		return "Still waiting for the user; call await_review again or continue with other work.\n"
	}
	var sb strings.Builder
	for _, p := range o.Proposals {
		fmt.Fprintf(&sb, "#%d %s: %s", p.ID, p.Path, p.Status)
		if p.Reason != "" {
			fmt.Fprintf(&sb, " (%s)", p.Reason)
		}
		if p.Edited {
			fmt.Fprintf(&sb, " with the user's edits; the text as saved:\n%s", strings.TrimSuffix(p.FinalText, "\n"))
		}
		switch {
		case p.Running:
			sb.WriteString("; running")
		case p.Ran != "":
			sb.WriteString("; " + p.Ran)
		}
		sb.WriteString("\n")
		if p.RanOutput != "" {
			sb.WriteString("The output ended with:\n")
			for l := range strings.Lines(p.RanOutput) {
				sb.WriteString("    " + strings.TrimSuffix(l, "\n") + "\n")
			}
		}
	}
	switch {
	case len(o.Proposals) == 0:
		sb.WriteString("no proposals\n")
	case len(o.Comments) > 0:
		sb.WriteString(CommentsText(o.Comments))
	case o.TimedOut:
		sb.WriteString("Still waiting for the user; call await_review again or continue with other work.\n")
	}
	return sb.String()
}

func proposeEdit(ed *core.Editor, agent string, in ProposeEditIn) (ProposeOut, error) {
	path := resolve(ed, in.Path)
	if in.Run && in.ReplaceAll {
		return ProposeOut{}, fmt.Errorf("run takes one edit; drop replace_all")
	}
	return superseding(ed, agent, in.Supersedes, in.Review || in.Run, func() ([]*core.Proposal, error) {
		if err := canRun(ed, in.Run, path); err != nil {
			return nil, err
		}
		ps, err := ed.ProposeEdit(agent, path, in.OldString, in.NewString, in.ReplaceAll, in.BaseVersion, in.Rationale)
		return runOnAccept(ed, in.Run, ps), err
	})
}

func writeFile(ed *core.Editor, agent string, in WriteFileIn) (ProposeOut, error) {
	path := resolve(ed, in.Path)
	return superseding(ed, agent, in.Supersedes, in.Review || in.Run, func() ([]*core.Proposal, error) {
		if err := canRun(ed, in.Run, path); err != nil {
			return nil, err
		}
		p, err := ed.WriteFile(agent, path, in.Content, in.BaseVersion, in.Rationale)
		if err != nil {
			return nil, err
		}
		return runOnAccept(ed, in.Run, []*core.Proposal{p}), nil
	})
}

func canRun(ed *core.Editor, run bool, path string) error {
	if !run {
		return nil
	}
	return ed.CanRun(path)
}

func runOnAccept(ed *core.Editor, run bool, ps []*core.Proposal) []*core.Proposal {
	if run {
		for _, p := range ps {
			ed.RunOnAccept(p)
		}
	}
	return ps
}

// superseding runs propose, first withdrawing the proposal it revises (if any)
// so the revision may overlap it, and restoring that proposal if propose fails.
// What it proposes then applies, unless review asks for the user or it revises
// something already in review.
func superseding(ed *core.Editor, agent string, old int, review bool, propose func() ([]*core.Proposal, error)) (ProposeOut, error) {
	undo := func() {}
	if old != 0 {
		var err error
		if undo, err = ed.Withdraw(agent, old); err != nil {
			return ProposeOut{}, err
		}
	}
	ps, err := propose()
	if err != nil {
		undo()
		return ProposeOut{}, err
	}
	held := ed.Settle(agent, ps, review || old != 0)
	if old != 0 {
		ed.Supersede(ed.Proposal(old), ps[0])
	}
	out := ProposeOut{Path: ps[0].Buf.Path, Held: held, Applied: ps[0].Status == core.Applied, Problem: ps[0].Reason}
	for _, p := range ps {
		out.IDs = append(out.IDs, p.ID)
	}
	return out, nil
}

func getProposals(ed *core.Editor, agent string, in GetProposalsIn) (ProposalsOut, error) {
	if in.Review {
		ds, done := ed.TakeReview()
		return ProposalsOut{Proposals: []ProposalInfo{}, Comments: commentInfos(ds), Review: true, Finished: done}, nil
	}
	var ps []*core.Proposal
	if len(in.IDs) == 0 {
		ps = ed.ProposalsBy(agent)
	}
	for _, id := range in.IDs {
		p := ed.Proposal(id)
		if p == nil {
			return ProposalsOut{}, fmt.Errorf("no proposal #%d", id)
		}
		ps = append(ps, p)
	}
	out := ProposalsOut{Proposals: []ProposalInfo{}, Comments: commentInfos(ed.TakeComments(ps))}
	for _, p := range ps {
		out.Proposals = append(out.Proposals, proposalInfo(p))
	}
	return out, nil
}

func proposalInfo(p *core.Proposal) ProposalInfo {
	return ProposalInfo{
		ID: p.ID, Path: p.Buf.Path, Session: p.Session, Turn: p.Turn,
		Status: string(p.Status), Reason: p.Reason, Rationale: p.Rationale,
		Edited: p.Edited, FinalText: p.Final,
		Running: p.Running(), Ran: p.Ran, RanOutput: p.RanTail,
	}
}

type AwaitReviewIn struct {
	IDs            []int `json:"ids,omitempty" jsonschema:"proposals to wait for"`
	Review         bool  `json:"review,omitempty" jsonschema:"wait for news of the review you sent with show_review instead: a comment, a rejection or its end"`
	TimeoutSeconds int   `json:"timeout_seconds,omitempty" jsonschema:"how long to wait (default 120, at most 600)"`
}

// pollInterval is how often await_review asks the editor for news.
var pollInterval = 300 * time.Millisecond

// awaitReview runs in `hiss mcp`, polling get_proposals, so the editor's event
// loop is never blocked waiting for the user.
var awaitReview = tool{
	name: "await_review",
	register: func(s *mcp.Server, fwd Forward) {
		mcp.AddTool(s, &mcp.Tool{
			Name: "await_review",
			Description: "Wait until the user has reviewed the given proposals (or the timeout passes) " +
				"and report each one's status. Accepted proposals are saved to disk.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in AwaitReviewIn) (*mcp.CallToolResult, ProposalsOut, error) {
			timeout := time.Duration(min(max(in.TimeoutSeconds, 0), 600)) * time.Second
			if timeout == 0 {
				timeout = 120 * time.Second
			}
			deadline := time.Now().Add(timeout)
			for {
				var out ProposalsOut
				raw, err := fwd(ctx, agentName(req), "await_poll", GetProposalsIn{IDs: in.IDs, Review: in.Review})
				if err == nil {
					err = json.Unmarshal(raw, &out)
				}
				if err != nil {
					return nil, out, err
				}
				waiting := slices.ContainsFunc(out.Proposals, func(p ProposalInfo) bool { return p.Status == string(core.Pending) || p.Running }) ||
					in.Review && !out.Finished
				if !waiting || len(out.Comments) > 0 || time.Now().After(deadline) {
					out.TimedOut = waiting && len(out.Comments) == 0
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Text()}}}, out, nil
				}
				select {
				case <-ctx.Done():
					return nil, out, ctx.Err()
				case <-time.After(pollInterval):
				}
			}
		})
	},
}

// awaitPoll is get_proposals for await_review: it also marks the calling
// session as waiting for the user. Agents can't call it directly.
var awaitPoll = tool{
	name: "await_poll",
	exec: func(ed *core.Editor, agent string, args json.RawMessage) (any, error) {
		var in GetProposalsIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		ed.MarkAwaiting()
		return getProposals(ed, agent, in)
	},
}
