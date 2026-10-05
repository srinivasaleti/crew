package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
	"crew/internal/task"
)

const sidebarWidth = 28

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold   = lipgloss.NewStyle().Bold(true)
	sel    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	notice = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	icons  = map[string]string{task.Todo: "○", task.Working: "●", task.Waiting: "◐", task.Review: "✓", task.Done: "✓"}
	tint   = map[string]lipgloss.Style{
		task.Todo:    dim,
		task.Working: lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		task.Waiting: lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true),
		task.Review:  lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		task.Done:    dim,
	}
)

func (m Model) View() string {
	bodyH := max(m.h-2-m.inputHeight(), 3)
	side := lipgloss.NewStyle().Width(sidebarWidth).Height(bodyH).MaxHeight(bodyH).Render(m.sidebar())
	divider := dim.Render(strings.TrimSuffix(strings.Repeat("│\n", bodyH), "\n"))
	mainW := max(m.w-sidebarWidth-3, 10)
	var main string
	if m.modal != noModal {
		main = lipgloss.Place(mainW, bodyH, lipgloss.Center, lipgloss.Center, m.modalView())
	} else {
		main = lipgloss.NewStyle().Width(mainW).Height(bodyH).MaxHeight(bodyH).
			Render(m.header(mainW) + "\n" + m.tabs(mainW) + "\n" +
				lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), " ", m.scrollbar()))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, side, divider, " ", main)

	input := m.input.View()
	if m.current() == nil {
		input = dim.Render("  Press ctrl+n to create a task, or pick one with ↑↓ to chat with its agent")
	} else if m.tab == tabChanges {
		input = dim.Render("  Viewing changes in " + tildify(m.current().Worktree))
	} else if m.tab == tabUsage {
		input = dim.Render("  Viewing usage for " + m.current().Title)
	}
	help := dim.Render("ctrl+n new task  ↑↓ switch  ctrl+c quit")
	if m.current() != nil {
		help = dim.Render("ctrl+n new task  ctrl+d delete task  tab/→ changes · usage  ← task list  ↑↓ switch  ⏎ send  alt+⏎ or \\⏎ new line  pgup/pgdn scroll  esc home  ctrl+c quit")
	}
	if m.listFocus {
		help = dim.Render("↑↓ choose task  →/⏎ open chat  ctrl+n new task  ctrl+d delete task  esc home  ctrl+c quit")
	}
	if m.tab != tabChat && m.current() != nil {
		help = dim.Render("tab/←/→ switch tab  esc back to chat  ↑↓ pgup/pgdn home/end scroll  r refresh  ctrl+c quit")
	}
	if m.notice != "" {
		help = notice.Render(m.notice)
	}
	return body + "\n" + dim.Render(strings.Repeat("─", m.w)) + "\n" + input + "\n" + help
}

func (m Model) modalView() string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(1, 2).Width(60)
	if m.modal == deleteModal {
		t := m.current()
		if t == nil {
			return ""
		}
		keep := "Its chat history is removed."
		if t.Worktree != "" {
			keep += " The worktree at " + tildify(t.Worktree) + " is kept."
		}
		return box.BorderForeground(lipgloss.Color("9")).Render(bold.Render("Delete task?") + "\n\n" +
			t.Title + "\n" + dim.Render(keep) + "\n\n" + warn.Render("y") + " delete    " + dim.Render("any other key cancels"))
	}
	label := func(i int, s string) string {
		if m.field == i {
			return sel.Render(s)
		}
		return bold.Render(s)
	}
	errLine := ""
	if m.formErr != "" {
		errLine = "\n" + warn.Render(m.formErr)
	}
	return box.Render(bold.Render("New task") + "\n\n" +
		label(0, "Name") + "\n" + m.name.View() + "\n\n" +
		label(1, "Folder") + dim.Render("  any git repo on this machine") + "\n" + m.folder.View() + errLine + "\n\n" +
		dim.Render("tab complete folder  ↑↓ switch field  ⏎ create  esc cancel"))
}

func (m Model) sidebar() string {
	var b strings.Builder
	// row draws one sidebar entry; right (the task's cost) is right-aligned.
	row := func(i int, icon, title, right string) {
		tw := sidebarWidth - 5
		if right != "" {
			tw -= len(right) + 1
		}
		title = ansi.Truncate(title, tw, "…")
		gap := strings.Repeat(" ", max(0, tw-ansi.StringWidth(title)))
		if right != "" {
			gap += " "
		}
		if i == m.sel && m.listFocus {
			bar := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("0")).Bold(true).Width(sidebarWidth)
			b.WriteString(bar.Render("▶ "+ansi.Strip(icon)+" "+title+gap+right) + "\n")
		} else if i == m.sel {
			b.WriteString(sel.Render("▶ ") + icon + " " + bold.Render(title) + gap + dim.Render(right) + "\n")
		} else {
			b.WriteString("  " + icon + " " + title + gap + dim.Render(right) + "\n")
		}
	}
	homeIcon := green.Render("✦")
	if m.conv(homeID).busy {
		homeIcon = tint[task.Working].Render("●")
	}
	row(0, homeIcon, "crew", "")
	b.WriteString("\n" + dim.Render("  TASKS") + "\n")
	if len(m.tasks) == 0 {
		b.WriteString(dim.Render("  none yet") + "\n")
	}
	for i, t := range m.tasks {
		icon := icons[t.Status]
		if icon == "" {
			icon = "?"
		}
		spent := ""
		if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
			spent = cost(u.Cost)
		}
		row(i+1, tint[t.Status].Render(icon), t.Title, spent)
	}
	if m.loadErr != "" {
		b.WriteString("\n" + warn.Render(ansi.Truncate(m.loadErr, sidebarWidth, "…")))
	}
	return b.String()
}

