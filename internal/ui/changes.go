package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/repo"
)

// openChanges switches the selected task to its Changes tab.
func (m *Model) openChanges() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	m.changes = true
	m.input.Blur()
	m.layout()
	if t.Worktree == "" {
		m.diffSummary = ""
		m.vp.SetContent(dim.Render("No changes yet. Send a message in the Chat tab to start the task."))
		return nil
	}
	m.vp.SetContent("")
	return m.loadDiff()
}

func (m *Model) closeChanges() {
	m.changes = false
	m.onSelect() // refocuses the input and redraws the chat
}

// loadDiff runs git in the background; the result arrives as diffMsg.
func (m *Model) loadDiff() tea.Cmd {
	t := m.current()
	if t == nil || t.Worktree == "" {
		return nil
	}
	m.diffSummary = "loading…"
	id, wt, base := t.ID, t.Worktree, t.Base
	return func() tea.Msg {
		diff, summary, err := repo.Diff(wt, base)
		return diffMsg{id, diff, summary, err}
	}
}

func (m Model) updateChanges(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "tab", "shift+tab", "left", "esc", "ctrl+g", "q":
		m.closeChanges()
		return m, nil
	case "r":
		return m, m.loadDiff()
	case "right":
		return m, nil // already on the last tab
	case "home":
		m.vp.GotoTop()
		return m, nil
	case "end":
		m.vp.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // up/down/pgup/pgdown scroll
	return m, cmd
}

// clip cuts each line to width so long diff lines don't wrap and break the layout.
func clip(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}
