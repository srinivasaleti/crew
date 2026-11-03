package reminders

import (
	"fmt"
	"strconv"
	"time"

	rem "crew/internal/reminders"
	"crew/internal/ui/terminal/widgets/look"
	"crew/internal/ui/terminal/widgets/table"
)

// The List tab's actions on a row (table.Table's Keys).
const (
	Edit    = ""
	New     = "new"
	Delete  = "delete"
	Pause   = "pause"
	Restore = "restore"
	Chat    = "chat"
)

// Table is the List tab: every reminder, the next to fire first, with chips for today, repeating,
// paused, done and deleted.
func Table(rs []rem.Reminder, now time.Time) table.Table {
	t := table.Table{
		Empty: "No reminders here · n adds one, or ask Claude in the Chat tab (⌘2)",
		Cols:  []table.Col{{Name: "when", Width: 20}, {Name: "repeats", Width: 24}, {Name: "what"}, {Name: "", Width: 22}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "today", Label: "Today"}, {ID: "repeating", Label: "Repeating"},
			{ID: "paused", Label: "Paused"}, {ID: "done", Label: "Done"}, {ID: "deleted", Label: "Deleted"}},
		Keys:   map[string]string{"n": New, "x": Delete, "p": Pause, "u": Restore, "c": Chat},
		Global: map[string]bool{"n": true, "c": true},
		Acts: []table.Act{{Label: "+ Reminder", Key: "n"}, {Label: "Edit", Key: "enter"}, {Label: "Pause", Key: "p"},
			{Label: "Delete", Key: "x"}, {Label: "Restore", Key: "u"}, {Label: "+ Ask Claude", Key: "c"}},
		Hint: "⏎ edit · n new · p pause or resume · x delete · u restore · c ask Claude · tab filter · / search",
	}
	active := 0
	var next *rem.Reminder
	for i, r := range rs {
		var tags []string
		state := ""
		switch {
		case r.Deleted:
			tags, state = []string{"deleted"}, look.Dim.Render("deleted · u restores")
		case r.Done:
			tags, state = []string{"done"}, look.Dim.Render("done")
		default:
			tags = []string{"all"}
			if r.Paused {
				tags, state = append(tags, "paused"), look.Orange.Render("paused")
				if !r.Until.IsZero() {
					state = look.Orange.Render("paused till " + rem.When(r.Until, now))
				}
			} else {
				active++
				if next == nil {
					next = &rs[i]
				}
			}
			if r.Every != "" {
				tags = append(tags, "repeating")
			}
			if r.At.Year() == now.Year() && r.At.YearDay() == now.YearDay() {
				tags = append(tags, "today")
			}
		}
		when, what := rem.When(r.At, now), r.Text
		if !r.Active() {
			when, what = look.Dim.Render(when), look.Dim.Render(what)
		}
		t.Rows = append(t.Rows, table.Row{ID: strconv.Itoa(r.ID), Cells: []string{when, rem.Describe(r.Every, r.At), what, state},
			Tags: tags, Search: r.Text + " " + rem.Describe(r.Every, r.At)})
	}
	t.Summary = fmt.Sprintf("%d active", active)
	if next != nil {
		t.Summary += " · next: " + next.Text + ", " + rem.When(next.At, now)
	}
	return t
}
