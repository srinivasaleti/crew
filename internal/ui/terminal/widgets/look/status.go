package look

import (
	"github.com/charmbracelet/lipgloss"

	"crew/internal/tasks"
)

// Tint is the colour of each task status.
var Tint = map[string]lipgloss.Style{
	tasks.Todo:       Dim,
	tasks.InProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
	tasks.InPR:       lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
	tasks.Blocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	tasks.Done:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
}

// Chip is the clickable status shown in a task's header.
func Chip(status string) string {
	return Tint[status].Render("[ " + tasks.Icons[status] + " " + status + " ▾ ]")
}
