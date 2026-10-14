package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/reminders"
	"crew/internal/tasks"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// The overview is the main pane for a section's row in the sidebar: a table (the table package)
// of what's in the section. TASKS builds its table in the tasks package, PULL REQUESTS in prs.go;
// this hands the table keys, clicks and the wheel, and opens what it asks for.

// ovOf is what you picked in one section's table; it lasts while crew runs.
func (m Model) ovOf(key string) *table.State {
	if m.ovs[key] == nil {
		m.ovs[key] = &table.State{}
	}
	return m.ovs[key]
}

// groupTable is the table of a section's row: TASKS or PULL REQUESTS (REQUESTS draws its own
// screen, in the requests package). A new sidebar section with a table adds its case here.
func (m Model) groupTable(r row) table.Table {
	if s := m.owner(r); s != nil {
		return s.Table(&m, r)
	}
	switch r.Section {
	case "prs":
		return m.prTable(r)
	case "reminders":
		return reminders.Table(m.rems, time.Now())
	}
	return tasks.Table(m.taskItems())
}

// openRow opens a row of a section's table with an action ("" for its default).
func (m *Model) openRow(r row, id, action string) tea.Cmd {
	if s := m.owner(r); s != nil {
		return s.Open(m, r, id, action)
	}
	switch r.Section {
	case "prs":
		return openListedPR(m, id, action)
	case "reminders":
		return m.remAction(id, action)
	}
	return openTask(m, id, action)
}

// focusBar marks the selected row of the area that has the keys (the task list, the overview).
var focusBar = look.FocusBar

// overview draws a section's table and says where its parts are.
func (m Model) overview(r row) (string, table.Layout) {
	return m.ovOf(m.rowKey(r)).View(m.groupTable(r), m.vp.Width, m.vp.Height, m.focus() == "overview")
}

// overviewKey handles keys on a section's table; ok is false for keys it doesn't use.
func (m *Model) overviewKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	r := m.selRow()
	id, action, open, handled := m.ovOf(m.rowKey(r)).Key(m.groupTable(r), msg, m.vp.Height)
	if open {
		return m.openRow(r, id, action), true
	}
	if handled {
		m.refresh()
	}
	return nil, handled
}

// clickOverview handles a click on the overview (x, y inside the main pane's content).
func (m *Model) clickOverview(x, y int) tea.Cmd {
	r := m.selRow()
	_, lay := m.overview(r)
	id, action, open := m.ovOf(m.rowKey(r)).Click(m.groupTable(r), lay, x, m.vp.YOffset+y, m.vp.Width)
	if open {
		return m.openRow(r, id, action)
	}
	m.refresh()
	return nil
}

// wheelOverview moves the overview's selection with the mouse wheel.
func (m *Model) wheelOverview(down bool) {
	r := m.selRow()
	m.ovOf(m.rowKey(r)).Wheel(m.groupTable(r), down)
	m.refresh()
}
