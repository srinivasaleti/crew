// Package core runs tasks' agents with no screen: it keeps what a UI mustn't decide (the budget,
// the queue, where a task runs) and tells whoever listens what happened. The web UI is built on it
// (internal/web); the terminal UI shares its rules (rules.go) and, later, its turns (docs/web-ui.md).
package core

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"crew/internal/agent"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/usage"
	"crew/internal/widgets/chat"
)

// Config is where core keeps and finds things: the folders crew uses.
type Config struct {
	Home                  string // crew's folder (~/.crew)
	Tasks                 tasks.Store
	Chats                 chat.Log
	Repos                 repo.Store
	Plugin, Plans, Splits string
}

// Core runs the agents of tasks.
type Core struct {
	cfg  Config
	ctx  context.Context // cancelled on exit; stops running agents
	mu   sync.Mutex
	runs map[string]*run // tasks with a turn going, by id
	subs map[chan Event]struct{}
}

// run is a task's turn in progress.
type run struct {
	stop    context.CancelFunc
	stopped bool    // you stopped it
	next    *string // a message sent meanwhile: it goes once the stopped turn ends
}

// Event is something a UI should show: a task changed ("task"), a message was added ("message"),
// a piece of the reply arrived ("delta"), or a turn started or ended ("turn").
type Event struct {
	Kind string        `json:"kind"`
	ID   string        `json:"id"`
	Msg  *chat.Message `json:"msg,omitempty"`
	Text string        `json:"text,omitempty"`
	Busy bool          `json:"busy,omitempty"`
}

// New makes a Core.
func New(ctx context.Context, cfg Config) *Core {
	return &Core{cfg: cfg, ctx: ctx, runs: map[string]*run{}, subs: map[chan Event]struct{}{}}
}

// Subscribe is the events from now until ctx ends.
func (c *Core) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 256)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	go func() { <-ctx.Done(); c.mu.Lock(); delete(c.subs, ch); c.mu.Unlock() }()
	return ch
}

