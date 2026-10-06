package ui

import (
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
