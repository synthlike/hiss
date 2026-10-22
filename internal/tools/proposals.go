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
	ReplaceAll  bool   `json:"replace_all,omitempty" jsonschema:"propose replacing every occurrence"`
	BaseVersion *int   `json:"base_version,omitempty" jsonschema:"buffer version you read; the edit is refused if the user changed this text since"`
	Rationale   string `json:"rationale,omitempty" jsonschema:"one line shown to the user next to the change"`
	Supersedes  int    `json:"supersedes,omitempty" jsonschema:"id of your pending proposal this revises; it is replaced and its comment thread carries over"`
}

type WriteFileIn struct {
	Path        string `json:"path" jsonschema:"absolute path, or relative to the project root"`
	Content     string `json:"content" jsonschema:"the complete new file content"`
	BaseVersion *int   `json:"base_version,omitempty" jsonschema:"buffer version you read, for an existing file"`
	Rationale   string `json:"rationale,omitempty" jsonschema:"one line shown to the user next to the change"`
	Supersedes  int    `json:"supersedes,omitempty" jsonschema:"id of your pending proposal this revises; it is replaced and its comment thread carries over"`
}

type ProposeOut struct {
	IDs  []int  `json:"ids"`
	Path string `json:"path"`
}

func (o ProposeOut) Text() string {
	return fmt.Sprintf("Proposed %v in %s; pending the user's review. Call await_review with these ids "+
		"before relying on the change.", o.IDs, o.Path)
}

type ProposalInfo struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Session   string `json:"session,omitempty"`
	Turn      int    `json:"turn,omitempty"`
	Status    string `json:"status" jsonschema:"pending, accepted, rejected or stale"`
	Reason    string `json:"reason,omitempty" jsonschema:"why it was rejected or went stale"`
	Rationale string `json:"rationale,omitempty"`
	Edited    bool   `json:"edited,omitempty" jsonschema:"the user changed your text before accepting it"`
	FinalText string `json:"final_text,omitempty" jsonschema:"the text as accepted, when edited; build on this rather than your proposal"`
}

type GetProposalsIn struct {
	IDs []int `json:"ids,omitempty" jsonschema:"proposals to report; all of yours if empty"`
}

type ProposalsOut struct {
	Proposals []ProposalInfo `json:"proposals"`
	Comments  []CommentInfo  `json:"new_comments,omitempty" jsonschema:"comments the user made since you last checked"`
	TimedOut  bool           `json:"timed_out,omitempty"`
}

func (o ProposalsOut) Text() string {
	var sb strings.Builder
	for _, p := range o.Proposals {
		fmt.Fprintf(&sb, "#%d %s: %s", p.ID, p.Path, p.Status)
		if p.Reason != "" {
			fmt.Fprintf(&sb, " (%s)", p.Reason)
		}
		if p.Edited {
			fmt.Fprintf(&sb, " with the user's edits; the text as saved:\n%s", strings.TrimSuffix(p.FinalText, "\n"))
		}
		sb.WriteString("\n")
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
	return superseding(ed, agent, in.Supersedes, func() ([]*core.Proposal, error) {
		return ed.ProposeEdit(agent, resolve(ed, in.Path), in.OldString, in.NewString, in.ReplaceAll, in.BaseVersion, in.Rationale)
	})
}

func writeFile(ed *core.Editor, agent string, in WriteFileIn) (ProposeOut, error) {
	return superseding(ed, agent, in.Supersedes, func() ([]*core.Proposal, error) {
		p, err := ed.WriteFile(agent, resolve(ed, in.Path), in.Content, in.BaseVersion, in.Rationale)
		return []*core.Proposal{p}, err
	})
}

// superseding runs propose, first withdrawing the proposal it revises (if any)
// so the revision may overlap it, and restoring that proposal if propose fails.
func superseding(ed *core.Editor, agent string, old int, propose func() ([]*core.Proposal, error)) (ProposeOut, error) {
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
	if old != 0 {
		ed.Supersede(ed.Proposal(old), ps[0])
	}
	out := ProposeOut{Path: ps[0].Buf.Path}
	for _, p := range ps {
		out.IDs = append(out.IDs, p.ID)
	}
	return out, nil
}

func getProposals(ed *core.Editor, agent string, in GetProposalsIn) (ProposalsOut, error) {
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
	}
}

type AwaitReviewIn struct {
	IDs            []int `json:"ids" jsonschema:"proposals to wait for"`
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
				raw, err := fwd(ctx, agentName(req), "await_poll", GetProposalsIn{IDs: in.IDs})
				if err == nil {
					err = json.Unmarshal(raw, &out)
				}
				if err != nil {
					return nil, out, err
				}
				waiting := slices.ContainsFunc(out.Proposals, func(p ProposalInfo) bool { return p.Status == string(core.Pending) })
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
