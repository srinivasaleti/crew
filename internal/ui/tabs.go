package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/repo"
	"crew/internal/task"
	"crew/internal/usage"
)

// tab is what the main pane shows for the selected task.
type tab int

const (
	tabChat tab = iota
	tabChanges
	tabUsage
)

var tabNames = []string{"Chat", "Changes", "Usage"}

// setTab switches the selected task's main pane and loads what the tab shows.
func (m *Model) setTab(t tab) tea.Cmd {
	if m.current() == nil {
		return nil
	}
	m.shellFocus = -1 // picking a tab is a chat action; ctrl+g goes back to the shells
	m.tab = t
	if t == tabChat {
		m.onSelect() // refocuses the message box and redraws the chat
		return nil
	}
	m.input.Blur()
	m.layout()
	return m.loadTab()
}

// loadTab fills the viewport for the Changes or Usage tab.
func (m *Model) loadTab() tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	switch m.tab {
	case tabChanges:
		if t.Worktree == "" {
			m.diffSummary = ""
			m.vp.SetContent(dim.Render("No changes yet. Send a message in the Chat tab to start the task."))
			return nil
		}
		m.diffSummary = "loading…"
		id, wt, base := t.ID, t.Worktree, t.Base
		return func() tea.Msg {
			diff, summary, err := repo.Diff(wt, base)
			return diffMsg{id, diff, summary, err}
		}
	case tabUsage:
		m.loadUsage(*t)
		m.vp.SetContent(clip(m.usageView(*t), m.vp.Width))
		m.vp.GotoTop()
	}
	return nil
}

// loadUsage refreshes the cached usage for a task from Claude's session log.
// ponytail: read synchronously; session logs are small enough. Move to a tea.Cmd if they get slow.
func (m *Model) loadUsage(t task.Task) {
	if u, ok, _ := usage.Load(t.Session); ok {
		m.usage[t.ID] = u
	} else {
		delete(m.usage, t.ID)
	}
}

// updateTab handles keys on the Changes and Usage tabs.
func (m Model) updateTab(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab", "right", "left", "esc", "q":
		return m.updateTabKeys(msg)
	case "r":
		return m, m.loadTab()
	case "home":
		m.vp.GotoTop()
		return m, nil
	case "end":
		m.vp.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // up/down/pgup/pgdown scroll
	return m, cmd
}

func (m Model) usageView(t task.Task) string {
	if t.Session == "" {
		return dim.Render("No usage yet. Send a message in the Chat tab to start the task.")
	}
	u, ok := m.usage[t.ID]
	if !ok {
		return dim.Render("No usage found yet for session " + t.Session + " in Claude's session log.")
	}
	var b strings.Builder
	line := func(label, value string) { fmt.Fprintf(&b, "  %-12s %s\n", label, value) }
	b.WriteString(bold.Render("Total") + "\n")
	line("Cost", cost(u.Cost))
	line("Tokens", tokens(u.Tokens()))
	line("Time", dur(u.APIMs)+" in API calls · "+dur(u.TotalMs)+" total")
	line("Lines", fmt.Sprintf("+%d −%d", u.Added, u.Removed))
	line("Turns", fmt.Sprint(len(u.Turns)))
	compacted := ""
	if u.Compactions > 0 {
		compacted = fmt.Sprintf(" · compacted %d×", u.Compactions)
	}
	line("Context", contextLabel(u.Context)+dim.Render(" re-sent with each message"+compacted+" · /compact shrinks it"))

	b.WriteString("\n" + bold.Render("By model") + "\n")
	for _, md := range u.Models {
		b.WriteString("  " + md.Name + "  " + dim.Render(cost(md.Cost)) + "\n")
		thinking := ""
		if md.Thinking > 0 {
			thinking = " (thinking " + tokens(md.Thinking) + ")"
		}
		b.WriteString(dim.Render(fmt.Sprintf("    input %s · output %s%s · cache read %s · cache write %s",
			tokens(md.Input), tokens(md.Output), thinking, tokens(md.CacheRead), tokens(md.CacheWrite))) + "\n")
	}

	b.WriteString("\n" + bold.Render("By turn") + dim.Render("   tokens   output   your message") + "\n")
	for i, tn := range u.Turns {
		prompt, _, _ := strings.Cut(tn.Prompt, "\n")
		fmt.Fprintf(&b, "  %3d   %8s %8s   %s\n", i+1, tokens(tn.Tokens), tokens(tn.Output), prompt)
	}
	b.WriteString("\n" + dim.Render("Read from Claude Code's session log. Cost is Claude Code's estimate\n"+
		"(API-equivalent if you're on a subscription). r refreshes."))
	return b.String()
}

// dur formats milliseconds as 9.3s or 2m05s.
func dur(ms int) string {
	if ms < 60_000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dm%02ds", ms/60_000, ms%60_000/1000)
}

// clip cuts each line to width so long lines don't wrap and break the layout.
func clip(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}

// updateTabKeys switches tabs from the Changes and Usage tabs.
func (m Model) updateTabKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := tab(len(tabNames))
	switch msg.String() {
	case "tab":
		return m, m.setTab((m.tab + 1) % n)
	case "shift+tab":
		return m, m.setTab((m.tab + n - 1) % n)
	case "right":
		if m.tab < n-1 {
			return m, m.setTab(m.tab + 1)
		}
	case "left":
		return m, m.setTab(m.tab - 1)
	case "esc", "q":
		return m, m.setTab(tabChat)
	}
	return m, nil
}
