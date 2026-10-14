package ui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

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
	case ev.Card != nil:
		return m.openCard(*ev.Card)
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

// openCard opens an issue's card over the list: its details, description and comments.
func (m *Model) openCard(i jira.Issue) tea.Cmd {
	var cmd tea.Cmd
	m.card, cmd = jira.OpenCard(m.cfg.Ctx, i)
	m.modal = cardModal
	m.input.Blur()
	return cmd
}

// updateCard hands a key to the card: n works on the issue in a task, a has Claude draft a comment.
func (m Model) updateCard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ev, cmd := m.card.KeyMsg(m.cfg.Ctx, msg)
	issue := m.card.Issue
	if m.card.Closed() {
		m.card = nil
		m.closeModal()
	}
	switch {
	case ev.URL != "":
		openURL(ev.URL)
	case ev.Work:
		return m, m.workOnIssue(issue)
	case ev.Draft:
		return m, draftComment(m.cfg.Ctx, issue.Key, m.card.Text())
	}
	return m, cmd
}

// draftMsg is Claude's draft of a comment coming back.
type draftMsg struct {
	key, text string
	err       error
}

// draftComment has Claude write a comment for an issue from its card, in one headless run with no
// tools; the draft goes in the card's comment box, and only your ⏎ posts it.
func draftComment(ctx context.Context, key, card string) tea.Cmd {
	prompt := "Draft one comment for this Jira issue, as the person it's assigned to: short, specific and " +
		"useful (a status update, a question or the next step), in plain text. Reply with the comment only.\n\n" + card
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "text", "--tools", "")
		cmd.Dir = os.TempDir() // no repo: the card is all it needs
		out, err := cmd.Output()
		if err != nil && strings.TrimSpace(string(out)) == "" {
			return draftMsg{key: key, err: err}
		}
		return draftMsg{key: key, text: string(out)}
	}
}
