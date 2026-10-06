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
	icons  = map[string]string{task.Todo: "○", task.InProgress: "◑", task.InPR: "⇡", task.Blocked: "⊘", task.Done: "✓"}
	tint   = map[string]lipgloss.Style{
		task.Todo:       dim,
		task.InProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		task.InPR:       lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
		task.Blocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		task.Done:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
	}
	// Agent activity, shown over the status icon while it applies.
	running = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	ready   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
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

	input := m.suggestionsView() + m.input.View()
	if m.onGroupRow() {
		input = dim.Render("  Press → or enter to open the group, ← to close it")
	} else if m.current() == nil {
		input = dim.Render("  Press ctrl+n to create a task, or pick one with ↑↓ to chat with its agent")
	} else if m.tab == tabChanges {
		input = dim.Render("  Viewing changes in " + tildify(m.current().Worktree))
	} else if m.tab == tabUsage {
		input = dim.Render("  Viewing usage for " + m.current().Title)
	}
	// Screen-specific keys first, then the shortcuts that work everywhere (from keyCommands).
	screen := "↑↓ switch"
	switch {
	case m.filtering:
		screen = "type to filter  ↑↓ choose  ⏎ open  esc clear"
	case m.listFocus:
		screen = "↑↓ choose  →/⏎ open  ← close group  esc home"
	case m.tab != tabChat && m.current() != nil:
		screen = "tab/←/→ switch tab  esc back to chat  ↑↓ pgup/pgdn home/end scroll  r refresh"
	case m.current() != nil:
		screen = "⏎ send  ↑↓ scroll  ← task list  tab/→ changes · usage  @ file  " + strings.ReplaceAll(slashNames(), ",", "") +
			"  alt+⏎ or \\⏎ new line  pgup/pgdn page  esc home"
	}
	help := dim.Render(screen + "  " + m.keyHelp())
	if m.notice != "" {
		help = notice.Render(m.notice)
	}
	return body + "\n" + dim.Render(strings.Repeat("─", m.w)) + "\n" + input + "\n" + help
}

func (m Model) modalView() string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(1, 2).Width(60)
	if m.modal == statusModal {
		return m.statusMenuView()
	}
	if m.modal == modelModal {
		return m.modelMenuView()
	}
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
		busy = running.Render("  ● working…")
	}
	t := m.current()
	if r := m.selRow(); r.kind == rowGroup {
		return bold.Render(r.label) + dim.Render(fmt.Sprintf("  %d tasks", r.count))
	}
	if t == nil {
		return bold.Render("crew") + dim.Render("  home")
	}
	spent, ctx := "", ""
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		spent = " · " + cost(u.Cost) + " · "
		ctx = contextLabel(u.Context)
	}
	// The status chip is clickable; click() finds it at the width of title+headerMeta.
	return ansi.Truncate(bold.Render(t.Title)+dim.Render(m.headerMeta(*t))+statusChip(t.Status)+dim.Render(spent)+ctx+busy, w, "…")
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
		if r := m.selRow(); r.kind == rowGroup {
			return m.groupView(r)
		}
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
			body = action(body)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Width(8).Render(name), text.Render(body)))
	}
	return b.String()
}

// headerMeta is the plain text between the task title and its status chip.
func (m Model) headerMeta(t task.Task) string {
	model := t.Model
	if model == "" {
		model = m.usage[t.ID].LastModel // what Claude actually answered with
	}
	if model == "" {
		model = agentName(t)
	}
	return " · " + filepath.Base(t.Repo) + " · " + model + " · "
}

func (m Model) homeView() string {
	counts := map[string]int{}
	busy, unread, archived := 0, len(m.unread), 0
	for _, t := range m.tasks {
		if t.Archived {
			archived++
			continue
		}
		counts[t.Status]++
		if m.conv(t.ID).busy {
			busy++
		}
	}
	var b strings.Builder
	b.WriteString(bold.Render("Welcome to crew") + "\n\n")
	b.WriteString(sel.Render("ctrl+n") + "  create a task: give it a name and pick a repo folder\n")
	b.WriteString(sel.Render("↑↓    ") + "  pick a task and chat with its agent; your first message starts it\n")
	b.WriteString(sel.Render("ctrl+s") + "  set a task's status (todo, in progress, in PR, blocked, done) or archive it\n")
	b.WriteString(sel.Render("ctrl+f") + "  filter tasks by title, repo or status (archived included)\n")
	b.WriteString(sel.Render("→ ←   ") + "  open and close status groups in the list\n")
	b.WriteString(sel.Render("ctrl+o") + "  open or close archived tasks\n\n")
	b.WriteString(bold.Render("Tasks") + "  " + strconv.Itoa(len(m.tasks)-archived) + " active")
	if archived > 0 {
		b.WriteString(dim.Render(fmt.Sprintf(" · %d archived", archived)))
	}
	b.WriteString("\n  ")
	for _, s := range task.Statuses {
		b.WriteString(tint[s].Render(fmt.Sprintf("%s %d %s", icons[s], counts[s], statusLabels[s])) + "   ")
	}
	b.WriteString("\n  " + running.Render(fmt.Sprintf("● %d agents running", busy)) + "   " +
		ready.Render(fmt.Sprintf("◐ %d replies ready", unread)) + "\n\n")
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

// actionStyles colours an agent action by what kind of tool it is.
var actionStyles = []struct {
	tools []string
	icon  string
	color lipgloss.Color
}{
	{[]string{"Bash"}, "❯", "11"},                                       // shell: yellow
	{[]string{"Edit", "MultiEdit", "Write", "NotebookEdit"}, "✎", "13"}, // file changes: magenta
	{[]string{"Read", "LS"}, "≡", "14"},                                 // reading: cyan
	{[]string{"Grep", "Glob"}, "⌕", "14"},                               // searching: cyan
	{[]string{"WebFetch", "WebSearch"}, "↗", "12"},                      // web: blue
	{[]string{"Task", "Agent"}, "◆", "10"},                              // subagents: green
}

// action renders a "Tool: detail" line from the agent, or one of crew's own steps
// ("setting up worktree…"), with an icon and colour for its kind.
func action(text string) string {
	name, detail, ok := strings.Cut(text, ": ")
	if !ok && !strings.ContainsAny(text, " ") {
		name, detail = text, "" // a tool call with no detail
	} else if !ok || strings.Contains(name, " ") {
		return green.UnsetBold().Render("● " + text) // crew's own step
	}
	icon, color := "▸", lipgloss.Color("8")
	for _, st := range actionStyles {
		for _, tool := range st.tools {
			if strings.HasPrefix(name, tool) {
				icon, color = st.icon, st.color
			}
		}
	}
	c := lipgloss.NewStyle().Foreground(color)
	return c.Render(icon+" ") + c.Bold(true).Render(name) + c.Render("  "+detail)
}

// contextLabel shows how much conversation each message re-sends, turning yellow then red as it
// grows so you know when /compact would save tokens.
func contextLabel(n int) string {
	s := "ctx " + tokens(n)
	switch {
	case n >= 150_000:
		return warn.Render(s + " · /compact")
	case n >= 80_000:
		return notice.Render(s)
	default:
		return dim.Render(s)
	}
}
