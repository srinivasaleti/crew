// Package ui is the crew terminal UI.
package ui

import (
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/task"
)

type mode int

const (
	listMode mode = iota
	newMode
	deleteMode
)

type reloadMsg struct{}

type item struct{ task.Task }

func (i item) FilterValue() string { return i.Title + " " + i.Repo }

type Model struct {
	store task.Store
	root  string
	repos []string
	list  list.Model
	input textinput.Model
	repo  int // index into repos for the new-task form
	mode  mode
	w     int
}

func New(store task.Store, root string, repos []string) Model {
	l := list.New(nil, delegate{}, 0, 0)
	l.Title = "Tasks"
	l.SetShowHelp(false)
	in := textinput.New()
	in.Placeholder = "What should the agent do?"
	in.CharLimit = 120
	m := Model{store: store, root: root, repos: repos, list: l, input: in}
	m.reload()
	return m
}

func (m *Model) reload() tea.Cmd {
	tasks, err := m.store.Load()
	items := make([]list.Item, len(tasks))
	for i, t := range tasks {
		items[i] = item{t}
	}
	cmd := m.list.SetItems(items)
	if err != nil {
		return tea.Batch(cmd, m.status(err.Error()))
	}
	return cmd
}

func (m *Model) status(msg string) tea.Cmd { return m.list.NewStatusMessage(warn.Render(msg)) }

func (m Model) selected() (task.Task, bool) {
	it, ok := m.list.SelectedItem().(item)
	return it.Task, ok
}

// edit opens the task file in $EDITOR (supports values like "code -w").
func (m Model) edit(t task.Task) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command("sh", "-c", editor+` "$1"`, "sh", m.store.Path(t.ID))
	return tea.ExecProcess(c, func(error) tea.Msg { return reloadMsg{} })
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.list.SetSize(msg.Width, max(msg.Height-detailHeight-3, 3))
		m.input.Width = msg.Width - 12
		return m, nil
	case reloadMsg:
		return m, m.reload()
	case tea.KeyMsg:
		switch m.mode {
		case newMode:
			return m.updateNew(msg)
		case deleteMode:
			return m.updateDelete(msg)
		}
		if m.list.FilterState() == list.Filtering {
			break // keys go to the filter input
		}
		switch msg.String() {
		case "n":
			if len(m.repos) == 0 {
				return m, m.status("no git repos in " + m.root + ", set CREW_ROOT")
			}
			m.mode = newMode
			m.input.Reset()
			return m, m.input.Focus()
		case "e":
			if t, ok := m.selected(); ok {
				return m, m.edit(t)
			}
		case "d":
			if _, ok := m.selected(); ok {
				m.mode = deleteMode
			}
			return m, nil
		case "enter":
			if _, ok := m.selected(); ok {
				return m, m.list.NewStatusMessage("starting tasks arrives in the next version")
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = listMode
		return m, nil
	case "tab":
		m.repo = (m.repo + 1) % len(m.repos)
		return m, nil
	case "shift+tab":
		m.repo = (m.repo + len(m.repos) - 1) % len(m.repos)
		return m, nil
	case "enter", "ctrl+e":
		title := strings.TrimSpace(m.input.Value())
		if title == "" {
			return m, nil
		}
		t := task.Task{ID: m.store.NewID(title), Title: title, Repo: m.repos[m.repo], Status: task.Todo, Created: time.Now()}
		m.mode = listMode
		if err := m.store.Save(t); err != nil {
			return m, m.status(err.Error())
		}
		m.list.ResetFilter()
		cmd := m.reload()
		for i, li := range m.list.Items() {
			if li.(item).ID == t.ID {
				m.list.Select(i)
			}
		}
		if msg.String() == "ctrl+e" {
			return m, tea.Batch(cmd, m.edit(t))
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = listMode
	if msg.String() != "y" {
		return m, nil
	}
	t, _ := m.selected()
	if err := m.store.Delete(t.ID); err != nil {
		return m, m.status(err.Error())
	}
	return m, m.reload()
}
