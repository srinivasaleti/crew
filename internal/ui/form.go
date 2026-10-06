package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/task"
)

type modal int

const (
	noModal modal = iota
	newModal
	deleteModal
	statusModal
	modelModal
)

// openNew shows the new-task form, with the folder prefilled from the last attached repo.
func (m *Model) openNew() tea.Cmd {
	m.modal, m.formErr = newModal, ""
	m.name.Reset()
	m.folder.Reset()
	if repos := m.cfg.Repos.List(); len(repos) > 0 {
		m.folder.SetValue(tildify(repos[len(repos)-1]))
		m.folder.CursorEnd()
	}
	m.input.Blur()
	return m.focusField(0)
}

func (m *Model) openDelete() {
	t := m.current()
	switch {
	case t == nil:
		// delete only applies to tasks; ignore on the home screen
	case m.conv(t.ID).busy:
		m.notice = t.Title + " is busy, wait for its agent to finish"
	default:
		m.modal = deleteModal
	}
}

func (m *Model) closeModal() {
	m.modal = noModal
	m.name.Blur()
	m.folder.Blur()
	m.onSelect()
}

func (m *Model) focusField(i int) tea.Cmd {
	m.field = i
	if i == 0 {
		m.folder.Blur()
		return m.name.Focus()
	}
	m.name.Blur()
	m.suggestFolders()
	return m.folder.Focus()
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.modal == statusModal {
		return m.updateStatus(msg)
	}
	if m.modal == modelModal {
		return m.updateModelMenu(msg)
	}
	if m.modal == deleteModal {
		if msg.String() == "y" {
			m.deleteCurrent()
		}
		m.closeModal()
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.closeModal()
		return m, nil
	case "up", "shift+tab":
		return m, m.focusField(0)
	case "down":
		return m, m.focusField(1)
	case "tab":
		if m.field == 0 {
			return m, m.focusField(1)
		}
	case "enter":
		if m.field == 0 {
			if strings.TrimSpace(m.name.Value()) == "" {
				m.formErr = "Give the task a name"
				return m, nil
			}
			m.formErr = ""
			return m, m.focusField(1)
		}
		m.create()
		return m, nil
	}
	var cmd tea.Cmd
	if m.field == 0 {
		m.name, cmd = m.name.Update(msg)
	} else {
		m.folder, cmd = m.folder.Update(msg) // tab accepts the folder suggestion
		m.suggestFolders()
	}
	return m, cmd
}

func (m *Model) create() {
	name := strings.TrimSpace(m.name.Value())
	if name == "" {
		m.formErr = "Give the task a name"
		m.focusField(0)
		return
	}
	rp, err := m.cfg.Repos.Attach(strings.TrimSpace(m.folder.Value()))
	if err != nil {
		m.formErr = err.Error()
		return
	}
	t := task.Task{ID: m.cfg.Tasks.NewID(name), Title: name, Repo: rp, Agent: agent.Claude.Name,
		Status: task.Todo, Created: time.Now()}
	if err := m.cfg.Tasks.Save(t); err != nil {
		m.formErr = err.Error()
		return
	}
	m.closeModal()
	m.reload()
	m.tab = tabChat // a new task opens on its chat, wherever ctrl+n was pressed
	m.selectID(t.ID)
}

func (m *Model) deleteCurrent() {
	t := m.current()
	if t == nil {
		return
	}
	id, title, wt := t.ID, t.Title, t.Worktree
	if err := m.cfg.Tasks.Delete(id); err != nil {
		m.notice = err.Error()
		return
	}
	m.cfg.Chats.Delete(id)
	delete(m.convs, id)
	m.stopShells(id)
	m.tab = tabChat
	m.reload()
	m.notice = "Deleted " + title
	if wt != "" {
		m.notice += " (worktree kept at " + tildify(wt) + ")"
	}
}

// suggestFolders offers sub-folders matching what has been typed so far; tab accepts one.
func (m *Model) suggestFolders() {
	typed := m.folder.Value()
	// Split what was typed (not the ~-expanded path) so dir+prefix always rebuilds typed exactly.
	dir, prefix := filepath.Split(typed)
	readDir := expand(dir)
	if readDir == "" {
		readDir = "."
	}
	entries, _ := os.ReadDir(readDir)
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && strings.HasPrefix(n, prefix) && (!strings.HasPrefix(n, ".") || strings.HasPrefix(prefix, ".")) {
			out = append(out, dir+n+"/")
		}
	}
	m.folder.SetSuggestions(out)
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

func tildify(p string) string {
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}
