package core

import (
	"slices"
	"strings"
	"time"
)

type AgentState string

const (
	Idle    AgentState = "idle"
	Working AgentState = "working"
	Waiting AgentState = "waiting on you"
)

// HookAgent names the agent behind hook sessions: Claude Code, which sends
// this as its MCP client name, so it matches the name on its proposals.
const HookAgent = "claude-code"

// Session is one agent conversation, as reported by Claude Code hooks.
type Session struct {
	ID       string
	Turn     int // user prompts so far
	State    AgentState
	Note     string      // what the agent is waiting for, from its notification
	Seen     time.Time   // last hook
	awaiting time.Time   // last await_review poll
	watcher  string      // background comment watcher started when the session stopped
	watched  []*Proposal // pending when the watcher registered; it reports once all are reviewed
}

// Awaiting reports whether the session is blocked in await_review.
func (s *Session) Awaiting(now time.Time) bool { return now.Sub(s.awaiting) < 2*time.Second }

// HookEvent is the part of a Claude Code hook's input hiss uses.
type HookEvent struct {
	SessionID        string `json:"session_id"`
	Event            string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
	ToolName         string `json:"tool_name"`
}

// Hook updates agent state from a hook event.
func (e *Editor) Hook(ev HookEvent) {
	if ev.SessionID == "" {
		return
	}
	if ev.Event == "SessionEnd" {
		delete(e.sessions, ev.SessionID)
		return
	}
	s := e.sessions[ev.SessionID]
	if s == nil {
		s = &Session{ID: ev.SessionID, State: Idle}
		e.sessions[ev.SessionID] = s
	}
	s.Seen = e.now()
	switch ev.Event {
	case "UserPromptSubmit":
		s.Turn++
		s.State, s.Note = Working, ""
		s.watcher = "" // the session is busy again; its watcher stands down
	case "PreToolUse":
		s.State, s.Note = Working, ""
		if strings.HasPrefix(ev.ToolName, "mcp__hiss__") {
			e.caller = s.ID
		}
	case "PostToolUse":
		s.State, s.Note = Working, ""
	case "Notification":
		switch ev.NotificationType {
		case "idle_prompt":
			s.State, s.Note = Idle, ""
		case "auth_success", "elicitation_complete", "elicitation_response":
		default:
			s.State, s.Note = Waiting, ev.Message
			e.notify(HookAgent, ev.Message)
		}
	case "Stop":
		s.State, s.Note = Idle, ""
	}
}

// Sessions returns live sessions, most recently active first.
func (e *Editor) Sessions() []*Session {
	var ss []*Session
	for _, s := range e.sessions {
		ss = append(ss, s)
	}
	slices.SortFunc(ss, func(a, b *Session) int { return b.Seen.Compare(a.Seen) })
	return ss
}

// Caller returns the session that most recently started a hiss tool call, or nil.
func (e *Editor) Caller() *Session { return e.sessions[e.caller] }

// MarkAwaiting records that the calling session is waiting in await_review.
func (e *Editor) MarkAwaiting() {
	if s := e.Caller(); s != nil {
		s.awaiting = e.now()
	}
}

// Now returns the editor's clock.
func (e *Editor) Now() time.Time { return e.now() }
