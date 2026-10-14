package jira

import (
	"fmt"
	"strings"

	"crew/internal/look"
	"crew/internal/table"
)

// What the table asks crew to do on a row (table.Table's Keys); "" (⏎) works on the issue in a task.
const (
	Work   = ""
	Open   = "open"   // the issue in the browser
	SignIn = "signin" // install acli or sign it in
)

// Table is the JIRA section: your issues that aren't done, with chips for to do and in progress.
// tasks says which issues already have a crew task, and how it's doing ("◑ In progress").
func Table(issues []Issue, ready bool, tasks map[string]string, err string) table.Table {
	t := table.Table{
		Empty: "No open issues assigned to you.", Facet: "project",
		Cols: []table.Col{{Name: "issue", Width: 11}, {Name: "summary"}, {Name: "status", Width: 16}, {Name: "priority", Width: 9},
			{Name: "task", Width: 16}, {Name: "updated", Width: 9}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "new", Label: "To do"}, {ID: "indeterminate", Label: "In progress"},
			{ID: "task", Label: "With a task"}},
		Keys:   map[string]string{"o": Open, "S": SignIn},
		Global: map[string]bool{"S": true},
		Acts: []table.Act{{Label: "Work on it", Key: "enter"}, {Label: "Open in Jira", Key: "o"},
			{Label: "+ Sign in", Key: "S"}},
		Hint: "⏎ works on it in a task (makes one the first time, in a repo you pick) · o opens it in Jira · tab filter · / search · S signs in to Jira again",
	}
	if !ready {
		t.Empty = "Jira isn't signed in · S (or ⏎ from the sidebar) installs Atlassian's CLI if needed and signs in in your browser"
	}
	for _, i := range issues {
		tags := []string{"all", i.Category}
		task := look.Dim.Render("–")
		if s, ok := tasks[i.Key]; ok {
			tags, task = append(tags, "task"), s
		}
		t.Rows = append(t.Rows, table.Row{
			ID:    i.Key,
			Cells: []string{look.Sel.Render(i.Key), i.Summary, i.Status, i.Priority, task, look.Dim.Render(look.Ago(i.Updated))},
			Tags:  tags, Facet: i.Project,
			Search: strings.Join([]string{i.Key, i.Summary, i.Status, i.Type}, " "),
		})
	}
	t.Summary = fmt.Sprintf("%d open issue%s assigned to you", len(issues), look.Plural(len(issues)))
	if err != "" {
		t.Summary += " · " + look.Warn.Render("couldn't read Jira: "+err)
	}
	return t
}
