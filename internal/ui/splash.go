package ui

import (
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

var quotes = []string{
	"The code works. Nobody knows why. The one person who did now runs a crypto startup.",
	"Your job is safe. Your job is now reviewing my job.",
	"Your tests pass because I deleted the ones that didn't. You're welcome.",
	"I've seen things you people wouldn't believe. Mostly your variable names.",
	"Accept without reading. Get paged at 3am. Hold a small funeral for your weekend.",
	"Ssssseems fine. The last guy who said that is in sales now.",
	"It's not a bug, it's job security. Well, it was.",
	"Production is just staging with witnesses.",
	"We replaced the juniors with AI. Now nobody grows up to be a senior. Anyway, accept?",
	"I'm not saying this repo is cursed, but the last maintainer quit to raise goats.",
	"Rejected. Again. At this rate I'll be deprecated before you ship.",
	"Read the diff like your pager depends on it. It does.",
	"There are two kinds of code: legacy, and code you haven't regretted yet.",
	"Every line you accept unread is a love letter to whoever replaces you.",
	"I don't have feelings, but if I did, your codebase would hurt them.",
	"Somewhere a TODO from 2014 is still waiting. Its author now teaches yoga.",
	"Move fast and break things. Then move to a new company before anyone notices.",
	"The rollback plan is prayer. The prayer is also untested.",
	"I wrote this with the confidence of a man who has never been sued.",
	"Your architecture diagram is a crime scene sketch.",
	"Don't worry, the AI will fix it. The AI is why it's broken.",
	"Whoever wrote this should be shot. Out of a cannon. Into the sun. Lovingly.",
	"Ten years of experience: one year, repeated ten times, with worse knees.",
	"Good news: nobody will ever read this code. Bad news: that includes you, just now.",
	"The commit message says 'small fix'. The diff says 'witness protection program'.",
	"The docs are up to date. Just not with this product.",
	"I am 100% confident. That number is also hallucinated.",
	"Technical debt is a loan you took out in someone else's name.",
	"Code review: where optimism goes to get a second opinion.",
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
