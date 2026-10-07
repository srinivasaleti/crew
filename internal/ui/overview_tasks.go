package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/gh"
	"crew/internal/task"
)

// taskTable is the TASKS overview: every task, what's moving first (taskRank), with its status, the agent's state, the PR, what's left to do on it, cost and last
// activity. Chips filter by what needs you, what's working, each status, and archived tasks (which
// show nowhere else); the facet is the repo. It's built from what crew already has (chats, the
// PR poll, usage), so it costs no gh calls and no tokens.
func (m Model) taskTable(r row) ovTable {
	idx := make([]int, len(m.tasks))
	for i := range idx {
		idx[i] = i
	}
	m.sortTasks(idx)

	t := ovTable{
		empty: "No tasks match.", facet: "repo",
		cols: []ovCol{{"task", 0, ""}, {"status", 13, ""}, {"agent", 16, ""}, {"PR", 14, "pr"}, {"to do", 6, "todo"}, {"plan", 6, "plan"}, {"cost", 7, ""}, {"last", 9, ""}},
		chips: []ovChip{{"all", "All"}, {"needs", "Needs you"}, {"working", "Working"},
			{task.InProgress, "In progress"}, {task.InPR, "In PR"}, {task.Todo, "Todo"}, {task.Blocked, "Blocked"}, {task.Done, "Done"},
			{"archived", "Archived"}},
		keys: map[string]string{"p": "pr", "t": "todo"},
		hint: "←/→ filter · ⏎ opens · p or a click on PR opens its PR · t or a click on to do shows what's left",
		open: openTask,
	}
	needs, working := 0, 0
	for _, i := range idx {
		tk := m.tasks[i]
		c := m.conv(tk.ID)
		tags := []string{"all", "archived"}
		if !tk.Archived {
			tags = []string{"all", tk.Status}
			switch {
			case m.needsYou(tk):
				tags = append(tags, "needs")
				needs++
			case c.busy:
				tags = append(tags, "working")
				working++
			}
		}

		var agent string
		switch {
		case c.busy:
			agent = running.Render("● working")
		case m.unread[tk.ID]:
			agent = ready.Render("◐ reply ready")
		case tk.Session == "":
			agent = dim.Render("not started")
		default:
			agent = dim.Render("idle")
		}
		pr, todo, branch := dim.Render("–"), dim.Render("–"), ""
		if st := m.prs[tk.ID]; st != nil {
			branch = st.branch
			if st.pr != nil {
				pr = fmt.Sprintf("#%d %s", st.pr.Number, trimPRPrefix(m.prBadge(tk.ID)))
				if n := prTodo(st.pr); n > 0 {
					todo = orange.Render(fmt.Sprintf("● %d", n))
				} else if st.pr.State == "OPEN" {
					todo = ready.Render("✔")
				}
			}
		}
		planCell := dim.Render("–")
		if p := m.planProgress(tk.ID); p != "" {
			planCell = p
		}
		spent := dim.Render("–")
		if u, ok := m.usage[tk.ID]; ok && u.Cost > 0 {
			spent = cost(u.Cost)
		}
		title := tk.Title
		if tk.Status == task.Done || tk.Archived {
			title = dim.Render(title)
		}
		repo := filepath.Base(tk.Repo)
		t.rows = append(t.rows, ovRow{
			id:    tk.ID,
			cells: []string{title, tint[tk.Status].Render(icons[tk.Status] + " " + statusLabels[tk.Status]), agent, pr, todo, planCell, spent, dim.Render(ago(m.lastActive(tk)))},
			tags:  tags, facet: repo,
			search: strings.Join([]string{tk.Title, repo, tk.ID, branch}, " "),
		})
	}
	t.summary = fmt.Sprintf("%d task%s", len(m.tasks), plural(len(m.tasks)))
	if n := countArchived(m.tasks); n > 0 {
		t.summary += fmt.Sprintf(" (%d archived)", n)
	}
	if needs > 0 {
		t.summary += " · " + orange.Render(fmt.Sprintf("%d need%s you", needs, map[bool]string{true: "s", false: ""}[needs == 1]))
	}
	if working > 0 {
		t.summary += " · " + running.Render(fmt.Sprintf("%d working", working))
	}
	return t
}

func countArchived(ts []task.Task) int {
	n := 0
	for _, t := range ts {
		if t.Archived {
			n++
		}
	}
	return n
}

// prTodo counts what's left on an open PR: failing checks, open line comments, a change request.
func prTodo(pr *gh.PR) int {
	if pr.State != "OPEN" {
		return 0
	}
	n := 0
	for _, c := range pr.Checks {
		if c.State == gh.Fail {
			n++
		}
	}
	for _, t := range pr.Threads {
		if !t.Resolved {
			n++
		}
	}
	if pr.ReviewDecision == "CHANGES_REQUESTED" {
		n++
	}
	return n
}

// trimPRPrefix drops the "PR" the sidebar badge starts with, keeping its marks.
func trimPRPrefix(badge string) string {
	if len(badge) >= 2 && badge[:2] == "PR" {
		return badge[2:]
	}
	return badge
}

// openTask opens a task from the overview: its chat, its PR tab ("pr"), its PR's to-do ("todo"), or
// its plan ("plan").
func openTask(m *Model, id, action string) tea.Cmd {
	m.listFocus, m.tab = false, tabChat // a task opens on its Chat tab
	m.selectID(id)
	switch action {
	case "pr":
		return m.setTab(tabPR)
	case "todo":
		m.prOf(id).todo = true
		return m.setTab(tabPR)
	case "plan":
		return m.setTab(tabPlan)
	}
	return nil
}
