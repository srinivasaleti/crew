package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/jira"
	"crew/internal/requests"
	"crew/internal/table"
	"crew/internal/tasks"
)

// JIRA lives in its own package (jira: the setup, the token in the Keychain, your issues, the
// table and the setup form, as jira.Screen). This is crew's side of it: the sidebar row, reading
// the list on the PR poll's clock, showing the setup form in REQUESTS' popup, and working on an
// issue, which is a normal crew task made from it (issue: in the task file).

// jiraRows is the JIRA section's sidebar row.
func (m Model) jiraRows() []row {
	if !m.sectionOn("jira") {
		return nil
	}
	return []row{{kind: rowGroup, group: "jira", label: "JIRA", count: m.jirav.Count()}}
}

// loadJira reads your issues in the background, while the section is on.
func (m Model) loadJira() tea.Cmd {
	if !m.sectionOn("jira") {
		return nil
	}
	return m.jirav.Load()
}

// jiraTable is the section's table, with each issue's crew task and how it's doing.
func (m Model) jiraTable() table.Table {
	tasksFor := map[string]string{}
	for _, t := range m.tasks {
		switch {
		case t.Issue == "":
		case m.conv(t.ID).busy:
			tasksFor[t.Issue] = running.Render("● working")
		default:
			tasksFor[t.Issue] = tint[t.Status].Render(icons[t.Status] + " " + tasks.Labels[t.Status])
		}
	}
	return m.jirav.Table(tasksFor)
}

// jiraAction does what the table asks on an issue.
func (m *Model) jiraAction(key, action string) tea.Cmd {
	ev, setup := m.jirav.Act(key, action)
	switch {
	case setup != nil:
		return m.openJiraSetup(setup)
	case ev.URL != "":
		openURL(ev.URL)
	case ev.Work != nil:
		return m.workOnIssue(*ev.Work)
	}
	return nil
}

// workOnIssue opens the issue's crew task, or the new-task form filled in from it the first time.
func (m *Model) workOnIssue(i jira.Issue) tea.Cmd {
	if !m.sectionOn("tasks") {
		m.notice = "Working on an issue makes a task, and TASKS is off · t on home turns it on"
		return nil
	}
	for _, t := range m.tasks {
		if t.Issue == i.Key {
			return openTask(m, t.ID, "")
		}
	}
	cmd := m.openNew()
	m.newTask.From(i.Key+" "+i.Summary, i.Key, i.Brief())
	return cmd
}

// openJiraSetup shows the setup form; when it closes, JIRA re-reads its setup and its list.
func (m *Model) openJiraSetup(f *requests.Form) tea.Cmd {
	m.form = f
	m.formDone = func(m *Model) tea.Cmd {
		m.jirav.Reload()
		return m.loadJira()
	}
	m.modal = reqsModal
	m.input.Blur()
	return nil
}
