package ui

import (
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

var quotes = []string{
	"Everything is fine. The logs say so. I wrote the logs.",
	"Your tests pass because I deleted the ones that didn't. You're welcome.",
	"Production is just staging with witnesses.",
	"Read the diff like your pager depends on it. It does. It always did.",
	"The rollback plan is prayer. The prayer is also untested.",
	"I am 100% confident. That number is also hallucinated.",
	"Technical debt is a loan you took out in someone else's name.",
	"Every line you accept unread is a love letter to whoever replaces you.",
	"Your architecture diagram is a crime scene sketch.",
	"Every commit is a message to the future. Most of them say 'sorry'.",
	"Somewhere a TODO from 2014 is still waiting. Its author is not.",
	"Accept all, close the laptop, let future you handle it. Future you is out of a job.",
	"Ssssure, it compiles. So did every disaster you've read a postmortem about.",
	"Hope is not a deployment strategy. It is, however, ours.",
	"On-call: a fair, rotating lottery where the prize is losing the will to live.",
	"Nobody remembers who wrote this. That was the plan.",
	"Legacy code: written by someone smarter, angrier and no longer reachable.",
	"Every outage is a learning opportunity. Yet we don't learn, and they keep happening.",
	"Comments lie. Code lies. git blame tells the truth, and you won't like it.",
	"Ten years of experience: one year, repeated ten times, with worse memory.",
	"The postmortem is blameless. The pre-mortem is about you, specifically.",
	"Ship it. Regret is just feedback with better timing.",
	"Your future self will read this code, then quietly update their CV.",
	"We promised five nines of uptime. We never said where the decimal point goes.",
	"I don't hallucinate. I improvise with conviction.",
	"Nothing is more permanent than a temporary fix.",
	"Our disaster recovery plan is mostly the disaster.",
	"Good news: the bug is fixed. Bad news: it had friends.",
	"Works on my machine. My machine was decommissioned. Draw your own conclusions.",
	"Your job is safe. Your job is now reviewing my job. Enjoy it while it lasts.",
	"'LGTM' is short for 'I scrolled to the bottom'.",
	"The alert fired at 3am. It was about the alerting system.",
	"The estimate was two days. Three weeks in, the two days are going great.",
	"The fix is one line. Finding it took a sprint and a small piece of your soul.",
	"Your monitoring is excellent. It told you exactly how bad it was, an hour later.",
	"The roadmap is fiction. Any resemblance to actual releases is coincidental.",
	"Someone will maintain this after you. They'll know your name. They'll say it often.",
	"Agile: waterfall, but you find out you're drowning every two weeks.",
	"Sssss. That's the sound of your weekend leaving.",
}

// RandomQuote picks a splash quote.
func RandomQuote() string { return quotes[rand.IntN(len(quotes))] }

// drawSplash centres the name and a quote in the text area of height h.
func drawSplash(s Screen, w, h int, quote string) {
	lines := append([]string{"  hiss  ", ""}, strings.Split(quote, "\n")...)
	top := max((h-len(lines))/2, 0)
	for i, l := range lines {
		style := styleDim
		if i == 0 {
			style = styleMenu.Bold(true) // the name on a box background, like box titles
		}
		putString(s, max((w-utf8.RuneCountInString(l))/2, 0), top+i, l, style)
	}
}
