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
	m.conv(t.ID).busy = true
	if t.Status == task.Todo {
		m.setStatus(t.ID, task.InProgress) // the first message starts the work
	}
	return wait(t.ID, a.Run(m.cfg.Ctx, t.Worktree, t.Session, resume, t.Model, prompt))
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

// command runs a crew slash command typed in a task's message box.
func (m *Model) command(t *task.Task, text string) {
	name, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "/model":
		if arg == "" {
			m.openModelMenu()
			return
		}
		if arg == "default" {
			arg = ""
		}
		m.add(t.ID, chat.You, text)
		t.Model = arg
		if err := m.cfg.Tasks.Save(*t); err != nil {
			m.add(t.ID, chat.Error, err.Error())
			return
		}
		m.add(t.ID, chat.Tool, "model set to "+m.modelName(*t)+" from the next message on")
	case "/compact":
		// Claude Code summarises the conversation so later messages re-send far fewer tokens.
		m.add(t.ID, chat.You, text)
		switch {
		case t.Session == "":
			m.add(t.ID, chat.Error, "nothing to compact yet, the task hasn't started")
		case m.conv(t.ID).busy:
			m.add(t.ID, chat.Error, "wait for the agent to finish, then /compact")
		default:
			m.pending = m.run(*t, "/compact", true)
		}
	default:
		m.add(t.ID, chat.You, text)
		m.add(t.ID, chat.Error, "unknown command "+name+" · try /model or /compact")
	}
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
