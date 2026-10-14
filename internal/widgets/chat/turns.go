package chat

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/tasks"
	"crew/internal/usage"
	"crew/internal/widgets/look"
)

// Turns: a message goes to the chat's agent CLI as one headless run (a turn), whose stream of
// events comes back as EventMsg and ends with TurnEndMsg. A message sent while the agent works
// stops the turn and goes next. Where the first turn runs (a worktree, a section's folder) and what
// a new session is told are crew's (Host).

// Outgoing is a message for the agent. A question asked from the PR tab remembers its row
// (TalkKey) so the answer shows there.
type Outgoing struct {
	Text           string
	TalkKey, Asked string
	Raw            bool // a command (like /crew:plan) that must lead the prompt, so no task brief
}

// StartedMsg is where a chat's first turn runs, set up (a task's worktree, a section's folder), or
// why it couldn't be. Host.Where's command returns it.
type StartedMsg struct {
	ID, Repo, Worktree, Base, Prompt string
	Err                              error
}

// EventMsg is the next event of a running turn.
type EventMsg struct {
	ID string
	Ev agent.Event
	ch <-chan agent.Event
}

// Next waits for the turn's next event.
func (e EventMsg) Next() tea.Cmd { return wait(e.ID, e.ch) }

// TurnEndMsg is a turn that ended.
type TurnEndMsg struct{ ID string }

// Send gives a message to the task's agent. If the agent is working, it's stopped first (as with
// esc) and the message goes as soon as that turn ends, so you can steer it; Claude Code keeps the
// stopped turn's work in the session.
func (cs *Chats) Send(h Host, t tasks.Task, q Outgoing) tea.Cmd {
	c := cs.Conv(t.ID)
	if c.Busy {
		if c.next != nil { // sent again before the stop finished: both go together
			c.next.Text += "\n\n" + q.Text
		} else {
			c.next = &q
		}
		if c.stop != nil && !c.stopped {
			c.stopped = true
			c.stop()
			cs.Add(h, t.ID, Tool, "stopping to take your message…")
		}
		return nil
	}
	if at, ctx := autoCompactAt(), h.Context(t.ID); at > 0 && ctx >= at && HasSession(t) {
		c.next, c.last = &q, nil // q goes when the compaction turn ends
		cs.Add(h, t.ID, Tool, fmt.Sprintf("context is %s, so crew compacts it before sending your message (CREW_AUTO_COMPACT)", look.Tokens(ctx)))
		return cs.Run(h, t, "/compact", true)
	}
	return cs.dispatch(h, t, q)
}

func (cs *Chats) dispatch(h Host, t tasks.Task, q Outgoing) tea.Cmd {
	if t.Section() && t.Session == "" {
		h.SaveTask(t) // a section's chat is kept from its first message
	}
	if q.TalkKey != "" {
		h.Placed(t, q, len(cs.Conv(t.ID).Msgs))
	}
	cs.Add(h, t.ID, You, q.Text)
	c := cs.Conv(t.ID)
	c.last, c.retry = &q, nil
	switch {
	case q.Raw && t.Session == "":
		return cs.Start(h, t, q.Text)
	case q.Raw:
		return cs.Run(h, t, q.Text, Resumable(t))
	case t.Session == "":
		return cs.Start(h, t, h.Intro(t)+"\n\n"+q.Text)
	case !HasSession(t): // the first message after /new: a new session, told the task again
		return cs.Run(h, t, h.Intro(t)+"\n\n"+q.Text, false)
	}
	return cs.Run(h, t, q.Text, true)
}

// autoCompactAt is CREW_AUTO_COMPACT: the context size, in tokens, from which crew runs /compact
// before sending your next message. Unset or 0 turns it off.
func autoCompactAt() int {
	n, _ := strconv.Atoi(os.Getenv("CREW_AUTO_COMPACT"))
	return n
}

// Start sets up where the chat runs (Host.Where), then its first turn runs (Started).
func (cs *Chats) Start(h Host, t tasks.Task, prompt string) tea.Cmd {
	cs.Conv(t.ID).Busy = true
	cs.Add(h, t.ID, Tool, "setting up worktree…")
	return h.Where(t, prompt)
}

// Started runs the first turn where Start set the chat up, or says why it couldn't.
func (cs *Chats) Started(h Host, msg StartedMsg) tea.Cmd {
	t := h.Task(msg.ID)
	if msg.Err != nil || t == nil {
		cs.Conv(msg.ID).Busy = false
		cs.Conv(msg.ID).next = nil // setting up failed; send again once it's fixed
		if msg.Err != nil {
			cs.Add(h, msg.ID, Error, msg.Err.Error())
		}
		return nil
	}
	t.Repo, t.Worktree, t.Base, t.Session = msg.Repo, msg.Worktree, msg.Base, agent.NewSessionID()
	h.SaveTask(*t)
	cs.Add(h, t.ID, Tool, "worktree ready: "+msg.Worktree)
	return cs.Run(h, *t, msg.Prompt, false)
}

