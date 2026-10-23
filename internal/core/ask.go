package core

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Question is an agent's question for the user, answered in a popup over the
// editor. The agent waits in the ask tool until it is answered or withdrawn.
type Question struct {
	ID       int
	Agent    string
	Text     string
	Options  []string // suggested answers; the user can also type one
	Selected int      // the option under the cursor; len(Options) is the typed answer
	Input    string   // the typed answer
	Answer   string
	Answered bool

	hidden bool // closed with esc; :answer reopens it
}

// Ask queues a question from agent and opens it unless the user is busy.
func (e *Editor) Ask(agent, text string, options []string) *Question {
	e.nextQ++
	q := &Question{ID: e.nextQ, Agent: agent, Text: text, Options: options}
	e.questions = append(e.questions, q)
	e.notify(agent, "asks: "+text)
	e.openQuestion()
	return q
}

// Question returns the queued question with id, or nil once it is dropped.
func (e *Editor) Question(id int) *Question {
	for _, q := range e.questions {
		if q.ID == id {
			return q
		}
	}
	return nil
}

// DropQuestion removes a question once its agent has the answer or gives up.
func (e *Editor) DropQuestion(id int) {
	q := e.Question(id)
	if q == nil {
		return
	}
	e.questions = slices.DeleteFunc(e.questions, func(o *Question) bool { return o == q })
	if e.Asking == q {
		e.Asking = nil
	}
	if !q.Answered {
		e.Msg = fmt.Sprintf("%s withdrew its question", q.Agent)
	}
}

// QuestionCount returns how many questions are waiting for an answer.
func (e *Editor) QuestionCount() int {
	n := 0
	for _, q := range e.questions {
		if !q.Answered {
			n++
		}
	}
	return n
}

// openQuestion shows the oldest unanswered question, unless one is open or
// the user is typing, picking or in a menu.
func (e *Editor) openQuestion() {
	if e.Asking != nil || e.Mode != Normal || e.Picker != nil || e.Menu != nil {
		return
	}
	for _, q := range e.questions {
		if !q.Answered && !q.hidden {
			e.Asking = q
			return
		}
	}
}

// reopenQuestions shows questions closed with esc again.
func (e *Editor) reopenQuestions() {
	for _, q := range e.questions {
		q.hidden = false
	}
	e.openQuestion()
	if e.Asking == nil {
		e.Msg = "no questions"
	}
}

// askKey handles a key in the question popup: up/down choose an option,
// typing writes an answer of your own, enter answers, esc closes it for later.
func (e *Editor) askKey(k string) {
	q := e.Asking
	n := len(q.Options) + 1 // the options, then the typed answer
	typed := func(s string) { q.Input, q.Selected = q.Input+s, len(q.Options) }
	switch k {
	case "esc", "C-c":
		q.hidden, e.Asking = true, nil
		e.Msg = "question closed (:answer to reopen)"
	case "enter":
		answer := q.Input
		if q.Selected < len(q.Options) {
			answer = q.Options[q.Selected]
		}
		if strings.TrimSpace(answer) == "" {
			return
		}
		q.Answer, q.Answered, e.Asking = answer, true, nil
		e.Msg = "answered " + q.Agent
	case "down", "C-n", "tab":
		q.Selected = (q.Selected + 1) % n
	case "up", "C-p", "backtab":
		q.Selected = (q.Selected - 1 + n) % n
	case "backspace":
		_, size := utf8.DecodeLastRuneInString(q.Input)
		q.Input, q.Selected = q.Input[:len(q.Input)-size], len(q.Options)
	case "space":
		typed(" ")
	default:
		if utf8.RuneCountInString(k) == 1 {
			typed(k)
		}
	}
}
