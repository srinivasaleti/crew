package tasks

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/repo"
	"crew/internal/section"
	task "crew/internal/tasks"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// TASKS, as crew's plugin: its row with the tasks in motion under it, the table of every task,
// and the new-task form as its popup. Opening a task is crew's: its workspace (chat, tabs, shells)
// is what every section's chat reuses, so crew draws it (Host.OpenTask), and a task's status menu
// and delete prompt are crew's too, as they work from inside the task as well.

// Section is TASKS.
type Section struct {
	section.Base
	store task.Store // the task files
	repos repo.Store // the attached repos, for the form
	form  *Form      // the new-task form, while it's open
}

// NewSection makes TASKS from the task files and the attached repos.
func NewSection(store task.Store, repos repo.Store) *Section {
	return &Section{store: store, repos: repos}
}

// Def is TASKS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "tasks", Label: "TASKS", Letter: "t", About: "tasks, each with its own worktree, branch and agent chat"}
}

// Rows are TASKS (once there's a task) with the tasks in motion under it.
func (s *Section) Rows(h section.Host) []section.Row {
	ts := h.Tasks()
	if len(ts) == 0 {
		return nil
	}
	rows := []section.Row{{Kind: section.Group, Section: "tasks", Label: "TASKS", Count: len(ts) - task.CountArchived(ts)}}
	for _, i := range moving(h) {
		rows = append(rows, section.Row{Kind: section.Task, Section: "tasks", Key: "t:" + ts[i].ID, Depth: 1, Index: i})
	}
	return rows
}

// moving are the tasks the sidebar lists under TASKS, as indexes into the tasks: the ones in
// progress, working or with a reply ready, plus the one that's open. They keep one order (newest
// first) so ↑↓ switching doesn't jump as agents reply; the icons show what needs you.
func moving(h section.Host) []int {
	open, _ := strings.CutPrefix(h.SelKey(), "t:")
	var idx []int
	for i, t := range h.Tasks() {
		busy := h.Chats().Conv(t.ID).Busy
		if t.ID == open || !t.Archived && (t.Status == task.InProgress || busy || h.Unread(t.ID)) {
			idx = append(idx, i)
		}
	}
	return idx
}

// Lines draws a task's two lines: its status (or its agent's state) and title, then what's going
// on with it in a few words.
func (s *Section) Lines(h section.Host, r section.Row, selected bool) string {
	t := h.Tasks()[r.Index]
	icon := task.Tint[t.Status].Render(task.Icons[t.Status])
	switch {
	case h.Chats().Conv(t.ID).Busy:
		icon = look.Running.Render("●")
	case h.Unread(t.ID):
		icon = look.Ready.Render("◐")
	case task.Icons[t.Status] == "":
		icon = "?"
	}
	title := t.Title
	if (t.Status == task.Done || t.Archived) && !selected {
		title = look.Dim.Render(title)
	}
	w, indent := h.SidebarWidth(), strings.Repeat("  ", r.Depth)
	title = ansi.Truncate(title, w-5-len(indent), "…")
	var meta []string
	if t.Archived {
		meta = append(meta, "archived")
	}
	meta = append(meta, filepath.Base(t.Repo))
	if v, _ := h.PR(); v.Badge(t.ID) != "" {
		meta = append(meta, v.Badge(t.ID))
	}
	if p := h.PlanProgress(t.ID); p != "" {
		meta = append(meta, "plan "+p)
	}
	if c := h.Cost(t.ID); c > 0 {
		meta = append(meta, look.Cost(c))
	}
	second := ansi.Truncate(strings.Join(meta, look.Dim.Render(" · ")), w-5-len(indent), "…")
	pad := "    " + indent
	switch {
	case selected && h.ListFocused():
		bar := look.FocusBar.Width(w)
		return bar.Render("▶ "+indent+ansi.Strip(icon)+" "+ansi.Strip(title)) + "\n" + bar.UnsetBold().Render(pad+ansi.Strip(second))
	case selected:
		return look.Sel.Render("▶ ") + indent + icon + " " + look.Bold.Render(title) + "\n" + pad + look.Dim.Render(second)
	}
	return "  " + indent + icon + " " + title + "\n" + pad + look.Dim.Render(second)
}

// ListKey is n, which makes a task from the sidebar too.
func (s *Section) ListKey(h section.Host, _ section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() != "n" {
		return nil, false
	}
	return s.New(h), true
}

// ShowsTable is TASKS' own row; a task's row is its workspace.
func (s *Section) ShowsTable(_ section.Host, r section.Row) bool { return r.Kind == section.Group }

// Table is every task with what crew knows about it right now.
func (s *Section) Table(h section.Host, _ section.Row) table.Table {
	ts := h.Tasks()
	items := make([]Item, len(ts))
	v, _ := h.PR()
	for i, t := range ts {
		it := Item{Task: t, Busy: h.Chats().Conv(t.ID).Busy, Unread: h.Unread(t.ID), Plan: h.PlanProgress(t.ID),
			Cost: h.Cost(t.ID), Last: lastActive(h, t)}
		if it.Branch, it.PR = v.Branch(t.ID); it.PR != nil {
			it.Badge = v.Badge(t.ID)
		}
		items[i] = it
	}
	return Table(items)
}

// lastActive is when a task last had a message, else when it was last saved.
func lastActive(h section.Host, t task.Task) time.Time {
	if msgs := h.Chats().Conv(t.ID).Msgs; len(msgs) > 0 {
		if at, err := time.Parse(time.RFC3339, msgs[len(msgs)-1].At); err == nil {
			return at
		}
	}
	return t.Updated
}

// Open opens a task from the table: its chat, or the tab the action names; New opens the form,
// Status and Delete crew's menu and prompt over the list.
func (s *Section) Open(h section.Host, _ section.Row, id, action string) tea.Cmd {
	switch action {
	case New:
		return s.New(h)
	case Status:
		h.StatusMenu(id)
		return nil
	case Delete:
		h.AskDelete(id)
		return nil
	case OpenPR:
		return h.OpenTask(id, "pr")
	case OpenTodo:
		v, _ := h.PR()
		v.ShowTodo(id)
		return h.OpenTask(id, "pr")
	case OpenPlan:
		return h.OpenTask(id, "plan")
	}
	return h.OpenTask(id, "")
}

// New opens the new-task form.
func (s *Section) New(h section.Host) tea.Cmd {
	var cmd tea.Cmd
	s.form, cmd = NewForm(s.store, s.repos)
	h.SectionModal(s)
	h.Input().Blur()
	return cmd
}

// NewFrom opens the new-task form filled in from an issue: its title, its key and its text.
func (s *Section) NewFrom(h section.Host, title, issue, brief string) tea.Cmd {
	cmd := s.New(h)
	s.form.From(title, issue, brief)
	return cmd
}

// ModalOpen reports whether the new-task form is open.
func (s *Section) ModalOpen(section.Host) bool { return s.form != nil }

// ModalView draws the form.
func (s *Section) ModalView(section.Host) string { return s.form.View() }

// ModalKey hands a key to the form; a task it makes opens on its chat, wherever n was pressed.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	cmd, t, closed := s.form.Key(msg)
	if !closed {
		return cmd
	}
	s.form = nil
	h.SectionModal(nil)
	h.Reselect()
	if t != nil {
		h.ReloadTasks()
		h.OpenTask(t.ID, "")
	}
	return cmd
}
