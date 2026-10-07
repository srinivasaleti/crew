package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/look"
	"crew/internal/task"
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
)

// openNew shows the new-task form, with the folder prefilled from the last attached repo.
func (m *Model) openNew() tea.Cmd {
	m.modal, m.formErr = newModal, ""
	m.name.Reset()
	m.folder.Reset()
	m.desc.Reset()
	m.repoSel = -1
	if repos := m.recentRepos(); len(repos) > 0 {
		m.repoSel = 0
		m.folder.SetValue(tildify(repos[0]))
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
	m.name.Blur()
	m.folder.Blur()
	m.desc.Blur()
	switch i {
	case 0:
		return m.name.Focus()
	case 1:
		m.suggestFolders()
		return m.folder.Focus()
	}
	return m.desc.Focus()
}

// recentRepos are the repos you've used, the most recent first.
func (m Model) recentRepos() []string {
	repos := m.cfg.Repos.List()
	slices.Reverse(repos)
	return repos
}

// formRepos are the recent repos matching what's typed in the form's folder field (all of them
// while it still holds a picked one).
func (m Model) formRepos() []string {
	typed := strings.ToLower(strings.TrimSpace(m.folder.Value()))
	var out []string
	for i, r := range m.recentRepos() {
		if i == m.repoSel || typed == "" || strings.Contains(strings.ToLower(tildify(r)), typed) || m.repoSel >= 0 {
			out = append(out, r)
		}
	}
	return out[:min(len(out), 6)]
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
	case "up", "down":
		if repos := m.formRepos(); m.field == 1 && len(repos) > 0 { // pick one of your recent repos
			i := -1 // typed: ↓ picks the first match
			if m.repoSel >= 0 {
				i = slices.Index(repos, m.recentRepos()[m.repoSel])
			}
			if msg.String() == "up" {
				i = max(i-1, 0)
			} else {
				i = min(i+1, len(repos)-1)
			}
			m.repoSel = slices.Index(m.recentRepos(), repos[i])
			m.folder.SetValue(tildify(repos[i]))
			m.folder.CursorEnd()
			return m, nil
		}
		if msg.String() == "up" {
			return m, m.focusField(max(m.field-1, 0))
		}
		return m, m.focusField(min(m.field+1, 2))
	case "shift+tab":
		return m, m.focusField(max(m.field-1, 0))
	case "tab":
		if m.field != 1 { // in the folder, tab completes a folder name
			return m, m.focusField(min(m.field+1, 2))
		}
	case "enter":
		if m.field == 0 && strings.TrimSpace(m.name.Value()) == "" {
			m.formErr = "Give the task a name"
			return m, nil
		}
		if m.field < 2 {
			m.formErr = ""
			return m, m.focusField(m.field + 1)
		}
		m.create()
		return m, nil
	}
	var cmd tea.Cmd
	switch m.field {
	case 0:
		m.name, cmd = m.name.Update(msg)
	case 1:
		m.folder, cmd = m.folder.Update(msg) // tab accepts the folder suggestion
		m.repoSel = -1                       // typing: the list filters to what matches
		m.suggestFolders()
	default:
		m.desc, cmd = m.desc.Update(msg)
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
		Status: task.Todo, Created: time.Now(), Body: strings.TrimSpace(m.desc.Value())}
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
	os.Remove(m.planPath(id))
	delete(m.convs, id)
	delete(m.plans, id)
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
		dim.Render(running) + "\n\n" + warn.Render("y") + " / " + warn.Render("ctrl+c") + " quit    " + dim.Render("any other key cancels"))
}

var statusLabels = map[string]string{
	task.Todo: "Todo", task.InProgress: "In progress", task.InPR: "In PR", task.Blocked: "Blocked", task.Done: "Done",
}

// menuItem is one row of the status menu; value is a status, "archive", "unarchive", or "" for a separator.
type menuItem struct{ label, value string }

func (m Model) menuItems() []menuItem {
	var items []menuItem
	for _, s := range task.Statuses {
		items = append(items, menuItem{statusLabels[s], s})
	}
	items = append(items, menuItem{})
	if t := m.current(); t != nil && t.Archived {
		return append(items, menuItem{"Unarchive", "unarchive"})
	}
	return append(items, menuItem{"Archive", "archive"})
}

// openStatus shows the status menu for the selected task, with its current status highlighted.
func (m *Model) openStatus() {
	t := m.current()
	if t == nil {
		return
	}
	m.modal, m.menuIdx = statusModal, 0
	for i, it := range m.menuItems() {
		if it.value == t.Status {
			m.menuIdx = i
		}
	}
	m.input.Blur()
}

func (m Model) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	switch k := msg.String(); k {
	case "up", "down":
		step := 1
		if k == "up" {
			step = -1
		}
		for i := m.menuIdx + step; i >= 0 && i < len(items); i += step {
			if items[i].value != "" { // skip the separator
				m.menuIdx = i
				break
			}
		}
	case "enter":
		m.applyMenu(items[m.menuIdx])
	case "1", "2", "3", "4", "5":
		m.applyMenu(items[k[0]-'1'])
	case "esc", "q", "ctrl+s":
		m.closeModal()
	}
	return m, nil
}

func (m *Model) applyMenu(it menuItem) {
	m.closeModal()
	t := m.current()
	if t == nil || it.value == "" {
		return
	}
	switch it.value {
	case "archive":
		if m.conv(t.ID).busy {
			m.notice = t.Title + " is busy, wait for its agent to finish"
			return
		}
		t.Archived = true
		m.notice = "Archived " + t.Title + " · ctrl+o shows archived tasks"
	case "unarchive":
		t.Archived = false
		m.notice = "Unarchived " + t.Title
	default:
		m.notice = t.Title + " → " + statusLabels[it.value]
		if t.Archived {
			m.notice = "Unarchived " + m.notice // picking a status brings the task back to the list
		}
		t.Status, t.Archived = it.value, false
	}
	id := t.ID
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.notice = err.Error()
		return
	}
	m.reload() // re-sorts the list
	if it.value == "archive" {
		m.selKey = "g:tasks" // the task left the list; its overview's Archived chip has it
		m.onSelect()
		return
	}
	m.selectID(id) // follow the task to its new place
}

func (m Model) statusMenuView() string {
	t := m.current()
	if t == nil {
		return ""
	}
	var b string
	for i, it := range m.menuItems() {
		switch {
		case it.value == "":
			b += dim.Render("──────────────────") + "\n"
			continue
		case i == m.menuIdx:
			b += sel.Render("▶ ")
		default:
			b += "  "
		}
		icon := "⌫"
		if st, ok := tint[it.value]; ok {
			icon = st.Render(icons[it.value])
		}
		line := icon + " " + it.label
		if it.value == t.Status {
			line += dim.Render("  current")
		}
		b += line + "\n"
	}
	return menuBox.Render(bold.Render(ansi.Truncate(t.Title, 26, "…")) + "\n\n" + b + "\n" +
		dim.Render("↑↓ ⏎ or 1-5 · esc"))
}

var menuBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Width(32)

// statusChip is the clickable status shown in the task header.
func statusChip(status string) string {
	return tint[status].Render("[ " + icons[status] + " " + status + " ▾ ]")
}

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
