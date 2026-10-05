// Package ui is the crew terminal UI: a sidebar (crew home + one chat per task) and a main pane.
package ui

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/chat"
	"crew/internal/repo"
	"crew/internal/task"
	"crew/internal/usage"
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

	tab         tab                    // which tab of the selected task the main pane shows
	usage       map[string]usage.Usage // per task, read from Claude's session logs
	listFocus   bool                   // keys drive the task list (← from an empty chat box); → or enter returns
	diffSummary string                 // e.g. "3 files changed, 10 insertions(+)"; "loading…" while git runs

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
	// Enter itself sends (handled in updateChat); a trailing \ then Enter also inserts a new line.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	name := textinput.New()
	name.Placeholder = "Sort orgs alphabetically"
	name.CharLimit = 100
	folder := textinput.New()
	folder.Placeholder = "~/code/my-app"
	folder.ShowSuggestions = true
	m := Model{cfg: cfg, convs: map[string]*conv{}, usage: map[string]usage.Usage{}, input: in, vp: viewport.New(0, 0), name: name, folder: folder}
	m.reload()
	return m
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

const maxInputLines = 6

// layout sizes the message box to its content and gives the rest of the height to the chat.
func (m *Model) layout() {
	m.input.SetWidth(max(m.w-2, 10))
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
	m.vp.Width = max(m.w-sidebarWidth-5, 10) // leaves room for the scroll bar
	m.vp.Height = max(m.h-4-m.inputHeight(), 3)
}

// inputHeight is how many rows the bottom input area takes (the home screen shows a one-line hint).
func (m Model) inputHeight() int {
	if m.current() == nil || m.tab != tabChat {
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
	for _, t := range m.tasks {
		m.loadUsage(t)
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

// moveSel moves the sidebar selection by delta (home is 0), clamped to the list.
func (m *Model) moveSel(delta int) {
	if n := m.sel + delta; n >= 0 && n <= len(m.tasks) {
		m.sel, m.tab = n, tabChat
		m.onSelect()
	}
}
