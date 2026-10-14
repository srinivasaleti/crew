package jira

import (
	"fmt"
	"strings"

	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// What the table asks crew to do on a row (table.Table's Keys); "" (⏎) opens the issue.
const (
	View     = ""         // the issue in the main pane: details, description, what's attached
	Comments = "comments" // its comments
	Chat     = "chat"     // a Claude chat about it
	Work     = "work"     // work on it in a crew task
	Open     = "open"     // the issue in the browser
	SignIn   = "signin"   // install acli or sign it in
)

// Table is the JIRA section: your issues that aren't done, under their space's heading (spaces names
// them by key), with chips for to do and in progress.
// tasks says which issues already have a crew task, and how it's doing ("◑ In progress").
func Table(issues []Issue, spaces map[string]string, ready bool, tasks map[string]string, err string) table.Table {
	t := table.Table{
		Empty: "No open issues assigned to you.", Groups: map[string]string{},
		Cols: []table.Col{{Name: "issue", Width: 11}, {Name: "summary"}, {Name: "status", Width: 16}, {Name: "priority", Width: 9},
			{Name: "task", Width: 16}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "new", Label: "To do"}, {ID: "indeterminate", Label: "In progress"},
			{ID: "task", Label: "With a task"}},
		Keys:   map[string]string{"n": Work, "o": Open, "c": Chat, "m": Comments, "S": SignIn},
		Global: map[string]bool{"S": true},
		Acts: []table.Act{{Label: "Open", Key: "enter"}, {Label: "Comments", Key: "m"}, {Label: "Chat", Key: "c"},
			{Label: "Work on it", Key: "n"}, {Label: "Open in Jira", Key: "o"}, {Label: "+ Sign in", Key: "S"}},
		Hint: "⏎ opens it · m its comments · c chat with Claude about it · n works on it in a task · o opens it in Jira · ←/→ fold a space · tab filter · / search · S signs in again",
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
			Cells: []string{look.Sel.Render(i.Key), i.Summary, i.Status, shortPriority(i.Priority), task},
			Tags:  tags, Group: i.Project,
			Search: strings.Join([]string{i.Key, i.Summary, i.Status, i.Type}, " "),
		})
	}
	for _, i := range issues {
		t.Groups[i.Project] = i.Project
		if name := spaces[i.Project]; name != "" {
			t.Groups[i.Project] = name
		}
	}
	t.Summary = fmt.Sprintf("%d open issue%s assigned to you", len(issues), look.Plural(len(issues)))
	if err != "" {
		t.Summary += " · " + look.Warn.Render("couldn't read Jira: "+err)
	}
	return t
}

// shortPriority is a priority without its explanation: "P4 - No Trade Impact" is P4.
func shortPriority(p string) string {
	short, _, _ := strings.Cut(p, " - ")
	return short
}
