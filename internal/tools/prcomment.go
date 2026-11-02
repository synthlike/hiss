package tools

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

type DraftPRCommentIn struct {
	Path      string `json:"path" jsonschema:"a file the PR changes, absolute or relative to the project root"`
	Line      int    `json:"line" jsonschema:"1-based line of the file as checked out (the PR's head) the comment is on, or its last line"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"first line, for a comment on several lines within one hunk"`
	Body      string `json:"body" jsonschema:"the comment in GitHub Markdown; a suggestion block proposes replacement lines"`
}

type DraftPRCommentOut struct {
	Where string `json:"where"`
	PR    int    `json:"pr"`
}

func (o DraftPRCommentOut) Text() string {
	return fmt.Sprintf("Drafted a comment on %s for #%d. It's pending: it publishes in the user's name, so they "+
		"accept it (:gca) or drop it before :pr-submit sends their review.", o.Where, o.PR)
}

func draftPRComment(ed *core.Editor, agent string, in DraftPRCommentIn) (DraftPRCommentOut, error) {
	first := in.StartLine
	if first == 0 {
		first = in.Line
	}
	where, n, err := ed.DraftPRComment(agent, resolve(ed, in.Path), first, in.Line, in.Body)
	if err != nil {
		return DraftPRCommentOut{}, err
	}
	return DraftPRCommentOut{Where: where, PR: n}, nil
}

var draftPRCommentTool = define(&mcp.Tool{
	Name: "draft_pr_comment",
	Description: "In a review of a GitHub PR the user checked out with :pr, draft an official review comment " +
		"on lines of the PR's diff (changed lines and the three around each hunk). It stays pending until the " +
		"user accepts it; their :pr-submit sends accepted drafts as one review. Only when the user asks for " +
		"comments: they publish in the user's name.",
}, draftPRComment)
