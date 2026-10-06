package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	if t != nil && text == "" {
		m.conv(t.ID).paused = false
		return m.sendNext(*t) // ⏎ on an empty box resumes a waiting queue
	}
	if t == nil {
		return nil
	}
	cmd := strings.HasPrefix(text, "/")
	m.input.Reset()
	m.argHint = ""
	m.layout()
	if cmd {
		m.command(t, text) // crew commands are handled here; /compact is passed on to the agent
		next := m.pending
		m.pending = nil
		return next
	}
	return m.send(*t, queued{text: text})
}

// queued is a message waiting for the agent to finish its turn. A question asked from the PR tab
// remembers its row (talkKey) so the answer shows there.
type queued struct {
	text           string
	talkKey, asked string
}

// send gives a message to the task's agent now, or queues it while the agent is busy (or while
// earlier messages are still queued, to keep their order).
func (m *Model) send(t task.Task, q queued) tea.Cmd {
	c := m.conv(t.ID)
	c.paused = false // you're talking again
	if c.busy || len(c.queue) > 0 {
		c.queue = append(c.queue, q)
		if t.ID == m.currentID() {
			m.refresh()
		}
		if c.busy {
			return nil
		}
		return m.sendNext(t) // idle with a queue (after a stop): it goes after the waiting ones
	}
	if at, ctx := autoCompactAt(), m.usage[t.ID].Context; at > 0 && ctx >= at && usage.Exists(t.Session) {
		c.queue = append(c.queue, q) // sent when the compaction turn ends
		m.add(t.ID, chat.Tool, fmt.Sprintf("context is %s, so crew compacts it before sending your message (CREW_AUTO_COMPACT)", tokens(ctx)))
		return m.run(t, "/compact", true)
	}
	return m.dispatch(t, q)
}

// sendNext sends the first queued message, if the agent is free.
func (m *Model) sendNext(t task.Task) tea.Cmd {
	c := m.conv(t.ID)
	if c.busy || len(c.queue) == 0 {
		return nil
	}
	q := c.queue[0]
	c.queue = c.queue[1:]
	return m.dispatch(t, q)
}

func (m *Model) dispatch(t task.Task, q queued) tea.Cmd {
	if q.talkKey != "" {
		st := m.prOf(t.ID)
		st.talk[q.talkKey] = append(st.talk[q.talkKey], talkTurn{len(m.conv(t.ID).msgs), q.asked})
	}
	m.add(t.ID, chat.You, q.text)
	switch {
	case t.Session == "":
		return m.start(t, brief(t)+"\n\n"+q.text)
	case !usage.Exists(t.Session): // the first message after /new: a new session, told the task again
		return m.run(t, brief(t)+"\n\n"+q.text, false)
	}
	return m.run(t, q.text, true)
}

// autoCompactAt is CREW_AUTO_COMPACT: the context size, in tokens, from which crew runs /compact
// before sending your next message. Unset or 0 turns it off.
func autoCompactAt() int {
	n, _ := strconv.Atoi(os.Getenv("CREW_AUTO_COMPACT"))
	return n
}

// unqueueLast takes the last plain queued message back into the message box to edit (↑).
func (m *Model) unqueueLast() bool {
	c := m.conv(m.currentID())
	n := len(c.queue)
	if n == 0 || c.queue[n-1].talkKey != "" || m.input.Value() != "" {
		return false
	}
	m.input.SetValue(c.queue[n-1].text)
	c.queue = c.queue[:n-1]
	m.layout()
	m.refresh()
	return true
}

// queueView lists the queued messages under the chat.
func (m *Model) queueView(c *conv, w int) string {
	if len(c.queue) == 0 {
		return ""
	}
	var blocks []string
	for _, q := range c.queue {
		text := q.text
		if q.asked != "" {
			text = q.asked + dim.Render("  (about a PR comment)")
		}
		blocks = append(blocks, m.block(dim, "you · queued", "", dim.Render(wrap(text, w-2))))
	}
	hint := "sent in order when the agent finishes · ↑ edits the last one"
	if c.paused {
		hint = "waiting because the last turn stopped · ⏎ on an empty box sends the next one · ↑ edits the last one"
	}
	return "\n\n" + strings.Join(blocks, "\n\n") + "\n" + dim.Render("  "+hint)
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
