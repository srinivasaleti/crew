package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
)

// modelChoices are the models offered by /model; an empty value means Claude Code's default.
var modelChoices = []menuItem{
	{"Default", ""},
	{"Opus 5.5", "claude-opus-5-5"},
	{"Sonnet 5.5", "claude-sonnet-5-5"},
	{"Haiku 4.5", "claude-haiku-4-5-20251001"},
	{"Fable 5.1", "claude-fable-5-1"},
}

// isModel reports whether a task's model setting (an id or an alias like "sonnet") is this choice.
func isModel(setting, choice string) bool {
	if setting == "" || choice == "" {
		return setting == choice
	}
	return setting == choice || strings.Contains(choice, "-"+setting+"-")
}

// openModelMenu shows the model picker for the selected task, with its current model highlighted.
func (m *Model) openModelMenu() {
	t := m.current()
	if t == nil {
		return
	}
	m.modal, m.menuIdx = modelModal, 0
	for i, c := range modelChoices {
		if isModel(t.Model, c.value) {
			m.menuIdx = i
		}
	}
	m.input.Blur()
}

func (m Model) updateModelMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "up":
		m.menuIdx = max(m.menuIdx-1, 0)
	case "down":
		m.menuIdx = min(m.menuIdx+1, len(modelChoices)-1)
	case "enter":
		m.pickModel(m.menuIdx)
	case "esc", "q":
		m.closeModal()
	case "ctrl+c":
		return m, tea.Quit
	default:
		if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= len(modelChoices) {
			m.pickModel(n - 1)
		}
	}
	return m, nil
}

// pickModel saves the chosen model on the task; it applies from the next message.
func (m *Model) pickModel(i int) {
	m.closeModal()
	t := m.current()
	if t == nil {
		return
	}
	t.Model = modelChoices[i].value
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	m.add(t.ID, chat.Tool, "model set to "+m.modelName(*t)+" from the next message on")
}

func (m Model) modelMenuView() string {
	t := m.current()
	if t == nil {
		return ""
	}
	var b strings.Builder
	for i, c := range modelChoices {
		if i == m.menuIdx {
			b.WriteString(sel.Render("▶ "))
		} else {
			b.WriteString("  ")
		}
		label := c.label
		if c.value == "" {
			def := "Claude Code's default"
			if last := m.usage[t.ID].LastModel; t.Model == "" && last != "" {
				def = last // what the default resolved to last time
			}
			label += dim.Render("  " + def)
		}
		if isModel(t.Model, c.value) {
			label += green.Render("  current")
		}
		b.WriteString(label + "\n")
	}
	return menuBox.Width(44).Render(bold.Render(ansi.Truncate("Model · "+t.Title, 38, "…")) + "\n\n" + b.String() + "\n" +
		dim.Render("↑↓ ⏎ or 1-"+strconv.Itoa(len(modelChoices))+" · esc"))
}
