package ui

import (
	"context"
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
	if t == nil || text == "" {
		return nil
	}
	cmd := strings.HasPrefix(text, "/")
	if m.conv(t.ID).busy && !cmd {
		return nil // the agent is mid-turn; commands still work
	}
	m.input.Reset()
	m.layout()
	if cmd {
		m.command(t, text) // crew commands are handled here; /compact is passed on to the agent
		next := m.pending
		m.pending = nil
		return next
	}
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
	c := m.conv(t.ID)
	c.busy, c.stopped = true, false
	if t.Status == task.Todo {
		m.setStatus(t.ID, task.InProgress) // the first message starts the work
	}
	ctx, stop := context.WithCancel(m.cfg.Ctx)
	c.stop = stop
	if t.ID == m.currentID() {
		m.refresh() // show the "working…" line right away
	}
	return wait(t.ID, a.Run(ctx, t.Worktree, t.Session, resume, t.Model, prompt))
}

// stopAgent interrupts the selected task's running turn. It reports whether one was running.
func (m *Model) stopAgent() bool {
	t := m.current()
	if t == nil {
		return false
	}
	c := m.conv(t.ID)
	if c.stop == nil || c.stopped {
		return false
	}
	c.stopped = true
	c.stop()
	m.add(t.ID, chat.Tool, "stopping…")
	return true
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

// modelName is the model a task's next message will use: the one chosen with /model, else the
// one Claude last answered with, else the CLI default.
func (m Model) modelName(t task.Task) string {
	switch {
	case t.Model != "":
		return t.Model
	case m.usage[t.ID].LastModel != "":
		return m.usage[t.ID].LastModel + " (default)"
	default:
		return "Claude Code's default"
	}
}