// tabs is the second header line: the Chat / Changes / Usage tab bar for a task, a plain rule on home.
func (m Model) tabs(w int) string {
	if m.current() == nil {
		return dim.Render(strings.Repeat("─", w))
	}
	// Same highlight bar as the focused task in the sidebar.
	active := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	inactive := dim.Padding(0, 1)
	var bar []string
	for i, name := range tabNames {
		if tab(i) == m.tab {
			bar = append(bar, active.Render(name))
		} else {
			bar = append(bar, inactive.Render(name))
		}
	}
	extra := "tab or ←/→ to switch"
	switch {
	case m.tab == tabChanges && m.diffSummary != "":
		extra = m.diffSummary
	case m.tab == tabUsage:
		extra = "from Claude Code's session log"
	}
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render("   "+extra), w, "…")
}

func (m Model) header(w int) string {
	busy := ""
	if m.conv(m.currentID()).busy {
		busy = tint[task.Working].Render("  ● working…")
	}
	t := m.current()
	if t == nil {
		return bold.Render("crew") + dim.Render("  home")
	}
	meta := " · " + filepath.Base(t.Repo) + " · " + agentName(*t) + " · " + t.Status
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		meta += " · " + cost(u.Cost) + " · " + tokens(u.Tokens()) + " tokens"
	}
	return ansi.Truncate(bold.Render(t.Title)+dim.Render(meta)+busy, w, "…")
}

// refresh re-renders the selected chat into the viewport and scrolls to the latest message.
func (m *Model) refresh() {
	if m.tab != tabChat {
		return // the Changes/Usage tab owns the viewport until you return to Chat
	}
	m.vp.SetContent(m.render())
	m.vp.GotoBottom()
}

func (m *Model) render() string {
	w := max(m.vp.Width, 20)
	msgs := m.conv(m.currentID()).msgs
	t := m.current()
	if len(msgs) == 0 {
		if t == nil {
			return m.homeView()
		}
		return dim.Render(lipgloss.NewStyle().Width(w).Render(
			"Send a message to start this task. " + agentName(*t) + " gets the task below plus your message.\n\n" + brief(*t)))
	}
	agentLabel := "crew"
	if t != nil {
		agentLabel = agentName(*t)
	}
	text := lipgloss.NewStyle().Width(w - 8)
	var b strings.Builder
	for i, msg := range msgs {
		if i > 0 {
			if msg.Role == chat.Tool && msgs[i-1].Role == chat.Tool {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		var label lipgloss.Style
		var name, body = "", msg.Text
		switch msg.Role {
		case chat.You:
			label, name = sel, "you"
		case chat.Agent:
			label, name = green, agentLabel
		case chat.Error:
			label, name, body = warn, "error", warn.Render(body)
		default:
			body = dim.Render("▸ " + body)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Width(8).Render(name), text.Render(body)))
	}
	return b.String()
}

func (m Model) homeView() string {
	counts := map[string]int{}
	for _, t := range m.tasks {
		counts[t.Status]++
	}
	var b strings.Builder
	b.WriteString(bold.Render("Welcome to crew") + "\n\n")
	b.WriteString(sel.Render("ctrl+n") + "  create a task: give it a name and pick a repo folder\n")
	b.WriteString(sel.Render("↑↓    ") + "  pick a task and chat with its agent; your first message starts it\n\n")
	b.WriteString(bold.Render("Tasks") + "  " + strconv.Itoa(len(m.tasks)) + " total   " +
		tint[task.Working].Render("● "+strconv.Itoa(counts[task.Working])+" working") + "   " +
		tint[task.Waiting].Render("◐ "+strconv.Itoa(counts[task.Waiting])+" waiting on you") + "\n\n")
	b.WriteString(bold.Render("Repos") + "\n")
	repos := m.cfg.Repos.List()
	if len(repos) == 0 {
		b.WriteString(dim.Render("  none yet, you pick one when creating a task") + "\n")
	}
	for _, r := range repos {
		b.WriteString("  " + tildify(r) + "\n")
	}
	return b.String()
}

// scrollbar draws a one-column track beside the viewport with a thumb sized to the visible share.
// It stays blank when everything fits.
func (m Model) scrollbar() string {
	h, total := m.vp.Height, m.vp.TotalLineCount()
	if h <= 0 {
		return ""
	}
	rows := make([]string, h)
	if total <= h {
		for i := range rows {
			rows[i] = " "
		}
		return strings.Join(rows, "\n")
	}
	thumb := max(1, h*h/total)
	top := m.vp.YOffset * (h - thumb) / (total - h)
	for i := range rows {
		if i >= top && i < top+thumb {
			rows[i] = sel.Render("┃")
		} else {
			rows[i] = dim.Render("│")
		}
	}
	return strings.Join(rows, "\n")
}

func cost(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

// tokens formats a count compactly: 950, 12.3k, 1.2M.
func tokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return strconv.Itoa(n)
	}
}
