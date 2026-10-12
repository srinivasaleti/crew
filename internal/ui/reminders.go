package ui

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/reminders"
	"crew/internal/tasks"
)

// REMINDERS lives in its own package (reminders: the reminders, how they repeat, `crew remind`, the
// List table and the form). This is crew's side of it: the section's row with two tabs, List (the
// table, with the form as a popup) and Chat (Claude managing them with `crew remind`, locked down
// like the REQUESTS chat), and the tick that fires what's due.

const remChatKind = "reminders"

// remPaneNames are REMINDERS' tabs.
var remPaneNames = []string{"List", "Chat"}

// remStore is the reminders file.
func (m Model) remStore() reminders.Store {
	return reminders.Store{Path: m.cfg.Reminders + "/reminders.json"}
}

// loadReminders re-reads the reminders: the chat's Claude changes them with `crew remind`.
func (m *Model) loadReminders() { m.rems = m.remStore().Load() }

// changeReminders loads, changes and saves the reminders, then shows them.
func (m *Model) changeReminders(f func(rs []reminders.Reminder) ([]reminders.Reminder, error)) {
	if err := m.remStore().Change(f); err != nil {
		m.notice = err.Error()
	}
	m.loadReminders()
	if m.onRemPage() {
		m.refresh()
	}
}

// remRows is the REMINDERS section's sidebar row.
func (m Model) remRows() []row {
	if !m.sectionOn("reminders") {
		return nil
	}
	n := 0
	for _, r := range m.rems {
		if r.Active() {
			n++
		}
	}
	return []row{{kind: rowGroup, group: "reminders", label: "REMINDERS", count: n}}
}

// remRowBadge is the next reminder's time on the REMINDERS row, short so the count still fits:
// "4:00pm" today, else "Tue 4:00pm"; "" when none is due.
func (m Model) remRowBadge() string {
	now := time.Now()
	for _, r := range m.rems {
		if !r.Active() {
			continue
		}
		if r.At.YearDay() == now.YearDay() && r.At.Year() == now.Year() {
			return dim.Render(r.At.Format("3:04pm"))
		}
		return dim.Render(r.At.Format("Mon 3:04pm"))
	}
	return ""
}

// remAlertFor is how long a reminder that fired stays on the help line, unless you open REMINDERS.
const remAlertFor = 10 * time.Minute

// remStatus is the reminder that fired last, for the start of the help line, so it can't slip by
// unseen when a notification is hidden; "" once you've opened REMINDERS or after remAlertFor.
func (m Model) remStatus() string {
	if m.remAlert == "" || time.Since(m.remAlertAt) > remAlertFor || m.onRemPage() {
		return ""
	}
	return "◷ " + m.remAlert + " · " + m.remAlertAt.Format("3:04pm")
}

// onRemPage reports whether REMINDERS is in the main pane (either tab).
func (m Model) onRemPage() bool { r := m.selRow(); return r.kind == rowGroup && r.group == "reminders" }

// onRemChat reports whether REMINDERS' Chat tab is on screen.
func (m Model) onRemChat() bool { return m.onRemPage() && m.remPane == 1 }

// setRemPane switches REMINDERS' tab; the chat gets the message box.
func (m *Model) setRemPane(i int) tea.Cmd {
	m.remPane = i
	if i == 1 {
		m.ensureRemChat()
	}
	m.onSelect()
	if i == 1 {
		m.focusOn(areaMain, -1)
		return m.input.Focus()
	}
	return nil
}

// remTabs is REMINDERS' tab bar.
func (m Model) remTabs(w int) string {
	active := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range remPaneNames {
		if i == m.remPane {
			bar = append(bar, active.Render(tabLabel(n, i)))
		} else {
			bar = append(bar, dim.Padding(0, 1).Render(tabLabel(n, i)))
		}
	}
	about := "   n new · ⏎ edit · c ask Claude"
	if m.remPane == 1 {
		about = "   Claude manages your reminders with crew remind, and nothing else"
	}
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render(about), w, "…")
}

// remAction does what the List table asks for on a reminder (its id, "" for a Global key).
func (m *Model) remAction(id, action string) tea.Cmd {
	n, _ := strconv.Atoi(id)
	switch action {
	case reminders.New:
		return m.openRemForm(reminders.Reminder{})
	case reminders.Chat:
		return m.setRemPane(1)
	}
	r := reminders.Find(m.rems, n)
	if r == nil {
		return nil
	}
	switch action {
	case reminders.Edit:
		return m.openRemForm(*r)
	case reminders.Delete, reminders.Pause, reminders.Restore:
		var did string
		m.changeReminders(func(rs []reminders.Reminder) ([]reminders.Reminder, error) {
			if r := reminders.Find(rs, n); r != nil {
				switch {
				case action == reminders.Delete:
					r.Deleted, did = true, "Deleted "+strconv.Quote(r.Text)+" · u restores it (Deleted)"
				case action == reminders.Restore:
					r.Deleted, did = false, "Restored "+strconv.Quote(r.Text)
				case r.Paused:
					reminders.Resume(r, time.Now())
					did = "Resumed " + strconv.Quote(r.Text) + " · next " + reminders.When(r.At, time.Now())
				default:
					r.Paused, did = true, "Paused "+strconv.Quote(r.Text)+" · p resumes it"
				}
			}
			return rs, nil
		})
		if m.notice == "" {
			m.notice = did
		}
	}
	return nil
}

