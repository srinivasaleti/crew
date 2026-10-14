package app

import (
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/section"
	"crew/internal/tasks"
)

// Each area (task list, chat, every shell) sits in a rounded frame with its name in the top
// border. The frame of the area that has the keys is violet; the others are dim.
var (
	frameFocused = lipgloss.NewStyle().Foreground(accent)
	frameIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	frameTitle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
)

// frame draws content inside a w×h box (outer size). Content lines are cut or padded to fit.
func frame(content string, w, h int, title string, focused bool) string {
	border, name := frameIdle, dim
	if focused {
		border, name = frameFocused, frameTitle
	}
	iw, ih := max(w-2, 1), max(h-2, 1)
	title = ansi.Truncate(title, max(iw-4, 1), "…")
	top := border.Render("╭─ ") + name.Render(title) + border.Render(" "+strings.Repeat("─", max(iw-3-ansi.StringWidth(title), 0))+"╮")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, h)
	out = append(out, top)
	side := border.Render("│")
	for i := 0; i < ih; i++ {
		l := ""
		if i < len(lines) {
			l = ansi.Truncate(lines[i], iw, "")
		}
		out = append(out, side+l+strings.Repeat(" ", max(iw-ansi.StringWidth(l), 0))+side)
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return strings.Join(out, "\n")
}

// Layout, left to right: the task list frame, the chat frame, and the shell frames stacked in a
// column. Every frame takes one column and one row on each side. Below them is the help line.

// sidebarWidth is the task list's inside width: about a quarter of the window, 30 to 46 columns;
// 0 while a panel is maximised.
func (m Model) sidebarWidth() int {
	if m.zoom {
		return 0
	}
	if m.sideW > 0 { // dragged
		return min(max(m.sideW, 24), max(m.w/2, 24))
	}
	return min(max(m.w/6, 26), 32) // sections and the open item: "PULL REQUESTS (12)" fits
}

// sideOuter is the task list with its frame, or 0 while it's hidden.
func (m Model) sideOuter() int {
	if m.zoom {
		return 0
	}
	return m.sidebarWidth() + 2
}

// mainWidth is everything right of the task list frame.
func (m Model) mainWidth() int { return max(m.w-m.sideOuter(), 24) }

// panelWidth is the outer width of the shell column, 0 when the selected task has no shells.
// While a panel is maximised the shells get 70% when one of them has the keys, and none otherwise.
// You can drag the border or use ⌘⌥⇧←/→ to change the share (panelPct).
func (m Model) panelWidth() int {
	if len(m.taskShells()) == 0 {
		return 0
	}
	pct := m.panelPct
	if pct == 0 {
		pct = 45
	}
	if m.zoom {
		if !m.shellActive() {
			return 0
		}
		pct = 70
	}
	return min(max(m.mainWidth()*pct/100, 32), m.mainWidth()-26)
}

// chatWidth is the inside width of the chat frame.
func (m Model) chatWidth() int { return max(m.mainWidth()-m.panelWidth()-2, 20) }

// bodyHeight is the outer height of the frames: the whole window but the help line.
func (m Model) bodyHeight() int { return max(m.h-1, 8) }

// contentY is the screen row the main pane's content starts on: under the header and the tabs,
// and on a task under its buttons too (taskBar).
func (m Model) contentY() int {
	if m.current() != nil {
		return 4
	}
	return 3
}

const maxInputLines = 6

// layout sizes the message box to its content and gives the rest of the height to the chat.
func (m *Model) layout() {
	m.input.SetWidth(max(m.chatWidth()-4, 10)) // inside the message box's frame
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
	m.resizeShells()
	m.vp.Width = max(m.chatWidth()-2, 10) // leaves room for the scroll bar
	if m.tab == tabFiles && m.current() != nil {
		m.vp.Width = max(m.vp.Width-m.treeWidth()-1, 10) // the tree and its divider sit on the left
	}
	// Inside the chat frame: header, tab bar, a task's buttons, the conversation, a rule, then the
	// message box.
	m.vp.Height = max(m.bodyHeight()-2-m.contentY()-m.inputHeight(), 3)
	m.resizeEditor()
	if m.modal == prModal { // the message box sits in the popup
		m.input.SetWidth(m.prv.PopupInner(m))
		m.input.SetHeight(3)
		m.prv.RefreshPopup(m, false)
	}
}

// inputHeight is how many rows the bottom input area takes (the home screen shows a one-line hint).
func (m Model) inputHeight() int {
	if m.selPR() != nil && m.revEditing() {
		return m.input.Height() + 1 // what's being edited, then the box
	}
	if m.tab == tabPlan && m.current() != nil && m.planOf(m.currentID()).answering > 0 {
		return m.input.Height() + 1 // what it answers, then the box
	}
	if !m.chatOpen() {
		return 1
	}
	n := m.input.Height() + len(m.sugg)
	if len(m.sugg) == 0 && m.argHint != "" {
		n++ // the "expects: …" line
	}
	if ask, _ := m.askView(m.chatWidth() - 4); ask != "" {
		n += strings.Count(ask, "\n") // Claude's questions, as buttons
	}
	return n + 1 // the box's two borders, in place of the rule above it
}

// boxedInput says whether the bottom of the main pane is the chat's message box, drawn in a frame.
func (m Model) boxedInput() bool { return m.chatOpen() }

// modalOrigin is where a modal w×h sits on the screen: centred in the main pane, as lipgloss.Place
// puts it.
func (m Model) modalOrigin(w, h int) (left, top int) {
	mainW, bodyH := m.chatWidth(), m.bodyHeight()
	return m.sideOuter() + 1 + (mainW - w) - int(math.Round(float64(mainW-w)*0.5)), 1 + (bodyH - 2 - h) - int(math.Round(float64(bodyH-2-h)*0.5))
}

