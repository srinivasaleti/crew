package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/repo"
	"crew/internal/task"
	"crew/internal/ui"
)

func main() {
	root := os.Getenv("CREW_ROOT") // folder holding your repos, e.g. ~/oolio
	if root == "" {
		root, _ = os.Getwd()
	}
	m := ui.New(task.Store{Dir: task.DefaultDir()}, root, repo.List(root))
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
