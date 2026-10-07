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

// pluginDir is the crew repo's plugin/ folder, set by make (-ldflags -X main.pluginDir=…).
var pluginDir string

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
		Ctx:    ctx,
		Tasks:  task.Store{Dir: filepath.Join(home, "tasks")},
		Chats:  chat.Log{Dir: filepath.Join(home, "chats")},
		Repos:  repo.Store{Path: filepath.Join(home, "repos")},
		Plugin: pluginDir,
		Plans:  filepath.Join(home, "plans"),
		Keys:   filepath.Join(home, "keys"),
	})
	// crew runs inside its app (desktop/), which sends Shift+Enter and ⌘ keys as their own sequences.
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
