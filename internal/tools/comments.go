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
	ID     int    `json:"id,omitempty" jsonschema:"proposal the comment is on"`
	Step   int    `json:"step,omitempty" jsonschema:"trail step the comment is on"`
	Path   string `json:"path"`
	LineNo int    `json:"line_no,omitempty" jsonschema:"1-based line of the trail step"`
	Text   string `json:"text"`
	Line   string `json:"line,omitempty" jsonschema:"diff line the comment is on: '+ text' for a proposed line, '- text' for a replaced one"`
}

func commentInfos(ds []core.Delivery) []CommentInfo {
	var out []CommentInfo
	for _, d := range ds {
		if s := d.Step; s != nil {
			out = append(out, CommentInfo{Step: s.N, Path: s.Buf.Path, LineNo: s.Line() + 1, Text: d.Text})
			continue
		}
		out = append(out, CommentInfo{ID: d.Proposal.ID, Path: d.Proposal.Buf.Path, Text: d.Text, Line: d.Line})
	}
	return out
}

// CommentsText tells an agent about new comments and how to answer them.
func CommentsText(cs []CommentInfo) string {
	var sb strings.Builder
	for _, c := range cs {
		switch {
		case c.Step > 0:
			fmt.Fprintf(&sb, "The user commented in hiss on step %d of your trail (%s:%d):\n", c.Step, c.Path, c.LineNo)
		case c.Line != "":
			fmt.Fprintf(&sb, "The user commented in hiss on your proposal #%d (%s) on the diff line\n    %s\n", c.ID, c.Path, c.Line)
		default:
			fmt.Fprintf(&sb, "The user commented in hiss on your proposal #%d (%s):\n", c.ID, c.Path)
		}
		fmt.Fprintf(&sb, "> %s\n", c.Text)
	}
	sb.WriteString("Answer each in the editor with the hiss reply tool (id for a proposal, step for a trail step). " +
		"If a comment asks for a change to a proposal, propose a revision with propose_edit or write_file, " +
		"setting supersedes to the proposal's id.\n")
	return sb.String()
}

type ReplyIn struct {
	ID   int    `json:"id,omitempty" jsonschema:"proposal whose thread to answer in"`
	Step int    `json:"step,omitempty" jsonschema:"trail step whose thread to answer in, instead of id"`
	Text string `json:"text" jsonschema:"your answer, shown in the editor; keep it short"`
}

type ReplyOut struct {
	OK bool `json:"ok"`
}

func reply(ed *core.Editor, agent string, in ReplyIn) (ReplyOut, error) {
	var err error
	switch {
	case in.Step > 0 && in.ID > 0:
		err = fmt.Errorf("set id or step, not both")
	case in.Step > 0:
		err = ed.ReplyStep(agent, in.Step, in.Text)
	default:
		err = ed.Reply(agent, in.ID, in.Text)
	}
	if err != nil {
		return ReplyOut{}, err
	}
	return ReplyOut{OK: true}, nil
}

var replyTool = define(&mcp.Tool{
	Name: "reply",
	Description: "Answer the user's comment on one of your proposals (id) or trail steps (step); " +
		"the reply appears under it in the editor.",
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
