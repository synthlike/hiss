package core

import (
	"testing"
	"time"
)

func TestAgentStates(t *testing.T) {
	e := newEditor(t, "x := 1\n")
	clock := time.Unix(1000, 0)
	e.now = func() time.Time { return clock }
	hook := func(event, extra string) {
		ev := HookEvent{SessionID: "s1", Event: event}
		switch event {
		case "Notification":
			ev.NotificationType, ev.Message = extra, "Claude needs your permission"
		case "PreToolUse":
			ev.ToolName = extra
		}
		e.Hook(ev)
	}
	steps := []struct {
		event, extra string
		state        AgentState
		turn         int
	}{
		{"SessionStart", "", Idle, 0},
		{"UserPromptSubmit", "", Working, 1},
		{"Notification", "permission_prompt", Waiting, 1},
		{"PostToolUse", "", Working, 1},
		{"Stop", "", Idle, 1},
		{"UserPromptSubmit", "", Working, 2},
		{"Notification", "idle_prompt", Idle, 2},
	}
	for _, st := range steps {
		hook(st.event, st.extra)
		s := e.Sessions()[0]
		if s.State != st.state || s.Turn != st.turn {
			t.Fatalf("after %s %s: state=%s turn=%d, want %s %d", st.event, st.extra, s.State, s.Turn, st.state, st.turn)
		}
	}
	hook("SessionEnd", "")
	if len(e.Sessions()) != 0 {
		t.Error("SessionEnd should remove the session")
	}
}

func TestProposalRecordsCallerTurn(t *testing.T) {
	e := newEditor(t, "a\n")
	e.Hook(HookEvent{SessionID: "s1", Event: "UserPromptSubmit"})
	e.Hook(HookEvent{SessionID: "s2", Event: "UserPromptSubmit"})
	e.Hook(HookEvent{SessionID: "s2", Event: "UserPromptSubmit"})
	e.Hook(HookEvent{SessionID: "s2", Event: "PreToolUse", ToolName: "Bash"}) // not a hiss call
	e.Hook(HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__propose_edit"})
	p := propose(t, e, "a", "b")
	if p.Session != "s1" || p.Turn != 1 {
		t.Fatalf("proposal session=%q turn=%d", p.Session, p.Turn)
	}
	keys(e, "] h space a")
	o := e.Buf.Text.Spans(0, 1)[0].Origin
	if o.Session != "s1" || o.Turn != 1 || o.Proposal != p.ID {
		t.Errorf("origin = %+v", o)
	}
}

func TestAwaiting(t *testing.T) {
	e := newEditor(t, "a\n")
	clock := time.Unix(1000, 0)
	e.now = func() time.Time { return clock }
	e.Hook(HookEvent{SessionID: "s1", Event: "PreToolUse", ToolName: "mcp__hiss__await_review"})
	e.MarkAwaiting()
	s := e.Sessions()[0]
	if !s.Awaiting(clock) || s.Awaiting(clock.Add(3*time.Second)) {
		t.Error("awaiting should last about two seconds after the last poll")
	}
}
