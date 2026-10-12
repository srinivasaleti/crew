package ui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/look"
	"crew/internal/table"
	"crew/internal/tasks"
)

// TASKS lives in its own package (tasks: the table, the new-task form, the status menu). This is
// crew's side of it: what crew knows about each task, opening a row, the form and the menu as
// modals, and deleting a task (which reaches its chat, plan and shells).

// taskItems is every task with what crew knows about it right now.
func (m Model) taskItems() []tasks.Item {
	items := make([]tasks.Item, len(m.tasks))
	for i, t := range m.tasks {
		it := tasks.Item{Task: t, Busy: m.conv(t.ID).busy, Unread: m.unread[t.ID], Plan: m.planProgress(t.ID),
			Cost: m.usage[t.ID].Cost, Last: m.lastActive(t)}
		if st := m.prs[t.ID]; st != nil {
			it.Branch, it.PR = st.branch, st.pr
			if st.pr != nil {
				it.Badge = m.prBadge(t.ID)
			}
		}
		items[i] = it
	}
	return items
}

// lastActive is when a task last had a message, else when it was last saved.
func (m Model) lastActive(t tasks.Task) time.Time {
	if msgs := m.conv(t.ID).msgs; len(msgs) > 0 {
		if at, err := time.Parse(time.RFC3339, msgs[len(msgs)-1].At); err == nil {
			return at
		}
	}
	return t.Updated
}

// openTask opens a task from the TASKS table: its chat, or the tab the action names. tasks.New
// opens the new-task form instead.
func openTask(m *Model, id, action string) tea.Cmd {
	switch action {
	case tasks.New:
		return m.openNew()
	case tasks.Status:
		m.openStatus(id)
		return nil
	case tasks.Delete:
		m.openDelete(id)
		return nil
	}
	m.listFocus, m.tab = false, tabChat // a task opens on its Chat tab
	m.selectID(id)
	switch action {
	case tasks.OpenPR:
		return m.setTab(tabPR)
	case tasks.OpenTodo:
		m.prOf(id).todo = true
		return m.setTab(tabPR)
	case tasks.OpenPlan:
		return m.setTab(tabPlan)
	}
	return nil
}

// taskActs are the buttons over an open task, as REQUESTS has over its lists; each presses a key.
func (m Model) taskActs() []table.Act {
	acts := []table.Act{{Label: "Status", Key: "ctrl+s"}, {Label: "Terminal", Key: "ctrl+t"}, {Label: "Find file", Key: "ctrl+p"}}
	if agentRunning(&m) {
		acts = append(acts, table.Act{Label: "Stop", Key: "ctrl+x"})
	}
	return append(acts, table.Act{Label: "Delete", Key: "ctrl+d"}, table.Act{Label: "All tasks", Key: "esc"})
}

// taskBar draws the task's buttons w wide and says where each one is.
func (m Model) taskBar(w int) (string, [][3]int) {
	var at [][3]int
	bar, x := " ", 1
	for i, a := range m.taskActs() {
		b := look.Button(a.Label, keyNames(a.Key))
		if x+ansi.StringWidth(b) > w {
			break
		}
		at = append(at, [3]int{x, x + ansi.StringWidth(b), i})
		bar += b + " "
		x += ansi.StringWidth(b) + 1
	}
	return bar, at
}

// clickTaskBar presses the button at column x of the task's buttons.
func (m *Model) clickTaskBar(x int) {
	_, at := m.taskBar(m.chatWidth())
	for _, a := range at {
		if x < a[0] || x >= a[1] {
			continue
		}
		if k := m.taskActs()[a[2]].Key; k == "esc" {
			m.goUp()
		} else {
			m.pending, _ = m.runKeyCommand(k)
		}
	}
}

// openNew shows the new-task form.
func (m *Model) openNew() tea.Cmd {
	var cmd tea.Cmd
	m.newTask, cmd = tasks.NewForm(m.cfg.Tasks, m.cfg.Repos)
	m.modal = newModal
	m.input.Blur()
	return cmd
}

// updateNewTask hands a key to the new-task form; a task it makes opens on its chat.
func (m Model) updateNewTask(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, t, closed := m.newTask.Key(msg)
	if !closed {
		return m, cmd
	}
	m.closeModal()
	if t != nil {
		m.reload()
		m.tab, m.listFocus = tabChat, false // a new task opens on its chat, wherever n was pressed
		m.selectID(t.ID)
	}
	return m, cmd
}

// openStatus shows a task's status menu: over its chat (ctrl+s, the header's status), or over
// TASKS (s on a row).
func (m *Model) openStatus(id string) {
	if t := m.find(id); t != nil && !t.Section() {
		m.statusMenu, m.modal = tasks.NewMenu(*t), statusModal
		m.input.Blur()
	}
}

// updateStatus hands a key to the status menu.
func (m Model) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if pick, closed := m.statusMenu.Key(msg.String()); closed {
		m.applyStatus(pick)
	}
	return m, nil
}

// applyStatus closes the status menu and saves what was picked ("" picks nothing). On TASKS you
// stay on the list, with the bar on the task; on a task you follow it.
func (m *Model) applyStatus(pick string) {
	id := m.statusMenu.Task.ID
	m.closeModal()
	t := m.find(id)
	if t == nil || pick == "" {
		return
	}
	if pick == "archive" && m.conv(id).busy {
		m.notice = t.Title + " is busy, wait for its agent to finish"
		return
	}
	m.notice = tasks.SetStatus(t, pick)
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.notice = err.Error()
		return
	}
	onList := m.current() == nil
	m.reload() // re-sorts the list
	switch {
	case onList:
		r := m.selRow()
		m.ovOf(m.rowKey(r)).SelectID(m.groupTable(r), id)
		m.refresh()
	case pick == "archive":
		m.selKey = "g:tasks" // the task left the list; its overview's Archived chip has it
		m.onSelect()
	default:
		m.selectID(id) // follow the task to its new place
	}
}

// openDelete asks before deleting a task (ctrl+d on it, x on its row in TASKS).
func (m *Model) openDelete(id string) {
	t := m.find(id)
	switch {
	case t == nil || t.Section():
		// delete only applies to tasks
	case m.conv(id).busy:
		m.notice = t.Title + " is busy, wait for its agent to finish"
	default:
		m.modal, m.delID = deleteModal, id
		m.input.Blur()
	}
}

// deleteTask deletes a task, its chat and plan; its worktree is kept.
func (m *Model) deleteTask(id string) {
	t := m.find(id)
	if t == nil {
		return
	}
	title, wt := t.Title, t.Worktree
	if err := m.cfg.Tasks.Delete(id); err != nil {
		m.notice = err.Error()
		return
	}
	m.cfg.Chats.Delete(id)
	os.Remove(m.planPath(id))
	delete(m.convs, id)
	delete(m.plans, id)
	m.stopShells(id)
	m.tab = tabChat
	m.reload()
	m.notice = "Deleted " + title
	if wt != "" {
		m.notice += " (worktree kept at " + tildify(wt) + ")"
	}
}