// publish tells every subscriber. ponytail: a page too slow to keep up misses events; it reloads the
// task's chat when it reconnects. Add sequence numbers if that ever shows.
func (c *Core) publish(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// TaskView is a task as a UI lists it.
type TaskView struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Label   string `json:"label"` // the status as it reads: "In progress"
	Icon    string `json:"icon"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Working bool   `json:"working"`
}

func (c *Core) view(t tasks.Task) TaskView {
	c.mu.Lock()
	_, working := c.runs[t.ID]
	c.mu.Unlock()
	return TaskView{t.ID, t.Title, t.Status, tasks.Labels[t.Status], tasks.Icons[t.Status], t.Repo, t.Branch, chat.AgentName(t), t.Model, t.Issue, working}
}

// statusOrder is how a UI groups tasks: what's moving first.
var statusOrder = map[string]int{tasks.InProgress: 0, tasks.Todo: 1, tasks.InPR: 2, tasks.Blocked: 3, tasks.Done: 4}

// List is the tasks to show, grouped by status and newest first within one: not the chats sections
// keep, not the archived.
func (c *Core) List() ([]TaskView, error) {
	all, err := c.cfg.Tasks.Load()
	var out []TaskView
	for _, t := range all {
		if !t.Section() && !t.Archived {
			out = append(out, c.view(t))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return statusOrder[out[i].Status] < statusOrder[out[j].Status] })
	return out, err
}

// NewTask is what a new task needs; the rest is as the terminal UI's form makes it.
type NewTask struct{ Title, Repo, Body, Agent, Model, Branch string }

// Create saves a new task. Its repo is attached if it wasn't.
func (c *Core) Create(n NewTask) (TaskView, error) {
	n.Title = strings.TrimSpace(n.Title)
	if n.Title == "" {
		return TaskView{}, errors.New("give the task a name")
	}
	if n.Agent == "" {
		n.Agent = agent.Claude.Name
	}
	if _, ok := agent.Get(n.Agent); !ok {
		return TaskView{}, errors.New("unknown agent " + n.Agent)
	}
	rp, err := c.cfg.Repos.Attach(strings.TrimSpace(n.Repo))
	if err != nil {
		return TaskView{}, err
	}
	t := tasks.Task{ID: c.cfg.Tasks.NewID(n.Title), Title: n.Title, Repo: rp, Agent: n.Agent, Model: n.Model,
		Branch: strings.TrimSpace(n.Branch), Status: tasks.Todo, Created: time.Now(), Body: strings.TrimSpace(n.Body)}
	if err := c.cfg.Tasks.Save(t); err != nil {
		return TaskView{}, err
	}
	c.publish(Event{Kind: "task", ID: t.ID})
	return c.view(t), nil
}

// UI is which UI the app opens next time; SetUI chooses it.
func (c *Core) UI() string           { return UIMode(c.cfg.Home) }
func (c *Core) SetUI(m string) error { return SetUIMode(c.cfg.Home, m) }

// Chat is a task's messages.
func (c *Core) Chat(id string) ([]chat.Message, error) { return c.cfg.Chats.Load(id) }

// task is a task by id, or why not.
func (c *Core) task(id string) (tasks.Task, error) {
	all, err := c.cfg.Tasks.Load()
	for _, t := range all {
		if t.ID == id {
			return t, nil
		}
	}
	if err == nil {
		err = errors.New("no task " + id)
	}
	return tasks.Task{}, err
}

// say adds a message to a task's chat, saved and published.
func (c *Core) say(id, role, text string, sub bool) {
	m := chat.Message{Role: role, Text: text, At: time.Now().Format(time.RFC3339), Sub: sub}
	c.cfg.Chats.Append(id, m) // ponytail: a failed write only loses history, the chat keeps going
	c.publish(Event{Kind: "message", ID: id, Msg: &m})
}

// Spent is what a task's sessions cost so far, in USD.
func (c *Core) Spent(t tasks.Task) float64 {
	u, _ := usage.LoadTask(t.Session, t.Previous)
	return u.Cost
}

// Send gives a message to a task's agent. Over its budget, nothing is sent. While the agent works,
// the turn is stopped and the message goes as soon as it ends, so you can steer it (Claude Code
// keeps the stopped turn's work in the session).
func (c *Core) Send(id, text string) error {
	t, err := c.task(id)
	if err != nil {
		return err
	}
	if t.Section() {
		return errors.New("not a task")
	}
	if spent, budget := c.Spent(t), BudgetOf(t); budget > 0 && spent >= budget {
		return errors.New(BudgetNote(spent, budget))
	}
	c.mu.Lock()
	if r := c.runs[id]; r != nil {
		if r.next != nil { // sent again before the stop finished: both go together
			*r.next += "\n\n" + text
		} else {
			r.next = &text
		}
		first := !r.stopped
		r.stopped = true
		c.mu.Unlock()
		if first {
			r.stop()
			c.say(id, chat.Tool, "stopping to take your message…", false)
		}
		return nil
	}
	ctx, stop := context.WithCancel(c.ctx)
	c.runs[id] = &run{stop: stop}
	c.mu.Unlock()
	go c.turns(ctx, id, text)
	return nil
}

// Stop interrupts a task's running turn. It reports whether one was running.
func (c *Core) Stop(id string) bool {
	c.mu.Lock()
	r := c.runs[id]
	if r == nil || r.stopped {
		c.mu.Unlock()
		return false
	}
	r.stopped = true
	c.mu.Unlock()
	r.stop()
	c.say(id, chat.Tool, "stopping…", false)
	return true
}

// turns runs a task's turn, then the one for the message sent while it ran, until none is left.
func (c *Core) turns(ctx context.Context, id, text string) {
	c.publish(Event{Kind: "task", ID: id})
	c.publish(Event{Kind: "turn", ID: id, Busy: true})
	for {
		c.turn(ctx, id, text)
		c.mu.Lock()
		r := c.runs[id]
		r.stop() // releases the turn's context
		if r.next == nil {
			stopped := r.stopped
			delete(c.runs, id)
			c.mu.Unlock()
			if stopped {
				c.say(id, chat.Tool, "stopped by you · your next message continues the session", false)
			}
			c.publish(Event{Kind: "turn", ID: id})
			c.publish(Event{Kind: "task", ID: id})
			return
		}
		text, r.next, r.stopped = *r.next, nil, false
		ctx, r.stop = context.WithCancel(c.ctx)
		c.mu.Unlock()
	}
}

// turn is one run of the task's agent: the worktree and session on the first message, then the
// agent's events into the chat.
func (c *Core) turn(ctx context.Context, id, text string) {
	t, err := c.task(id)
	if err != nil {
		c.say(id, chat.Error, err.Error(), false)
		return
	}
	c.say(id, chat.You, text, false)
	prompt, resume := text, true
	intro := func() string { return chat.Brief(t) + PlanNote(t, filepath.Join(c.cfg.Plans, t.ID+".md")) + "\n\n" }
	switch {
	case t.Session == "":
		c.say(id, chat.Tool, "setting up worktree…", false)
		parent := ""
		if p, err := c.task(t.Parent); err == nil {
			parent = p.Worktree
		}
		rp, wt, base, err := Workspace(c.cfg.Repos, t, parent)
		if err != nil {
			c.say(id, chat.Error, err.Error(), false)
			return
		}
		t.Repo, t.Worktree, t.Base, t.Session = rp, wt, base, agent.NewSessionID()
		c.cfg.Tasks.Save(t)
		c.say(id, chat.Tool, "worktree ready: "+wt, false)
		prompt, resume = intro()+text, false
	case !chat.HasSession(t): // the first message after /new: a new session, told the task again
		prompt, resume = intro()+text, false
	}
	if t.Status == tasks.Todo { // the first message starts the work
		t.Status = tasks.InProgress
		c.cfg.Tasks.Save(t)
		c.publish(Event{Kind: "task", ID: id})
	}
	a, ok := agent.Get(chat.AgentName(t))
	if !ok {
		c.say(id, chat.Error, "unknown agent "+chat.AgentName(t), false)
		return
	}
	for ev := range a.Run(ctx, t.Worktree, t.Session, resume, t.Model, prompt, AgentArgs(t, c.cfg.Plugin, c.cfg.Plans, c.cfg.Splits)) {
		switch ev.Kind {
		case agent.KindDelta:
			if !ev.Sub {
				c.publish(Event{Kind: "delta", ID: id, Text: ev.Text})
			}
		case agent.KindText:
			c.say(id, chat.Agent, ev.Text, ev.Sub)
		case agent.KindTool:
			c.say(id, chat.Tool, ev.Text, ev.Sub)
		case agent.KindResult:
			c.say(id, chat.Result, ev.Text, ev.Sub)
		case agent.KindError:
			c.say(id, chat.Error, turnError(a, ev.Text), false)
		}
	}
}

// turnError says a failed turn's error plainly when it means the agent can't be used.
func turnError(a agent.Agent, text string) string {
	switch {
	case strings.Contains(text, "executable file not found"):
		return a.Title + "'s CLI (" + a.Command + ") isn't installed, or crew can't find it"
	case a.AuthError != nil && a.AuthError(text):
		return a.Title + "'s login expired, or you're signed out · sign in with `" + a.Command + "` in a terminal, then send your message again\n\n" + a.Title + " said: " + text
	}
	return text
}
