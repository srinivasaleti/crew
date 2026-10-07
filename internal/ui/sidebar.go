package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/task"
)

// The sidebar is a list of rows: home, then sections. TASKS is the first section: the live tasks,
// the ones that need you first (taskRank). Archived tasks live in the TASKS overview (its Archived
// chip); one you open shows at the end of the list while it's selected. New sections can sit beside
// TASKS later. The selection is a row key, so it survives collapsing, filtering and reloads.

type rowKind int

const (
	rowHome rowKind = iota
	rowGroup
	rowTask
	rowPR // a PR in the PULL REQUESTS section (task indexes m.prList)
)

type row struct {
	kind  rowKind
	group string // group key: "tasks" (the section)
	label string // group label, e.g. "TASKS"
	count int    // tasks in the group (matching the filter)
	depth int    // indent level
	task  int    // index into m.tasks for task rows
}

func (m Model) isOpen(key string) bool {
	if m.filter.Value() != "" {
		return true // a filter shows every match
	}
	return !m.collapsed[key]
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

// taskRank orders tasks by what they need from you: 0 needs you (a reply is ready, or its PR has
// feedback), 1 working, then in progress, in PR, blocked, todo, and done last.
func (m Model) taskRank(t task.Task) int {
	c := m.conv(t.ID)
	switch {
	case m.unread[t.ID], m.prs[t.ID] != nil && m.prs[t.ID].pr != nil && prTodo(m.prs[t.ID].pr) > 0:
		return 0
	case c.busy:
		return 1
	}
	return 2 + map[string]int{task.InProgress: 0, task.InPR: 1, task.Blocked: 2, task.Todo: 3, task.Done: 4}[t.Status]
}

// lastActive is when a task last had a message, else when it was last saved.
func (m Model) lastActive(t task.Task) time.Time {
	if msgs := m.conv(t.ID).msgs; len(msgs) > 0 {
		if at, err := time.Parse(time.RFC3339, msgs[len(msgs)-1].At); err == nil {
			return at
		}
	}
	return t.Updated
}

// sortTasks orders task indexes by taskRank, then the most recently active first.
func (m Model) sortTasks(idx []int) {
	slices.SortStableFunc(idx, func(a, b int) int {
		ta, tb := m.tasks[a], m.tasks[b]
		if ra, rb := m.taskRank(ta), m.taskRank(tb); ra != rb {
			return ra - rb
		}
		return m.lastActive(tb).Compare(m.lastActive(ta))
	})
}

func (m Model) rows() []row {
	rows := []row{{kind: rowHome}}
	var live []int
	viewing := -1 // an archived task you opened from the overview
	for i, t := range m.tasks {
		switch {
		case t.Archived && m.selKey == "t:"+t.ID:
			viewing = i
		case !t.Archived && m.matches(t):
			live = append(live, i)
		}
	}
	if len(live) == 0 && viewing < 0 && len(m.tasks) == 0 {
		return append(rows, m.prRows()...)
	}
	rows = append(rows, row{kind: rowGroup, group: "tasks", label: "TASKS", count: len(live)})
	if m.isOpen("tasks") {
		m.sortTasks(live)
		for _, i := range live {
			rows = append(rows, row{kind: rowTask, group: "tasks", depth: 1, task: i})
		}
	}
	if viewing >= 0 {
		rows = append(rows, row{kind: rowTask, group: "archived", depth: 1, task: viewing})
	}
	return append(rows, m.prRows()...)
}

func (m Model) rowKey(r row) string {
	switch r.kind {
	case rowGroup:
		return "g:" + r.group
	case rowTask:
		return "t:" + m.tasks[r.task].ID
	case rowPR:
		return "p:" + m.prList[r.task].URL
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
	if r.kind == rowPR && m.rowKey(r) != m.selKey {
		m.prPane = paneConversation // another PR opens on its conversation
	}
	m.selKey, m.tab, m.shellFocus, m.editFocus = m.rowKey(r), tabChat, -1, false
	m.onSelect()
}

func (m *Model) moveSel(delta int) {
	rows := m.rows()
	i := min(max(m.selIndex(rows)+delta, 0), len(rows)-1)
	m.selectRow(rows[i])
}

func (m *Model) goHome() { m.selectRow(row{kind: rowHome}) }

// selectID selects a task, opening TASKS so it's visible.
func (m *Model) selectID(id string) {
	m.collapsed["tasks"] = false
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
		}
		return k == "right" || k == "left"
	case rowTask:
		if k == "left" && m.filter.Value() == "" && r.group == "tasks" {
			m.collapsed[r.group] = true
			m.selKey = "g:" + r.group
			m.onSelect()
			return true
		}
	}
	return false
}

// toggleArchived (ctrl+o) shows the archived tasks: the TASKS overview on its Archived chip.
func (m *Model) toggleArchived() {
	m.selKey, m.tab, m.listFocus = "g:tasks", tabChat, false
	ov := m.ovOf("g:tasks")
	if ov.chip == "archived" {
		ov.chip = "" // pressed again: back to the live tasks
	} else {
		ov.chip = "archived"
	}
	ov.sel = 0
	m.onSelect()
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
	return max(m.bodyHeight()-2-m.sidebarPinned(), 3) // inside the task list frame
}

// rowHeight is how many lines a row takes: a task has a second line (repo, PR, plan, cost).
func rowHeight(r row) int {
	if r.kind == rowTask || r.kind == rowPR {
		return 2
	}
	return 1
}