// openRemForm opens the new or edit popup.
func (m *Model) openRemForm(r reminders.Reminder) tea.Cmd {
	var cmd tea.Cmd
	m.remForm, cmd = reminders.NewForm(r)
	m.modal = remModal
	m.input.Blur()
	return cmd
}

// updateRemForm hands a key to the popup and saves what it makes.
func (m Model) updateRemForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, saved, closed := m.remForm.Key(msg)
	if !closed {
		return m, cmd
	}
	m.modal, m.remForm = noModal, nil
	if saved != nil {
		r := *saved
		m.changeReminders(func(rs []reminders.Reminder) ([]reminders.Reminder, error) {
			if old := reminders.Find(rs, r.ID); old != nil {
				old.Text, old.At, old.Every, old.Done = r.Text, r.At, r.Every, false
				return rs, nil
			}
			r.ID = reminders.NextID(rs)
			return append(rs, r), nil
		})
		if m.notice == "" {
			m.notice = "Saved · next " + reminders.When(r.At, time.Now())
		}
	}
	m.onSelect()
	return m, nil
}

type remTickMsg struct{ gen int }

// remTick fires due reminders every 15 seconds while REMINDERS is on.
func (m *Model) remTick() tea.Cmd {
	if !m.sectionOn("reminders") {
		return nil // turning it on starts it again
	}
	gen := m.remGen
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return remTickMsg{gen} })
}

// restartRemTick fires what's due now and starts a new ticker (an older one's ticks are dropped).
func (m *Model) restartRemTick() tea.Cmd {
	m.remGen++
	gen := m.remGen
	return func() tea.Msg { return remTickMsg{gen} }
}

// onRemTick re-reads the reminders and tells you about the ones that are due.
func (m *Model) onRemTick(msg remTickMsg) tea.Cmd {
	if msg.gen != m.remGen || !m.sectionOn("reminders") {
		return nil
	}
	rs := m.remStore().Load()
	fired, changed := reminders.Fire(rs, time.Now())
	if changed {
		m.changeReminders(func([]reminders.Reminder) ([]reminders.Reminder, error) { return rs, nil })
	} else {
		m.rems = rs // the chat or a terminal may have changed them
	}
	for _, r := range fired {
		notify("Reminder", r.Text)
		m.remAlert, m.remAlertAt = r.Text, time.Now()
		m.notice = "◷ Reminder: " + r.Text
		if r.Every != "" {
			m.notice += " · next " + reminders.When(reminders.Find(m.rems, r.ID).At, time.Now())
		}
	}
	return m.remTick()
}

// remChat is the REMINDERS chat, or nil before it's first opened.
func (m Model) remChat() *tasks.Task {
	for i := range m.chats {
		if m.chats[i].Kind == remChatKind {
			return &m.chats[i]
		}
	}
	return nil
}

func (m *Model) ensureRemChat() *tasks.Task {
	if t := m.remChat(); t != nil {
		return t
	}
	m.chats = append(m.chats, tasks.Task{ID: "_reminders", Title: "Reminders", Kind: remChatKind, Status: tasks.InProgress,
		Created: time.Now(), Body: reminders.ChatBrief(crewExe(), time.Now())})
	return &m.chats[len(m.chats)-1]
}

// startRemChat runs the chat's first turn in the reminders folder.
func (m *Model) startRemChat(t tasks.Task, prompt string) tea.Cmd {
	dir := m.cfg.Reminders
	return func() tea.Msg { return startedMsg{t.ID, "", dir, "", prompt, os.MkdirAll(dir, 0o755)} }
}

// remTurnEnded shows what the chat's Claude changed.
func (m *Model) remTurnEnded(id string) {
	if t := m.find(id); t != nil && t.Kind == remChatKind {
		m.loadReminders()
	}
}

// remHeader is the main pane's first line on REMINDERS' Chat tab.
func (m Model) remHeader(w int) string {
	return ansi.Truncate(bold.Render("Claude")+dim.Render(" · reminders")+m.chatMeta(m.remChat()), w, "…")
}

// renderRemChat fills the main pane with the REMINDERS chat.
func (m *Model) renderRemChat() {
	w := max(m.vp.Width, 20)
	follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
	t := m.remChat()
	if t == nil || len(m.conv(t.ID).msgs) == 0 {
		m.vp.SetContent(dim.Render(lipgloss.NewStyle().Width(w).Render("Tell Claude what to remember: \"stand-up every weekday at 4pm\", " +
			"\"move the timesheet reminder to Fridays 5:30\", \"pause everything next week, I'm on leave\", \"what's coming up tomorrow?\". " +
			"Claude changes your reminders with crew remind and nothing else, and says what it changed. The List tab (⌘1) shows them.\n\n" +
			"/compact, /new and /model work as in a task.")))
	} else {
		m.vp.SetContent(m.renderChat(m.conv(t.ID).msgs, w, agentName(*t), m.conv(t.ID).busy))
	}
	if follow {
		m.vp.GotoBottom()
	}
}
