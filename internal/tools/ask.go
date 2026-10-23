package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/synthlike/hiss/internal/core"
)

type AskIn struct {
	Question       string   `json:"question" jsonschema:"the question, shown in a popup in the editor"`
	Options        []string `json:"options,omitempty" jsonschema:"suggested answers; the user can also type their own"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" jsonschema:"how long to wait (default 300, at most 600)"`
}

// AskOut is the user's answer, or a note that none came in time.
type AskOut struct {
	Answer   string `json:"answer,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

func (a AskOut) Text() string {
	if a.TimedOut {
		return "The user didn't answer in hiss, so the question was withdrawn. Ask in chat if you still need to know.\n"
	}
	return "The user answered in hiss:\n> " + a.Answer + "\n"
}

type askPollIn struct {
	ID       int  `json:"id"`
	Withdraw bool `json:"withdraw,omitempty"`
}

type askPollOut struct {
	Answered bool   `json:"answered,omitempty"`
	Answer   string `json:"answer,omitempty"`
	Gone     bool   `json:"gone,omitempty"`
}

// askTool runs in `hiss mcp`: it puts the question on the editor (ask_start),
// then polls for the answer, so the editor's event loop never blocks.
var askTool = tool{
	name: "ask_start",
	register: func(s *mcp.Server, fwd Forward) {
		mcp.AddTool(s, &mcp.Tool{
			Name: "ask",
			Description: "Ask the user a question in a popup in the editor and wait for the answer. " +
				"Offer options when the likely answers are known; the user can also type their own.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in AskIn) (*mcp.CallToolResult, AskOut, error) {
			if strings.TrimSpace(in.Question) == "" {
				return nil, AskOut{}, errors.New("question is empty")
			}
			timeout := time.Duration(min(max(in.TimeoutSeconds, 0), 600)) * time.Second
			if timeout == 0 {
				timeout = 300 * time.Second
			}
			deadline := time.Now().Add(timeout)
			agent := agentName(req)
			poll := func(ctx context.Context, in askPollIn) (askPollOut, error) {
				var out askPollOut
				raw, err := fwd(ctx, agent, "ask_poll", in)
				if err == nil {
					err = json.Unmarshal(raw, &out)
				}
				return out, err
			}
			withdraw := func(id int) { // the call's context may be done already
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				poll(ctx, askPollIn{ID: id, Withdraw: true})
			}
			result := func(out AskOut) (*mcp.CallToolResult, AskOut, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Text()}}}, out, nil
			}

			var q askPollIn
			raw, err := fwd(ctx, agent, "ask_start", in)
			if err == nil {
				err = json.Unmarshal(raw, &q)
			}
			if err != nil {
				return nil, AskOut{}, err
			}
			for {
				out, err := poll(ctx, q)
				switch {
				case err != nil:
					withdraw(q.ID)
					return nil, AskOut{}, err
				case out.Answered:
					return result(AskOut{Answer: out.Answer})
				case out.Gone || time.Now().After(deadline):
					withdraw(q.ID)
					return result(AskOut{TimedOut: true})
				}
				select {
				case <-ctx.Done():
					withdraw(q.ID)
					return nil, AskOut{}, ctx.Err()
				case <-time.After(pollInterval):
				}
			}
		})
	},
	exec: func(ed *core.Editor, agent string, args json.RawMessage) (any, error) {
		var in AskIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		return askPollIn{ID: ed.Ask(agent, in.Question, in.Options).ID}, nil
	},
}

// askPoll reports whether a question is answered, dropping it once it is or
// when the asking agent withdraws it. Agents can't call it directly.
var askPoll = tool{
	name: "ask_poll",
	exec: func(ed *core.Editor, _ string, args json.RawMessage) (any, error) {
		var in askPollIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		q := ed.Question(in.ID)
		switch {
		case q == nil:
			return askPollOut{Gone: true}, nil
		case in.Withdraw || q.Answered:
			ed.DropQuestion(q.ID)
		}
		return askPollOut{Answered: q.Answered, Answer: q.Answer}, nil
	},
}