// sideWindow is the rows of the list that fit in h lines from m.sideTop, and whether the list
// scrolls (then the first and last lines hold "↑ n more" / "↓ n more").
func (m Model) sideWindow(rows []row, h int) (from, to int, scrolls bool) {
	total := 0
	for _, r := range rows {
		total += rowHeight(r)
	}
	if total <= h {
		return 0, len(rows), false
	}
	from = min(max(m.sideTop, 0), len(rows)-1)
	used := 0
	for to = from; to < len(rows) && used+rowHeight(rows[to]) <= h-2; to++ {
		used += rowHeight(rows[to])
	}
	for from > 0 && to == len(rows) && used+rowHeight(rows[from-1]) <= h-2 { // fill up at the end
		from--
		used += rowHeight(rows[from])
	}
	return from, to, true
}

// ensureVisible scrolls the list so the selected row is on screen.
func (m *Model) ensureVisible() {
	rows := m.rows()[1:] // home is pinned
	h, i := m.sidebarRegion(), m.selIndex(m.rows())-1
	if _, _, scrolls := m.sideWindow(rows, h); !scrolls || i < 0 {
		m.sideTop = 0
		return
	}
	if i < m.sideTop {
		m.sideTop = i
	}
	for {
		if _, to, _ := m.sideWindow(rows, h); i < to || m.sideTop >= len(rows)-1 {
			return
		}
		m.sideTop++
	}
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
		lines = append(lines, warn.Render(ansi.Truncate("  "+m.loadErr, m.sidebarWidth(), "…")))
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
	from, to, scrolls := m.sideWindow(rows, h)
	if scrolls {
		lines = append(lines, moreLine("↑", from))
	}
	for i := from; i < to; i++ {
		lines = append(lines, m.rowLine(rows[i], sel == i+1))
	}
	if scrolls {
		lines = append(lines, moreLine("↓", len(rows)-to))
	}
	return strings.Join(lines, "\n")
}

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
	if r.kind == rowPR {
		return m.prLines(r, selected)
	}
	indent := strings.Repeat("  ", r.depth)
	var icon, title, right, badge string
	switch r.kind {
	case rowHome:
		icon, title = green.Render("✦"), "crew"
	case rowGroup:
		icon = "▸"
		if m.isOpen(r.group) {
			icon = "▾"
		}
		icon, title = bold.Render(icon), bold.Render(fmt.Sprintf("%s (%d)", r.label, r.count))
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
		return m.taskLines(r, t, indent, icon, title, selected)
	}
	tw := m.sidebarWidth() - 5 - len(indent) - ansi.StringWidth(badge)
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
		bar := focusBar.Width(m.sidebarWidth())
		return bar.Render("▶ " + indent + ansi.Strip(icon) + " " + ansi.Strip(title) + gap + ansi.Strip(badge) + right)
	case selected:
		return sel.Render("▶ ") + indent + icon + " " + bold.Render(title) + gap + badge + dim.Render(right)
	default:
		return "  " + indent + icon + " " + title + gap + badge + dim.Render(right)
	}
}

// taskLines draws a task's two lines: its title, then what's going on with it in a few words.
func (m Model) taskLines(r row, t task.Task, indent, icon, title string, selected bool) string {
	w := m.sidebarWidth()
	title = ansi.Truncate(title, w-5-len(indent), "…")
	var meta []string
	if r.group == "archived" {
		meta = append(meta, "archived")
	}
	meta = append(meta, filepath.Base(t.Repo))
	if b := m.prBadge(t.ID); b != "" {
		meta = append(meta, b)
	}
	if p := m.planProgress(t.ID); p != "" {
		meta = append(meta, "plan "+p)
	}
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		meta = append(meta, cost(u.Cost))
	}
	second := ansi.Truncate(strings.Join(meta, dim.Render(" · ")), w-5-len(indent), "…")
	pad := "    " + indent
	switch {
	case selected && m.listFocus:
		bar := focusBar.Width(w)
		return bar.Render("▶ "+indent+ansi.Strip(icon)+" "+ansi.Strip(title)) + "\n" + bar.UnsetBold().Render(pad+ansi.Strip(second))
	case selected:
		return sel.Render("▶ ") + indent + icon + " " + bold.Render(title) + "\n" + pad + dim.Render(second)
	}
	return "  " + indent + icon + " " + title + "\n" + pad + dim.Render(second)
}

// clickSidebar selects the clicked row; a click on a group's ▾/▸ arrow opens or closes it.
func (m *Model) clickSidebar(x, y int) {
	m.focusOn(areaTasks, -1) // any click on the list gives it the keys, even below the rows
	all := m.rows()
	if y == 0 {
		m.goHome()
		return
	}
	rows, h := all[1:], m.sidebarRegion()
	off := y - m.sidebarPinned() // line within the list area
	from, to, scrolls := m.sideWindow(rows, h)
	if scrolls {
		if off <= 0 || off >= h-1 {
			return // the ↑ / ↓ marker lines
		}
		off--
	}
	i, line := -1, 0
	for j := from; j < to && i < 0; j++ { // rows take one or two lines
		if off >= line && off < line+rowHeight(rows[j]) {
			i = j
		}
		line += rowHeight(rows[j])
	}
	if i < 0 {
		return
	}
	r := rows[i]
	if r.kind == rowGroup && x <= 3+2*r.depth { // the ▾/▸ arrow folds the group
		m.selKey = m.rowKey(r)
		m.toggleGroup(r.group)
		m.onSelect()
		return
	}
	m.selectRow(r) // anywhere else selects the row (a group shows its overview)
}
