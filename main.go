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
		Tasks: task.Store{Dir: filepath.Join(home, "tasks")},
		Chats: chat.Log{Dir: filepath.Join(home, "chats")},
		Repos: repo.Store{Path: filepath.Join(home, "repos")},
	})
	// Ask the terminal to report Shift+Enter distinctly (xterm modifyOtherKeys), so it can
	// insert a new line instead of sending; reset on exit.
	fmt.Print("\x1b[>4;1m")
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	fmt.Print("\x1b[>4m")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
