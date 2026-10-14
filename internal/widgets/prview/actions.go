package prview

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/gh"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/markdown"
)

// askAbout sends your question with the row's feedback to the agent; the answer shows in the popup.
func (v *View) askAbout(h Host, it prItem, question string) tea.Cmd {
	t, st := h.Agent(true), v.Of(h.Ref().Key)
	if t == nil {
		return nil
	}
	if h.Chats().Conv(t.ID).Busy {
		h.Notice("Claude stops its current step and takes your question")
	}
	text := fmt.Sprintf("About %s on PR #%d:\n\n%s\n\n%s", it.about, st.pr.Number, it.context, question)
	cmd := h.Send(*t, chat.Outgoing{Text: text, TalkKey: it.key, Asked: question})
	v.Render(h)
	return cmd
}

// askAll sends every failing check, open line comment and change request to the agent at once,
// and shows the chat.
func (v *View) askAll(h Host) tea.Cmd {
	st := v.Of(h.Ref().Key)
	if st.pr == nil {
		return nil
	}
	var asks []string
	for _, it := range v.items(h, st.pr) {
		if !it.head && needsYou(it) && it.context != "" && (it.thread == nil || !it.thread.Outdated) {
			asks = append(asks, fmt.Sprintf("%d. %s", len(asks)+1, it.context))
		}
	}
	if len(asks) == 0 {
		h.Notice("Nothing waiting on you: no failing checks, open comments or change requests")
		return nil
	}
	t := h.Agent(true)
	if t == nil {
		return nil
	}
	text := fmt.Sprintf("Please work through this feedback on PR #%d, then summarise what you changed:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
	if t.PR != "" {
		text = fmt.Sprintf("Please go through this feedback on PR #%d: what each asks for and how you'd address it:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
		cmd := h.Send(*t, chat.Outgoing{Text: text})
		h.ShowChat(*t)
		return cmd
	}
	h.ShowChat(*t)
	return h.Send(*t, chat.Outgoing{Text: text})
}

// reply posts your reply on GitHub. A line comment gets it in its thread. GitHub has no replies
// for reviews and conversation comments, so those get a new PR comment that quotes them.
func (v *View) reply(h Host, ref Ref, it prItem, text string) tea.Cmd {
	st := v.Of(ref.Key)
	dir, prURL, id := ref.Dir, st.pr.URL, ref.Key
	h.Notice("Posting your reply…")
	var post func() error
	switch {
	case it.thread != nil:
		threadID := it.thread.ID
		post = func() error { return gh.ReplyThread(dir, prURL, threadID, text) }
	case it.review != nil:
		body := quote(it.review.Comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	default:
		body := quote(*it.comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	}
	return func() tea.Msg { return PostedMsg{id, "Reply posted", post()} }
}

// toggleResolved resolves or reopens a line-comment thread, showing the change right away.
func (v *View) toggleResolved(h Host, ref Ref, th *gh.Thread) tea.Cmd {
	st := v.Of(ref.Key)
	resolve := !th.Resolved
	for i := range st.pr.Threads {
		if st.pr.Threads[i].ID == th.ID {
			st.pr.Threads[i].Resolved = resolve
		}
	}
	v.Render(h)
	done := "Resolved"
	if !resolve {
		done = "Reopened"
	}
	dir, prURL, id, threadID := ref.Dir, st.pr.URL, ref.Key, th.ID
	return func() tea.Msg { return PostedMsg{id, done, gh.SetResolved(dir, prURL, threadID, resolve)} }
}

func (v *View) readLog(h Host, ref Ref, c *gh.Check) tea.Cmd {
	h.Notice("Reading the failed log…")
	id, dir, name, u := ref.Key, ref.Dir, c.Name, c.URL
	return func() tea.Msg {
		log, err := gh.FailedLog(dir, u)
		return LogMsg{id, name, log, err}
	}
}

// create pushes the branch and opens a PR, on the second P.
func (v *View) create(h Host, t tasks.Task) tea.Cmd {
	st := v.Of(t.ID)
	switch {
	case t.Worktree == "" || !gh.Installed():
		h.Notice("Needs the task's branch and the GitHub CLI (make setup)")
	case st.pr != nil && st.pr.State == "OPEN":
		h.Notice("This branch already has an open PR")
	case !st.armed:
		st.armed = true
		h.Notice("Press P again to push " + taskBranch(t.Worktree, t.ID) + " to origin and open a PR")
	default:
		st.armed, st.loading = false, true
		h.Notice("Pushing and opening a PR…")
		id, dir := t.ID, t.Worktree
		return func() tea.Msg {
			u, err := gh.Create(dir, taskBranch(dir, id))
			return CreatedMsg{id, u, err}
		}
	}
	return nil
}

// quote is a comment as a Markdown quote, for Claude and for replies that quote it.
func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(markdown.Slim(c.Body), "\n", "\n> ")
}
