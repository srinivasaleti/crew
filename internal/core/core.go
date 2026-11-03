// Package core runs tasks' agents with no screen: it keeps what a UI mustn't decide (the budget,
// the queue, where a task runs) and tells whoever listens what happened. The web UI is built on it
// (internal/ui/web); the terminal UI shares its rules (rules.go) and, later, its turns (docs/web-ui.md).
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"crew/internal/agent"
	"crew/internal/plan"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/usage"
)

// Config is where core keeps and finds things: the folders crew uses.
type Config struct {
	Home                  string // crew's folder (~/.crew)
	Tasks                 tasks.Store
	Chats                 Log
	Repos                 repo.Store
	Plugin, Plans, Splits string
}

// Core runs the agents of tasks.
type Core struct {
	cfg   Config
	ctx   context.Context // cancelled on exit; stops running agents
	mu    sync.Mutex
	runs  map[string]*run // tasks with a turn going, by id
	subs  map[chan Event]struct{}
	kinds map[string]ChatKind
}

// ChatKind is how a section's chat runs (a PR's, REQUESTS', a container's): where its agent works
// and what it's given on top of its own arguments, which is how a locked-down chat stays locked down.
type ChatKind struct {
	Dir  func(t tasks.Task) (string, error)
	Args func(t tasks.Task) []string
}

// RegisterChat says how chats of a kind run: a task file's kind:, or "pr" for one with pr:.
func (c *Core) RegisterChat(kind string, k ChatKind) {
	c.mu.Lock()
	c.kinds[kind] = k
	c.mu.Unlock()
}

