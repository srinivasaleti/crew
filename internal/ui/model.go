// Package ui is the crew terminal UI: a sidebar of chats (crew home + one per task) and a chat pane.
package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/home"
	"crew/internal/repo"
	"crew/internal/task"
)

type Config struct {
	Ctx   context.Context // cancelled on exit; stops running agents
	Home  string          // crew data dir, working dir for the home assistant
	Tasks task.Store
	Chats chat.Log
	Repos repo.Store
}

const homeID = "_home" // chat log id of the home chat; task ids never contain "_"

// conv is one chat's in-memory state.
type conv struct {
	msgs []chat.Message
	busy bool
}

type (
	homeMsg struct {
		reply home.Reply
		err   error
	}
	startedMsg struct {
		id, repo, worktree, prompt string
		err                        error
	}
	eventMsg struct {
		id string
		ev agent.Event
		ch <-chan agent.Event
	}
	turnEndMsg struct{ id string }
)

type Model struct {
	cfg     Config
	tasks   []task.Task
	loadErr string
	sel     int // 0 = home chat, i = tasks[i-1]
	convs   map[string]*conv
	input   textinput.Model
	vp      viewport.Model
	w, h    int
}

func New(cfg Config) Model {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "Message crew…"
	in.Focus()
	m := Model{cfg: cfg, convs: map[string]*conv{}, input: in, vp: viewport.New(0, 0)}
	m.reload()
	return m
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

// reload re-reads tasks from disk, keeping the selected chat.
func (m *Model) reload() {
	cur := m.currentID()
	tasks, err := m.cfg.Tasks.Load()
	m.tasks, m.loadErr = tasks, ""
	if err != nil {
		m.loadErr = err.Error()
	}
	m.sel = 0
	m.selectID(cur)
}

func (m *Model) selectID(id string) {
	for i, t := range m.tasks {
		if t.ID == id {
			m.sel = i + 1
		}
	}
	m.onSelect()
}

func (m *Model) onSelect() {
	if t := m.current(); t != nil {
		m.input.Placeholder = "Message " + agentName(*t) + " about " + t.Title + "…"
	} else {
		m.input.Placeholder = "Message crew…"
	}
	m.refresh()
}

func (m Model) current() *task.Task {
	if m.sel == 0 || m.sel > len(m.tasks) {
		return nil
	}
	return &m.tasks[m.sel-1]
}

func (m Model) currentID() string {
	if t := m.current(); t != nil {
		return t.ID
	}
	return homeID
}

func (m *Model) find(id string) *task.Task {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			return &m.tasks[i]
		}
	}
	return nil
}

// conv returns a chat's state, loading its history from disk the first time.
func (m *Model) conv(id string) *conv {
	c, ok := m.convs[id]
	if !ok {
		msgs, _ := m.cfg.Chats.Load(id)
		c = &conv{msgs: msgs}
		m.convs[id] = c
	}
	return c
}

func (m *Model) add(id, role, text string) {
	msg := chat.Message{Role: role, Text: text}
	c := m.conv(id)
	c.msgs = append(c.msgs, msg)
	m.cfg.Chats.Append(id, msg) // ponytail: a failed write only loses history, the chat keeps going
	if id == m.currentID() {
		m.refresh()
	}
}

func (m *Model) setStatus(id, status string) {
	if t := m.find(id); t != nil && t.Status != status {
		t.Status = status
		m.cfg.Tasks.Save(*t)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.vp.Width = max(m.w-sidebarWidth-3, 10)
		m.vp.Height = max(m.h-5, 3)
		m.input.Width = max(m.w-4, 10)
		m.refresh()
		return m, nil

	case homeMsg:
		m.conv(homeID).busy = false
		if msg.err != nil {
			m.add(homeID, chat.Error, msg.err.Error())
			return m, nil
		}
		if msg.reply.Reply != "" {
			m.add(homeID, chat.Agent, msg.reply.Reply)
		}
		return m, tea.Batch(m.apply(msg.reply.Actions)...)

	case startedMsg:
		t := m.find(msg.id)
		if msg.err != nil || t == nil {
			m.conv(msg.id).busy = false
			if msg.err != nil {
				m.add(msg.id, chat.Error, msg.err.Error())
			}
			return m, nil
		}
		t.Repo, t.Worktree, t.Session = msg.repo, msg.worktree, agent.NewSessionID()
		m.cfg.Tasks.Save(*t)
		m.add(t.ID, chat.Tool, "worktree ready: "+msg.worktree)
		return m, m.run(*t, msg.prompt, false)

	case eventMsg:
		switch msg.ev.Kind {
		case agent.KindText:
			m.add(msg.id, chat.Agent, msg.ev.Text)
		case agent.KindTool:
			m.add(msg.id, chat.Tool, msg.ev.Text)
		case agent.KindError:
			m.add(msg.id, chat.Error, msg.ev.Text)
		}
		return m, wait(msg.id, msg.ch)

	case turnEndMsg:
		m.conv(msg.id).busy = false
		m.setStatus(msg.id, task.Waiting)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			if m.sel > 0 {
				m.sel--
				m.onSelect()
			}
			return m, nil
		case "down":
			if m.sel < len(m.tasks) {
				m.sel++
				m.onSelect()
			}
			return m, nil
		case "esc":
			m.sel = 0
			m.onSelect()
			return m, nil
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		case "enter":
			return m, m.submit()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submit sends the typed message to the selected chat.
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	id := m.currentID()
	if text == "" || m.conv(id).busy {
		return nil
	}
	m.input.Reset()
	m.add(id, chat.You, text)
	t := m.current()
	switch {
	case t == nil:
		return m.askHome(text)
	case t.Session == "":
		return m.start(*t, brief(*t)+"\n\n"+text)
	default:
		return m.run(*t, text, true)
	}
}

func (m *Model) askHome(text string) tea.Cmd {
	c := m.conv(homeID)
	c.busy = true
	hist := c.msgs[:len(c.msgs)-1] // the new message is passed separately
	if len(hist) > 20 {
		hist = hist[len(hist)-20:]
	}
	hist = slices.Clone(hist)
	tasks, repos, agents := slices.Clone(m.tasks), m.cfg.Repos.List(), agentNames()
	ctx, dir := m.cfg.Ctx, m.cfg.Home
	return func() tea.Msg {
		r, err := home.Ask(ctx, dir, tasks, repos, agents, hist, text)
		return homeMsg{r, err}
	}
}

// start creates the task's worktree in the background, then runs the first agent turn.
func (m *Model) start(t task.Task, prompt string) tea.Cmd {
	m.conv(t.ID).busy = true
	m.add(t.ID, chat.Tool, "setting up worktree…")
	repos := m.cfg.Repos
	return func() tea.Msg {
		rp, err := repos.Resolve(t.Repo)
		var wt string
		if err == nil {
			wt, err = repo.AddWorktree(rp, t.ID)
		}
		return startedMsg{t.ID, rp, wt, prompt, err}
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
	m.setStatus(t.ID, task.Working)
	return wait(t.ID, a.Run(m.cfg.Ctx, t.Worktree, t.Session, resume, prompt))
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

func agentNames() []string {
	var names []string
	for _, a := range agent.All {
		names = append(names, a.Name)
	}
	return names
}

// brief is the first prompt an agent gets for its task.
func brief(t task.Task) string {
	s := "Task: " + t.Title
	if t.Body != "" {
		s += "\n\n" + t.Body
	}
	return s
}
