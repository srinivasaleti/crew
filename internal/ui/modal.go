package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
	"crew/internal/look"
)

type modal int

const (
	noModal modal = iota
	newModal
	deleteModal
	statusModal
	modelModal
	quitModal
	prModal      // a PR comment, check or review in a popup (pr_popup.go)
	paletteModal // commands and shortcuts (palette.go)
	reqsModal    // the requests screen's popup (the requests package)
	setupModal   // the Setup panel: what crew needs, installed and signed in (setup.go)
)

func (m *Model) closeModal() {
	m.modal = noModal
	m.newTask, m.statusMenu = nil, nil
	m.onSelect()
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal == quitModal {
		if k := msg.String(); k == "y" || k == "ctrl+c" {
			return m, tea.Quit
		}
		m.closeModal() // any other key cancels
		return m, nil
	}
	if msg.String() == "ctrl+c" {
		m.openQuit() // from any window
		return m, nil
	}
	if m.modal == prModal {
		m, cmd, _ := m.updatePopup(msg)
		return m, cmd
	}
	if m.modal == paletteModal {
		return m.updatePalette(msg)
	}
	if m.modal == statusModal {
		return m.updateStatus(msg)
	}
	if m.modal == modelModal {
		return m.updateModelMenu(msg)
	}
	if m.modal == reqsModal {
		return m.updateReqsPopup(msg)
	}
	if m.modal == setupModal {
		return m.updateSetup(msg)
	}
	if m.modal == deleteModal {
		if msg.String() == "y" {
			m.deleteTask(m.delID)
		}
		m.closeModal()
		return m, nil
	}
	return m.updateNewTask(msg)
}

var tildify = look.Tildify

// openQuit asks before quitting (ctrl+c); a second ctrl+c or y quits.
func (m *Model) openQuit() {
	m.modal = quitModal
	m.input.Blur()
}

// quitView says what quitting stops.
func (m Model) quitView() string {
	agents, shells := 0, 0
	for _, c := range m.convs {
		if c.busy {
			agents++
		}
	}
	for _, list := range m.shells {
		shells += len(list)
	}
	running := "Nothing is running."
	if agents+shells > 0 {
		running = fmt.Sprintf("%d agent%s running and %d shell%s open will be stopped.", agents, plural(agents), shells, plural(shells))
	}
	return menuBox.Width(44).BorderForeground(lipgloss.Color("9")).Render(bold.Render("Quit crew?") + "\n\n" +
		dim.Render(running) + "\n\n" + warn.Render("y") + " quit    " + dim.Render("any other key cancels"))
}

// menuItem is one row of the model menu.
type menuItem struct{ label, value string }

var menuBox = look.MenuBox

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

// openModelMenu shows the model picker for the chat on screen (a task's or a PR's), with its current model highlighted.
func (m *Model) openModelMenu() {
	t := m.chatTask()
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
	t := m.chatTask()
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
	t := m.chatTask()
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
