package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/chat"
	"crew/internal/gh"
)

// diffHunk colours the code a line comment is on, keeping the last lines (where the comment is).
func diffHunk(h string, w int) string {
	lines := strings.Split(strings.TrimRight(h, "\n"), "\n")
	lines = lines[max(len(lines)-12, 0):]
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "@@"):
			l = dim.Render(l)
		case strings.HasPrefix(l, "+"):
			l = green.Render(l)
		case strings.HasPrefix(l, "-"):
			l = warn.Render(l)
		}
		if i == len(lines)-1 {
			l = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(l) // the commented line
		}
		lines[i] = clip(l, w)
	}
	return strings.Join(lines, "\n")
}

// ---- The popup: one row's whole conversation, rendered as Markdown, with its actions. ----

// popupSize is the popup's outer size, inside the main pane.
func (m Model) popupSize() (w, h int) { return max(m.chatWidth()-4, 40), max(m.bodyHeight()-4, 12) }

// popupInner is the width of the popup's text.
func (m Model) popupInner() int { w, _ := m.popupSize(); return w - 4 }

func (m *Model) openPopup(key string) {
	st := m.prOf(m.currentID())
	m.selectKey(key)
	st.popKey, st.input = key, ""
	m.modal = prModal
	m.refreshPopup(true)
}

func (m *Model) closePopup() {
	st := m.prOf(m.currentID())
	st.popKey, st.input = "", ""
	m.modal = noModal
	m.input.Reset()
	m.input.Blur()
	m.layout()
	m.renderPR()
}

// refreshPopup sizes the popup's viewport and redraws its text; top scrolls to the start, otherwise
// it stays put (or follows Claude's answer if you're at the bottom).
func (m *Model) refreshPopup(top bool) {
	st := m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		m.closePopup()
		return
	}
	_, h := m.popupSize()
	inputH := 0
	if st.input != "" {
		m.input.SetWidth(m.popupInner())
		inputH = m.input.Height() + 1
	}
	follow := m.pop.AtBottom()
	m.pop.Width, m.pop.Height = m.popupInner(), max(h-2-3-inputH, 3) // border, title, rule, hint line
	m.pop.SetContent(m.popupBody(*it, m.popupInner()))
	switch {
	case top:
		m.pop.GotoTop()
	case follow:
		m.pop.GotoBottom()
	}
}

// popupBody is the conversation: every comment as Markdown, then what you asked Claude about it.
func (m *Model) popupBody(it prItem, w int) string {
	var b []string
	say := func(c gh.Comment) {
		b = append(b, bold.Render(c.Author)+dim.Render("  "+ago(c.At)), m.markdown(showMD(c.Body), w), "")
	}
	switch {
	case it.thread != nil:
		if h := it.thread.DiffHunk; h != "" {
			b = append(b, sel.Render(where(it.thread)), diffHunk(h, w), "")
		}
		for _, c := range it.thread.Comments {
			say(c)
		}
	case it.review != nil:
		say(it.review.Comment)
	case it.comment != nil:
		say(*it.comment)
	case it.check != nil:
		b = append(b, checkIcon(it.check.State)+" "+it.check.Name, dim.Render(it.check.URL), "",
			dim.Render("Close this and press ⏎ on the check to read its failed log."), "")
	}
	turns := m.prOf(m.currentID()).talk[it.key]
	if len(turns) > 0 {
		c := m.conv(m.currentID())
		b = append(b, claude.Render("── with Claude ──"), "")
		for i, tt := range turns {
			b = append(b, bold.Render("You")+dim.Render(": ")+tt.asked, "")
			actions := 0
			for _, msg := range c.msgs[min(tt.at+1, len(c.msgs)):] {
				if msg.Role == chat.You {
					break
				}
				switch msg.Role {
				case chat.Agent:
					b = append(b, m.markdown(msg.Text, w))
				case chat.Tool:
					actions++
				case chat.Error:
					b = append(b, warn.Render(msg.Text))
				}
			}
			if actions > 0 {
				b = append(b, dim.Render(fmt.Sprintf("(%d actions · the Chat tab lists them)", actions)))
			}
			if i == len(turns)-1 && c.busy {
				b = append(b, claude.Render("Claude is working…"))
			}
			b = append(b, "")
		}
	}
	return strings.Join(b, "\n")
}

// popupKeys is what the popup offers for a row.
func popupKeys(it prItem) string {
	var keys []string
	if it.context != "" {
		keys = append(keys, "c ask Claude", "s fix it")
	}
	if it.thread != nil {
		keys = append(keys, "r reply in thread")
		if it.thread.Resolved {
			keys = append(keys, "x reopen")
		} else {
			keys = append(keys, "x resolve")
		}
		keys = append(keys, "g go to code")
	} else if it.review != nil || it.comment != nil {
		keys = append(keys, "r reply")
	}
	return strings.Join(append(keys, "o GitHub", "n next open", "esc close"), " · ")
}

