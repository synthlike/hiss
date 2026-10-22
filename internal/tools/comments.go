package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

// CommentInfo is a user comment delivered to an agent.
type CommentInfo struct {
	ID   int    `json:"id" jsonschema:"proposal the comment is on"`
	Path string `json:"path"`
	Text string `json:"text"`
	Line string `json:"line,omitempty" jsonschema:"diff line the comment is on: '+ text' for a proposed line, '- text' for a replaced one"`
}

func commentInfos(ds []core.Delivery) []CommentInfo {
	var out []CommentInfo
	for _, d := range ds {
		out = append(out, CommentInfo{ID: d.Proposal.ID, Path: d.Proposal.Buf.Path, Text: d.Text, Line: d.Line})
	}
	return out
}

// CommentsText tells an agent about new comments and how to answer them.
func CommentsText(cs []CommentInfo) string {
	var sb strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&sb, "The user commented in hiss on your proposal #%d (%s)", c.ID, c.Path)
		if c.Line != "" {
			fmt.Fprintf(&sb, " on the diff line\n    %s\n", c.Line)
		} else {
			sb.WriteString(":\n")
		}
		fmt.Fprintf(&sb, "> %s\n", c.Text)
	}
	sb.WriteString("Answer in the editor with the hiss reply tool. If the comment asks for a change, " +
		"propose a revision with propose_edit or write_file, setting supersedes to the proposal's id.\n")
	return sb.String()
}

type ReplyIn struct {
	ID   int    `json:"id" jsonschema:"proposal whose thread to answer in"`
	Text string `json:"text" jsonschema:"your answer, shown under the hunk; keep it short"`
}

type ReplyOut struct {
	OK bool `json:"ok"`
}

func reply(ed *core.Editor, agent string, in ReplyIn) (ReplyOut, error) {
	if err := ed.Reply(agent, in.ID, in.Text); err != nil {
		return ReplyOut{}, err
	}
	return ReplyOut{OK: true}, nil
}

var replyTool = define(&mcp.Tool{
	Name:        "reply",
	Description: "Answer the user's comment on one of your proposals; the reply appears under the hunk in the editor.",
}, reply)

type watchArgs struct {
	SessionID string `json:"session_id"`
	Watcher   string `json:"watcher"`
	Register  bool   `json:"register,omitempty"`
}

// WatchOut is the reply to the comment watcher's poll.
type WatchOut struct {
	Superseded bool           `json:"superseded,omitempty"`
	Comments   []CommentInfo  `json:"comments,omitempty"`
	Reviewed   []ProposalInfo `json:"reviewed,omitempty"`
}

// ReviewedText tells an agent the user has reviewed everything it left pending.
func ReviewedText(ps []ProposalInfo) string {
	return "The user finished reviewing the proposals you left pending:\n" +
		ProposalsOut{Proposals: ps}.Text() +
		"Accepted changes are saved to disk. Continue the task, adjusting for any rejections.\n"
}

// watchComments serves `hiss hook watch`; agents can't call it.
var watchComments = tool{
	name: "watch_comments",
	exec: func(ed *core.Editor, _ string, args json.RawMessage) (any, error) {
		var in watchArgs
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		r := ed.WatchComments(in.SessionID, in.Watcher, in.Register)
		out := WatchOut{Superseded: r.Superseded, Comments: commentInfos(r.Comments)}
		for _, p := range r.Reviewed {
			out.Reviewed = append(out.Reviewed, proposalInfo(p))
		}
		return out, nil
	},
}
