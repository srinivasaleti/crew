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
)

// The rules both UIs share: what a task's agent may spend, what it's told, what it's given, and
// where it runs. The terminal UI (internal/ui/terminal/app) calls these too, so there is one copy.

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
	if AgentName(t) != agent.Claude.Name {
		return "\n\nThis task has a plan at " + planPath + ". Read it, work through its steps in order, and mark each one [~] while you work on it and [x] when it's done."
	}
	return "\n\nThis task has a plan at " + planPath + ". Read it and work through it with the crew:plan skill."
}

// AgentArgs is what every run of a task's agent gets on top of its own arguments: crew's plugin (its
// skills and its hook) and access to the plans and splits folders, so Claude can write the task's plan
// and a proposed split. Gemini gets the plans folder only: crew's skills are a Claude Code plugin.
func AgentArgs(t tasks.Task, plugin, plans, splits string) []string {
	if AgentName(t) == agent.Gemini.Name {
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

// Theme is the web UI's colours: "dark" (the default) or "light", kept in <home>/theme. Not in the
// page's storage: crew serves it on a new port each launch, so the page's origin, and its storage, change.
func (c *Core) Theme() string {
	b, _ := os.ReadFile(filepath.Join(c.cfg.Home, "theme"))
	if strings.TrimSpace(string(b)) == "light" {
		return "light"
	}
	return "dark"
}

// SetTheme keeps the web UI's colours.
func (c *Core) SetTheme(t string) error {
	if t != "dark" && t != "light" {
		return fmt.Errorf("say dark or light")
	}
	return os.WriteFile(filepath.Join(c.cfg.Home, "theme"), []byte(t+"\n"), 0o644)
}

// Plugin is crew's Claude Code plugin folder (its skills), or "" when it isn't there.
func (c *Core) Plugin() string {
	if _, err := os.Stat(filepath.Join(c.cfg.Plugin, ".claude-plugin", "plugin.json")); c.cfg.Plugin != "" && err == nil {
		return c.cfg.Plugin
	}
	return ""
}

// ModelChoice is a model you can pick for a task; an empty Value is the agent CLI's own default.
type ModelChoice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Models are the models a task of this agent can use, as both UIs offer them (/model, the web's picker).
func Models(agentName string) []ModelChoice {
	if agentName == agent.Gemini.Name {
		return geminiModels // Gemini CLI's aliases, which follow its newest models
	}
	return claudeModels
}

var geminiModels = []ModelChoice{{"Default", ""}, {"Pro", "pro"}, {"Flash", "flash"}, {"Flash Lite", "flash-lite"}}

var claudeModels = []ModelChoice{
	{"Default", ""},
	{"Opus 5.5", "claude-opus-5-5"},
	{"Sonnet 5.5", "claude-sonnet-5-5"},
	{"Haiku 4.5", "claude-haiku-4-5-20251001"},
	{"Fable 5.1", "claude-fable-5-1"},
}

// IsModel reports whether a task's model setting (an id, or an alias like "sonnet") is this choice.
func IsModel(setting, choice string) bool {
	if setting == "" || choice == "" {
		return setting == choice
	}
	return setting == choice || strings.Contains(choice, "-"+setting+"-")
}

// SetModel sets the model a task's agent uses from its next message on ("" or "default": the CLI's
// own), and says so in its chat.
func (c *Core) SetModel(id, model string) error {
	t, err := c.task(id)
	if err != nil {
		return err
	}
	if model = strings.TrimSpace(model); model == "default" {
		model = ""
	}
	t.Model = model
	if err := c.cfg.Tasks.Save(t); err != nil {
		return err
	}
	name := "the default model"
	for _, m := range Models(AgentName(t)) {
		if model != "" && IsModel(model, m.Value) {
			name = m.Label
		}
	}
	if model != "" && name == "the default model" {
		name = model
	}
	c.say(id, Tool, "model set to "+name+" from the next message on", false)
	c.publish(Event{Kind: "task", ID: id})
	return nil
}
