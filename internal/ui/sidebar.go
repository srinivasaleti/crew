package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/task"
)

// The sidebar is a list of rows: home, then sections. TASKS is the first section: one collapsible
// group per status, then ARCHIVED with the same status groups nested inside. New sections can sit
// beside TASKS later. The selection is a row key, so it survives collapsing, filtering and reloads.

type rowKind int

const (
	rowHome rowKind = iota
	rowGroup
	rowTask
)

type row struct {
	kind  rowKind
	group string // group key: "tasks" (the section), a status ("in-pr"), "archived", or "archived/<status>"
	label string // group label, e.g. "IN PR"
	count int    // tasks in the group (matching the filter)
	depth int    // indent level
	task  int    // index into m.tasks for task rows
}

// groupOrder is the order status groups appear in under TASKS.
var groupOrder = []string{task.Todo, task.InProgress, task.InPR, task.Blocked, task.Done}

// closedByDefault groups start collapsed; ctrl+o / ←/→ / enter / clicks change it per session.
var closedByDefault = map[string]bool{task.Todo: true, task.Done: true, "archived": true}

func groupKey(t task.Task) string {
	status := t.Status
	if _, ok := statusLabels[status]; !ok {
		status = task.Todo // unknown statuses still show up somewhere
	}
	if t.Archived {
		return "archived/" + status
	}
	return status
}

// parentGroup is the group a group sits in ("" for the TASKS section itself).
func parentGroup(key string) string {
	switch {
	case key == "tasks":
		return ""
	case strings.HasPrefix(key, "archived/"):
		return "archived"
	default:
		return "tasks"
	}
}

func (m Model) isOpen(key string) bool {
	if m.filter.Value() != "" {
		return true // a filter shows every match
	}
	if closed, ok := m.collapsed[key]; ok {
		return !closed
	}
	return !closedByDefault[key]
}

