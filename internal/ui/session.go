package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/task"
)

// submit sends the typed message to the selected task's agent.
func (m *Model) submit() tea.Cmd {
	t := m.current()
	text := strings.TrimSpace(m.input.Value())
	if t == nil || text == "" || m.conv(t.ID).busy {
		return nil
	}
	m.input.Reset()
	m.layout()
	m.add(t.ID, chat.You, text)
	if t.Session == "" {
		return m.start(*t, brief(*t)+"\n\n"+text)
	}
	return m.run(*t, text, true)
}

// start creates the task's worktree in the background, then runs the first agent turn.
func (m *Model) start(t task.Task, prompt string) tea.Cmd {
	m.conv(t.ID).busy = true
	m.add(t.ID, chat.Tool, "setting up worktree…")
	repos := m.cfg.Repos
	return func() tea.Msg {
		rp, err := repos.Resolve(t.Repo)
		var wt, base string
		if err == nil {
			wt, base, err = repo.AddWorktree(rp, t.ID)
		}
		return startedMsg{t.ID, rp, wt, base, prompt, err}
	}
}

func (m *Model) run(t task.Task, prompt string, resume bool) tea.Cmd {
	a, ok := agent.Get(agentName(t))
	if !ok {
		m.conv(t.ID).busy = false
		m.add(t.ID, chat.Error, "unknown agent "+agentName(t))
		return nil
	}
	m.conv(t.ID).busy = true
	if t.Status == task.Todo {
		m.setStatus(t.ID, task.InProgress) // the first message starts the work
	}
	return wait(t.ID, a.Run(m.cfg.Ctx, t.Worktree, t.Session, resume, prompt))
}

// wait delivers the next agent event; the stream ends with turnEndMsg.
func wait(id string, ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return turnEndMsg{id}
		}
		return eventMsg{id, ev, ch}
	}
}

func agentName(t task.Task) string {
	if t.Agent == "" {
		return agent.Claude.Name
	}
	return t.Agent
}

// brief is what an agent is told about its task before your first message.
func brief(t task.Task) string {
	s := "Task: " + t.Title
	if t.Body != "" {
		s += "\n\n" + t.Body
	}
	return s
}
