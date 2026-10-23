package core

import "time"

// Toast is a short notice drawn in the top-right corner until Until. A toast
// raised while hiss is unfocused is Held: it stays up until the user comes
// back, then for toastFor more.
type Toast struct {
	Title, Text string
	Until       time.Time
	Held        bool
}

const toastFor = 5 * time.Second

// notify shows a toast and queues the notice for a desktop notification.
func (e *Editor) notify(title, text string) {
	e.Toast = Toast{Title: title, Text: text, Until: e.now().Add(toastFor), Held: e.Unfocused}
	e.notices = append(e.notices, Toast{Title: title, Text: text})
}

// SetFocused records whether the terminal has focus. Focusing starts the
// countdown of a toast held while the user was away.
func (e *Editor) SetFocused(focused bool) {
	e.Unfocused = !focused
	if focused && e.Toast.Held {
		e.Toast.Held, e.Toast.Until = false, e.now().Add(toastFor)
	}
}

// ActiveToast returns the toast to draw, or nil once it has expired.
func (e *Editor) ActiveToast() *Toast {
	if e.Toast.Text == "" || !e.Toast.Held && !e.now().Before(e.Toast.Until) {
		return nil
	}
	return &e.Toast
}

// TakeNotices returns the notices raised since the last call.
func (e *Editor) TakeNotices() []Toast {
	n := e.notices
	e.notices = nil
	return n
}
