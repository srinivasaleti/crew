package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/jira"
	"crew/internal/table"
	"crew/internal/tasks"
)

// JIRA lives in its own package (jira: your issues, read with Atlassian's CLI, and the table, as
// jira.Screen). This is crew's side of it: the sidebar row, reading the list on the PR poll's clock
// once acli is signed in (crew's Setup checks it, like gh), installing and signing in through Setup's
// shell, and working on an issue, which is a normal crew task made from it (issue: in the task file).

// jiraReady reports whether Atlassian's CLI is installed and signed in.
func (m Model) jiraReady() bool { return m.setup.Jira.OK() }

// jiraRows is the JIRA section's sidebar row.
func (m Model) jiraRows() []row {
	if !m.sectionOn("jira") {
		return nil
	}
	return []row{{kind: rowGroup, group: "jira", label: "JIRA", count: m.jirav.Count()}}
}

// loadJira reads your issues in the background, while the section is on and acli is signed in.
func (m Model) loadJira() tea.Cmd {
	if !m.sectionOn("jira") || !m.jiraReady() {
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
	return m.jirav.Table(m.jiraReady(), tasksFor)
}

// jiraAction does what the table asks on an issue.
func (m *Model) jiraAction(key, action string) tea.Cmd {
	ev := m.jirav.Act(m.jiraReady(), key, action)
	switch {
	case ev.SignIn:
		return m.jiraSignIn()
	case ev.URL != "":
		openURL(ev.URL)
	case ev.Work != nil:
		return m.workOnIssue(*ev.Work)
	}
	return nil
}

// jiraSignIn installs Atlassian's CLI if it isn't, else signs it in (the browser opens), in a shell
// panel; Setup checks again when that shell exits, and the list loads once it's signed in.
func (m *Model) jiraSignIn() tea.Cmd {
	t := m.setup.Jira
	if !t.Installed {
		return m.runFix(t.Install, "then S signs in to Jira")
	}
	return m.signIn(t)
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
