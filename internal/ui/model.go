// Package ui is the crew terminal UI: a sidebar (crew home + one chat per task) and a main pane.
package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/task"
)

type Config struct {
	Ctx   context.Context // cancelled on exit; stops running agents
	Tasks task.Store
	Chats chat.Log
	Repos repo.Store
}

const homeID = "_home" // id of the home screen; task ids never contain "_"

// conv is one task chat's in-memory state.
type conv struct {
	msgs []chat.Message
	busy bool
}

type (
	startedMsg struct {
		id, repo, worktree, base, prompt string
		err                              error
	}
	diffMsg struct {
		id, diff, summary string
		err               error
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
	sel     int // 0 = home, i = tasks[i-1]
	convs   map[string]*conv
	input   textarea.Model // message box; grows up to maxInputLines
	vp      viewport.Model
	w, h    int
	notice  string // one-line feedback shown above the help line, cleared on the next key

	changes     bool   // main pane shows the selected task's changes instead of its chat
	listFocus   bool   // keys drive the task list (← from an empty chat box); → or enter returns
	diffSummary string // e.g. "3 files changed, 10 insertions(+)"; "loading…" while git runs

	// modal windows
	modal   modal
	field   int // focused field in the new-task form: 0 name, 1 folder
	name    textinput.Model
	folder  textinput.Model
	formErr string
}

func New(cfg Config) Model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "> "
		}
		return "  "
	})
	in.FocusedStyle.CursorLine = lipgloss.NewStyle()
	in.FocusedStyle.Base = lipgloss.NewStyle()
	in.BlurredStyle.Base = lipgloss.NewStyle()
	// Terminals send Shift+Enter as plain Enter, so new lines use keys that arrive distinctly.
	// Enter itself sends (handled in Update); "" then Enter also inserts a new line.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	name := textinput.New()
	name.Placeholder = "Sort orgs alphabetically"
	name.CharLimit = 100
	folder := textinput.New()
	folder.Placeholder = "~/code/my-app"
	folder.ShowSuggestions = true
	m := Model{cfg: cfg, convs: map[string]*conv{}, input: in, vp: viewport.New(0, 0), name: name, folder: folder}
	m.reload()
	return m
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

const maxInputLines = 6

// shiftEnter holds the ways terminals report Shift+Enter once main turns on xterm's
// modifyOtherKeys (ESC[>4;1m): xterm style and CSI-u style. bubbletea v1 doesn't decode
// them and delivers them as an unknown-CSI message whose String() has this form.
var shiftEnter = map[string]bool{
	fmt.Sprintf("?CSI%+v?", []byte("27;2;13~")): true,
	fmt.Sprintf("?CSI%+v?", []byte("13;2u")):    true,
}

// layout sizes the message box to its content and gives the rest of the height to the chat.
func (m *Model) layout() {
	m.input.SetWidth(max(m.w-2, 10))
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
	m.vp.Width = max(m.w-sidebarWidth-5, 10) // leaves room for the scroll bar
	m.vp.Height = max(m.h-4-m.inputHeight(), 3)
}

// inputHeight is how many rows the bottom input area takes (the home screen shows a one-line hint).
func (m Model) inputHeight() int {
	if m.current() == nil || m.changes {
		return 1
	}
	return m.input.Height()
}

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
		if !m.listFocus {
			m.input.Focus()
		}
	} else {
		m.input.Blur()
	}
	m.layout()
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

// conv returns a task chat's state, loading its history from disk the first time.
func (m *Model) conv(id string) *conv {
	c, ok := m.convs[id]
	if !ok {
		c = &conv{}
		if id != homeID {
			c.msgs, _ = m.cfg.Chats.Load(id)
		}
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
		m.layout()
		m.name.Width, m.folder.Width = 44, 44
		m.refresh()
		return m, nil

	case startedMsg:
		t := m.find(msg.id)
		if msg.err != nil || t == nil {
			m.conv(msg.id).busy = false
			if msg.err != nil {
				m.add(msg.id, chat.Error, msg.err.Error())
			}
			return m, nil
		}
		t.Repo, t.Worktree, t.Base, t.Session = msg.repo, msg.worktree, msg.base, agent.NewSessionID()
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
		if m.changes && msg.id == m.currentID() {
			return m, m.loadDiff() // the agent may have changed files; keep the open view current
		}
		return m, nil

	case diffMsg:
		if !m.changes || msg.id != m.currentID() {
			return m, nil
		}
		if msg.err != nil {
			m.diffSummary = "error"
			m.vp.SetContent(warn.Render(msg.err.Error()))
			return m, nil
		}
		m.diffSummary = msg.summary
		m.vp.SetContent(clip(msg.diff, m.vp.Width))
		return m, nil

	case tea.MouseMsg: // wheel/trackpad scrolls the chat or the diff; must stay before fmt.Stringer
		if m.modal != noModal {
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		m.notice = ""
		if m.modal != noModal {
			return m.updateModal(msg)
		}
		if m.changes {
			return m.updateChanges(msg)
		}
		if m.listFocus {
			return m.updateList(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+n":
			return m, m.openNew()
		case "ctrl+d":
			m.openDelete()
			return m, nil
		case "tab", "shift+tab", "ctrl+g":
			return m, m.openChanges()
		case "right":
			if m.input.Value() == "" { // with text typed, arrows move the cursor instead
				return m, m.openChanges()
			}
		case "left":
			if m.input.Value() == "" && m.current() != nil {
				m.listFocus = true
				m.input.Blur()
				return m, nil
			}
		case "up":
			if m.input.LineCount() > 1 {
				break // move between lines of a multi-line message
			}
			if m.sel > 0 {
				m.sel--
				m.onSelect()
			}
			return m, nil
		case "down":
			if m.input.LineCount() > 1 {
				break
			}
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
			if v := m.input.Value(); strings.HasSuffix(v, "\\") {
				m.input.SetValue(strings.TrimSuffix(v, "\\") + "\n")
				m.layout()
				return m, nil
			}
			return m, m.submit()
		}

	case fmt.Stringer: // must stay after tea.KeyMsg, which is also a Stringer
		if shiftEnter[msg.String()] && m.modal == noModal && m.current() != nil {
			m.input.InsertString("\n")
			m.layout()
			return m, nil
		}
	}
	if m.modal != noModal {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd
}

// submit sends the typed message to the selected task's agent.
func (m *Model) submit() tea.Cmd {
	t := m.current()
	text := strings.TrimSpace(m.input.Value())
	if t == nil || text == "" || m.conv(t.ID).busy {
		return nil
	}
	m.input.Reset()
	m.layout()
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

// brief is what an agent is told about its task before your first message.
func brief(t task.Task) string {
	s := "Task: " + t.Title
	if t.Body != "" {
		s += "\n\n" + t.Body
	}
	return s
}

// updateList handles keys while the task list has focus.
func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+n":
		m.listFocus = false
		return m, m.openNew()
	case "ctrl+d":
		m.openDelete()
	case "up":
		if m.sel > 0 {
			m.sel--
			m.onSelect()
		}
	case "down":
		if m.sel < len(m.tasks) {
			m.sel++
			m.onSelect()
		}
	case "right", "enter", "tab":
		if m.current() != nil {
			m.listFocus = false
			return m, m.input.Focus()
		}
	case "esc":
		m.sel = 0
		m.onSelect()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}
