package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/table"
	"crew/internal/tasks"
)

// The sidebar is a list of rows: home, then sections (TASKS, PULL REQUESTS). A section's row shows its
// overview on the right, where you pick a task or PR; the one you opened shows under its section while
// it's selected. esc (⌘↑) goes back up: task or PR → its list → home. The selection is a row key, so
// it survives reloads.

type rowKind int

const (
	rowHome rowKind = iota
	rowGroup
	rowTask
	rowPR  // a PR in the PULL REQUESTS section (task indexes m.prList)
	rowReq // the REQUESTS chat (the sidebar's "Claude" row)
	rowApp // an application in REQUESTS (label is its name); its main pane is a table too
)

// isItem reports whether a row is one thing inside a section (not home or a section's row).
func isItem(k rowKind) bool { return k == rowTask || k == rowPR || k == rowReq || k == rowApp }

type row struct {
	kind  rowKind
	group string // group key: "tasks" (the section)
	label string // group label, e.g. "TASKS"
	count int    // tasks in the group (matching the filter)
	depth int    // indent level
	task  int    // index into m.tasks for task rows
}

// rows are home, TASKS with the open task under it, then PULL REQUESTS with the open PR.
func (m Model) rows() []row {
	rows := []row{{kind: rowHome}}
	if len(m.tasks) == 0 || !m.sectionOn("tasks") {
		return append(append(rows, m.prRows()...), m.reqRows()...)
	}
	live := len(m.tasks) - tasks.CountArchived(m.tasks)
	rows = append(rows, row{kind: rowGroup, group: "tasks", label: "TASKS", count: live})
	if id, ok := strings.CutPrefix(m.selKey, "t:"); ok {
		if i := slices.IndexFunc(m.tasks, func(t tasks.Task) bool { return t.ID == id }); i >= 0 {
			rows = append(rows, row{kind: rowTask, group: "tasks", depth: 1, task: i})
		}
	}
	return append(append(rows, m.prRows()...), m.reqRows()...)
}

