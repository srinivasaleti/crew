package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/section"
	taskssec "crew/internal/sections/tasks"
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
	if r.Kind == section.Task || s.ShowsTable(&m, r) {
		return nil, r, false // a task's workspace, or a table: crew draws them
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

// HasKeys names who has the keys (section.Host).
func (m *Model) HasKeys() string { return m.focus() }

// ContentY is the screen line the main pane's content starts on (section.Host).
func (m *Model) ContentY() int { return m.contentY() }

// MainSize is the main pane's size, frame included (section.Host).
func (m *Model) MainSize() (int, int) { return m.chatWidth(), m.bodyHeight() }

// Unread reports whether a chat has a reply you haven't seen (section.Host).
func (m *Model) Unread(id string) bool { return m.unread[id] }

// Plugin is crew's plugin folder, or "" when it isn't there (section.Host).
func (m *Model) Plugin() string {
	if m.pluginOK() {
		return m.cfg.Plugin
	}
	return ""
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
			return m.OpenTask(t.ID, "")
		}
	}
	return m.taskSec().NewFrom(m, title, issue, brief)
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

// clickModal hands a click on a section's popup to it (x, y on the screen); a click outside it
// does nothing.
func (m *Model) clickModal(x, y int) {
	s := m.modalSec()
	if s == nil {
		return
	}
	box := s.ModalView(m)
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	if left, top := m.modalOrigin(bw, bh); x >= left && x < left+bw && y >= top && y < top+bh {
		m.pending = s.ModalClick(m, x-left, y-top)
	}
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

// SectionModal makes a section's popup the open modal; nil closes it (section.Host).
func (m *Model) SectionModal(s section.Section) {
	m.popup = s
	if s != nil {
		m.modal = secModal
	} else {
		m.modal = noModal
	}
}

// Suggesting reports whether the message box's suggestions are open (section.Host).
func (m *Model) Suggesting() bool { return len(m.sugg) > 0 }

// modalSec is the section whose popup is open, or nil.
func (m Model) modalSec() section.Section {
	if m.modal == secModal && m.popup != nil && m.popup.ModalOpen(&m) {
		return m.popup
	}
	return nil
}

// taskSec is TASKS, which crew asks for the new-task form.
func (m Model) taskSec() *taskssec.Section { return m.secOf("tasks").(*taskssec.Section) }

// SidebarWidth is the sidebar's width (section.Host).
func (m *Model) SidebarWidth() int { return m.sidebarWidth() }

// ReloadTasks reads the tasks from disk again (section.Host).
func (m *Model) ReloadTasks() { m.reload() }

// OpenTask opens a task's workspace on its chat, or on the PR or plan tab (section.Host).
func (m *Model) OpenTask(id, tab string) tea.Cmd {
	m.listFocus, m.tab = false, tabChat // a task opens on its Chat tab
	m.selectID(id)
	switch tab {
	case "pr":
		return m.setTab(tabPR)
	case "plan":
		return m.setTab(tabPlan)
	}
	return nil
}

// StatusMenu opens a task's status menu (section.Host).
func (m *Model) StatusMenu(id string) { m.openStatus(id) }

// AskDelete asks before deleting a task (section.Host).
func (m *Model) AskDelete(id string) { m.openDelete(id) }

// PlanProgress is a task's plan's progress, "" without a plan (section.Host).
func (m *Model) PlanProgress(id string) string { return m.planProgress(id) }

// Cost is what a task's sessions cost (section.Host).
func (m *Model) Cost(id string) float64 { return m.usage[id].Cost }

// whereNow is what screen time counts you as in: the task (by title) or the section.
func (m Model) whereNow() string {
	r := m.selRow()
	switch {
	case m.current() != nil:
		return m.current().Title
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
