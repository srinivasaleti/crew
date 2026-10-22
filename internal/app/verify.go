package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/proc"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// The verify gate: a task's own checks (repo.Checks: lint, build, tests) must pass before P pushes
// it or opens its PR. crew runs them itself, in the task's worktree, so it costs no tokens. A pass
// lasts until the agent's next turn ends (it may have changed the code); /verify runs them by hand,
// /verify fix sends the failure to the agent.

// verifyState is a task's last run of its checks.
type verifyState struct {
	command string
	running bool
	ok      bool
	out     string // the end of what the checks printed
	at      time.Time
}

// verifyMsg is a task's checks, run.
type verifyMsg struct {
	id, command, out string
	ok               bool
}

// runVerify runs a task's checks in the background; nil when there are none to run, or they're
// running already.
func (m *Model) runVerify(t tasks.Task) tea.Cmd {
	if vs := m.verify[t.ID]; vs != nil && vs.running {
		return nil
	}
	dir := m.taskDir(t)
	if dir == "" || t.Worktree == "" {
		return nil
	}
	command := repo.Checks(dir)
	if command == "" {
		return nil
	}
	m.verify[t.ID] = &verifyState{command: command, running: true}
	id, ctx := t.ID, m.cfg.Ctx
	return tea.Batch(m.startSpin(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		cmd := proc.Command(ctx, "/bin/sh", "-c", command)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return verifyMsg{id: id, command: command, out: tailLines(string(out), 60), ok: err == nil}
	})
}

// onVerify keeps a run's result and says it in the task's chat.
func (m *Model) onVerify(msg verifyMsg) tea.Cmd {
	m.verify[msg.id] = &verifyState{command: msg.command, ok: msg.ok, out: msg.out, at: time.Now()}
	t := m.find(msg.id)
	if t == nil {
		return nil
	}
	if msg.ok {
		m.add(t.ID, chat.Tool, "checks pass ("+msg.command+")")
		if m.currentID() == t.ID && m.tab == tabPR {
			m.notice = "Checks pass · P pushes it"
		}
		return nil
	}
	m.add(t.ID, chat.Error, "checks fail ("+msg.command+") · /verify fix sends this to the agent\n\n"+msg.out)
	m.notice = t.Title + ": checks fail · /verify fix sends the failure to " + chat.AgentName(*t)
	return nil
}

// CheckFirst is the verify gate before P (prview.Host): ready when the task's checks passed since
// its agent's last turn, or it has none; else it starts them and says so.
func (m *Model) CheckFirst(id string) (ready bool, cmd tea.Cmd) {
	t := m.find(id)
	if t == nil {
		return true, nil
	}
	vs := m.verify[id]
	switch {
	case vs != nil && vs.running:
		m.notice = "Checks are running (" + vs.command + ") · P again once they pass"
		return false, nil
	case vs != nil && vs.ok:
		return true, nil
	}
	cmd = m.runVerify(*t)
	if cmd == nil {
		return true, nil // nothing to check
	}
	m.notice = "Running the checks first (" + m.verify[id].command + ") · P again once they pass"
	return false, cmd
}

// verifyLabel is the task header's word on its checks: running, passed or failed; "" before a run.
func (m Model) verifyLabel(id string) string {
	switch vs := m.verify[id]; {
	case vs == nil:
		return ""
	case vs.running:
		return running.Render(" ● checking")
	case vs.ok:
		return ready.Render(" ✓ checks")
	}
	return warn.Render(" ✗ checks")
}

// runVerifyCmd is /verify: run the task's checks now; /verify fix sends their failure to the agent.
func runVerifyCmd(m *Model, t *tasks.Task, arg string) {
	m.add(t.ID, chat.You, strings.TrimSpace("/verify "+arg))
	if arg == "fix" {
		vs := m.verify[t.ID]
		if vs == nil || vs.ok || vs.running {
			m.add(t.ID, chat.Error, "no failing checks to fix · /verify runs them")
			return
		}
		m.pending = m.send(*t, chat.Outgoing{Text: fmt.Sprintf("The checks (`%s`) fail. Fix what makes them fail, the simplest way "+
			"that's correct, run them again, and commit when they pass. Don't push. The end of their output:\n\n```\n%s\n```", vs.command, vs.out)})
		return
	}
	if cmd := m.runVerify(*t); cmd != nil {
		m.add(t.ID, chat.Tool, "running the checks: "+m.verify[t.ID].command)
		m.pending = cmd
		return
	}
	if vs := m.verify[t.ID]; vs != nil && vs.running {
		m.add(t.ID, chat.Tool, "the checks are running already")
		return
	}
	m.add(t.ID, chat.Error, "no checks found for this repo · set them with: git config crew.verify \"make check\" (run in the repo)")
}

// tailLines is the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}