func (m Model) rowKey(r row) string {
	switch r.kind {
	case rowGroup:
		return "g:" + r.group
	case rowTask:
		return "t:" + m.tasks[r.task].ID
	case rowPR:
		return "p:" + m.prList[r.task].URL
	case rowReq:
		return "q:"
	case rowApp:
		return "a:" + r.label
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

// onGroupRow reports whether the selected row's main pane is a table: a section's, or an application's.
func (m Model) onGroupRow() bool { r := m.selRow(); return r.kind == rowGroup && r.group != "reqs" }

// selectRow makes a row the selection and refreshes the main pane.
func (m *Model) selectRow(r row) {
	if r.kind == rowPR && m.rowKey(r) != m.selKey {
		m.prPane = paneConversation // another PR opens on its conversation
	}
	if r.kind == rowGroup && m.selRow().group != r.group { // a list you come to from elsewhere opens on All
		st := m.ovOf(m.rowKey(r))
		st.Chip, st.Sel = "", 0
	}
	if m.rowKey(r) != m.selKey {
		m.vp.GotoTop() // a new row starts at the top (home keeps where you scrolled)
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

// selectID selects a task.
func (m *Model) selectID(id string) {
	m.selKey = "t:" + id
	m.onSelect()
}

// selectSection selects the row with this key, reporting whether there is one.
func (m *Model) selectSection(key string) bool {
	for _, r := range m.rows() {
		if m.rowKey(r) == key {
			m.selectRow(r)
			return true
		}
	}
	return false
}

// parentRow is the row the selection sits under in the sidebar: a task's or PR's section, a
// request's application, an application's REQUESTS. ok is false on home and a section's row.
func (m Model) parentRow() (p row, ok bool) {
	rows := m.rows()
	i := m.selIndex(rows)
	if !isItem(rows[i].kind) {
		return row{}, false
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].depth < rows[i].depth {
			return rows[j], true
		}
	}
	return row{}, false
}

// goUp (esc, ⌘↑) goes up a level: a task, PR or request to its list (with the list's bar on it);
// a section's table or home's main pane back to its row in the sidebar (where ⏎ came in); from the
// sidebar, home.
func (m *Model) goUp() {
	p, ok := m.parentRow()
	switch {
	case !ok && !m.listFocus: // a section's or home's main pane: back to its row in the sidebar
		m.focusOn(areaTasks, -1)
		return
	case !ok:
		m.goHome()
		return
	}
	id := m.selKey[2:]
	st := m.ovOf(m.rowKey(p))
	if p.group == "reqs" { // the requests screen keeps its own cursor
		m.selectRow(p)
		m.focusOn(areaMain, -1)
		return
	}
	st.SelectID(m.groupTable(p), id)
	m.selectRow(p)
	m.focusOn(areaMain, -1)
}

// stepItem (⌘⇧↓, ⌘⇧↑) opens the next or previous item in its list's order, as the list is filtered
// (chip, facet, search).
func (m *Model) stepItem(d int) tea.Cmd {
	p, ok := m.parentRow()
	if !ok {
		return nil
	}
	if p.group == "reqs" {
		return nil // the requests screen moves its own cursor
	}
	t, st := m.groupTable(p), m.ovOf(m.rowKey(p))
	rows := st.Filtered(t)
	i := slices.IndexFunc(rows, func(o table.Row) bool { return o.ID == m.selKey[2:] }) + d
	if i < 0 || i >= len(rows) {
		return nil
	}
	st.Sel = i
	return m.openRow(p, rows[i].ID, "")
}

// openSearch (ctrl+f) searches the table you're in or came from (TASKS when there's none).
func (m *Model) openSearch() {
	r, ok := m.selRow(), m.onGroupRow()
	if !ok {
		if r, ok = m.parentRow(); !ok {
			ok = m.selectSection("g:tasks")
			r = m.selRow()
		}
	}
	if !ok || r.kind != rowGroup || r.group == "reqs" {
		return // no tasks yet, or REQUESTS (its screen has no search yet)
	}
	m.selectRow(r)
	m.ovOf(m.rowKey(r)).Searching = true
	m.focusOn(areaMain, -1)
}

// toggleArchived (ctrl+o) shows the archived tasks: the TASKS overview on its Archived chip.
func (m *Model) toggleArchived() {
	if !m.sectionOn("tasks") {
		return
	}
	m.selKey, m.tab, m.listFocus = "g:tasks", tabChat, false
	ov := m.ovOf("g:tasks")
	if ov.Chip == "archived" {
		ov.Chip = "" // pressed again: back to the live tasks
	} else {
		ov.Chip = "archived"
	}
	ov.Sel = 0
	m.onSelect()
}

// ---- rendering and scrolling ----

// sidebarPinned is how many lines stay at the top: home, a load error, and a gap.
func (m Model) sidebarPinned() int {
	n := 2
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
	if isItem(r.kind) {
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
	if m.loadErr != "" {
		lines = append(lines, warn.Render(ansi.Truncate("  "+m.loadErr, m.sidebarWidth(), "…")))
	}
	lines = append(lines, "")

	rows, h := all[1:], m.sidebarRegion()
	if len(rows) == 0 {
		return strings.Join(append(lines, dim.Render("  nothing here yet · n on home makes a task; t, p, r there turn sections on")), "\n")
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
	switch r.kind {
	case rowPR:
		return m.prLines(r, selected)
	case rowApp:
		return m.reqAppLines(r, selected)
	case rowReq:
		return m.itemLines(r, claude.Render("✦"), "Claude", "makes and sends requests", selected)
	}
	indent := strings.Repeat("  ", r.depth)
	var icon, title, right, badge string
	switch r.kind {
	case rowHome:
		icon, title = green.Render("✦"), "crew"
	case rowGroup:
		icon, title = bold.Render("≡"), bold.Render(fmt.Sprintf("%s (%d)", r.label, r.count))
		if t := m.reqChat(); r.group == "reqs" && t != nil && (m.conv(t.ID).busy || m.unread[t.ID]) {
			badge = ready.Render("◐")
			if m.conv(t.ID).busy {
				badge = running.Render("●")
			}
		}
		if r.group == "prs" && m.prListLoading {
			badge = sel.Render(spinFrames[m.spin%len(spinFrames)])
			if len(m.prList) == 0 {
				title = bold.Render(r.label) // no count until the first list is in
			}
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
		if (t.Status == tasks.Done || t.Archived) && !selected {
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
func (m Model) taskLines(r row, t tasks.Task, indent, icon, title string, selected bool) string {
	w := m.sidebarWidth()
	title = ansi.Truncate(title, w-5-len(indent), "…")
	var meta []string
	if t.Archived {
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

// clickSidebar selects the clicked row.
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
	m.selectRow(rows[i]) // a group shows its overview
}
