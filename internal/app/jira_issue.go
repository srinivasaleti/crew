package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/jira"
	"crew/internal/tasks"
)

// A Jira issue opened from JIRA's table, in the main pane like a listed PR: its row under JIRA in the
// sidebar ("j:<key>"), and three tabs: Issue and Comments (drawn by the card, jira.Card, which also
// edits the issue, changes its status and posts comments, each only on your key) and Chat, a Claude
// chat about the issue. The chat is a task underneath, like a PR's (kind: jira, issue: <key>, kept in
// m.chats), so it has a task chat's /compact, /new, /model, usage and stopping; it runs in
// <JiraChat>/<id>, where Claude reads the issue with acli.

// jiraChatKind marks an issue's chat in its task file.
const jiraChatKind = "jira"

// selIssue is the card of the issue selected in the sidebar, if an issue's row is.
func (m Model) selIssue() *jira.Card {
	if r := m.selRow(); r.kind == rowIssue {
		return m.cards[r.label]
	}
	return nil
}

// openIssue opens an issue in the main pane on a tab, reading it from Jira the first time.
func (m *Model) openIssue(i jira.Issue, pane int) tea.Cmd {
	var cmd tea.Cmd
	if m.cards[i.Key] == nil {
		m.cards[i.Key], cmd = jira.OpenCard(m.cfg.Ctx, i, m.jirav.Statuses(i.Project))
	}
	m.selKey, m.tab, m.issuePane = "j:"+i.Key, tabChat, jira.PaneIssue
	m.onSelect()
	m.focusOn(areaMain, -1)
	m.setIssuePane(pane)
	return cmd
}

// openLinked opens a linked issue or subtask in crew: from your list when it's there, else read
// from Jira by its key.
func (m *Model) openLinked(from *jira.Card, key string) tea.Cmd {
	if i, ok := m.jirav.Find(key); ok {
		return m.openIssue(i, jira.PaneIssue)
	}
	project, _, _ := strings.Cut(key, "-")
	site := strings.TrimSuffix(from.Issue.URL, from.Issue.Key)
	return m.openIssue(jira.Issue{Key: key, Project: project, Summary: key, URL: site + key}, jira.PaneIssue)
}

// setIssuePane opens one of an issue's tabs; Chat gets the keys for its message box.
func (m *Model) setIssuePane(p int) {
	m.issuePane = p
	if c := m.selIssue(); c != nil && p == jira.PaneChat {
		t := m.ensureJiraChat(c)
		delete(m.unread, t.ID)
		m.input.Placeholder = "Message " + agentName(*t) + " about " + c.Issue.Key + "…"
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.sugg = nil
	m.layout()
	m.vp.GotoTop()
	m.renderIssueRow()
	if p == jira.PaneChat {
		m.vp.GotoBottom()
	}
}

// issueLines draws an issue's two sidebar lines: its key and summary, then its status and type.
func (m Model) issueLines(r row, selected bool) string {
	c := m.cards[r.label]
	return m.itemLines(r, sel.Render("◆"), c.Issue.Key+" "+c.Issue.Summary, c.Issue.Status+" · "+c.Issue.Type, selected)
}

// issueHeader is the main pane's first line for an issue.
func (m Model) issueHeader(c *jira.Card, w int) string {
	meta := ""
	if t := m.taskFor(c.Issue.Key); t != nil {
		meta = dim.Render(" · ") + tint[t.Status].Render(icons[t.Status]+" task")
	}
	if c.Loading() {
		meta += dim.Render(" · reading Jira…")
	}
	return ansi.Truncate(sel.Render(c.Issue.Key)+" "+bold.Render(c.Issue.Summary)+meta+m.chatMeta(m.jiraChat(c.Issue.Key)), w, "…")
}

// taskFor is the crew task working on an issue, if there is one.
func (m Model) taskFor(key string) *tasks.Task {
	for i := range m.tasks {
		if m.tasks[i].Issue == key {
			return &m.tasks[i]
		}
	}
	return nil
}

// issueTabs is the tab bar for an issue: Issue, Comments (with how many), Chat.
func (m Model) issueTabs(c *jira.Card, w int) string {
	active := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range jira.PaneNames {
		if i == m.issuePane {
			bar = append(bar, active.Render(tabLabel(n, i)))
		} else {
			bar = append(bar, dim.Padding(0, 1).Render(tabLabel(n, i)))
		}
	}
	about := fmt.Sprintf("   %s · %s · %s", c.Issue.Type, c.Issue.Status, c.Issue.Priority)
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render(about), w, "…")
}

