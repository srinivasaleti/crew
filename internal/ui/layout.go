package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Each area (task list, chat, every shell) sits in a rounded frame with its name in the top
// border. The frame of the area that has the keys is violet; the others are dim.
var (
	frameFocused = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	frameIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	frameTitle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
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

const sideOuter = sidebarWidth + 2 // task list frame

// mainWidth is everything right of the task list frame.
func (m Model) mainWidth() int { return max(m.w-sideOuter, 24) }

// panelWidth is the outer width of the shell column, 0 when the selected task has no shells.
func (m Model) panelWidth() int {
	if len(m.taskShells()) == 0 {
		return 0
	}
	return max(m.mainWidth()*45/100, 32)
}

// chatWidth is the inside width of the chat frame.
func (m Model) chatWidth() int { return max(m.mainWidth()-m.panelWidth()-2, 20) }

// bodyHeight is the outer height of the frames: the whole window but the help line.
func (m Model) bodyHeight() int { return max(m.h-1, 8) }

const contentY = 3

const maxInputLines = 6

// layout sizes the message box to its content and gives the rest of the height to the chat.
func (m *Model) layout() {
	m.input.SetWidth(max(m.chatWidth(), 10))
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
	m.resizeShells()
	m.vp.Width = max(m.chatWidth()-2, 10) // leaves room for the scroll bar
	if m.tab == tabFiles && m.current() != nil {
		m.vp.Width = max(m.vp.Width-m.treeWidth()-1, 10) // the tree and its divider sit on the left
	}
	// Inside the chat frame: header, tab bar, the conversation, a rule, then the message box.
	m.vp.Height = max(m.bodyHeight()-2-3-m.inputHeight(), 3)
	m.resizeEditor()
	if m.modal == prModal { // the message box sits in the popup
		m.input.SetWidth(m.popupInner())
		m.input.SetHeight(3)
		m.refreshPopup(false)
	}
}

// inputHeight is how many rows the bottom input area takes (the home screen shows a one-line hint).
func (m Model) inputHeight() int {
	if m.current() == nil || m.tab != tabChat {
		return 1
	}
	n := m.input.Height() + len(m.sugg)
	if len(m.sugg) == 0 && m.argHint != "" {
		n++ // the "expects: …" line
	}
	return n
}

// click handles a left click: on the header status chip it opens the menu, on a menu row it picks it.
func (m *Model) click(x, y int) {
	if x < sideOuter && m.modal == noModal {
		m.clickSidebar(y - 1) // inside the task list frame
		return
	}
	t := m.current()
	if t == nil {
		if m.modal == noModal && m.onGroupRow() && y >= contentY {
			m.listFocus = false
			m.pending = m.clickOverview(x-(sideOuter+1), y-contentY)
		}
		return
	}
	mainX := sideOuter + 1 // inside the chat frame
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
		case m.modal == modelModal && row >= 0 && row < len(modelChoices):
			m.pickModel(row)
		case m.modal == statusModal && row >= 0 && row < len(m.menuItems()) && m.menuItems()[row].value != "":
			m.applyMenu(m.menuItems()[row])
		}
		return
	}
	if m.modal != noModal {
		return
	}
	if y == 2 { // the tab bar
		if i := tabAt(x - mainX); i >= 0 {
			m.pending = m.setTab(tab(i))
		}
		return
	}
	if pw := m.panelWidth(); pw > 0 && x >= mainX+m.chatWidth()+1 {
		if i := m.shellAt(y); i >= 0 {
			m.shellFocus = i // clicking a shell gives it the keys
			m.input.Blur()
		}
		return
	}
	if m.shellActive() {
		m.focusChat() // clicking the chat side takes the keys back from a shell
	}
	if y != 1 { // the header line
		return
	}
	x0 := mainX + ansi.StringWidth(t.Title+m.headerMeta(*t))
	if x >= x0 && x < x0+ansi.StringWidth(statusChip(t.Status)) {
		m.openStatus()
	}
}

// tabAt is the tab under column x of the tab bar (each name padded by one space, joined by one), or -1.
func tabAt(x int) int {
	pos := 0
	for i, name := range tabNames {
		w := len(name) + 2
		if x >= pos && x < pos+w {
			return i
		}
		pos += w + 1
	}
	return -1
}
