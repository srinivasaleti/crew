package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/task"
)

const detailHeight = 6

var (
	dim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold  = lipgloss.NewStyle().Bold(true)
	sel   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	icons = map[string]string{task.Todo: "○", task.Working: "●", task.Waiting: "◐", task.Review: "✓", task.Done: "✓"}
	tint  = map[string]lipgloss.Style{
		task.Todo:    dim,
		task.Working: lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		task.Waiting: lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true),
		task.Review:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		task.Done:    dim,
	}
)

// delegate renders one task per row: cursor, icon, title, repo, status.
type delegate struct{}

func (delegate) Height() int                         { return 1 }
func (delegate) Spacing() int                        { return 0 }
func (delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (delegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	t := li.(item).Task
	st := tint[t.Status]
	icon := icons[t.Status]
	if icon == "" {
		icon = "?"
	}
	tw := max(m.Width()-32, 10)
	title := pad(ansi.Truncate(t.Title, tw, "…"), tw)
	cursor := "  "
	if index == m.Index() {
		cursor, title = sel.Render("▶ "), bold.Render(title)
	}
	fmt.Fprintf(w, "%s%s %s  %s  %s", cursor, st.Render(icon), title,
		dim.Render(pad(ansi.Truncate(t.Repo, 12, "…"), 12)), st.Render(t.Status))
}

func pad(s string, n int) string { return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s))) }

func (m Model) View() string {
	rule := dim.Render(strings.Repeat("─", m.w))
	box := lipgloss.NewStyle().Width(m.w).Height(detailHeight).MaxHeight(detailHeight)
	help := "↑↓ move  ⏎ start  n new  e edit  d delete  / filter  q quit"
	var body string
	switch m.mode {
	case newMode:
		body = bold.Render("New task") + "\n\n" +
			"Title: " + m.input.View() + "\n" +
			"Repo:  ‹ " + sel.Render(m.repos[m.repo]) + " ›  " + dim.Render("(tab to change)")
		help = "⏎ create  ctrl+e create + edit description  tab repo  esc cancel"
	default:
		body = m.detail()
		if m.mode == deleteMode {
			t, _ := m.selected()
			help = warn.Render(fmt.Sprintf("Delete %q? y to confirm, any other key cancels", t.Title))
		}
	}
	return m.list.View() + "\n" + rule + "\n" + box.Render(body) + "\n" + rule + "\n" + dim.Render(help)
}

func (m Model) detail() string {
	t, ok := m.selected()
	if !ok {
		return dim.Render("No tasks yet. Press n to create one.")
	}
	desc := t.Body
	if desc == "" {
		desc = dim.Render("(no description, press e to add one)")
	}
	meta := fmt.Sprintf("Repo: %s   Status: %s   Created: %s   File: %s",
		t.Repo, t.Status, ago(t.Created), m.store.Path(t.ID))
	return bold.Render(t.Title) + "\n" + dim.Render(meta) + "\n\n" + desc
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