// matches reports whether a task matches every word of the filter (title, repo, status, id).
func (m Model) matches(t task.Task) bool {
	words := strings.Fields(strings.ToLower(m.filter.Value()))
	hay := strings.ToLower(strings.Join([]string{t.Title, filepath.Base(t.Repo), t.Status, t.ID}, " "))
	if t.Archived {
		hay += " archived"
	}
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (m Model) rows() []row {
	rows := []row{{kind: rowHome}}
	byGroup := map[string][]int{}
	archived := 0
	for i, t := range m.tasks {
		if m.matches(t) {
			byGroup[groupKey(t)] = append(byGroup[groupKey(t)], i)
			if t.Archived {
				archived++
			}
		}
	}
	matched := 0
	for _, idx := range byGroup {
		matched += len(idx)
	}
	if matched == 0 {
		return rows
	}
	rows = append(rows, row{kind: rowGroup, group: "tasks", label: "TASKS", count: matched})
	if !m.isOpen("tasks") {
		return rows
	}
	addGroup := func(key string, depth int) {
		idx := byGroup[key]
		if len(idx) == 0 {
			return
		}
		status := strings.TrimPrefix(key, "archived/")
		rows = append(rows, row{kind: rowGroup, group: key, label: strings.ToUpper(statusLabels[status]), count: len(idx), depth: depth})
		if m.isOpen(key) {
			for _, i := range idx {
				rows = append(rows, row{kind: rowTask, group: key, depth: depth + 1, task: i})
			}
		}
	}
	for _, s := range groupOrder {
		addGroup(s, 1)
	}
	if archived > 0 {
		rows = append(rows, row{kind: rowGroup, group: "archived", label: "ARCHIVED", count: archived, depth: 1})
		if m.isOpen("archived") {
			for _, s := range groupOrder {
				addGroup("archived/"+s, 2)
			}
		}
	}
	return rows
}

func (m Model) rowKey(r row) string {
	switch r.kind {
	case rowGroup:
		return "g:" + r.group
	case rowTask:
		return "t:" + m.tasks[r.task].ID
	}
	return "home"
}

// selIndex is the selected row's index in rows (0, home, when the selection isn't visible).
func (m Model) selIndex(rows []row) int {
	for i, r := range rows {
		if m.rowKey(r) == m.selKey {
			return i
		}
	}
	return 0
}

func (m Model) selRow() row {
	rows := m.rows()
	return rows[m.selIndex(rows)]
}

func (m Model) onGroupRow() bool { return m.selRow().kind == rowGroup }

// selectRow makes a row the selection and refreshes the main pane.
func (m *Model) selectRow(r row) {
	m.selKey, m.tab = m.rowKey(r), tabChat
	m.onSelect()
}

func (m *Model) moveSel(delta int) {
	rows := m.rows()
	i := min(max(m.selIndex(rows)+delta, 0), len(rows)-1)
	m.selectRow(rows[i])
}

func (m *Model) goHome() { m.selectRow(row{kind: rowHome}) }

// selectID selects a task, opening the groups it sits in so it's visible.
func (m *Model) selectID(id string) {
	if t := m.find(id); t != nil {
		m.collapsed["tasks"] = false
		m.collapsed[groupKey(*t)] = false
		if t.Archived {
			m.collapsed["archived"] = false
		}
	}
	m.selKey = "t:" + id
	m.onSelect()
}

func (m *Model) toggleGroup(key string) {
	m.collapsed[key] = m.isOpen(key)
	m.ensureVisible()
}

// groupNav opens (→) and closes (←) groups like folders; enter or space toggles a group.
// ← on a task closes its group and selects the group's row. It reports whether the key was used.
func (m *Model) groupNav(k string) bool {
	r := m.selRow()
	switch r.kind {
	case rowGroup:
		open := m.isOpen(r.group)
		switch {
		case k == "right" && !open, k == "left" && open, k == "enter", k == " ":
			m.toggleGroup(r.group)
			return true
		case k == "left" && parentGroup(r.group) != "":
			m.selKey = "g:" + parentGroup(r.group) // ← on a closed group steps out to its parent
			m.onSelect()
			return true
		}
		return k == "right" || k == "left"
	case rowTask:
		if k == "left" && m.filter.Value() == "" {
			m.collapsed[r.group] = true
			m.selKey = "g:" + r.group
			m.onSelect()
			return true
		}
	}
	return false
}

// toggleArchived (ctrl+o) jumps to the ARCHIVED group and opens or closes it.
func (m *Model) toggleArchived() {
	for _, t := range m.tasks {
		if t.Archived {
			m.collapsed["tasks"] = false
			m.toggleGroup("archived")
			m.selKey = "g:archived"
			m.onSelect()
			return
		}
	}
	m.notice = "No archived tasks"
}

// ---- rendering and scrolling ----

// sidebarPinned is how many lines stay at the top: home, the filter line or a load error, and a gap.
func (m Model) sidebarPinned() int {
	n := 2
	if m.filtering || m.filter.Value() != "" {
		n++
	}
	if m.loadErr != "" {
		n++
	}
	return n
}

func (m Model) sidebarRegion() int {
	return max(max(m.h-2-m.inputHeight(), 3)-m.sidebarPinned(), 3)
}

// ensureVisible scrolls the list so the selected row is on screen. With more rows than fit, the
// first and last lines hold "↑ n more" / "↓ n more".
func (m *Model) ensureVisible() {
	rows := m.rows()[1:] // home is pinned
	h, i := m.sidebarRegion(), m.selIndex(m.rows())-1
	if len(rows) <= h {
		m.sideTop = 0
		return
	}
	visible := h - 2
	switch {
	case i < 0:
		m.sideTop = 0
	case i < m.sideTop:
		m.sideTop = i
	case i >= m.sideTop+visible:
		m.sideTop = i - visible + 1
	}
	m.sideTop = min(max(m.sideTop, 0), len(rows)-visible)
}

func (m Model) sidebar() string {
	all := m.rows()
	sel := m.selIndex(all)
	var lines []string
	lines = append(lines, m.rowLine(all[0], sel == 0))
	if m.filtering || m.filter.Value() != "" {
		lines = append(lines, "  "+sel2(m.filtering).Render("⌕ ")+m.filter.View())
	}
	if m.loadErr != "" {
		lines = append(lines, warn.Render(ansi.Truncate("  "+m.loadErr, sidebarWidth, "…")))
	}
	lines = append(lines, "")

	rows, h := all[1:], m.sidebarRegion()
	if len(rows) == 0 {
		msg := "  no tasks yet · ctrl+n"
		if m.filter.Value() != "" {
			msg = "  no matches"
		}
		return strings.Join(append(lines, dim.Render(msg)), "\n")
	}
	from, to := 0, len(rows)
	if len(rows) > h {
		from = m.scrollTop(len(rows), h)
		to = min(from+h-2, len(rows))
		lines = append(lines, moreLine("↑", from))
	}
	for i := from; i < to; i++ {
		lines = append(lines, m.rowLine(rows[i], sel == i+1))
	}
	if len(rows) > h {
		lines = append(lines, moreLine("↓", len(rows)-to))
	}
	return strings.Join(lines, "\n")
}

// scrollTop is the first visible row, clamped in case groups closed since the last scroll.
func (m Model) scrollTop(n, h int) int { return min(max(m.sideTop, 0), max(n-(h-2), 0)) }

func sel2(focused bool) lipgloss.Style {
	if focused {
		return sel
	}
	return dim
}

func moreLine(arrow string, n int) string {
	if n == 0 {
		return ""
	}
	return dim.Render(fmt.Sprintf("  %s %d more", arrow, n))
}

// rowLine draws one sidebar row; selected rows get the ▶ marker, or a highlight bar when the
// list has focus.
func (m Model) rowLine(r row, selected bool) string {
	indent := strings.Repeat("  ", r.depth)
	var icon, title, right string
	switch r.kind {
	case rowHome:
		icon, title = green.Render("✦"), "crew"
	case rowGroup:
		icon = "▸"
		if m.isOpen(r.group) {
			icon = "▾"
		}
		style := dim
		if st, ok := tint[r.group]; ok {
			style = st // live status groups keep their colour; archived ones stay dim
		} else if r.group == "tasks" {
			style = bold
		}
		icon, title = style.Render(icon), style.Render(fmt.Sprintf("%s (%d)", r.label, r.count))
		if r.group == "archived" {
			right = "ctrl+o"
		}
	case rowTask:
		t := m.tasks[r.task]
		icon = tint[t.Status].Render(icons[t.Status])
		switch {
		case m.conv(t.ID).busy:
			icon = running.Render("●")
		case m.unread[t.ID]:
			icon = ready.Render("◐")
		case icons[t.Status] == "":
			icon = "?"
		}
		title = t.Title
		if (t.Status == task.Done || t.Archived) && !selected {
			title = dim.Render(title)
		}
		if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
			right = cost(u.Cost)
		}
	}
	tw := sidebarWidth - 5 - len(indent)
	if right != "" {
		tw -= len(right) + 1
	}
	title = ansi.Truncate(title, tw, "…")
	gap := strings.Repeat(" ", max(0, tw-ansi.StringWidth(title)))
	if right != "" {
		gap += " "
	}
	switch {
	case selected && m.listFocus:
		bar := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("0")).Bold(true).Width(sidebarWidth)
		return bar.Render("▶ " + indent + ansi.Strip(icon) + " " + ansi.Strip(title) + gap + right)
	case selected:
		return sel.Render("▶ ") + indent + icon + " " + bold.Render(title) + gap + dim.Render(right)
	default:
		return "  " + indent + icon + " " + title + gap + dim.Render(right)
	}
}

