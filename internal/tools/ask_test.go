package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/synthlike/hiss/internal/core"
)

func TestAskWaitsForAnswer(t *testing.T) {
	pollInterval = 10 * time.Millisecond
	cs, reqs := setup(t, "x\n")
	go func() {
		time.Sleep(50 * time.Millisecond)
		onEditor(reqs, func(ed *core.Editor) {
			if ed.Asking == nil || ed.Asking.Text != "Commit?" {
				t.Errorf("asking = %+v", ed.Asking)
				return
			}
			ed.HandleKey("down")
			ed.HandleKey("enter")
		})
	}()
	res := call(t, cs, "ask", map[string]any{"question": "Commit?", "options": []string{"yes", "no"}})
	if out := structured[AskOut](t, res); out.Answer != "no" || out.TimedOut {
		t.Fatalf("ask = %+v", out)
	}
	if got := text(res); !strings.Contains(got, "> no") {
		t.Errorf("text = %q", got)
	}
	onEditor(reqs, func(ed *core.Editor) {
		if ed.QuestionCount() != 0 || ed.Question(1) != nil {
			t.Error("answered question not dropped")
		}
	})
}

func TestAskTimesOutAndWithdraws(t *testing.T) {
	pollInterval = 10 * time.Millisecond
	cs, reqs := setup(t, "x\n")
	res := call(t, cs, "ask", map[string]any{"question": "Commit?", "timeout_seconds": 1})
	if out := structured[AskOut](t, res); !out.TimedOut {
		t.Fatalf("ask = %+v", out)
	}
	onEditor(reqs, func(ed *core.Editor) {
		if ed.Asking != nil || ed.QuestionCount() != 0 {
			t.Error("timed-out question still shown")
		}
	})
}
