package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/core"
	"crew/internal/gh"
	"crew/internal/tasks"
	"crew/internal/ui/terminal/widgets/chat"
)

// What the web task view has that the terminal gets as commands: /run-plan, /stack (a PR per plan
// step), /summary and /repo. The rules are core's (RunAsk, PushStack, SummaryAsk, MoveRepo); this is
// the terminal's side of them.

// stackMsg is a task's stacked PRs, opened (or not: err).
type stackMsg struct {
	id, said string
	err      error
}

// runRunPlan is /run-plan [next] [stacked]: the agent works through the plan's steps (next: only the
// next one), a commit per step; stacked says /stack will open a PR per step.
func runRunPlan(m *Model, t *tasks.Task, arg string) {
	m.add(t.ID, chat.You, strings.TrimSpace("/run-plan "+arg))
	if !m.canUseSkill(t, "plan") {
		return
	}
	b, _ := os.ReadFile(m.planPath(t.ID))
	if err := core.CanRunPlan(string(b)); err != nil {
		m.add(t.ID, chat.Error, err.Error()+" · /plan writes it")
		return
	}
	if stacked := strings.Contains(arg, "stacked"); stacked != t.Stacked {
		t.Stacked = stacked
		if err := m.cfg.Tasks.Save(*t); err != nil {
			m.add(t.ID, chat.Error, err.Error())
			return
		}
	}
	m.setTab(tabPlan)
	m.pending = m.send(*t, chat.Outgoing{Text: core.RunAsk(m.planPath(t.ID), strings.Contains(arg, "next")), Raw: true})
}

// runStack is /stack: after the task's checks pass, open (or update) a PR per plan step that has a
// "Step N:" commit, each on the one before. Like P, it only happens on your command.
func runStack(m *Model, t *tasks.Task, _ string) {
	m.add(t.ID, chat.You, "/stack")
	switch {
	case t.Worktree == "":
		m.add(t.ID, chat.Error, "nothing to push yet · the worktree is made with the first message")
		return
	case !gh.Installed():
		m.add(t.ID, chat.Error, "needs the GitHub CLI (gh): make setup")
		return
	case m.chat.Conv(t.ID).Busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then /stack")
		return
	case m.stacking[t.ID]:
		m.add(t.ID, chat.Tool, "the stack is being pushed already")
		return
	}
	if ready, cmd := m.CheckFirst(t.ID); !ready {
		m.add(t.ID, chat.Tool, "running the checks first · /stack again once they pass")
		m.pending = cmd
		return
	}
	m.stacking[t.ID] = true
	m.add(t.ID, chat.Tool, "opening a PR per step…")
	task, text, file := *t, m.planText(t.ID), filepath.Join(m.cfg.Plans, t.ID+".stack.json")
	m.pending = tea.Batch(m.startSpin(), func() tea.Msg {
		var said string
		err := core.PushStack(task, text, file, func(s string) { said = s })
		return stackMsg{id: task.ID, said: said, err: err}
	})
}

// onStack says in the task's chat how /stack went.
func (m *Model) onStack(msg stackMsg) {
	delete(m.stacking, msg.id)
	if msg.err != nil {
		m.add(msg.id, chat.Error, msg.err.Error())
		return
	}
	m.add(msg.id, chat.Tool, msg.said)
}

// planText is the task's plan file, "" when it has none.
func (m Model) planText(id string) string {
	b, _ := os.ReadFile(m.planPath(id))
	return string(b)
}

// summaryPath is where crew:summary writes a task's summary, next to its plan.
func (m Model) summaryPath(id string) string { return filepath.Join(m.cfg.Plans, id+".summary.md") }

// runSummary is /summary: Claude writes (or brings up to date) the task explained with diagrams;
// /summary show puts the last one in the chat (diagrams as their Mermaid source).
func runSummary(m *Model, t *tasks.Task, arg string) {
	if arg == "show" {
		m.add(t.ID, chat.You, "/summary show")
		b, err := os.ReadFile(m.summaryPath(t.ID))
		if err != nil {
			m.add(t.ID, chat.Error, "no summary yet · /summary writes one")
			return
		}
		m.add(t.ID, chat.Agent, string(b))
		return
	}
	if !m.canUseSkill(t, "summary") {
		return
	}
	os.MkdirAll(m.cfg.Plans, 0o755)
	m.pending = m.send(*t, chat.Outgoing{Text: core.SummaryAsk(m.summaryPath(t.ID), *t), Raw: true})
}

// runRepo is /repo <name>: move the task to another attached repo (its next message starts there, in
// a new worktree and session); /repo alone lists them.
func runRepo(m *Model, t *tasks.Task, name string) {
	m.add(t.ID, chat.You, strings.TrimSpace("/repo "+name))
	if name == "" {
		var names []string
		for _, r := range m.cfg.Repos.List() {
			names = append(names, filepath.Base(r))
		}
		m.add(t.ID, chat.Tool, "in "+filepath.Base(t.Repo)+" · /repo <name> moves it to one of: "+strings.Join(names, ", "))
		return
	}
	if m.chat.Conv(t.ID).Busy || m.stacking[t.ID] {
		m.add(t.ID, chat.Error, "wait for the agent to finish")
		return
	}
	moved, err := core.MoveRepo(m.cfg.Repos, *t, name)
	switch {
	case err != nil:
		m.add(t.ID, chat.Error, err.Error())
		return
	case moved.Repo == t.Repo:
		m.add(t.ID, chat.Tool, "it's in "+filepath.Base(t.Repo)+" already")
		return
	}
	if err := m.cfg.Tasks.Save(moved); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	*t = moved
	delete(m.fileTabs, t.ID) // the old worktree's files, diff and checks
	delete(m.changes, t.ID)
	delete(m.verify, t.ID)
	os.Remove(filepath.Join(m.cfg.Plans, t.ID+".stack.json"))
	m.add(t.ID, chat.Tool, "moved to "+filepath.Base(t.Repo)+" · the next message sets up its worktree there, with a fresh session")
}
