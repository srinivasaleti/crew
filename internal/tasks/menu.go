package tasks

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/look"
)

// Menu is a task's status menu (ctrl+s, or a click on the status in its header): the statuses,
// then archive or unarchive.
type Menu struct {
	Task  Task
	Items []MenuItem
	idx   int // the highlighted item
}

// MenuItem is one row: Value is a status, "archive", "unarchive", or "" for the separator.
type MenuItem struct{ Label, Value string }

// NewMenu opens the menu on t's status.
func NewMenu(t Task) *Menu {
	m := &Menu{Task: t}
	for _, s := range Statuses {
		m.Items = append(m.Items, MenuItem{Labels[s], s})
	}
	m.Items = append(m.Items, MenuItem{})
	if t.Archived {
		m.Items = append(m.Items, MenuItem{"Unarchive", "unarchive"})
	} else {
		m.Items = append(m.Items, MenuItem{"Archive", "archive"})
	}
	for i, it := range m.Items {
		if it.Value == t.Status {
			m.idx = i
		}
	}
	return m
}

// Key handles a key: pick is the value chosen; closed is set when the menu is done.
func (m *Menu) Key(k string) (pick string, closed bool) {
	switch k {
	case "up", "down":
		step := 1
		if k == "up" {
			step = -1
		}
		for i := m.idx + step; i >= 0 && i < len(m.Items); i += step {
			if m.Items[i].Value != "" { // skip the separator
				m.idx = i
				break
			}
		}
	case "enter":
		return m.Items[m.idx].Value, true
	case "1", "2", "3", "4", "5":
		return m.Items[k[0]-'1'].Value, true
	case "esc", "q", "ctrl+s":
		return "", true
	}
	return "", false
}

// SetStatus applies a menu pick to t and says what changed.
func SetStatus(t *Task, pick string) (notice string) {
	switch pick {
	case "archive":
		t.Archived = true
		return "Archived " + t.Title + " · ctrl+o shows archived tasks"
	case "unarchive":
		t.Archived = false
		return "Unarchived " + t.Title
	}
	notice = t.Title + " → " + Labels[pick]
	if t.Archived {
		notice = "Unarchived " + notice // picking a status brings the task back to the list
	}
	t.Status, t.Archived = pick, false
	return notice
}

// View draws the menu. Its items start on the box's fifth line (border, padding, title, gap).
func (m *Menu) View() string {
	var b string
	for i, it := range m.Items {
		switch {
		case it.Value == "":
			b += look.Dim.Render("──────────────────") + "\n"
			continue
		case i == m.idx:
			b += look.Sel.Render("▶ ")
		default:
			b += "  "
		}
		icon := "⌫"
		if st, ok := Tint[it.Value]; ok {
			icon = st.Render(Icons[it.Value])
		}
		line := icon + " " + it.Label
		if it.Value == m.Task.Status {
			line += look.Dim.Render("  current")
		}
		b += line + "\n"
	}
	return look.MenuBox.Render(look.Bold.Render(ansi.Truncate(m.Task.Title, 26, "…")) + "\n\n" + b + "\n" +
		look.Dim.Render("↑↓ ⏎ or 1-5 · esc"))
}

// DeleteView asks before deleting t.
func DeleteView(t Task) string {
	keep := "Its chat history and plan are removed."
	if t.Worktree != "" {
		keep += " The worktree at " + look.Tildify(t.Worktree) + " is kept."
	}
	return look.MenuBox.Width(60).BorderForeground(lipgloss.Color("9")).Render(look.Bold.Render("Delete task?") + "\n\n" +
		t.Title + "\n" + look.Dim.Render(keep) + "\n\n" + look.Warn.Render("y") + " delete    " + look.Dim.Render("any other key cancels"))
}
