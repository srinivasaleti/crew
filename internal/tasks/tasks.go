// Package tasks is TASKS, all of it: task files on disk (store.go), how a task's status looks, the
// order tasks are listed in, the TASKS table, the new-task form and the status menu. What a task
// shows once opened (chat, plan, PR, files) is crew's ui. ui hands this package what
// it knows about each task (an Item) and does what opening a row asks.
package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/look"
	"crew/internal/table"
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

// Item is a task with what crew knows about it right now.
type Item struct {
	Task
	Busy   bool    // its agent is working
	Unread bool    // its agent replied while you were elsewhere
	PR     *gh.PR  // its pull request; nil without one
	Badge  string  // the PR's badge, as the sidebar shows it ("PR ✓")
	Branch string  // its worktree's branch
	Plan   string  // its plan's progress ("2/5"); "" without a plan
	Cost   float64 // what its sessions cost, in USD
	Last   time.Time
}

// NeedsYou reports whether a task waits on you: a reply is ready, or its PR has feedback.
func (it Item) NeedsYou() bool { return it.Unread || it.PR != nil && PRTodo(it.PR) > 0 }

// rank orders tasks: what's moving first (in progress, the agent working, a reply ready), then
// in PR, blocked, todo, and done last.
func (it Item) rank() int {
	if it.Unread || it.Busy {
		return 0
	}
	return map[string]int{InProgress: 0, InPR: 1, Blocked: 2, Todo: 3, Done: 4}[it.Status]
}

// Sort orders items by rank, then the most recently active first.
func Sort(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if ra, rb := a.rank(), b.rank(); ra != rb {
			return ra - rb
		}
		return b.Last.Compare(a.Last)
	})
}

// PRTodo counts what's left on an open PR: failing checks, open line comments, a change request.
func PRTodo(pr *gh.PR) int {
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

// What opening a row of the table asks crew to do (with no action, it opens the task's chat).
const (
	OpenPR   = "pr"     // its PR tab
	OpenTodo = "todo"   // its PR tab, on what's left
	OpenPlan = "plan"   // its Plan tab
	New      = "new"    // the new-task form
	Status   = "status" // its status menu, over the list
	Delete   = "delete" // asks to delete it, over the list
)

// Table is the TASKS overview: every task, what's moving first, with its status, the agent's
// state, the PR, what's left to do on it, cost and last activity. Chips filter by what needs you,
// what's working, each status, and archived tasks (which show nowhere else); the facet is the
// repo. It's built from what crew already has, so it costs no gh calls and no tokens.
func Table(items []Item) table.Table {
	items = slices.Clone(items)
	Sort(items)
	t := table.Table{
		Empty: "No tasks match.", Facet: "repo",
		Cols: []table.Col{{Name: "task"}, {Name: "status", Width: 13}, {Name: "agent", Width: 16}, {Name: "PR", Width: 14, Action: OpenPR},
			{Name: "to do", Width: 6, Action: OpenTodo}, {Name: "plan", Width: 6, Action: OpenPlan}, {Name: "cost", Width: 7}, {Name: "last", Width: 9}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "needs", Label: "Needs you"}, {ID: "working", Label: "Working"},
			{ID: InProgress, Label: "In progress"}, {ID: InPR, Label: "In PR"}, {ID: Todo, Label: "Todo"},
			{ID: Blocked, Label: "Blocked"}, {ID: Done, Label: "Done"}, {ID: "archived", Label: "Archived"}},
		Keys:   map[string]string{"p": OpenPR, "t": OpenTodo, "l": OpenPlan, "n": New, "s": Status, "x": Delete},
		Global: map[string]bool{"n": true},
		Acts: []table.Act{{Label: "+ Task", Key: "n"}, {Label: "Open", Key: "enter"}, {Label: "Status", Key: "s"}, {Label: "PR", Key: "p"},
			{Label: "To do", Key: "t"}, {Label: "Plan", Key: "l"}, {Label: "Delete", Key: "x"}},
		Hint: "⏎ opens · n new task · s status · x delete · p its PR · t what's left · l its plan · ←/→ filter · / search · ctrl+o archived",
	}
	needs, working, archived := 0, 0, 0
	for _, it := range items {
		tags := []string{"all", "archived"}
		if it.Archived {
			archived++
		} else {
			tags = []string{"all", it.Status}
			switch {
			case it.NeedsYou():
				tags = append(tags, "needs")
				needs++
			case it.Busy:
				tags = append(tags, "working")
				working++
			}
		}
		var agent string
		switch {
		case it.Busy:
			agent = look.Running.Render("● working")
		case it.Unread:
			agent = look.Ready.Render("◐ reply ready")
		case it.Session == "":
			agent = look.Dim.Render("not started")
		default:
			agent = look.Dim.Render("idle")
		}
		pr, todo, plan, spent := look.Dim.Render("–"), look.Dim.Render("–"), look.Dim.Render("–"), look.Dim.Render("–")
		if it.PR != nil {
			pr = fmt.Sprintf("#%d %s", it.PR.Number, strings.TrimPrefix(it.Badge, "PR"))
			if n := PRTodo(it.PR); n > 0 {
				todo = look.Orange.Render(fmt.Sprintf("● %d", n))
			} else if it.PR.State == "OPEN" {
				todo = look.Ready.Render("✔")
			}
		}
		if it.Plan != "" {
			plan = it.Plan
		}
		if it.Cost > 0 {
			spent = fmt.Sprintf("$%.2f", it.Cost)
		}
		title := it.Title
		if it.Status == Done || it.Archived {
			title = look.Dim.Render(title)
		}
		repo := filepath.Base(it.Repo)
		t.Rows = append(t.Rows, table.Row{
			ID:    it.ID,
			Cells: []string{title, Tint[it.Status].Render(Icons[it.Status] + " " + Labels[it.Status]), agent, pr, todo, plan, spent, look.Dim.Render(look.Ago(it.Last))},
			Tags:  tags, Facet: repo,
			Search: strings.Join([]string{it.Title, repo, it.ID, it.Branch}, " "),
		})
	}
	if len(items) == 0 {
		t.Empty = "No tasks yet: n creates one, with a name, a repo and what to do"
	}
	t.Summary = fmt.Sprintf("%d task%s", len(items), look.Plural(len(items)))
	if archived > 0 {
		t.Summary += fmt.Sprintf(" (%d archived)", archived)
	}
	if needs > 0 {
		t.Summary += " · " + look.Orange.Render(fmt.Sprintf("%d need%s you", needs, map[bool]string{true: "s", false: ""}[needs == 1]))
	}
	if working > 0 {
		t.Summary += " · " + look.Running.Render(fmt.Sprintf("%d working", working))
	}
	return t
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
