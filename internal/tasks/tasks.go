// Package tasks is crew's tasks: task files on disk (store.go), how a task's status reads and looks,
// and archiving. The TASKS section (sections/tasks) lists them; crew's app opens one.
package tasks

import (
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"

	"crew/internal/widgets/look"
)

// Labels, Icons and Tint are how each status reads.
var (
	Labels = map[string]string{
		Todo: "Todo", InProgress: "In progress", InPR: "In PR", Blocked: "Blocked", Done: "Done",
	}
	Icons = map[string]string{Todo: "○", InProgress: "◑", InPR: "⇡", Blocked: "⊘", Done: "✓"}
	Tint  = map[string]lipgloss.Style{
		Todo:       look.Dim,
		InProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		InPR:       lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
		Blocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		Done:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
	}
)

// Chip is the clickable status shown in a task's header.
func Chip(status string) string {
	return Tint[status].Render("[ " + Icons[status] + " " + status + " ▾ ]")
}

// CountArchived is how many tasks are archived.
func CountArchived(ts []Task) int {
	n := 0
	for _, t := range ts {
		if t.Archived {
			n++
		}
	}
	return n
}

// ArchiveDays is how long a done task stays in the list before it's archived (CREW_ARCHIVE_DAYS,
// default 7; 0 turns it off).
func ArchiveDays() int {
	if v, err := strconv.Atoi(os.Getenv("CREW_ARCHIVE_DAYS")); err == nil && v >= 0 {
		return v
	}
	return 7
}

// AutoArchive archives done tasks not updated for ArchiveDays, in place and on disk, and returns
// how many.
func AutoArchive(store Store, all []Task) int {
	days, n := ArchiveDays(), 0
	if days == 0 {
		return 0
	}
	for i, t := range all {
		last := t.Updated
		if last.IsZero() {
			last = t.Created
		}
		if t.Status == Done && !t.Archived && time.Since(last) > time.Duration(days)*24*time.Hour {
			all[i].Archived = true
			if store.Save(all[i]) == nil {
				n++
			}
		}
	}
	return n
}

// SetStatus applies a menu pick to t and says what changed.
func SetStatus(t *Task, pick string) (notice string) {
	switch pick {
	case "archive":
		t.Archived = true
		return "Archived " + t.Title + " · ⌘O shows archived tasks"
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

// Subtasks are the tasks split from the task id, oldest (first) first.
func Subtasks(all []Task, id string) []Task {
	var subs []Task
	for i := len(all) - 1; i >= 0; i-- { // all is newest first
		if all[i].Parent == id && id != "" {
			subs = append(subs, all[i])
		}
	}
	return subs
}

// WaitsOn are the subtasks t comes after that aren't done yet, by title. A subtask's agent starts
// once there are none.
func WaitsOn(t Task, all []Task) []string {
	var open []string
	for _, id := range t.After {
		for _, o := range all {
			if o.ID == id && o.Status != Done {
				open = append(open, o.Title)
			}
		}
	}
	return open
}
