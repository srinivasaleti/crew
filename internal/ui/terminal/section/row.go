// Package section is crew's sections as plugins: what a section (TASKS, PULL REQUESTS, REQUESTS,
// REMINDERS, FOCUS, JIRA) gives crew, and what crew gives it. A section lives in its own package
// under sections/, owns its rows in the sidebar, its main pane, its messages and its clocks, and
// reaches the rest of crew only through Host. crew holds the sections in a list and asks the one
// whose row is selected; it has no code of its own for any of them.
package section

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// Row kinds crew knows: home, a section's own row, and a task's row.
const (
	Home  = ""      // crew's home row: the zero Row is home
	Group = "group" // a section's own row
	// Task is a task's row (Index indexes Host.Tasks): crew opens it as the task's workspace (its
	// chat, tabs and shells), not as a page its section draws.
	Task = "task"
)

// Row is one row of the sidebar: home, a section's own row, or something open in a section (a task,
// a PR, an issue), which shows under its section's row while it's selected.
type Row struct {
	Kind    string // Home, Group, or the section's kind of item ("task", "pr", "issue"…)
	Section string // the section's key ("" for home)
	Key     string // the selection's key ("g:jira", "j:OS-12"); "" lets crew make one from the kind
	Label   string // a section's label ("JIRA"), or the item's name
	Count   int    // how many items a section has, shown on its row
	Depth   int    // indent level
	Index   int    // for the section's own use (an index into its list)
}

// Item reports whether a row is one thing inside a section (not home or a section's row).
func (r Row) Item() bool { return r.Kind != Home && r.Kind != Group }

// TabLabel is tab i's name with the ⌘ key that jumps to it, e.g. "Plan ⌘2".
func TabLabel(name string, i int) string { return fmt.Sprintf("%s ⌘%d", name, i+1) }

// TabAt is the tab under column x of a tab bar of these names (each label padded by one space,
// joined by one), or -1.
func TabAt(names []string, x int) int {
	pos := 0
	for i, name := range names {
		w := ansi.StringWidth(TabLabel(name, i)) + 2
		if x >= pos && x < pos+w {
			return i
		}
		pos += w + 1
	}
	return -1
}
