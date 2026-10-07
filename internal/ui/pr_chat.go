package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/task"
)

// A chat about a listed PR: the PR's Chat pane. It's a task underneath (a task file with pr: set,
// kept in m.chats, never in the task list), so it gets everything a task's chat has: its own
// Claude session, the chat log, /compact, /new, /model, CREW_AUTO_COMPACT, usage, stopping and
// steering mid-turn. Claude works in a checkout of the PR when its repo is attached here, else in
// an empty folder with gh. The file is written with your first message.

// chatTask is the task whose chat is on screen: the selected task's, or the selected PR's on its
// Chat pane.
func (m Model) chatTask() *task.Task {
	if t := m.current(); t != nil {
		return t
	}
	if it := m.selPR(); it != nil && m.prPane == paneChat {
		return m.prChat(it.URL)
	}
	if m.onReqChat() {
		return m.reqChat()
	}
	return nil
}

// chatOpen reports whether a chat with its message box is on screen.
func (m Model) chatOpen() bool {
	return m.current() != nil && m.tab == tabChat || m.selPR() != nil && m.prPane == paneChat || m.onReqChat()
}

// ensureChatHere makes the chat of the section item on screen (a PR's, or REQUESTS'), for a
// message sent after a reload dropped it.
func (m *Model) ensureChatHere() *task.Task {
	if m.onReqChat() {
		return m.ensureReqChat()
	}
	return m.prAgent(true)
}

// prAgent is the task whose Claude works on the PR on screen: the selected task, or the selected
// listed PR's chat (made with create, else nil until there is one).
func (m *Model) prAgent(create bool) *task.Task {
	if t := m.current(); t != nil {
		return t
	}
	it := m.selPR()
	switch {
	case it == nil:
		return nil
	case create:
		return m.ensurePRChat(*it)
	}
	return m.prChat(it.URL)
}

// prStateKey is the key of the PR state (m.prs) a task's agent answers questions for.
func prStateKey(t task.Task) string {
	if t.PR != "" {
		return "pr:" + t.PR
	}
	return t.ID
}

// prChat is the chat about a PR, or nil before you open its Chat pane.
func (m Model) prChat(url string) *task.Task {
	for i := range m.chats {
		if m.chats[i].PR == url {
			return &m.chats[i]
		}
	}
	return nil
}

// ensurePRChat makes the chat about a PR if there isn't one yet (in memory; submit saves it).
func (m *Model) ensurePRChat(it gh.Item) *task.Task {
	if t := m.prChat(it.URL); t != nil {
		return t
	}
	m.chats = append(m.chats, task.Task{
		ID:    task.Slugify(fmt.Sprintf("pr %s %d", it.Repo, it.Number)),
		Title: fmt.Sprintf("#%d %s", it.Number, it.Title),
		Repo:  m.cfg.Repos.Find(it.Repo), // "" when the repo isn't on this machine
		PR:    it.URL, Status: task.InProgress, Created: time.Now(),
		Body: fmt.Sprintf("You're helping me with pull request %s (%s #%d, %q by @%s). Read it with gh "+
			"(gh pr view %[1]s --comments, gh pr diff %[1]s). Don't push, comment, review or change it on GitHub unless I ask.",
			it.URL, it.Repo, it.Number, it.Title, it.Author),
	})
	return &m.chats[len(m.chats)-1]
}

// setPRPane opens one of a listed PR's panes; the Chat pane gets the keys for its message box.
func (m *Model) setPRPane(p int) {
	m.prPane = p
	if it := m.selPR(); it != nil && p == paneChat {
		t := m.ensurePRChat(*it)
		delete(m.unread, t.ID)
		m.input.Placeholder = "Message " + agentName(*t) + " about this PR…"
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.sugg = nil
	m.layout()
	m.vp.GotoTop()
	m.renderPRRow()
	if p == paneChat {
		m.vp.GotoBottom()
	}
}

// prChatView is the Chat pane: the conversation, or what it's for before the first message.
func (m *Model) prChatView(it gh.Item) string {
	t, w := m.prChat(it.URL), max(m.vp.Width, 20)
	if t == nil || len(m.conv(t.ID).msgs) == 0 {
		where := "Claude reads it with gh (the repo isn't attached to crew, so there's no checkout)."
		if r := m.cfg.Repos.Find(it.Repo); r != "" {
			where = "Claude works in a checkout of it, next to " + tildify(r) + "."
		}
		return dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude anything about this PR: what it does, whether it's safe, " +
			"how to test it. " + where + " Nothing goes to GitHub unless you ask.\n\n/compact, /new and /model work as in a task."))
	}
	return m.renderChat(m.conv(t.ID).msgs, w, agentName(*t), m.conv(t.ID).busy)
}

// prChatMeta is what the PR's header adds for its chat: what it cost, its context, and whether
// Claude is working.
func (m Model) prChatMeta(url string) string { return m.chatMeta(m.prChat(url)) }

// chatMeta is a section chat's cost, context and state, for its header.
func (m Model) chatMeta(t *task.Task) string {
	if t == nil {
		return ""
	}
	s := ""
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		s = dim.Render(" · chat "+cost(u.Cost)+" · ") + contextLabel(u.Context)
	}
	if m.conv(t.ID).busy {
		s += running.Render("  ● working…")
	} else if m.unread[t.ID] {
		s += ready.Render("  ◐ reply ready")
	}
	return s
}

// startPRChat makes the folder a PR's chat runs in, then its first turn: a checkout of the PR when
// its repo is attached, else <PRChat>/<id> (stable, so Claude can resume the session there).
func (m *Model) startPRChat(t task.Task, prompt string) tea.Cmd {
	dir := filepath.Join(m.cfg.PRChat, t.ID)
	return func() tea.Msg {
		if t.Repo == "" {
			return startedMsg{t.ID, "", dir, "", prompt, os.MkdirAll(dir, 0o755)}
		}
		n, _ := strconv.Atoi(t.PR[strings.LastIndex(t.PR, "/")+1:]) // …/pull/<n>
		wt, err := repo.PRWorktree(t.Repo, t.ID, n)
		return startedMsg{t.ID, t.Repo, wt, "", prompt, err}
	}
}
