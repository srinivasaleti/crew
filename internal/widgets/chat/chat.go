// Package chat is crew's chats with agents: each task's (and each section's) conversation, saved as
// JSON lines (Log), its turns with the agent CLI (turns.go), how it's drawn (view.go) and the
// questions Claude ends a reply with, as buttons (ask.go). crew hands it a Host for what belongs to
// crew: the tasks, where a chat runs and what its agent is told.
package chat

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/tasks"
	"crew/internal/widgets/markdown"
)

// Home is the id of crew's home screen, which has no chat; task ids never contain "_".
const Home = "_home"

// Host is what the chats need from crew.
type Host interface {
	Task(id string) *tasks.Task // a task or a section's chat, by id; nil when it's gone
	SaveTask(t tasks.Task) error
	SetStatus(id, status string)
	OnScreen() string // the id of the chat on screen, or Home
	Refresh()         // redraws the main pane
	// Intro is what a new session is told before your first message: the task, and its plan.
	Intro(t tasks.Task) string
	// Where sets up where a chat's first turn runs (a task's worktree, a section's folder); its
	// command returns a StartedMsg.
	Where(t tasks.Task, prompt string) tea.Cmd
	Args(t tasks.Task) []string // what every run of the chat's agent gets on top of its own arguments
	Context(id string) int      // the tokens a chat re-sends with each message, for CREW_AUTO_COMPACT
	Spin() tea.Cmd              // starts the working spinner
	// Placed says where a question asked from the PR tab (q.TalkKey) went in its chat: its index.
	Placed(t tasks.Task, q Outgoing, at int)
}

// Conv is one chat's in-memory state.
type Conv struct {
	Msgs    []Message
	Busy    bool
	stop    context.CancelFunc // cancels the running turn; nil when idle
	stopped bool               // the running turn was stopped by you
	next    *Outgoing          // a message you sent while the agent worked: it goes once the stopped turn ends
	live    string             // the reply as it's being written (not saved; the full text replaces it)
	started time.Time          // when the running turn began
	last    *Outgoing          // the last message sent, kept in case its turn fails on auth
	retry   *Outgoing          // a message whose turn failed because its agent was signed out
}

// Running reports whether a turn is running that you haven't stopped.
func (c *Conv) Running() bool { return c.stop != nil && !c.stopped }

// KeepForRetry keeps the last message to send again (its turn failed on sign-in).
func (c *Conv) KeepForRetry() { c.retry = c.last }

// Retry is the message kept to send again, or nil.
func (c *Conv) Retry() *Outgoing { return c.retry }

// TakeRetry is the message kept to send again, which it no longer keeps.
func (c *Conv) TakeRetry() Outgoing {
	q := *c.retry
	c.retry = nil
	return q
}

// NotForRetry stops keeping the last message to send again: it wasn't one (a command).
func (c *Conv) NotForRetry() { c.last = nil }

// Chats is every chat crew has in memory, by task id.
type Chats struct {
	ctx    context.Context // cancelled on exit; stops running agents
	log    Log
	md     *markdown.Renderer
	convs  map[string]*Conv
	Expand bool // ⌘R lists every tool call and quoted line instead of summaries
}

// New makes the chats, saved in log; md draws agent replies.
func New(ctx context.Context, log Log, md *markdown.Renderer) *Chats {
	return &Chats{ctx: ctx, log: log, md: md, convs: map[string]*Conv{}}
}

// Conv is a chat's state, its history loaded from disk the first time.
func (cs *Chats) Conv(id string) *Conv {
	c, ok := cs.convs[id]
	if !ok {
		c = &Conv{}
		if id != Home {
			c.Msgs, _ = cs.log.Load(id)
		}
		cs.convs[id] = c
	}
	return c
}

// All is every chat in memory, by id.
func (cs *Chats) All() map[string]*Conv { return cs.convs }

// Forget drops a chat from memory and deletes its log (its task was deleted).
func (cs *Chats) Forget(id string) {
	cs.log.Delete(id)
	delete(cs.convs, id)
}

// Add adds a message to a chat, saved, and redraws it if it's on screen.
func (cs *Chats) Add(h Host, id, role, text string) {
	cs.AddMsg(h, id, Message{Role: role, Text: text})
}

// AddMsg adds a message to a chat, saved, and redraws it if it's on screen.
func (cs *Chats) AddMsg(h Host, id string, msg Message) {
	msg.At = time.Now().Format(time.RFC3339)
	c := cs.Conv(id)
	c.Msgs = append(c.Msgs, msg)
	cs.log.Append(id, msg) // ponytail: a failed write only loses history, the chat keeps going
	if id == h.OnScreen() {
		h.Refresh()
	}
}