// renderIssueRow fills the main pane with the issue's open tab.
func (m *Model) renderIssueRow() {
	c, w := m.selIssue(), max(m.vp.Width, 20)
	if c == nil {
		return
	}
	if m.issuePane != jira.PaneChat {
		m.vp.SetContent(c.View(m.issuePane, w, m.vp.Height))
		if m.issuePane == jira.PaneComments && c.Typing() {
			m.vp.GotoBottom() // the comment box is at the bottom
		}
		return
	}
	follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
	if t := m.jiraChat(c.Issue.Key); t != nil && len(m.conv(t.ID).msgs) > 0 {
		m.vp.SetContent(m.renderChat(m.conv(t.ID).msgs, w, agentName(*t), m.conv(t.ID).busy))
	} else {
		m.vp.SetContent(dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude anything about " + c.Issue.Key +
			": how to approach it, what to ask the reporter, a plan, a better description. Claude reads the issue, its comments " +
			"and linked issues with acli, and changes nothing in Jira unless you ask it to.\n\n/compact, /new and /model work as in a task.")))
	}
	if follow {
		m.vp.GotoBottom()
	}
}

// updateIssueRow handles keys on an issue: the card's keys on Issue and Comments, the message box on
// Chat, tab/shift+tab to switch tabs, esc back to the list, and ↑↓ to scroll.
func (m Model) updateIssueRow(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c, k := m.selIssue(), msg.String()
	switching := k == "tab" || k == "shift+tab"
	if m.issuePane == jira.PaneChat && (!switching || m.input.Value() != "" || len(m.sugg) > 0) {
		return m.updateChat(msg) // the message box, as in a task's chat
	}
	if m.issuePane != jira.PaneChat {
		ev, cmd, ok := c.KeyMsg(m.cfg.Ctx, m.issuePane, msg)
		if ok {
			return m, tea.Batch(cmd, m.onCardEvent(c, ev))
		}
	}
	if switching {
		d := map[string]int{"shift+tab": -1, "tab": 1}[k]
		m.setIssuePane((m.issuePane + d + len(jira.PaneNames)) % len(jira.PaneNames))
		return m, nil
	}
	if k == "esc" {
		m.goUp()
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // ↑↓ pgup pgdn scroll
	return m, cmd
}

// onCardEvent does what a key on the card asks of crew.
func (m *Model) onCardEvent(c *jira.Card, ev jira.CardEvent) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case ev.URL != "":
		openURL(ev.URL)
	case ev.Work:
		return m.workOnIssue(c.Issue)
	case ev.Open != "":
		return m.openLinked(c, ev.Open)
	case ev.Draft:
		cmd = draftComment(m.cfg.Ctx, c.Issue.Key, c.Text())
	}
	if ev.Comments && m.issuePane != jira.PaneComments {
		m.setIssuePane(jira.PaneComments)
	}
	m.renderIssueRow()
	return cmd
}

// onCardChanged redraws the issue on screen when its card took a message.
func (m *Model) onCardChanged(c *jira.Card) {
	if c != nil && m.selIssue() == c {
		m.renderIssueRow()
	}
}

// issueHint is the line under an issue's tab.
func (m Model) issueHint(c *jira.Card) string {
	if m.issuePane == jira.PaneChat {
		return m.suggestionsView() + m.input.View()
	}
	return hint("  " + c.Hint(m.issuePane))
}

// jiraChat is the chat about an issue, or nil before you open its Chat tab.
func (m Model) jiraChat(key string) *tasks.Task {
	for i := range m.chats {
		if m.chats[i].Kind == jiraChatKind && m.chats[i].Issue == key {
			return &m.chats[i]
		}
	}
	return nil
}

// ensureJiraChat makes the chat about an issue if there isn't one yet (in memory; the first message
// saves it).
func (m *Model) ensureJiraChat(c *jira.Card) *tasks.Task {
	if t := m.jiraChat(c.Issue.Key); t != nil {
		return t
	}
	i := c.Issue
	m.chats = append(m.chats, tasks.Task{
		ID:    tasks.Slugify("jira " + i.Key),
		Title: i.Key + " " + i.Summary,
		Kind:  jiraChatKind, Issue: i.Key, Status: tasks.InProgress, Created: time.Now(),
		Body: fmt.Sprintf("You're helping me with Jira issue %s. Atlassian's CLI (acli) is signed in: read the issue with "+
			"`acli jira workitem view %[1]s --fields '*all' --json` and its comments with `acli jira workitem comment list --key %[1]s`. "+
			"Don't edit, comment on or move it in Jira unless I ask; when I do, use acli and say what you changed.\n\n%s",
			i.Key, i.Brief()),
	})
	return &m.chats[len(m.chats)-1]
}

// startJiraChat runs an issue chat's first turn in its own folder (stable, so Claude can resume the
// session there).
func (m *Model) startJiraChat(t tasks.Task, prompt string) tea.Cmd {
	dir := filepath.Join(m.cfg.JiraChat, t.ID)
	return func() tea.Msg { return startedMsg{t.ID, "", dir, "", prompt, os.MkdirAll(dir, 0o755)} }
}
