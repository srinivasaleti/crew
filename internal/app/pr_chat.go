package app

import (
	"crew/internal/tasks"
)

// A chat about a listed PR: the PR's Chat pane. It's a task underneath (a task file with pr: set,
// kept in m.chats, never in the task list), so it gets everything a task's chat has: its own
// Claude session, the chat log, /compact, /new, /model, CREW_AUTO_COMPACT, usage, stopping and
// steering mid-turn. Claude works in a checkout of the PR when its repo is attached here, else in
// an empty folder with gh. The file is written with your first message.

// chatTask is the task whose chat is on screen: the selected task's, or the selected PR's on its
// Chat pane.
func (m Model) chatTask() *tasks.Task {
	if t := m.current(); t != nil {
		return t
	}
	if s, r, ok := m.page(); ok {
		if t := s.Chat(&m, r); t != nil {
			return t
		}
	}
	return nil
}

// chatOpen reports whether a chat with its message box is on screen.
func (m Model) chatOpen() bool {
	if s, r, ok := m.page(); ok && s.ChatOpen(&m, r) {
		return true
	}
	return m.current() != nil && m.tab == tabChat
}

// ensureChatHere makes the chat of the section item on screen (a PR's, REQUESTS'…), for a
// message sent after a reload dropped it.
func (m *Model) ensureChatHere() *tasks.Task {
	if s, r, ok := m.page(); ok {
		if t := s.EnsureChat(m, r); t != nil {
			return t
		}
	}
	return m.prAgent(true)
}

// prAgent is the task whose Claude works on the PR on screen: the selected task, or the chat of
// the PR on a section's page (made with create, else nil until there is one).
func (m *Model) prAgent(create bool) *tasks.Task {
	if t := m.current(); t != nil {
		return t
	}
	if p, r, ok := m.prPage(); ok {
		return p.PRAgent(m, r, create)
	}
	return nil
}

// prStateKey is the key of the PR state (m.prs) a task's agent answers questions for.
func prStateKey(t tasks.Task) string {
	if t.PR != "" {
		return "pr:" + t.PR
	}
	return t.ID
}

// chatMeta is a section chat's cost, context and state, for its header.
func (m Model) chatMeta(t *tasks.Task) string {
	if t == nil {
		return ""
	}
	s := ""
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		s = dim.Render(" · chat "+cost(u.Cost)+" · ") + contextLabel(u.Context)
	}
	if m.chat.Conv(t.ID).Busy {
		s += running.Render("  ● working…")
	} else if m.unread[t.ID] {
		s += ready.Render("  ◐ reply ready")
	}
	return s
}
