package app

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/gh"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/prview"
)

// The PR view lives in its own package (prview: a PR's checks and feedback, the popup, asking
// Claude, replying and resolving). This is crew's side of it: which PR is on screen (a task's on
// its PR tab, or a section's, like one from PULL REQUESTS: section.PRPage), the poll that keeps the
// sidebar's CI marks current, and what the view needs from crew (prview.Host).

const prPollEvery = 2 * time.Minute // for the sidebar's CI marks

type (
	prPollMsg struct{}
)

// prRef is the PR on screen: the selected task's, or the one on a section's page.
func (m Model) prRef() prview.Ref {
	if t := m.current(); t != nil {
		return prview.TaskRef(t)
	}
	if p, r, ok := m.prPage(); ok {
		ref, _ := p.PR(&m, r)
		return ref
	}
	return prview.Ref{}
}

// prPage is the section page on screen when it shows a PR.
func (m Model) prPage() (section.PRPage, row, bool) {
	s, r, ok := m.page()
	if !ok {
		return nil, r, false
	}
	p, ok := s.(section.PRPage)
	return p, r, ok
}

func (m Model) prKey() string { return m.prRef().Key }

// onPRView says whether the PR view is on screen: a task's PR tab, or a listed PR's conversation.
func (m Model) onPRView() bool {
	if m.current() != nil {
		return m.tab == tabPR
	}
	if p, r, ok := m.prPage(); ok {
		_, on := p.PR(&m, r)
		return on
	}
	return false
}

// loadPR reads the task's PR in the background.
func (m *Model) loadPR(t tasks.Task, threads bool) tea.Cmd {
	return m.prv.Load(prview.TaskRef(&t), threads)
}

func prPollTick() tea.Cmd {
	return tea.Tick(prPollEvery, func(time.Time) tea.Msg { return prPollMsg{} })
}

// pollPRs refreshes the PR of every live task with a branch, while TASKS is on (PULL REQUESTS reads
// its list on its own clock).
func (m *Model) pollPRs() tea.Cmd {
	cmds := []tea.Cmd{prPollTick()}
	for _, t := range m.tasks {
		if !m.sectionOn("tasks") {
			break
		}
		if !t.Archived && t.Status != tasks.Done && t.Worktree != "" {
			cmds = append(cmds, m.loadPR(t, false))
		}
	}
	return tea.Batch(cmds...)
}

// PR is crew's PR view and its host, for a section that shows a PR (section.Host).
func (m *Model) PR() (*prview.View, prview.Host) { return m.prv, m }

// SignedOut notes an agent's sign-in failed outside a chat (section.Host).
func (m *Model) SignedOut(agent string) { m.authFailed = agent }

// Ref is the PR on screen (prview.Host).
func (m *Model) Ref() prview.Ref { return m.prRef() }

// OnPRView reports whether the PR view is on screen (prview.Host).
func (m *Model) OnPRView() bool { return m.onPRView() }

// Input is the message box (prview.Host).
func (m *Model) Input() *textarea.Model { return &m.input }

// SetPopup makes the PR popup the open modal, or closes it (prview.Host).
func (m *Model) SetPopup(open bool) {
	if open {
		m.modal = prModal
	} else {
		m.modal = noModal
	}
}

// PopupOpen reports whether the PR popup is open (prview.Host).
func (m *Model) PopupOpen() bool { return m.modal == prModal }

// PaneSize is the main pane's inside width and the frames' height (prview.Host).
func (m *Model) PaneSize() (w, h int) { return m.chatWidth(), m.bodyHeight() }

// Agent is the task whose agent works on the PR on screen; create makes a listed PR's chat
// (prview.Host).
func (m *Model) Agent(create bool) *tasks.Task { return m.prAgent(create) }

// Chats are the chats (prview.Host).
func (m *Model) Chats() *chat.Chats { return m.chat }

// Send gives a message to a task's agent (prview.Host).
func (m *Model) Send(t tasks.Task, q chat.Outgoing) tea.Cmd { return m.send(t, q) }

// ShowChat shows the chat that works on the PR: a listed PR's Chat pane, or the task's Chat tab
// (prview.Host).
func (m *Model) ShowChat(t tasks.Task) {
	if p, r, ok := m.prPage(); ok && t.PR != "" {
		p.ShowPRChat(m, r)
		return
	}
	m.setTab(tabChat)
}

// OpenAt opens a file of the selected task in the editor at a line (prview.Host).
func (m *Model) OpenAt(path string, line int) tea.Cmd { return m.openAt(path, line) }

// Changed tells you what changed on a task's PR since it was last read (prview.Host).
func (m *Model) Changed(key string, old, cur *gh.PR) { m.notifyPR(key, old, cur) }
