package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/reminders"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// The chats live in their own package (chat: messages, turns, drawing, Claude's questions). This is
// crew's side of them: short ways to reach the chats from the model, and what they need from crew
// (chat.Host): the tasks, where each kind of chat runs, and what its agent is told and given.

// add adds a message to a chat.
func (m *Model) add(id, role, text string) { m.chat.Add(m, id, role, text) }

// addMsg adds a message to a chat.
func (m *Model) addMsg(id string, msg chat.Message) { m.chat.AddMsg(m, id, msg) }

// send gives a message to a task's agent (chat.Chats.Send).
func (m *Model) send(t tasks.Task, q chat.Outgoing) tea.Cmd { return m.chat.Send(m, t, q) }

// start sets up where a task's chat runs, then runs its first turn.
func (m *Model) start(t tasks.Task, prompt string) tea.Cmd { return m.chat.Start(m, t, prompt) }

// run runs one turn of a task's agent.
func (m *Model) run(t tasks.Task, prompt string, resume bool) tea.Cmd {
	return m.chat.Run(m, t, prompt, resume)
}

// stopAgent interrupts the running turn of the chat on screen. It reports whether one was running.
func (m *Model) stopAgent() bool {
	t := m.chatTask()
	if t == nil {
		return false
	}
	return m.chat.Stop(m, t.ID)
}

// Task is a task or a section's chat, by id (chat.Host).
func (m *Model) Task(id string) *tasks.Task { return m.find(id) }

// SaveTask saves a task's file (chat.Host).
func (m *Model) SaveTask(t tasks.Task) error { return m.cfg.Tasks.Save(t) }

// SetStatus changes a task's status and saves it (chat.Host).
func (m *Model) SetStatus(id, status string) { m.setStatus(id, status) }

// OnScreen is the id of the chat on screen (chat.Host).
func (m *Model) OnScreen() string { return m.currentID() }

// Refresh redraws the main pane (chat.Host).
func (m *Model) Refresh() { m.refresh() }

// Intro is what a new session is told before your first message: the task, and its plan (chat.Host).
func (m *Model) Intro(t tasks.Task) string { return chat.Brief(t) + m.planNote(t) }

// Where sets up where a chat's first turn runs (chat.Host): a section's chat in its own folder, a
// task in its worktree (made now, or the one that has its branch).
func (m *Model) Where(t tasks.Task, prompt string) tea.Cmd {
	repos := m.cfg.Repos
	switch {
	case t.PR != "":
		return m.startPRChat(t, prompt)
	case t.Kind == reqChatKind:
		return m.startReqChat(t, prompt)
	case t.Kind == remChatKind:
		return m.startRemChat(t, prompt)
	case t.Kind == jiraChatKind:
		return m.startJiraChat(t, prompt)
	}
	return func() tea.Msg {
		rp, err := repos.Resolve(t.Repo)
		var wt, base string
		switch {
		case err != nil:
		case t.Branch != "": // an existing branch: its worktree, wherever it is
			wt, _, err = repo.UseBranch(rp, t.Branch)
		default:
			wt, base, err = repo.AddWorktree(rp, t.ID)
		}
		return chat.StartedMsg{ID: t.ID, Repo: rp, Worktree: wt, Base: base, Prompt: prompt, Err: err}
	}
}

// Args is what every run of a chat's agent gets (chat.Host): crew's plugin and the plans folder,
// or, for the REQUESTS and REMINDERS chats, their lock-down.
func (m *Model) Args(t tasks.Task) []string {
	switch t.Kind {
	case reqChatKind:
		return m.reqChatArgs() // locked down: keys must never reach it
	case remChatKind:
		return reminders.ChatArgs(crewExe()) // locked down: crew remind only
	}
	return m.agentExtra(t)
}

// Context is the tokens a chat re-sends with each message (chat.Host).
func (m *Model) Context(id string) int { return m.usage[id].Context }

// Spin starts the working spinner (chat.Host).
func (m *Model) Spin() tea.Cmd { return m.startSpin() }

// Placed remembers where a question asked from the PR tab went in its chat, so its answer shows
// under the comment (chat.Host).
func (m *Model) Placed(t tasks.Task, q chat.Outgoing, at int) {
	st := m.prOf(prStateKey(t))
	st.talk[q.TalkKey] = append(st.talk[q.TalkKey], talkTurn{at, q.Asked})
}
