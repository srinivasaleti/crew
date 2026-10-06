// Package ui is the crew terminal UI: a sidebar (crew home + one chat per task) and a main pane.
package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
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
	msgs    []chat.Message
	busy    bool
	stop    context.CancelFunc // cancels the running turn; nil when idle
	stopped bool               // the running turn was stopped by you
}

type Model struct {
	cfg     Config
	tasks   []task.Task // all tasks, archived included; the sidebar groups and filters them
	loadErr string
	selKey  string // selected sidebar row: "home", "g:<group>" or "t:<task id>"
	convs   map[string]*conv
	input   textarea.Model // message box; grows up to maxInputLines
	vp      viewport.Model
	w, h    int
	notice  string // one-line feedback shown above the help line, cleared on the next key

	tab    tab                    // which tab of the selected task the main pane shows
	usage  map[string]usage.Usage // per task, read from Claude's session logs
	unread map[string]bool        // tasks whose agent replied while you were looking elsewhere

	collapsed map[string]bool // sidebar groups you opened or closed, by group key
	sideTop   int             // first visible sidebar row when the list scrolls
	filter    textinput.Model // ctrl+f
	filtering bool            // typing in the filter
	menuIdx   int             // highlighted row of the status menu
	pending   tea.Cmd         // work started by a slash command, returned by submit

	// Terminal tab
	shells     map[string]*shellTerm // per task, the Terminal tab's shell
	shellFocus bool                  // typing goes to the shell; ctrl+] releases it to switch tabs

	// chat rendering
	expandActions bool                  // ctrl+t lists every tool call instead of summaries
	md            *glamour.TermRenderer // Markdown renderer for agent replies, rebuilt on resize
	mdWidth       int
	mdCache       map[string]string

	// @ file suggestions
	files       map[string][]string // per task, from git ls-files
	sugg        []string
	suggIdx     int
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
	// Enter itself sends (handled in updateChat); a trailing \ then Enter also inserts a new line.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	name := textinput.New()
	name.Placeholder = "Sort orgs alphabetically"
	name.CharLimit = 100
	folder := textinput.New()
	folder.Placeholder = "~/code/my-app"
	folder.ShowSuggestions = true
	filter := textinput.New()
	filter.Prompt = ""
	filter.Placeholder = "title, repo, status…"
	filter.CharLimit = 60
	filter.Width = sidebarWidth - 6
	m := Model{cfg: cfg, selKey: "home", collapsed: map[string]bool{}, shells: map[string]*shellTerm{}, mdCache: map[string]string{}, filter: filter, convs: map[string]*conv{}, usage: map[string]usage.Usage{}, unread: map[string]bool{}, files: map[string][]string{}, input: in, vp: viewport.New(0, 0), name: name, folder: folder}
	m.reload()
	return m
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

const maxInputLines = 6

// layout sizes the message box to its content and gives the rest of the height to the chat.
func (m *Model) layout() {
	m.input.SetWidth(max(m.w-2, 10))
	m.resizeShells()
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
	m.vp.Width = max(m.w-sidebarWidth-5, 10) // leaves room for the scroll bar
	m.vp.Height = max(m.h-4-m.inputHeight(), 3)
}

// inputHeight is how many rows the bottom input area takes (the home screen shows a one-line hint).
func (m Model) inputHeight() int {
	if m.current() == nil || m.tab != tabChat {
		return 1
	}
	return m.input.Height() + len(m.sugg)
}

// reload re-reads tasks from disk, archiving old done tasks; the selection (a row key) is kept.
func (m *Model) reload() {
	all, err := m.cfg.Tasks.Load()
	m.loadErr = ""
	if err != nil {
		m.loadErr = err.Error()
	}
	if n := m.autoArchive(all); n > 0 {
		m.notice = fmt.Sprintf("Archived %d done task(s) untouched for %d days", n, archiveDays())
	}
	m.tasks = all
	for _, t := range m.tasks {
		m.loadUsage(t)
	}
	m.onSelect()
}

// archiveDays is how long a done task stays in the list before it's archived (CREW_ARCHIVE_DAYS,
// default 7; 0 turns it off).
func archiveDays() int {
	if v, err := strconv.Atoi(os.Getenv("CREW_ARCHIVE_DAYS")); err == nil && v >= 0 {
		return v
	}
	return 7
}

// autoArchive archives done tasks not updated for archiveDays, in place, and returns how many.
func (m *Model) autoArchive(all []task.Task) int {
	days, n := archiveDays(), 0
	if days == 0 {
		return 0
	}
	for i, t := range all {
		last := t.Updated
		if last.IsZero() {
			last = t.Created
		}
		if t.Status == task.Done && !t.Archived && time.Since(last) > time.Duration(days)*24*time.Hour {
			all[i].Archived = true
			if m.cfg.Tasks.Save(all[i]) == nil {
				n++
			}
		}
	}
	return n
}

func (m *Model) onSelect() {
	delete(m.unread, m.currentID())
	m.sugg = nil
	if t := m.current(); t != nil {
		m.input.Placeholder = "Message " + agentName(*t) + " about " + t.Title + "…"
		if !m.listFocus {
			m.input.Focus()
		}
	} else {
		m.input.Blur()
	}
	m.layout()
	m.ensureVisible()
	m.refresh()
}

// current is the selected task, or nil when home or a group row is selected.
func (m Model) current() *task.Task {
	r := m.selRow()
	if r.kind != rowTask {
		return nil
	}
	return &m.tasks[r.task]
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
	msg := chat.Message{Role: role, Text: text, At: time.Now().Format(time.RFC3339)}
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
