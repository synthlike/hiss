package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// :pr-submit sends the drafts as one review of the commit :pr checked out,
// with an optional verdict and a word for the whole review. It publishes, so
// it asks first, and it checks whether the author has pushed since: the
// comments still pin to the reviewed commit, but may be outdated on GitHub.

// reviewEvent is a verdict :pr-submit can send.
type reviewEvent struct{ name, api, done string }

var reviewEvents = []reviewEvent{
	{"comment", "COMMENT", "commented"},
	{"approve", "APPROVE", "approved"},
	{"changes", "REQUEST_CHANGES", "requested changes"},
}

// ghReview is a review in the form GitHub's API takes.
type ghReview struct {
	CommitID string      `json:"commit_id"`
	Event    string      `json:"event"`
	Body     string      `json:"body,omitempty"`
	Comments []ghComment `json:"comments,omitempty"`
}

// submitPR checks the PR's head, then asks before sending the review (:prs).
func (e *Editor) submitPR(arg string) {
	p := e.pr
	ev, body := reviewEvents[0], strings.TrimSpace(arg)
	word, rest, _ := strings.Cut(body, " ")
	for _, x := range reviewEvents {
		if word == x.name {
			ev, body = x, strings.TrimSpace(rest)
		}
	}
	switch {
	case p == nil:
		e.Msg = "pr-submit: no PR checked out here"
		return
	case e.prBusy:
		e.Msg = "pr-submit: still busy with GitHub; try again in a moment"
		return
	case ev.api == "REQUEST_CHANGES" && body == "":
		e.Msg = "pr-submit: say what to change: :prs changes <text>"
		return
	case ev.api == "COMMENT" && body == "" && len(p.Drafts) == 0:
		e.Msg = "pr-submit: nothing to send: draft comments with :gc, or :prs <text>"
		return
	}
	r := &ghReview{CommitID: p.Head, Event: ev.api, Body: body, Comments: slices.Clone(p.Drafts)}
	e.prBusy = true
	e.Msg = fmt.Sprintf("pr-submit: checking #%d for new pushes…", p.N)
	root := e.Root
	go func() {
		head, err := prHead(root, p.N)
		e.Post(func(e *Editor) { e.confirmSubmit(p, ev, r, head, err) })
	}()
}

// prHead asks gh for PR n's head commit.
func prHead(root string, n int) (string, error) {
	out, err := runTool(root, "gh", "pr", "view", strconv.Itoa(n), "--json", "headRefOid")
	if err != nil {
		return "", err
	}
	var v prView
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.HeadRefOid == "" {
		return "", errors.New("gh pr view: unexpected output")
	}
	return v.HeadRefOid, nil
}

// confirmSubmit asks whether to send r, saying if the PR has moved on.
func (e *Editor) confirmSubmit(p *pullRequest, ev reviewEvent, r *ghReview, head string, err error) {
	if err != nil {
		e.prBusy = false
		e.Msg = failed(fmt.Sprintf("pr-submit: checking #%d", p.N), err)
		return
	}
	what := ev.name + ", " + plural(len(r.Comments), "comment")
	if r.Body != "" {
		what += ", with a note"
	}
	title := fmt.Sprintf("send the review to #%d: %s?", p.N, what)
	if head != p.Head {
		title = fmt.Sprintf("#%d has new pushes since %s, which you reviewed; send anyway: %s?",
			p.N, shortRev(p.Head), what)
	}
	e.Menu = &Menu{Title: title, Items: []MenuItem{
		{"y", "send it", func(e *Editor) { e.sendReview(p, ev, r) }},
	}, Else: func(e *Editor, _ string) {
		e.prBusy = false
		e.Msg = "pr-submit: not sent; the drafts are kept"
	}}
}

// sendReview posts r in the background. Sent drafts are dropped; ones that
// fail stay for another try.
func (e *Editor) sendReview(p *pullRequest, ev reviewEvent, r *ghReview) {
	e.Msg = fmt.Sprintf("pr-submit: sending the review to #%d…", p.N)
	root := e.Root
	go func() {
		url, err := postReview(root, p.URL, p.N, r)
		e.Post(func(e *Editor) {
			e.prBusy = false
			if err != nil {
				e.Msg = failed(fmt.Sprintf("pr-submit: sending the review to #%d", p.N), err) + "; the drafts are kept"
				return
			}
			p.Drafts = slices.DeleteFunc(p.Drafts, func(c ghComment) bool { return slices.Contains(r.Comments, c) })
			e.Msg = fmt.Sprintf("%s on #%d with %s: %s", ev.done, p.N, plural(len(r.Comments), "comment"), url)
			if err := e.saveDrafts(p); err != nil {
				e.Msg += "; saving drafts failed: " + err.Error()
			}
		})
	}()
}

// postReview sends r to PR n of the repository prURL is in, and returns the
// review's URL.
func postReview(root, prURL string, n int, r *ghReview) (string, error) {
	repo, err := prRepo(prURL)
	if err != nil {
		return "", err
	}
	host, name, _ := strings.Cut(repo, "/")
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	args := []string{"api", "--method", "POST", fmt.Sprintf("repos/%s/pulls/%d/reviews", name, n), "--input", "-"}
	if host != "github.com" {
		args = append(args, "--hostname", host)
	}
	out, err := runToolInput(root, data, "gh", args...)
	if err != nil {
		return "", err
	}
	var res struct {
		URL string `json:"html_url"`
	}
	json.Unmarshal([]byte(out), &res)
	return res.URL, nil
}