// Run runs one turn of the task's agent.
func (cs *Chats) Run(h Host, t tasks.Task, prompt string, resume bool) tea.Cmd {
	a, ok := agent.Get(AgentName(t))
	if !ok {
		cs.Conv(t.ID).Busy = false
		cs.Add(h, t.ID, Error, "unknown agent "+AgentName(t))
		return nil
	}
	c := cs.Conv(t.ID)
	c.Busy, c.stopped, c.started = true, false, time.Now()
	if t.Status == tasks.Todo {
		h.SetStatus(t.ID, tasks.InProgress) // the first message starts the work
	}
	ctx, stop := context.WithCancel(cs.ctx)
	c.stop = stop
	if t.ID == h.OnScreen() {
		h.Refresh() // show the "working…" line right away
	}
	return tea.Batch(wait(t.ID, a.Run(ctx, t.Worktree, t.Session, resume, t.Model, prompt, h.Args(t))), h.Spin())
}

// Stop interrupts a chat's running turn. It reports whether one was running.
func (cs *Chats) Stop(h Host, id string) bool {
	c := cs.Conv(id)
	if c.stop == nil || c.stopped {
		return false
	}
	c.stopped = true
	c.stop()
	cs.Add(h, id, Tool, "stopping…")
	return true
}

// Event takes a turn's reply, tool calls and their results as they come.
func (cs *Chats) Event(h Host, msg EventMsg) {
	ev, c := msg.Ev, cs.Conv(msg.ID)
	switch ev.Kind {
	case agent.KindDelta: // the reply as it's written; the full text replaces it
		if !ev.Sub {
			c.live += ev.Text
			if msg.ID == h.OnScreen() {
				h.Refresh()
			}
		}
	case agent.KindText:
		if !ev.Sub {
			c.live = ""
		}
		cs.AddMsg(h, msg.ID, Message{Role: Agent, Text: ev.Text, Sub: ev.Sub})
	case agent.KindTool:
		cs.AddMsg(h, msg.ID, Message{Role: Tool, Text: ev.Text, Sub: ev.Sub})
	case agent.KindResult:
		cs.AddMsg(h, msg.ID, Message{Role: Result, Text: ev.Text, Sub: ev.Sub})
	}
}

// TurnEnded closes a chat's turn and sends the message you stopped it for, if there is one.
// notify says whether to tell you it finished (it ended on its own and nothing else goes next).
func (cs *Chats) TurnEnded(h Host, id string) (next tea.Cmd, notify bool) {
	c := cs.Conv(id)
	c.Busy, c.live = false, ""
	if c.stop != nil {
		c.stop() // releases the turn's context
		c.stop = nil
	}
	notify = c.next == nil && !c.stopped
	if c.stopped && c.next == nil {
		cs.Add(h, id, Tool, "stopped by you · your next message continues the session")
	}
	if t := h.Task(id); t != nil && c.next != nil { // the message you stopped it for
		q := *c.next
		c.next = nil
		next = cs.dispatch(h, *t, q)
	}
	return next, notify
}

// wait delivers the next agent event; the stream ends with TurnEndMsg.
func wait(id string, ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return TurnEndMsg{id}
		}
		return EventMsg{id, ev, ch}
	}
}

// AgentName is the agent a task's chat runs: Claude unless it picked another.
func AgentName(t tasks.Task) string {
	if t.Agent == "" {
		return agent.Claude.Name
	}
	return t.Agent
}

// Brief is what an agent is told about its task before your first message. A section's chat keeps
// its whole brief in the body.
func Brief(t tasks.Task) string {
	if t.Section() {
		return t.Body
	}
	s := "Task: " + t.Title
	if t.Body != "" {
		s += "\n\n" + t.Body
	}
	return s
}

// Resumable reports whether the task's session has had a turn and can be resumed.
func Resumable(t tasks.Task) bool { return t.Session != "" && HasSession(t) }

// HasSession reports whether the task's session has had a turn, in its agent's own records.
func HasSession(t tasks.Task) bool {
	if a, ok := agent.Get(AgentName(t)); ok && a.HasSession != nil {
		return a.HasSession(t.Session)
	}
	return usage.Exists(t.Session)
}
