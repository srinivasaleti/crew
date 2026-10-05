package ui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
	"crew/internal/task"
)

const sidebarWidth = 28

var (
	dim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold  = lipgloss.NewStyle().Bold(true)
	sel   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	green = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	icons = map[string]string{task.Todo: "○", task.Working: "●", task.Waiting: "◐", task.Review: "✓", task.Done: "✓"}
	tint  = map[string]lipgloss.Style{
		task.Todo:    dim,
		task.Working: lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		task.Waiting: lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true),
		task.Review:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		task.Done:    dim,
	}
)

func (m Model) View() string {
	bodyH := max(m.h-3, 3)
	side := lipgloss.NewStyle().Width(sidebarWidth).Height(bodyH).MaxHeight(bodyH).Render(m.sidebar())
	divider := dim.Render(strings.TrimSuffix(strings.Repeat("│\n", bodyH), "\n"))
	mainW := max(m.w-sidebarWidth-3, 10)
	main := lipgloss.NewStyle().Width(mainW).Height(bodyH).MaxHeight(bodyH).
		Render(m.header(mainW) + "\n" + dim.Render(strings.Repeat("─", mainW)) + "\n" + m.vp.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, side, divider, " ", main)
	help := "↑↓ switch chat  ⏎ send  pgup/pgdn scroll  esc crew  ctrl+c quit"
	return body + "\n" + dim.Render(strings.Repeat("─", m.w)) + "\n" + m.input.View() + "\n" + dim.Render(help)
}

func (m Model) sidebar() string {
	var b strings.Builder
	row := func(i int, icon, title string) {
		title = ansi.Truncate(title, sidebarWidth-5, "…")
		if i == m.sel {
			b.WriteString(sel.Render("▶ ") + icon + " " + bold.Render(title) + "\n")
		} else {
			b.WriteString("  " + icon + " " + title + "\n")
		}
	}
	homeIcon := green.Render("✦")
	if m.conv(homeID).busy {
		homeIcon = tint[task.Working].Render("●")
	}
	row(0, homeIcon, "crew")
	b.WriteString("\n" + dim.Render("  TASKS") + "\n")
	if len(m.tasks) == 0 {
		b.WriteString(dim.Render("  none yet") + "\n")
	}
	for i, t := range m.tasks {
		icon := icons[t.Status]
		if icon == "" {
			icon = "?"
		}
		row(i+1, tint[t.Status].Render(icon), t.Title)
	}
	if m.loadErr != "" {
		b.WriteString("\n" + warn.Render(ansi.Truncate(m.loadErr, sidebarWidth, "…")))
	}
	return b.String()
}

func (m Model) header(w int) string {
	busy := ""
	if m.conv(m.currentID()).busy {
		busy = tint[task.Working].Render("  ● working…")
	}
	t := m.current()
	if t == nil {
		return bold.Render("crew") + dim.Render("  attach repos, create and start tasks by chatting") + busy
	}
	meta := " · " + filepath.Base(t.Repo) + " · " + agentName(*t) + " · " + t.Status
	return ansi.Truncate(bold.Render(t.Title)+dim.Render(meta)+busy, w, "…")
}

// refresh re-renders the selected chat into the viewport and scrolls to the latest message.
func (m *Model) refresh() {
	m.vp.SetContent(m.render())
	m.vp.GotoBottom()
}

func (m *Model) render() string {
	w := max(m.vp.Width, 20)
	msgs := m.conv(m.currentID()).msgs
	t := m.current()
	if len(msgs) == 0 {
		if t == nil {
			return dim.Render("Tell crew what to work on, for example:\n\n" +
				"  attach ~/code/my-app\n" +
				"  create a task in my-app to sort orgs alphabetically, then start it\n" +
				"  what's waiting on me?")
		}
		return dim.Render(lipgloss.NewStyle().Width(w).Render(
			"Send a message to start this task. " + agentName(*t) + " gets the task below plus your message.\n\n" + brief(*t)))
	}
	agentLabel := "crew"
	if t != nil {
		agentLabel = agentName(*t)
	}
	text := lipgloss.NewStyle().Width(w - 8)
	var b strings.Builder
	for i, msg := range msgs {
		if i > 0 {
			if msg.Role == chat.Tool && msgs[i-1].Role == chat.Tool {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		var label lipgloss.Style
		var name, body = "", msg.Text
		switch msg.Role {
		case chat.You:
			label, name = sel, "you"
		case chat.Agent:
			label, name = green, agentLabel
		case chat.Error:
			label, name, body = warn, "error", warn.Render(body)
		default:
			body = dim.Render("▸ " + body)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Width(8).Render(name), text.Render(body)))
	}
	return b.String()
}
