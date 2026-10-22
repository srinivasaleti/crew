package tasks

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	task "crew/internal/tasks"
	"crew/internal/widgets/look"
)

// Menu is a task's status menu (ctrl+s, or a click on the status in its header): the statuses,
// then archive or unarchive.
type Menu struct {
	Task  task.Task
	Items []MenuItem
	idx   int // the highlighted item
}

// MenuItem is one row: Value is a status, "archive", "unarchive", or "" for the separator.
type MenuItem struct{ Label, Value string }

// NewMenu opens the menu on t's status.
func NewMenu(t task.Task) *Menu {
	m := &Menu{Task: t}
	for _, s := range task.Statuses {
		m.Items = append(m.Items, MenuItem{task.Labels[s], s})
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
		if st, ok := task.Tint[it.Value]; ok {
			icon = st.Render(task.Icons[it.Value])
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

// DeleteView asks before deleting t. own is the worktree that goes with it ("" keeps it), loss what
// that loses: "" or "…" while crew reads it, "-" nothing, else what ("2 commits not pushed").
func DeleteView(t task.Task, own, loss string) string {
	what := look.Dim.Render("Its chat history and plan are removed.")
	switch {
	case own != "":
		what = look.Dim.Render("Its chat, plan, worktree (" + look.Tildify(own) + ") and branch " + t.ID + " are removed.")
		switch loss {
		case "", "…":
			what += "\n" + look.Dim.Render("Checking it for unsaved work…")
		case "-":
			what += "\n" + look.Dim.Render("Nothing in it is lost: every commit is pushed.")
		default:
			what += "\n" + look.Warn.Render("It has "+loss+": they're lost too.")
		}
	case t.Worktree != "":
		what = look.Dim.Render("Its chat history and plan are removed. Its worktree at " + look.Tildify(t.Worktree) +
			" is kept: its branch was there before the task.")
	}
	return look.MenuBox.Width(64).BorderForeground(lipgloss.Color("9")).Render(look.Bold.Render("Delete task?") + "\n\n" +
		t.Title + "\n" + what + "\n\n" + look.Warn.Render("y") + " delete    " + look.Dim.Render("any other key cancels"))
}
