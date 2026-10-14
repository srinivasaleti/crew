package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// submit sends the typed message to the agent of the chat on screen (a task's, or a PR's).
func (m *Model) submit() tea.Cmd {
	t := m.chatTask()
	if t == nil && m.chatOpen() {
		t = m.ensureChatHere() // a section's chat a reload dropped before its first message
	}
	text := strings.TrimSpace(m.input.Value())
	if t != nil && text == "" && m.chat.Conv(t.ID).Retry() != nil && m.agentReady(chat.AgentName(*t)) {
		q := m.chat.Conv(t.ID).TakeRetry() // the message that failed while its agent was signed out
		defer m.vp.GotoBottom()
		return m.send(*t, q)
	}
	if t != nil && text == "" && m.chat.Conv(t.ID).Retry() != nil {
		m.notice = "Sign in first · ⌘L signs in, then ⏎ sends your message again"
		return nil
	}
	if t == nil || text == "" {
		return nil
	}
	if answers, ok := chat.PickedAnswers(m.chatQuestions(), text); ok {
		text = answers // just the letters: the options they pick
	}
	cmd := strings.HasPrefix(text, "/")
	m.input.Reset()
	m.argHint = ""
	m.layout()
	defer m.vp.GotoBottom() // what you send shows, even if you'd scrolled up
	if cmd {
		m.command(t, text) // crew commands are handled here; /compact is passed on to the agent
		next := m.pending
		m.pending = nil
		return next
	}
	send := m.send(*t, chat.Outgoing{Text: text})
	m.layout() // Claude's question buttons go once you've answered
	return send
}

// modelName is the model a task's next message will use: the one chosen with /model, else the
// one Claude last answered with, else the CLI default.
func (m Model) modelName(t tasks.Task) string {
	switch {
	case t.Model != "":
		return t.Model
	case m.usage[t.ID].LastModel != "":
		return m.usage[t.ID].LastModel + " (default)"
	default:
		return chat.AgentName(t) + "'s default"
	}
}

type spinMsg struct{}

// startSpin starts the working spinner's clock if it isn't running.
func (m *Model) startSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick()
}

func spinTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

// onSpin moves the spinner on while any agent works or the PR list loads, and stops its clock when
// nothing does.
func (m *Model) onSpin() tea.Cmd {
	m.spin++
	if rs := m.review(); rs != nil && rs.running {
		m.renderPRRow()
	}
	for _, rs := range m.reviews {
		if rs.running {
			return spinTick()
		}
	}
	for _, c := range m.chat.All() {
		if c.Busy {
			if m.chat.Conv(m.currentID()).Busy {
				m.refresh()
			}
			return spinTick()
		}
	}
	secBusy := false
	for _, s := range m.secs {
		if s.Spin(m) {
			secBusy = true
		}
	}
	if m.prListLoading || m.reqv.Busy() || secBusy {
		if m.onReqsPage() {
			m.refresh() // a request's "sending…" shows in its row; the popup redraws on its own
		}
		return spinTick() // the sidebar redraws on every message
	}
	m.spinning = false
	return nil
}
