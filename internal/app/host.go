package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/section"
	"crew/internal/setup"
	"crew/internal/tasks"
)

// Sections that are plugins (the section package) live in m.secs. crew asks the section whose
// row is selected for its page; this is the way there, and what crew gives a section
// (section.Host).

// secOf is the section with this key, or nil.
func (m Model) secOf(key string) section.Section {
	for _, s := range m.secs {
		if s.Def().Key == key {
			return s
		}
	}
	return nil
}

// owner is the section a row is in, when it's a plugin.
func (m Model) owner(r row) section.Section { return m.secOf(r.Section) }

// page is the selected row's section when the section draws the main pane itself (anything but a
// table, which crew draws).
func (m Model) page() (section.Section, row, bool) {
	r := m.selRow()
	s := m.owner(r)
	if s == nil {
		return nil, r, false
	}
	if s.ShowsTable(&m, r) {
		return nil, r, false
	}
	return s, r, true
}

// secRows are a section's rows, while it's turned on.
func (m Model) secRows(key string) []row {
	s := m.secOf(key)
	if s == nil || !m.sectionOn(key) {
		return nil
	}
	return s.Rows(&m)
}

// Ctx is cancelled when crew quits (section.Host).
func (m *Model) Ctx() context.Context { return m.cfg.Ctx }

// SectionOn reports whether a section is turned on (section.Host).
func (m *Model) SectionOn(key string) bool { return m.sectionOn(key) }

// GoUp goes back to the list the selection came from (section.Host).
func (m *Model) GoUp() { m.goUp() }

// Select selects a sidebar row by its key and shows it (section.Host).
func (m *Model) Select(key string) {
	m.selKey, m.tab = key, tabChat
	m.onSelect()
}

// SelKey is the selected row's key (section.Host).
func (m *Model) SelKey() string { return m.selKey }

// SelectedRow is the selected row (section.Host).
func (m *Model) SelectedRow() row { return m.selRow() }

// ListFocused reports whether the sidebar has the keys (section.Host).
func (m *Model) ListFocused() bool { return m.listFocus }

// ItemLines draws an item's two sidebar lines (section.Host).
func (m *Model) ItemLines(r row, mark, title, second string, selected bool) string {
	return m.itemLines(r, mark, title, second, selected)
}

// SpinFrame is the working spinner's frame (section.Host).
func (m *Model) SpinFrame() int { return m.spin }

// ChatKey hands a key to the message box, as in a task's chat (section.Host).
func (m *Model) ChatKey(msg tea.KeyMsg) tea.Cmd {
	nm, cmd := m.updateChat(msg)
	*m = nm.(Model)
	return cmd
}

// ChatTyping reports whether something is typed in the message box or its suggestions are open
// (section.Host).
func (m *Model) ChatTyping() bool { return m.input.Value() != "" || len(m.sugg) > 0 }

// ClearSuggestions closes the message box's suggestions (section.Host).
func (m *Model) ClearSuggestions() { m.sugg = nil }

// ChatBox is the message box with its suggestions above it (section.Host).
func (m *Model) ChatBox() string { return m.suggestionsView() + m.input.View() }

// ChatMeta is a section chat's cost, context and state (section.Host).
func (m *Model) ChatMeta(t *tasks.Task) string { return m.chatMeta(t) }

// FindChat is the section chat that matches, or nil (section.Host).
func (m *Model) FindChat(match func(tasks.Task) bool) *tasks.Task {
	for i := range m.chats {
		if match(m.chats[i]) {
			return &m.chats[i]
		}
	}
	return nil
}

// AddChat adds a section's chat, in memory until its first message saves it (section.Host).
func (m *Model) AddChat(t tasks.Task) *tasks.Task {
	m.chats = append(m.chats, t)
	return &m.chats[len(m.chats)-1]
}

// MarkRead drops a chat's "reply ready" mark (section.Host).
func (m *Model) MarkRead(id string) { delete(m.unread, id) }

// Tasks are all the tasks, archived included (section.Host).
func (m *Model) Tasks() []tasks.Task { return m.tasks }

// WorkOn opens the task working on an issue, or the new-task form filled in from it the first
// time (section.Host).
func (m *Model) WorkOn(title, issue, brief string) tea.Cmd {
	if !m.sectionOn("tasks") {
		m.notice = "Working on an issue makes a task, and TASKS is off · t on home turns it on"
		return nil
	}
	for _, t := range m.tasks {
		if t.Issue == issue {
			return openTask(m, t.ID, "")
		}
	}
	cmd := m.openNew()
	m.newTask.From(title, issue, brief)
	return cmd
}

// Setup is what's installed and signed in, as last checked (section.Host).
func (m *Model) Setup() setup.Status { return m.setup }

// SignIn signs in to a tool in a shell (section.Host).
func (m *Model) SignIn(t setup.Tool) tea.Cmd { return m.signIn(t) }

// RunFix runs an install or sign-in command in a shell (section.Host).
func (m *Model) RunFix(command, hint string) tea.Cmd { return m.runFix(command, hint) }

// ownerClick hands a click in the main pane to the selected row's section, reporting whether it
// took it; what it starts is left in m.pending.
func (m *Model) ownerClick(x, y int) bool {
	r := m.selRow()
	s := m.owner(r)
	if s == nil {
		return false
	}
	cmd, ok := s.Click(m, r, x, y)
	if ok {
		m.pending = cmd
	}
	return ok
}

// ownerTabs reports whether the selected row's section shows tabs.
func (m Model) ownerTabs() bool {
	r := m.selRow()
	s := m.owner(r)
	if s == nil {
		return false
	}
	_, n := s.TabsHere(&m, r)
	return n > 0
}

// pageFocus names who has the keys on a section's own page, or "" when it isn't one.
func (m Model) pageFocus() string {
	if s, r, ok := m.page(); ok {
		return s.Focus(&m, r)
	}
	return ""
}

// Note is the notice on the help line now (section.Host).
func (m *Model) Note() string { return m.notice }

// Reselect shows the selection again (section.Host).
func (m *Model) Reselect() { m.onSelect() }

// SectionModal makes a section's popup the open modal, or closes it (section.Host).
func (m *Model) SectionModal(open bool) {
	if open {
		m.modal = secModal
	} else {
		m.modal = noModal
	}
}

// Suggesting reports whether the message box's suggestions are open (section.Host).
func (m *Model) Suggesting() bool { return len(m.sugg) > 0 }

// modalSec is the section whose popup is open, or nil.
func (m Model) modalSec() section.Section {
	for _, s := range m.secs {
		if s.ModalOpen(&m) {
			return s
		}
	}
	return nil
}

// whereNow is what screen time counts you as in: the task (by title) or the section.
func (m Model) whereNow() string {
	r := m.selRow()
	switch {
	case m.current() != nil:
		return m.current().Title
	case r.Kind == rowPR:
		return "PULL REQUESTS"
	case r.Kind == rowReq || r.Kind == rowApp:
		return "REQUESTS"
	case r.Item() && m.owner(r) != nil:
		return m.owner(r).Def().Label
	case r.Kind == rowGroup:
		return r.Label
	}
	return "home"
}

// WhereNow is what screen time counts you as in (section.Host).
func (m *Model) WhereNow() string { return m.whereNow() }

// Blurred reports whether crew's window isn't in front (section.Host).
func (m *Model) Blurred() bool { return m.blurred }

// Current is the task that's open, or nil (section.Host).
func (m *Model) Current() *tasks.Task { return m.current() }

// Say adds a line to a chat (section.Host).
func (m *Model) Say(id, role, text string) { m.add(id, role, text) }
