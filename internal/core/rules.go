package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"crew/internal/agent"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// The rules both UIs share: what a task's agent may spend, what it's told, what it's given, and
// where it runs. The terminal UI (internal/app) calls these too, so there is one copy.

// BudgetOf is a task's budget in USD: its own (/budget), else CREW_TASK_BUDGET; 0 for none. Section
// chats (a PR's, REQUESTS') have none.
func BudgetOf(t tasks.Task) float64 {
	switch {
	case t.Section() || t.Budget < 0:
		return 0
	case t.Budget > 0:
		return t.Budget
	}
	v, _ := strconv.ParseFloat(os.Getenv("CREW_TASK_BUDGET"), 64)
	return max(v, 0)
}

// BudgetNote is what crew says when a task is paused on its budget.
func BudgetNote(spent, budget float64) string {
	return fmt.Sprintf("budget reached: $%.2f of $%.2f · /budget <usd> raises it, /budget off removes it", spent, budget)
}

// PlanNote tells a new session about the task's plan, if it has one at planPath.
func PlanNote(t tasks.Task, planPath string) string {
	if _, err := os.Stat(planPath); err != nil {
		return ""
	}
	if chat.AgentName(t) != agent.Claude.Name {
		return "\n\nThis task has a plan at " + planPath + ". Read it, work through its steps in order, and mark each one [~] while you work on it and [x] when it's done."
	}
	return "\n\nThis task has a plan at " + planPath + ". Read it and work through it with the crew:plan skill."
}

// AgentArgs is what every run of a task's agent gets on top of its own arguments: crew's plugin (its
// skills and its hook) and access to the plans and splits folders, so Claude can write the task's plan
// and a proposed split. Gemini gets the plans folder only: crew's skills are a Claude Code plugin.
func AgentArgs(t tasks.Task, plugin, plans, splits string) []string {
	if chat.AgentName(t) == agent.Gemini.Name {
		return []string{"--include-directories", plans}
	}
	var a []string
	if _, err := os.Stat(filepath.Join(plugin, ".claude-plugin", "plugin.json")); plugin != "" && err == nil {
		a = append(a, "--plugin-dir", plugin)
	}
	return append(a, "--add-dir", plans, "--add-dir", splits)
}

// Workspace is where a task's first turn runs: its worktree (made now, or the one that has its
// branch). A subtask branches from its parent's branch, so it builds on the parent's work.
func Workspace(repos repo.Store, t tasks.Task, parentWorktree string) (repoPath, worktree, base string, err error) {
	if repoPath, err = repos.Resolve(t.Repo); err != nil {
		return
	}
	from := ""
	if parentWorktree != "" {
		from = repo.Branch(parentWorktree)
	}
	if t.Branch != "" { // an existing branch: its worktree, wherever it is
		worktree, _, err = repo.UseBranch(repoPath, t.Branch)
		return
	}
	worktree, base, err = repo.AddWorktree(repoPath, t.ID, from)
	return
}

// UIMode is which UI the app opens: "web" (the default) or "terminal", as ~/.crew/ui says.
func UIMode(home string) string {
	b, _ := os.ReadFile(filepath.Join(home, "ui"))
	if strings.TrimSpace(string(b)) == "terminal" {
		return "terminal"
	}
	return "web"
}

// SetUIMode chooses the UI the app opens next time. The UI that's running writes it and then quits,
// and the app starts the other one (desktop/main.go, exited).
func SetUIMode(home, mode string) error {
	if mode != "web" && mode != "terminal" {
		return fmt.Errorf("say web or terminal")
	}
	return os.WriteFile(filepath.Join(home, "ui"), []byte(mode+"\n"), 0o644)
}
