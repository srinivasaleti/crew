package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/task"
	"crew/internal/ui"
)

func main() {
	home := os.Getenv("CREW_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".crew")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := ui.New(ui.Config{
		Ctx:   ctx,
		Home:  home,
		Tasks: task.Store{Dir: filepath.Join(home, "tasks")},
		Chats: chat.Log{Dir: filepath.Join(home, "chats")},
		Repos: repo.Store{Path: filepath.Join(home, "repos")},
	})
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