func (m Model) popupView() string {
	st := m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		return ""
	}
	w, h := m.popupSize()
	inner := m.popupInner()
	title := it.about
	if title == "" {
		title = it.check.Name
	}
	status := ""
	switch {
	case it.thread != nil && it.thread.Resolved:
		status = "  " + doneMark + dim.Render(" resolved")
	case it.thread != nil && it.thread.Outdated:
		status = "  " + oldMark + dim.Render(" outdated (the code changed since)")
	case needsYou(*it):
		status = "  " + todoMark + orange.Render(" to do")
	}
	parts := []string{clip(bold.Render(title)+status, inner), dim.Render(strings.Repeat("─", inner)), m.pop.View()}
	switch st.input {
	case "ask":
		parts = append(parts, claude.Render("↳ Ask Claude")+dim.Render(" · ⏎ send · esc cancel"), m.input.View())
	case "reply":
		label := yellow.Render("↳ Reply in this thread on GitHub") + dim.Render(" · ⏎ posts it · esc cancel")
		if it.thread == nil {
			label = yellow.Render("↳ New PR comment quoting @"+strings.TrimPrefix(strings.Split(it.about, "'")[0], "@")) +
				dim.Render(" (GitHub can't reply in place here) · ⏎ posts it · esc cancel")
		}
		parts = append(parts, clip(label, inner), m.input.View())
	}
	parts = append(parts, clip(dim.Render(popupKeys(*it)), inner))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("141")).Padding(0, 1).Width(w - 2).Height(h - 2)
	return box.Render(strings.Join(parts, "\n"))
}

// updatePopup handles keys while the popup is open.
func (m Model) updatePopup(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	t, st := m.current(), m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		m.closePopup()
		return m, nil, true
	}
	k := msg.String()
	if st.input != "" {
		switch k {
		case "esc":
			st.input = ""
			m.input.Reset()
			m.input.Blur()
			m.refreshPopup(false)
			return m, nil, true
		case "enter":
			v := m.input.Value()
			if strings.HasSuffix(v, "\\") { // \⏎ adds a line, as in the chat
				m.input.SetValue(strings.TrimSuffix(v, "\\") + "\n")
				m.refreshPopup(false)
				return m, nil, true
			}
			text := strings.TrimSpace(v)
			if text == "" {
				return m, nil, true
			}
			mode := st.input
			st.input = ""
			m.input.Reset()
			m.input.Blur()
			var cmd tea.Cmd
			if mode == "ask" {
				cmd = m.askAboutPR(*it, text)
			} else {
				cmd = m.replyPR(*t, *it, text)
			}
			m.refreshPopup(false)
			m.pop.GotoBottom()
			return m, cmd, true
		}
		m.input.Focus()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.refreshPopup(false)
		return m, cmd, true
	}
	switch k {
	case "esc", "q":
		m.closePopup()
	case "up", "down", "pgup", "pgdown", "k", "j":
		var cmd tea.Cmd
		m.pop, cmd = m.pop.Update(msg)
		return m, cmd, true
	case "home":
		m.pop.GotoTop()
	case "end":
		m.pop.GotoBottom()
	case "c", "r":
		if k == "c" && it.context == "" || k == "r" && it.thread == nil && it.review == nil && it.comment == nil {
			break
		}
		st.input = map[string]string{"c": "ask", "r": "reply"}[k]
		m.input.Reset()
		m.input.Placeholder = "e.g. “is this right?” or “fix it but keep the API”…"
		if k == "r" {
			m.input.Placeholder = "Your reply…"
		}
		m.input.SetHeight(3)
		m.input.Focus()
		m.refreshPopup(false)
	case "s":
		if it.context != "" {
			cmd := m.askAboutPR(*it, "Please fix this. Keep the change small, then tell me what you changed.")
			m.refreshPopup(false)
			m.pop.GotoBottom()
			return m, cmd, true
		}
	case "x":
		if it.thread != nil {
			cmd := m.toggleResolved(*t, it.thread)
			m.refreshPopup(false)
			return m, cmd, true
		}
	case "g":
		if it.thread != nil {
			m.closePopup()
			return m, m.openAt(it.thread.Path, it.thread.Line), true
		}
	case "o":
		openURL(it.url)
	case "n":
		if m.nextOpenPR() {
			m.openPopup(m.selectedPRItem().key)
		}
	}
	return m, nil, true
}

// mousePopup: the wheel scrolls the popup, a click outside it closes it.
func (m *Model) mousePopup(msg tea.MouseMsg) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		m.pop, _ = m.pop.Update(msg)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		w, h := m.popupSize()
		left, top := sideOuter+1+(m.chatWidth()-w)/2, 1+(m.bodyHeight()-2-h)/2
		if msg.X < left || msg.X >= left+w || msg.Y < top || msg.Y >= top+h {
			m.closePopup()
		}
	}
}