// kindOf is the kind of a section's chat, and how it runs; ok is false for one nobody registered.
func (c *Core) kindOf(t tasks.Task) (ChatKind, bool) {
	kind := t.Kind
	if t.PR != "" {
		kind = "pr"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.kinds[kind]
	return k, ok
}

// FindChat is the section chat that match picks, or, when there's none, t saved as a new one (its id
// made from its title unless it has one, so the terminal's and the web's names agree). A section uses it for "the chat about this PR / this container".
func (c *Core) FindChat(match func(tasks.Task) bool, t tasks.Task) (tasks.Task, error) {
	all, _ := c.cfg.Tasks.Load()
	for _, x := range all {
		if x.Section() && match(x) {
			return x, nil
		}
	}
	if t.ID == "" {
		t.ID = c.cfg.Tasks.NewID(t.Title)
	}
	t.Created = time.Now()
	return t, c.cfg.Tasks.Save(t)
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
	Kind string   `json:"kind"`
	ID   string   `json:"id"`
	Msg  *Message `json:"msg,omitempty"`
	Text string   `json:"text,omitempty"`
	Busy bool     `json:"busy,omitempty"`
	Data any      `json:"data,omitempty"` // a section's own news (Kind is its key: "reminders")
}

// New makes a Core.
func New(ctx context.Context, cfg Config) *Core {
	return &Core{cfg: cfg, ctx: ctx, runs: map[string]*run{}, subs: map[chan Event]struct{}{}, kinds: map[string]ChatKind{}}
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

// Publish tells every UI listening: a section's background work uses it for its own news.
func (c *Core) Publish(ev Event) { c.publish(ev) }

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
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Status   string  `json:"status"`
	Label    string  `json:"label"` // the status as it reads: "In progress"
	Icon     string  `json:"icon"`
	Repo     string  `json:"repo"`
	Branch   string  `json:"branch,omitempty"`
	Agent    string  `json:"agent"`
	Model    string  `json:"model,omitempty"`
	Issue    string  `json:"issue,omitempty"`
	Working  bool    `json:"working"`
	Archived bool    `json:"archived,omitempty"`
	Body     string  `json:"body,omitempty"`
	Cost     float64 `json:"cost"`              // what its agent has spent, in USD
	Budget   float64 `json:"budget,omitempty"`  // what it may spend; 0 for none
	Plan     string  `json:"plan,omitempty"`    // its plan's progress, "2/5"
	Started  bool    `json:"started"`           // its first message set up its worktree
	Updated  string  `json:"updated,omitempty"` // RFC 3339
	Parent   string  `json:"parent,omitempty"`  // the task it's a subtask of
}

func (c *Core) view(t tasks.Task) TaskView {
	c.mu.Lock()
	_, working := c.runs[t.ID]
	c.mu.Unlock()
	v := TaskView{ID: t.ID, Title: t.Title, Status: t.Status, Label: tasks.Labels[t.Status], Icon: tasks.Icons[t.Status],
		Repo: t.Repo, Branch: t.Branch, Agent: AgentName(t), Model: t.Model, Issue: t.Issue, Working: working,
		Archived: t.Archived, Body: t.Body, Cost: c.Spent(t), Budget: BudgetOf(t), Started: t.Worktree != "", Parent: t.Parent}
	if !t.Updated.IsZero() {
		v.Updated = t.Updated.Format(time.RFC3339)
	}
	if p := plan.Parse(c.Plan(t.ID)); len(p.Steps) > 0 {
		v.Plan = fmt.Sprintf("%d/%d", p.Count(plan.Done), len(p.Steps))
	}
	return v
}

// Statuses are a task's statuses in the order a menu lists them.
var Statuses = []string{tasks.Todo, tasks.InProgress, tasks.InPR, tasks.Blocked, tasks.Done}

// statusOrder is how a UI groups tasks: what's moving first.
var statusOrder = map[string]int{tasks.InProgress: 0, tasks.Todo: 1, tasks.InPR: 2, tasks.Blocked: 3, tasks.Done: 4}

// List is the tasks to show, grouped by status and newest first within one: not the chats sections
// keep, and the archived only when asked for.
func (c *Core) List(archived bool) ([]TaskView, error) {
	all, err := c.cfg.Tasks.Load()
	var out []TaskView
	for _, t := range all {
		if !t.Section() && (archived || !t.Archived) {
			out = append(out, c.view(t))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return statusOrder[out[i].Status] < statusOrder[out[j].Status] })
	return out, err
}

// NewTask is what a new task needs; the rest is as the terminal UI's form makes it.
type NewTask struct{ Title, Repo, Body, Agent, Model, Branch, Issue string }

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
		Branch: strings.TrimSpace(n.Branch), Issue: n.Issue, Status: tasks.Todo, Created: time.Now(), Body: strings.TrimSpace(n.Body)}
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
func (c *Core) Chat(id string) ([]Message, error) { return c.cfg.Chats.Load(id) }

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
	m := Message{Role: role, Text: text, At: time.Now().Format(time.RFC3339), Sub: sub}
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
	if _, ok := c.kindOf(t); t.Section() && !ok {
		return errors.New("this chat isn't in the web UI yet")
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
			c.say(id, Tool, "stopping to take your message…", false)
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
	c.say(id, Tool, "stopping…", false)
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
				c.say(id, Tool, "stopped by you · your next message continues the session", false)
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
		c.say(id, Error, err.Error(), false)
		return
	}
	c.say(id, You, text, false)
	prompt, resume := text, true
	intro := func() string { return Brief(t) + PlanNote(t, filepath.Join(c.cfg.Plans, t.ID+".md")) + "\n\n" }
	args := AgentArgs(t, c.cfg.Plugin, c.cfg.Plans, c.cfg.Splits)
	kind, section := c.kindOf(t)
	section = section && t.Section()
	if section && kind.Args != nil {
		args = kind.Args(t) // e.g. REQUESTS', locked down so keys never reach it
	}
	switch {
	case section && t.Session == "": // a section's chat runs in its own folder
		dir, err := kind.Dir(t)
		if err != nil {
			c.say(id, Error, err.Error(), false)
			return
		}
		t.Worktree, t.Session = dir, agent.NewSessionID()
		c.cfg.Tasks.Save(t)
		prompt, resume = Brief(t)+"\n\n"+text, false
	case t.Session == "":
		c.say(id, Tool, "setting up worktree…", false)
		parent := ""
		if p, err := c.task(t.Parent); err == nil {
			parent = p.Worktree
		}
		rp, wt, base, err := Workspace(c.cfg.Repos, t, parent)
		if err != nil {
			c.say(id, Error, err.Error(), false)
			return
		}
		t.Repo, t.Worktree, t.Base, t.Session = rp, wt, base, agent.NewSessionID()
		c.cfg.Tasks.Save(t)
		c.say(id, Tool, "worktree ready: "+wt, false)
		prompt, resume = intro()+text, false
	case !HasSession(t): // the first message after /new: a new session, told the task again
		prompt, resume = intro()+text, false
	}
	if t.Status == tasks.Todo && !section { // the first message starts the work
		t.Status = tasks.InProgress
		c.cfg.Tasks.Save(t)
		c.publish(Event{Kind: "task", ID: id})
	}
	a, ok := agent.Get(AgentName(t))
	if !ok {
		c.say(id, Error, "unknown agent "+AgentName(t), false)
		return
	}
	for ev := range a.Run(ctx, t.Worktree, t.Session, resume, t.Model, prompt, args) {
		switch ev.Kind {
		case agent.KindDelta:
			if !ev.Sub {
				c.publish(Event{Kind: "delta", ID: id, Text: ev.Text})
			}
		case agent.KindText:
			c.say(id, Agent, ev.Text, ev.Sub)
		case agent.KindTool:
			c.say(id, Tool, ev.Text, ev.Sub)
		case agent.KindResult:
			c.say(id, Result, ev.Text, ev.Sub)
		case agent.KindError:
			c.say(id, Error, turnError(a, ev.Text), false)
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

// SetStatus changes a task's status (as the terminal's status menu does).
func (c *Core) SetStatus(id, status string) error {
	if _, ok := tasks.Labels[status]; !ok && status != "archive" && status != "unarchive" {
		return errors.New("no status " + status)
	}
	t, err := c.task(id)
	if err != nil {
		return err
	}
	tasks.SetStatus(&t, status)
	if err := c.cfg.Tasks.Save(t); err != nil {
		return err
	}
	c.publish(Event{Kind: "task", ID: id})
	return nil
}

// Plan is a task's plan as written (Markdown), "" when it has none.
func (c *Core) Plan(id string) string {
	b, _ := os.ReadFile(filepath.Join(c.cfg.Plans, id+".md"))
	return string(b)
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Changes is what a task's agent changed in its worktree: the diff and a one-line summary.
func (c *Core) Changes(id string) (diff, summary string, err error) {
	t, err := c.task(id)
	if err != nil || t.Worktree == "" {
		return "", "nothing yet: the first message sets up the worktree", err
	}
	diff, summary, err = repo.Diff(t.Worktree, t.Base)
	return ansi.ReplaceAllString(diff, ""), summary, err
}

// Edit renames a task and changes its description (what a new session of its agent is told).
func (c *Core) Edit(id, title, body string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("give the task a name")
	}
	t, err := c.task(id)
	if err != nil {
		return err
	}
	t.Title, t.Body = strings.TrimSpace(title), strings.TrimSpace(body)
	if err := c.cfg.Tasks.Save(t); err != nil {
		return err
	}
	c.publish(Event{Kind: "task", ID: id})
	return nil
}

// OwnWorktree is the worktree crew made for a task (<repo>.worktrees/<id>, on its own branch <id>),
// which deleting the task removes; "" for none, or for a branch that was there before the task (the
// New task form's Branch, /branch), whose worktree may be yours and is kept.
func OwnWorktree(t tasks.Task) string {
	if t.Worktree == "" || t.Branch != "" || filepath.Base(t.Worktree) != t.ID {
		return ""
	}
	return t.Worktree
}

// DeleteLoss is what deleting a task would lose with its worktree (uncommitted changes, unpushed
// commits), "" for nothing.
func (c *Core) DeleteLoss(id string) string {
	t, err := c.task(id)
	if err != nil || OwnWorktree(t) == "" {
		return ""
	}
	return repo.Unsaved(t.Worktree)
}

// Delete deletes a task, its chat, plan, proposed split and PR description, and the worktree and
// branch crew made for it (a branch that was there before is kept). note says what happened to it.
func (c *Core) Delete(id string) (note string, err error) {
	t, err := c.task(id)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	_, busy := c.runs[id]
	c.mu.Unlock()
	if busy {
		return "", errors.New(t.Title + " is busy, wait for its agent to finish")
	}
	if err := c.cfg.Tasks.Delete(id); err != nil {
		return "", err
	}
	for _, p := range []string{filepath.Join(c.cfg.Plans, id+".md"), filepath.Join(c.cfg.Splits, id+".md"), filepath.Join(c.cfg.Plans, id+".pr.md")} {
		os.Remove(p)
	}
	c.cfg.Chats.Delete(id)
	c.publish(Event{Kind: "task", ID: id})
	switch own := OwnWorktree(t); {
	case own != "":
		rp, err := c.cfg.Repos.Resolve(t.Repo)
		if err == nil {
			err = repo.RemoveWorktree(rp, own, id)
		}
		if err != nil {
			return "Deleted " + t.Title + ", but its worktree is still at " + own + ": " + err.Error(), nil
		}
		return "Deleted " + t.Title + " and its worktree", nil
	case t.Worktree != "":
		return "Deleted " + t.Title + " (its branch was there before it, so its worktree is kept at " + t.Worktree + ")", nil
	}
	return "Deleted " + t.Title, nil
}

// Repos are the attached repos, for picking one for a new task.
func (c *Core) Repos() []string { return c.cfg.Repos.List() }
