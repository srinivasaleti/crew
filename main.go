package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/requests"
	"crew/internal/setup"
	"crew/internal/tasks"
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
	if len(os.Args) > 1 && os.Args[1] == "req" { // crew req …: the HTTP client, for Claude and you
		os.Exit(requests.CLI(filepath.Join(home, "requests"), os.Args[2:], os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" { // crew doctor: what's installed and signed in
		if !setup.Report(os.Stdout, setup.Check(context.Background())) {
			os.Exit(1)
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := ui.New(ui.Config{
		Ctx:      ctx,
		Tasks:    tasks.Store{Dir: filepath.Join(home, "tasks")},
		Chats:    chat.Log{Dir: filepath.Join(home, "chats")},
		Repos:    repo.Store{Path: filepath.Join(home, "repos")},
		Plugin:   pluginDir,
		Plans:    filepath.Join(home, "plans"),
		PRChat:   filepath.Join(home, "prchats"),
		Reqs:     filepath.Join(home, "requests"),
		Keys:     filepath.Join(home, "keys"),
		Sections: filepath.Join(home, "sections"),
	})
	// crew runs inside its app (desktop/), which sends Shift+Enter and ⌘ keys as their own sequences.
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
