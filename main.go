package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/app"
	"crew/internal/repo"
	"crew/internal/sections/containers"
	"crew/internal/sections/reminders"
	"crew/internal/sections/requests"
	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// pluginDir is the crew repo's plugin/ folder, set by make (-ldflags -X main.pluginDir=…).
var pluginDir string

// plugin is crew's skills folder: the checkout's, so editing a skill applies on the next message,
// else the copy make bundles in crew.app (Contents/Resources/plugin), so the app works after the
// checkout moves or is gone.
func plugin() string {
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); pluginDir != "" && err == nil {
		return pluginDir
	}
	if exe, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(exe), "..", "Resources", "plugin")
		if _, err := os.Stat(filepath.Join(bundled, ".claude-plugin", "plugin.json")); err == nil {
			return filepath.Clean(bundled)
		}
	}
	return pluginDir
}

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
	if len(os.Args) > 1 && os.Args[1] == "remind" { // crew remind …: REMINDERS, for its chat's Claude and you
		os.Exit(reminders.CLI(filepath.Join(home, "reminders", "reminders.json"), os.Args[2:], os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "ctr" { // crew ctr …: read-only docker and kubectl, for a container chat's Claude
		os.Exit(containers.CLI(os.Args[2:], os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" { // crew doctor: what's installed and signed in
		if !setup.Report(os.Stdout, setup.Check(context.Background(), filepath.Join(home, "notify"))) {
			os.Exit(1)
		}
		return
	}
	if pid, ok := lock(home); !ok {
		// Two crews on one folder would run the same task's agent twice and fire every reminder twice.
		fmt.Printf("crew is already running for %s (pid %s), in another window.\n"+
			"Use that one, or set CREW_HOME to run a separate crew.\n\nPress return to close.", home, pid)
		bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := app.New(app.Config{
		Ctx:        ctx,
		Tasks:      tasks.Store{Dir: filepath.Join(home, "tasks")},
		Chats:      chat.Log{Dir: filepath.Join(home, "chats")},
		Repos:      repo.Store{Path: filepath.Join(home, "repos")},
		Plugin:     plugin(),
		Plans:      filepath.Join(home, "plans"),
		Splits:     filepath.Join(home, "splits"),
		PRChat:     filepath.Join(home, "prchats"),
		JiraChat:   filepath.Join(home, "jirachats"),
		Reqs:       filepath.Join(home, "requests"),
		Keys:       filepath.Join(home, "keys"),
		Sections:   filepath.Join(home, "sections"),
		Focus:      filepath.Join(home, "focus"),
		Reminders:  filepath.Join(home, "reminders"),
		Containers: filepath.Join(home, "containers"),
		Notify:     filepath.Join(home, "notify"),
	})
	// crew runs inside its app (desktop/), which sends Shift+Enter and ⌘ keys as their own sequences.
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// lock keeps crew to one at a time per folder: it holds a lock on home/lock while it runs, which the
// system drops when crew exits, however it exits. When another crew holds it, ok is false and pid
// is that crew's. crew req, remind and doctor don't take it: Claude runs them while crew does.
func lock(home string) (pid string, ok bool) {
	f, err := os.OpenFile(filepath.Join(home, "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return "", true // can't tell; better to run than to refuse
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		b, _ := io.ReadAll(f)
		f.Close()
		return strings.TrimSpace(string(b)), false
	}
	f.Truncate(0)
	f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return "", true // f stays open, and locked, until crew exits
}
