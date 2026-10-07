package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/task"
	"crew/internal/usage"
)

// submit sends the typed message to the selected task's agent.
func (m *Model) submit() tea.Cmd {
	t := m.current()
	text := strings.TrimSpace(m.input.Value())
	if t == nil || text == "" {
		return nil
	}
	cmd := strings.HasPrefix(text, "/")
	m.input.Reset()
	m.argHint = ""
	m.layout()
	defer m.vp.GotoBottom() // what you send shows, even if you'd scrolled up
	if cmd {
		m.command(t, text) // crew commands are handled here; /compact is passed on to the agent
		next := m.pending
		m.pending = nil
		return next
	}
	return m.send(*t, outgoing{text: text})
}

// outgoing is a message for the agent. A question asked from the PR tab remembers its row
// (talkKey) so the answer shows there.
type outgoing struct {
	text           string
	talkKey, asked string
	raw            bool // a command (like /crew:plan) that must lead the prompt, so no task brief
}

// send gives a message to the task's agent. If the agent is working, it's stopped first (as with
// esc) and the message goes as soon as that turn ends, so you can steer it; Claude Code keeps the
// stopped turn's work in the session.
func (m *Model) send(t task.Task, q outgoing) tea.Cmd {
	c := m.conv(t.ID)
	if c.busy {
		if c.next != nil { // sent again before the stop finished: both go together
			c.next.text += "\n\n" + q.text
		} else {
			c.next = &q
		}
		if c.stop != nil && !c.stopped {
			c.stopped = true
			c.stop()
			m.add(t.ID, chat.Tool, "stopping to take your message…")
		}
		return nil
	}
	if at, ctx := autoCompactAt(), m.usage[t.ID].Context; at > 0 && ctx >= at && usage.Exists(t.Session) {
		c.next = &q // sent when the compaction turn ends
		m.add(t.ID, chat.Tool, fmt.Sprintf("context is %s, so crew compacts it before sending your message (CREW_AUTO_COMPACT)", tokens(ctx)))
		return m.run(t, "/compact", true)
	}
	return m.dispatch(t, q)
}

func (m *Model) dispatch(t task.Task, q outgoing) tea.Cmd {
	if q.talkKey != "" {
		st := m.prOf(t.ID)
		st.talk[q.talkKey] = append(st.talk[q.talkKey], talkTurn{len(m.conv(t.ID).msgs), q.asked})
	}
	m.add(t.ID, chat.You, q.text)
	switch {
	case q.raw && t.Session == "":
		return m.start(t, q.text)
	case q.raw:
		return m.run(t, q.text, resumable(t))
	case t.Session == "":
		return m.start(t, brief(t)+m.planNote(t)+"\n\n"+q.text)
	case !usage.Exists(t.Session): // the first message after /new: a new session, told the task again
		return m.run(t, brief(t)+m.planNote(t)+"\n\n"+q.text, false)
	}
	return m.run(t, q.text, true)
}

// autoCompactAt is CREW_AUTO_COMPACT: the context size, in tokens, from which crew runs /compact
// before sending your next message. Unset or 0 turns it off.
func autoCompactAt() int {
	n, _ := strconv.Atoi(os.Getenv("CREW_AUTO_COMPACT"))
	return n
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
	c.busy, c.stopped, c.started = true, false, time.Now()
	if t.Status == task.Todo {
		m.setStatus(t.ID, task.InProgress) // the first message starts the work
	}
	ctx, stop := context.WithCancel(m.cfg.Ctx)
	c.stop = stop
	if t.ID == m.currentID() {
		m.refresh() // show the "working…" line right away
	}
	return tea.Batch(wait(t.ID, a.Run(ctx, t.Worktree, t.Session, resume, t.Model, prompt, m.agentExtra())), m.startSpin())
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

type spinMsg struct{}

// startSpin starts the working spinner's clock if it isn't running.
func (m *Model) startSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick()
}

func spinTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

// onSpin moves the spinner on while any agent works or the PR list loads, and stops its clock when
// nothing does.
func (m *Model) onSpin() tea.Cmd {
	m.spin++
	if rs := m.review(); rs != nil && rs.running {
		m.renderPRRow()
	}
	for _, rs := range m.reviews {
		if rs.running {
			return spinTick()
		}
	}
	for _, c := range m.convs {
		if c.busy {
			if m.conv(m.currentID()).busy {
				m.refresh()
			}
			return spinTick()
		}
	}
	if m.prListLoading {
		return spinTick() // the sidebar redraws on every message
	}
	m.spinning = false
	return nil
}
