package core

import "time"

// Toast is a short notice drawn in the top-right corner until Until.
type Toast struct {
	Title, Text string
	Until       time.Time
}

const toastFor = 5 * time.Second

// notify shows a toast and queues the notice for a desktop notification.
func (e *Editor) notify(title, text string) {
	e.Toast = Toast{title, text, e.now().Add(toastFor)}
	e.notices = append(e.notices, Toast{Title: title, Text: text})
}

// ActiveToast returns the toast to draw, or nil once it has expired.
func (e *Editor) ActiveToast() *Toast {
	if e.Toast.Text == "" || !e.now().Before(e.Toast.Until) {
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
