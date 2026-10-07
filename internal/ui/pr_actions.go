package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/edit"
	"crew/internal/gh"
	"crew/internal/task"
)

func openURL(u string) {
	if u != "" {
		if c := exec.Command("open", u); c.Start() == nil {
			go c.Wait()
		}
	}
}

// askAboutPR sends your question with the row's feedback to the agent; the answer shows in the popup.
func (m *Model) askAboutPR(it prItem, question string) tea.Cmd {
	t, st := m.current(), m.prOf(m.prKey())
	if t == nil {
		m.notice = "Claude works on a PR through its task · v reviews a listed PR"
		return nil
	}
	if m.conv(t.ID).busy {
		m.notice = "Claude stops its current step and takes your question"
	}
	text := fmt.Sprintf("About %s on PR #%d:\n\n%s\n\n%s", it.about, st.pr.Number, it.context, question)
	cmd := m.send(*t, outgoing{text: text, talkKey: it.key, asked: question})
	m.renderPR()
	return cmd
}

// askAllPR sends every failing check, open line comment and change request to the agent at once,
// and shows the chat.
func (m *Model) askAllPR() tea.Cmd {
	st := m.prOf(m.prKey())
	if st.pr == nil {
		return nil
	}
	var asks []string
	for _, it := range m.prItems(st.pr) {
		if !it.head && needsYou(it) && it.context != "" && (it.thread == nil || !it.thread.Outdated) {
			asks = append(asks, fmt.Sprintf("%d. %s", len(asks)+1, it.context))
		}
	}
	if len(asks) == 0 {
		m.notice = "Nothing waiting on you: no failing checks, open comments or change requests"
		return nil
	}
	t := m.current()
	if t == nil {
		m.notice = "Claude works on a PR through its task · v reviews a listed PR"
		return nil
	}
	text := fmt.Sprintf("Please work through this feedback on PR #%d, then summarise what you changed:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
	m.setTab(tabChat)
	return m.send(*t, outgoing{text: text})
}

// replyPR posts your reply on GitHub. A line comment gets it in its thread. GitHub has no replies
// for reviews and conversation comments, so those get a new PR comment that quotes them.
func (m *Model) replyPR(ref prRef, it prItem, text string) tea.Cmd {
	st := m.prOf(ref.key)
	dir, prURL, id := ref.dir, st.pr.URL, ref.key
	m.notice = "Posting your reply…"
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
	return func() tea.Msg { return prPostedMsg{id, "Reply posted", post()} }
}

// toggleResolved resolves or reopens a line-comment thread, showing the change right away.
func (m *Model) toggleResolved(ref prRef, th *gh.Thread) tea.Cmd {
	st := m.prOf(ref.key)
	resolve := !th.Resolved
	for i := range st.pr.Threads {
		if st.pr.Threads[i].ID == th.ID {
			st.pr.Threads[i].Resolved = resolve
		}
	}
	m.renderPR()
	done := "Resolved"
	if !resolve {
		done = "Reopened"
	}
	dir, prURL, id, threadID := ref.dir, st.pr.URL, ref.key, th.ID
	return func() tea.Msg { return prPostedMsg{id, done, gh.SetResolved(dir, prURL, threadID, resolve)} }
}

func (m *Model) readLog(ref prRef, c *gh.Check) tea.Cmd {
	m.notice = "Reading the failed log…"
	id, dir, name, u := ref.key, ref.dir, c.Name, c.URL
	return func() tea.Msg {
		log, err := gh.FailedLog(dir, u)
		return prLogMsg{id, name, log, err}
	}
}

// createPR pushes the branch and opens a PR, on the second P.
func (m *Model) createPR(t task.Task) tea.Cmd {
	st := m.prOf(t.ID)
	switch {
	case t.Worktree == "" || !gh.Installed():
		m.notice = "Needs the task's branch and the GitHub CLI (make setup)"
	case st.pr != nil && st.pr.State == "OPEN":
		m.notice = "This branch already has an open PR"
	case !st.armed:
		st.armed = true
		m.notice = "Press P again to push " + taskBranch(t.Worktree, t.ID) + " to origin and open a PR"
	default:
		st.armed, st.loading = false, true
		m.notice = "Pushing and opening a PR…"
		id, dir := t.ID, t.Worktree
		return func() tea.Msg {
			u, err := gh.Create(dir, taskBranch(dir, id))
			return prCreatedMsg{id, u, err}
		}
	}
	return nil
}

// openAt opens a file of the task in the editor at a line.
func (m *Model) openAt(path string, line int) tea.Cmd {
	if m.current() == nil {
		m.notice = "The code is in a task's worktree · o opens the comment on GitHub"
		return nil
	}
	cmd := m.setTab(tabFiles)
	m.openInEditor(path)
	if ed := m.editors[m.currentID()]; ed != nil && ed.doc().path == path {
		ed.doc().buf.MoveTo(edit.Pos{Row: max(line-1, 0)}, false)
		ed.center()
	}
	return cmd
}