// click handles a left click: on the header status chip it opens the menu, on a menu row it picks it.
func (m *Model) click(x, y int) {
	if m.modal == reqsModal { // the requests popup: its fields, options, buttons and ✕
		m.clickReqsPopup(x, y)
		return
	}
	if x < m.sideOuter() && m.modal == noModal {
		m.clickSidebar(x-1, y-1) // inside the task list frame
		return
	}
	mainX := m.sideOuter() + 1 // inside the chat frame
	if m.modal == statusModal || m.modal == modelModal {
		box := m.modalView()
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		mainW, bodyH := m.chatWidth(), m.bodyHeight()
		// Same centering math as lipgloss.Place.
		left := mainX + (mainW - bw) - int(math.Round(float64(mainW-bw)*0.5))
		top := 1 + (bodyH - 2 - bh) - int(math.Round(float64(bodyH-2-bh)*0.5)) // inside the chat frame
		row := y - top - 4                                                     // border, padding, title, blank line
		inside := x >= left && x < left+bw && y >= top && y < top+bh
		switch {
		case !inside:
			m.closeModal() // click outside closes
		case m.modal == modelModal && row >= 0 && row < len(m.menuChoices()):
			m.pickModel(row)
		case m.modal == statusModal && row >= 0 && row < len(m.statusMenu.Items) && m.statusMenu.Items[row].Value != "":
			m.applyStatus(m.statusMenu.Items[row].Value)
		}
		return
	}
	if m.modal == noModal && m.clickAsk(x, y) {
		return // an option Claude asked about
	}
	t := m.current()
	if t == nil {
		if m.modal == noModal && !m.inShells(x) {
			m.focusOn(areaMain, -1) // a click in the main pane gives it the keys
			switch {
			case m.onReqsPage():
				m.clickReqsPage(x-(m.sideOuter()+1), y)
			case m.selPR() != nil && y == 2: // a listed PR's tab bar
				if i := tabAt(paneNames, x-(m.sideOuter()+1)); i >= 0 {
					m.setPRPane(i)
					m.refresh()
				}
			case m.ownerClick(x-(m.sideOuter()+1), y): // a section's own page or tab bar
			case m.selRow().Kind == rowHome && y >= m.contentY():
				m.pending = m.clickHome(y - m.contentY())
			case m.onGroupRow() && y >= m.contentY():
				m.pending = m.clickOverview(x-(m.sideOuter()+1), y-m.contentY())
			}
		}
		return
	}
	if m.modal != noModal {
		return
	}
	if !m.inShells(x) {
		m.focusOn(areaMain, -1) // a click anywhere in the main pane gives it the keys
	}
	if y == 2 { // the tab bar
		if i := tabAt(tabNames, x-mainX); i >= 0 {
			m.pending = m.setTab(tab(i))
		}
		return
	}
	if y == 3 { // the task's buttons
		m.clickTaskBar(x - mainX)
		return
	}
	if pw := m.panelWidth(); pw > 0 && x >= mainX+m.chatWidth()+1 {
		if i := m.shellAt(y); i >= 0 {
			m.focusOn(areaShell, i) // clicking a shell gives it the keys
		}
		return
	}
	if y != 1 { // the header line
		return
	}
	x0 := mainX + ansi.StringWidth(t.Title+m.headerMeta(*t))
	if x >= x0 && x < x0+ansi.StringWidth(tasks.Chip(t.Status)) {
		m.openStatus(t.ID)
	}
}

// tabLabel is tab i's name with the ⌘ key that jumps to it, e.g. "Plan ⌘2".
var tabLabel = section.TabLabel

// tabAt is the tab under column x of a tab bar of these names.
var tabAt = section.TabAt

// resizeShellPanel changes the shells' share of the main area by d percent.
func (m *Model) resizeShellPanel(d int) {
	pct := m.panelPct
	if pct == 0 {
		pct = 45
	}
	m.panelPct = min(max(pct+d, 20), 80)
	m.layout()
	m.refresh()
}

// mouseResize drags the borders: between the task list and the main pane, and between the main
// pane and the shells. It reports whether it used the event.
func (m *Model) mouseResize(msg tea.MouseMsg) bool {
	border := m.sideOuter() + m.chatWidth() + 1 // the main pane's right edge; the shells' left is next to it
	side := m.sideOuter() - 1                   // the task list's right edge; the main pane's left is next to it
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && !m.zoom &&
		(msg.X == side || msg.X == side+1) && msg.Y < m.bodyHeight():
		m.resizingSide = true
	case m.resizingSide && msg.Action == tea.MouseActionMotion:
		m.sideW = max(msg.X-1, 24)
		m.layout()
		m.refresh()
	case m.resizingSide && msg.Action == tea.MouseActionRelease:
		m.resizingSide = false
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && m.panelWidth() > 0 &&
		(msg.X == border || msg.X == border+1) && msg.Y < m.bodyHeight():
		m.resizing = true
	case m.resizing && msg.Action == tea.MouseActionMotion:
		if mw := m.mainWidth(); mw > 0 {
			m.panelPct = min(max((m.w-msg.X)*100/mw, 20), 80)
			m.layout()
			m.refresh()
		}
	case m.resizing && msg.Action == tea.MouseActionRelease:
		m.resizing = false
	default:
		return false
	}
	return true
}

// inShells says whether screen column x is in the shells' column.
func (m Model) inShells(x int) bool {
	return m.panelWidth() > 0 && x >= m.sideOuter()+m.chatWidth()+2
}