// clickSidebar selects the clicked row; clicking a group opens or closes it.
func (m *Model) clickSidebar(y int) {
	all := m.rows()
	if y == 0 {
		m.listFocus = false
		m.goHome()
		return
	}
	rows, h := all[1:], m.sidebarRegion()
	off := y - m.sidebarPinned() // line within the list area
	i := off
	if len(rows) > h {
		if off <= 0 || off >= h-1 {
			return // the ↑ / ↓ marker lines
		}
		i = m.scrollTop(len(rows), h) + off - 1
	}
	if i < 0 || i >= len(rows) {
		return
	}
	r := rows[i]
	m.listFocus = false
	if r.kind == rowGroup {
		m.selKey = m.rowKey(r)
		m.toggleGroup(r.group)
		m.onSelect()
		return
	}
	m.selectRow(r)
}

// groupView is the main pane for a selected group row.
func (m Model) groupView(r row) string {
	state, action := "closed", "open it"
	if m.isOpen(r.group) {
		state, action = "open", "close it"
	}
	if r.group == "tasks" {
		return dim.Render(fmt.Sprintf("TASKS · %d tasks, %s.\n\nPress → or enter to %s.\n"+
			"Inside: one group per status (todo, in progress, in PR, blocked, done) and ARCHIVED.\n"+
			"ctrl+f filters all tasks.", r.count, state, action))
	}
	s := fmt.Sprintf("%s · %d tasks, %s.\n\nPress → or enter to %s (← closes, enter toggles).\n", r.label, r.count, state, action)
	if strings.HasPrefix(r.group, "archived") {
		s += "Select an archived task and press ctrl+s to change its status or unarchive it.\n"
	}
	return dim.Render(s + "ctrl+f filters all tasks, archived ones included.")
}
