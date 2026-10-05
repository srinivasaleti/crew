package ui

import (
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/task"
)

var statusLabels = map[string]string{
	task.Todo: "Todo", task.InProgress: "In progress", task.InPR: "In PR", task.Blocked: "Blocked", task.Done: "Done",
}

// menuItem is one row of the status menu; value is a status, "archive", "unarchive", or "" for a separator.
type menuItem struct{ label, value string }

func (m Model) menuItems() []menuItem {
	var items []menuItem
	for _, s := range task.Statuses {
		items = append(items, menuItem{statusLabels[s], s})
	}
	items = append(items, menuItem{})
	if t := m.current(); t != nil && t.Archived {
		return append(items, menuItem{"Unarchive", "unarchive"})
	}
	return append(items, menuItem{"Archive", "archive"})
}

// openStatus shows the status menu for the selected task, with its current status highlighted.
func (m *Model) openStatus() {
	t := m.current()
	if t == nil {
		return
	}
	m.modal, m.menuIdx = statusModal, 0
	for i, it := range m.menuItems() {
		if it.value == t.Status {
			m.menuIdx = i
		}
	}
	m.input.Blur()
}

func (m Model) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	switch k := msg.String(); k {
	case "up", "down":
		step := 1
		if k == "up" {
			step = -1
		}
		for i := m.menuIdx + step; i >= 0 && i < len(items); i += step {
			if items[i].value != "" { // skip the separator
				m.menuIdx = i
				break
			}
		}
	case "enter":
		m.applyMenu(items[m.menuIdx])
	case "1", "2", "3", "4", "5":
		m.applyMenu(items[k[0]-'1'])
	case "esc", "q", "ctrl+s":
		m.closeModal()
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) applyMenu(it menuItem) {
	m.closeModal()
	t := m.current()
	if t == nil || it.value == "" {
		return
	}
	switch it.value {
	case "archive":
		if m.conv(t.ID).busy {
			m.notice = t.Title + " is busy, wait for its agent to finish"
			return
		}
		t.Archived = true
		m.notice = "Archived " + t.Title + " · ctrl+o shows archived tasks"
	case "unarchive":
		t.Archived = false
		m.notice = "Unarchived " + t.Title
	default:
		m.notice = t.Title + " → " + statusLabels[it.value]
		if t.Archived {
			m.notice = "Unarchived " + m.notice // picking a status brings the task back to the list
		}
		t.Status, t.Archived = it.value, false
	}
	id := t.ID
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.notice = err.Error()
		return
	}
	m.reload() // re-sorts the list: done tasks sink, archived ones hide
	m.selectID(id)
}

func (m Model) statusMenuView() string {
	t := m.current()
	if t == nil {
		return ""
	}
	var b string
	for i, it := range m.menuItems() {
		switch {
		case it.value == "":
			b += dim.Render("──────────────────") + "\n"
			continue
		case i == m.menuIdx:
			b += sel.Render("▶ ")
		default:
			b += "  "
		}
		icon := "⌫"
		if st, ok := tint[it.value]; ok {
			icon = st.Render(icons[it.value])
		}
		line := icon + " " + it.label
		if it.value == t.Status {
			line += dim.Render("  current")
		}
		b += line + "\n"
	}
	return menuBox.Render(bold.Render(ansi.Truncate(t.Title, 26, "…")) + "\n\n" + b + "\n" +
		dim.Render("↑↓ ⏎ or 1-5 · esc"))
}

var menuBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(1, 2).Width(32)

// statusChip is the clickable status shown in the task header.
func statusChip(status string) string {
	return tint[status].Render("[ " + icons[status] + " " + status + " ▾ ]")
}

// click handles a left click: on the header status chip it opens the menu, on a menu row it picks it.
func (m *Model) click(x, y int) {
	if x < sidebarWidth && m.modal == noModal {
		m.clickSidebar(y)
		return
	}
	t := m.current()
	if t == nil {
		return
	}
	mainX := sidebarWidth + 2 // sidebar, divider, space
	if m.modal == statusModal || m.modal == modelModal {
		box := m.modalView()
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		mainW, bodyH := max(m.w-sidebarWidth-3, 10), max(m.h-2-m.inputHeight(), 3)
		// Same centering math as lipgloss.Place.
		left := mainX + (mainW - bw) - int(math.Round(float64(mainW-bw)*0.5))
		top := (bodyH - bh) - int(math.Round(float64(bodyH-bh)*0.5))
		row := y - top - 4 // border, padding, title, blank line
		inside := x >= left && x < left+bw && y >= top && y < top+bh
		switch {
		case !inside:
			m.closeModal() // click outside closes
		case m.modal == modelModal && row >= 0 && row < len(modelChoices):
			m.pickModel(row)
		case m.modal == statusModal && row >= 0 && row < len(m.menuItems()) && m.menuItems()[row].value != "":
			m.applyMenu(m.menuItems()[row])
		}
		return
	}
	if m.modal != noModal || y != 0 {
		return
	}
	x0 := mainX + ansi.StringWidth(t.Title+m.headerMeta(*t))
	if x >= x0 && x < x0+ansi.StringWidth(statusChip(t.Status)) {
		m.openStatus()
	}
}

// clickSidebar selects the clicked task or toggles the archived section. The line positions
// mirror sidebar(): crew, blank, TASKS, then one line per task, with a blank line and the
// ARCHIVED header before the first archived task (or after the list while they're hidden).
func (m *Model) clickSidebar(y int) {
	if y == 0 {
		m.listFocus = false
		m.sel, m.tab = 0, tabChat
		m.onSelect()
		return
	}
	line := 3
	header := -1
	for i, t := range m.tasks {
		if t.Archived && header < 0 {
			header = line + 1
			line += 2
		}
		if y == line {
			m.listFocus = false
			m.sel, m.tab = m.posOf(i), tabChat
			m.onSelect()
			return
		}
		line++
	}
	if m.archivedCount > 0 && !m.showArchived {
		header = line + 1
	}
	if y == header {
		m.toggleArchived()
	}
}
