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

// JIRA lives in its own package (jira: your issues, read with Atlassian's CLI, the table, as
// jira.Screen, and an issue's card). This is crew's side of it: the sidebar row, reading the list on
// start and on its own clock (CREW_JIRA_EVERY) while acli is installed, installing and signing in
// through Setup's shell, an issue opened in the main pane like a listed PR (jira_issue.go),
// and working on an issue, which is a normal crew task made from it (issue: in the task file).

// jiraReady reports whether Atlassian's CLI is installed and signed in: Setup says so, or the last
// read of the list worked (Setup's check can time out while crew starts).
func (m Model) jiraReady() bool { return m.setup.Jira.OK() || m.jirav.OK() }

// jiraPollMsg is JIRA's clock: read the list now, and again after jiraEvery.
type jiraPollMsg struct{}

// jiraEvery is how often JIRA reads your issues: CREW_JIRA_EVERY ("2m", "90s"), else 3 minutes,
// and never more often than every 30 seconds.
func jiraEvery() time.Duration {
	d, err := time.ParseDuration(os.Getenv("CREW_JIRA_EVERY"))
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return max(d, 30*time.Second)
}

// jiraPollTick is the next read of JIRA's clock.
func jiraPollTick() tea.Cmd {
	return tea.Tick(jiraEvery(), func(time.Time) tea.Msg { return jiraPollMsg{} })
}

// jiraRows is the JIRA section's sidebar row.
func (m Model) jiraRows() []row {
	if !m.sectionOn("jira") {
		return nil
	}
	rows := []row{{kind: rowGroup, group: "jira", label: "JIRA", count: m.jirav.Count()}}
	if key, ok := strings.CutPrefix(m.selKey, "j:"); ok && m.cards[key] != nil {
		rows = append(rows, row{kind: rowIssue, group: "jira", depth: 1, label: key})
	}
	return rows
}

// loadJira reads your issues in the background while the section is on and acli is installed (the
// read tells whether it's signed in), unless a read is already running; the JIRA row spins until
// it's back.
func (m *Model) loadJira() tea.Cmd {
	if !m.sectionOn("jira") || m.jirav.Loading() {
		return nil
	}
	if _, err := exec.LookPath("acli"); err != nil {
		return nil
	}
	return tea.Batch(m.jirav.Load(), m.startSpin())
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
		return m.openIssue(*ev.Card, ev.Pane)
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
