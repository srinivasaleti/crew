package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/task"
)

var (
	dim  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold = lipgloss.NewStyle().Bold(true)
	// accent is Claude's coral: the focused frame, selections, markers and the working spinner.
	accent = lipgloss.Color("#D97757")
	sel    = lipgloss.NewStyle().Foreground(accent).Bold(true)
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

func (m Model) View() string { return m.bordersOSC() + m.highlightSel(m.screen()) } // first, so a cut line can't drop it

// bordersOSC tells the app (desktop/) which columns are draggable borders, so it can show a
// resize cursor over them: OSC 7777 ; borders=x,x… BEL. It takes no room on screen, and
// terminals ignore it.
func (m Model) bordersOSC() string {
	var cols []string
	if !m.zoom {
		cols = append(cols, fmt.Sprint(m.sideOuter()-1), fmt.Sprint(m.sideOuter()))
	}
	if m.panelWidth() > 0 {
		b := m.sideOuter() + m.chatWidth() + 1
		cols = append(cols, fmt.Sprint(b), fmt.Sprint(b+1))
	}
	return "\x1b]7777;borders=" + strings.Join(cols, ",") + "\x07"
}

// screen draws the whole window (View adds the mouse selection on top).
func (m Model) screen() string {
	bodyH, f, cw := m.bodyHeight(), m.focus(), m.chatWidth()
	side := ""
	if !m.zoom {
		side = frame(m.sidebar(), m.sideOuter(), bodyH, "crew", f == "tasks")
	}
	mainFocused := f == "chat" || f == "overview" || f == "pr" || m.modal != noModal

	var inner string
	if m.modal != noModal {
		inner = lipgloss.Place(cw, bodyH-2, lipgloss.Center, lipgloss.Center, m.modalView())
	} else {
		content := lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), " ", m.scrollbar())
		if m.tab == tabFiles && m.current() != nil {
			if ed := m.editors[m.currentID()]; ed != nil {
				content = ed.view(m.editFocus)
			}
			content = lipgloss.JoinHorizontal(lipgloss.Top, m.treeView(), content)
		}
		bottom := dim.Render(strings.Repeat("─", cw)) + "\n" + m.inputView()
		if m.boxedInput() { // the message box, framed like Claude Code's prompt
			border := lipgloss.Color("240")
			if f == "chat" {
				border = accent
			}
			bottom = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(cw - 2).Render(m.inputView())
		}
		inner = m.header(cw) + "\n" + m.tabs(cw) + "\n" + content + "\n" + bottom
	}
	title := "home"
	switch r := m.selRow(); {
	case m.current() != nil:
		title = strings.ToLower(tabNames[m.tab]) + " · " + agentName(*m.current())
	case r.kind == rowGroup:
		title = strings.ToLower(r.label)
	case r.kind == rowPR:
		title = "pull request"
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, side, frame(inner, cw+2, bodyH, title, mainFocused))
	if m.panelWidth() > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.shellPanel())
	}
	return body + "\n" + m.focusBadge() + " " + m.helpLine()
}

// inputView is the bottom of the chat frame: the message box, or a hint when it doesn't apply.
func (m Model) inputView() string {
	s := m.inputArea()
	if m.input.Focused() && strings.HasSuffix(s, m.input.View()) {
		return s // what you're typing stays as you typed it
	}
	return keyNames(s)
}

// inputArea is the bottom of the chat frame before keyNames.
func (m Model) inputArea() string {
	switch {
	case m.selPR() != nil:
		return m.prRowHint()
	case m.onGroupRow():
		if m.listFocus {
			return dim.Render("  → or a click moves into this table")
		}
		return dim.Render("  ↑↓ pick a task · ←/→ filter · ⏎ opens it (esc comes back) · p its PR · t its to-do · / search")
	case m.current() == nil:
		return dim.Render("  Press ctrl+n to create a task, or open TASKS with ↑↓ and pick one")
	case m.tab == tabFiles:
		return dim.Render("  Browsing files in " + tildify(m.taskDir(*m.current())))
	case m.tab == tabChanges:
		return dim.Render("  Viewing changes in " + tildify(m.current().Worktree))
	case m.tab == tabUsage:
		return dim.Render("  Viewing usage for " + m.current().Title)
	case m.tab == tabPlan:
		return m.planInputView()
	case m.tab == tabPR:
		return dim.Render("  ● to do  ✔ resolved  ◐ outdated  · comment · ⏎ or click opens a row · t to do only · n next to do")
	case m.focus() != "chat" && m.input.Value() == "":
		return dim.Render("  ⇧← / ⇧→ to come back to the chat")
	}
	return m.suggestionsView() + m.input.View()
}

// helpLine lists this screen's keys, then the shortcuts that work everywhere (from keyCommands).
func (m Model) helpLine() string {
	if m.notice != "" {
		return notice.Render(m.notice)
	}
	return keyNames(m.helpKeys())
}

// helpKeys is the help line's keys for the screen you're on.
func (m Model) helpKeys() string {
	screen := "↑↓ switch"
	switch {
	case m.listFocus:
		screen = "↑↓ choose  →/⏎ into it  esc back"
	case m.shellActive():
		screen = fmt.Sprintf("shell %d has the keys  ⇧← chat  ⇧↑/⇧↓ other shell  ctrl+g next  click to focus", m.shellFocus+1)
	case m.editActive():
		return dim.Render(editorKeys) // crew's own shortcuts don't apply while editing
	case m.tab == tabFiles && m.current() != nil && m.tree().finder:
		screen = "type to find a file  ↑↓ choose  ⏎ open  esc close"
	case m.tab == tabFiles && m.current() != nil:
		screen = "↑↓ move  ⏎/space open folder  / find  c changed only  ⏎ or e edit  ⇧→ editor  ctrl+e VS Code  pgup/pgdn scroll file  ←/→ switch tab  esc chat  r refresh"
	case m.modal == prModal && m.prOf(m.currentID()).input != "":
		screen = "⏎ send  alt+⏎ or \\⏎ new line  esc cancel"
	case m.modal == prModal:
		screen = "↑↓ pgup/pgdn scroll  c ask Claude  s fix it  r reply  x resolve  g code  o GitHub  n next open  esc close"
	case m.tab == tabPlan && m.current() != nil:
		screen = "↑↓ pick  ⏎ answer a question  s work on the step  n next step  x change its mark  e edit the plan  r refresh  ←/→ switch tab  esc chat"
		if m.planOf(m.currentID()).answering > 0 {
			screen = "⏎ send the answer  a, b, … picks an option  esc cancel"
		}
	case m.tab == tabPR && m.current() != nil:
		screen = "↑↓ pick  ⏎ open or fold  t to do only  c ask Claude  s fix it  r reply  x resolve  n next open  a fix all  o GitHub  P create PR  R refresh  ←/→ switch tab  esc chat"
	case m.tab != tabChat && m.current() != nil:
		screen = "←/→ switch tab  esc back to chat  ↑↓ pgup/pgdn home/end scroll  r refresh"
	case m.current() != nil:
		send := "⏎ send"
		if m.conv(m.currentID()).busy {
			send = "⏎ send now (stops the current step)  esc stop"
		}
		screen = send + "  ↑↓ scroll  ←/→ tabs  ⌘⌥←/→ panels  @ file  " + strings.ReplaceAll(slashNames(), ",", "") +
			"  alt+⏎ or \\⏎ new line  pgup/pgdn page  esc all tasks"
	}
	return dim.Render(screen + "  ⌘⇧P all commands  " + m.keyHelp())
}

func (m Model) modalView() string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Width(60)
	if m.modal == prModal {
		return m.popupView()
	}
	if m.modal == paletteModal {
		return m.paletteView()
	}
	if m.modal == statusModal {
		return m.statusMenuView()
	}
	if m.modal == modelModal {
		return m.modelMenuView()
	}
	if m.modal == quitModal {
		return m.quitView()
	}
	if m.modal == deleteModal {
		t := m.current()
		if t == nil {
			return ""
		}
		keep := "Its chat history and plan are removed."
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
	repos := ""
	for _, r := range m.formRepos() {
		mark, st := "  ", dim
		if len(m.recentRepos()) > 0 && m.repoSel >= 0 && r == m.recentRepos()[m.repoSel] {
			mark, st = sel.Render("▶ "), lipgloss.NewStyle()
		}
		repos += "\n" + mark + st.Render(ansi.Truncate(tildify(r), 54, "…"))
	}
	if repos != "" {
		repos = "\n" + dim.Render("recent repos · ↑↓ to pick") + repos
	}
	return box.Width(64).Render(bold.Render("New task") + "\n\n" +
		label(0, "Name") + "\n" + m.name.View() + "\n\n" +
		label(1, "Repo") + dim.Render("  any git repo on this machine") + "\n" + m.folder.View() + repos + errLine + "\n\n" +
		label(2, "Description") + "\n" + m.desc.View() + "\n\n" +
		dim.Render("⏎ next · tab completes a folder · ⏎ on the description creates · esc cancel"))
}

// tabs is the second header line: the Chat / Changes / Usage tab bar for a task, a plain rule on home.
func (m Model) tabs(w int) string {
	if m.selPR() != nil {
		return m.prPaneTabs(w)
	}
	if m.current() == nil {
		return dim.Render(strings.Repeat("─", w))
	}
	// Same highlight bar as the focused task in the sidebar.
	active := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	inactive := dim.Padding(0, 1)
	var bar []string
	for i, name := range tabNames {
		if tab(i) == m.tab {
			bar = append(bar, active.Render(name))
		} else {
			bar = append(bar, inactive.Render(name))
		}
	}
	extra := "←/→ to switch"
	switch {
	case m.tab == tabFiles && m.tree().onlyChanged:
		extra = "changed files only · " + m.tree().file
	case m.tab == tabFiles:
		extra = m.tree().file
	case m.tab == tabChanges && m.diffSummary != "":
		extra = m.diffSummary
	case m.tab == tabUsage:
		extra = "from Claude Code's session log"
	case m.tab == tabPlan:
		extra = m.planSummary()
	case m.tab == tabPR && m.prOf(m.currentID()).loading:
		extra = "loading…"
	case m.tab == tabPR:
		extra = "via gh"
	}
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render("   "+extra), w, "…")
}

func (m Model) header(w int) string {
	busy := ""
	if m.conv(m.currentID()).busy {
		busy = running.Render("  ● working…")
	}
	t := m.current()
	if it := m.selPR(); it != nil {
		return m.prHeader(*it, w)
	}
	if r := m.selRow(); r.kind == rowGroup {
		return ansi.Truncate(bold.Render(r.label)+"  "+dim.Render(m.groupTable(r).summary), w, "…")
	}
	if t == nil {
		return bold.Render("crew") + dim.Render("  home")
	}
	spent, ctx := "", ""
	if u, ok := m.usage[t.ID]; ok && u.Cost > 0 {
		spent = " · " + cost(u.Cost) + " · "
		ctx = contextLabel(u.Context)
	}
	// The status chip is clickable; click() finds it at the width of title + headerMeta.
	if p := m.planProgress(t.ID); p != "" {
		spent = " · plan " + p + spent
	}
	return ansi.Truncate(bold.Render(t.Title)+dim.Render(m.headerMeta(*t))+statusChip(t.Status)+dim.Render(spent)+ctx+busy, w, "…")
}

// refresh re-renders the selected chat into the viewport and scrolls to the latest message.
func (m *Model) refresh() {
	if m.selPR() != nil {
		m.renderPRRow()
		return
	}
	if m.tab != tabChat {
		return // the Changes/Usage tab owns the viewport until you return to Chat
	}
	follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
	m.vp.SetContent(m.render())
	if m.current() != nil {
		if follow { // a chat follows new output unless you've scrolled up to read
			m.vp.GotoBottom()
		}
		return
	}
	m.vp.GotoTop() // the overview pages itself to fit
}

func (m *Model) render() string {
	w := max(m.vp.Width, 20)
	msgs := m.conv(m.currentID()).msgs
	t := m.current()
	if len(msgs) == 0 {
		if r := m.selRow(); r.kind == rowGroup {
			s, _ := m.overview(r)
			return s
		}
		if t == nil {
			return m.homeView()
		}
		return dim.Render(lipgloss.NewStyle().Width(w).Render(
			"Send a message to start this task. " + agentName(*t) + " gets the task below plus your message.\n\n" + brief(*t)))
	}
	if t == nil {
		return m.homeView()
	}
	return m.renderChat(msgs, w, agentName(*t), m.conv(t.ID).busy)
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

func (m Model) homeView() string { return keyNames(m.homeText()) }

func (m Model) homeText() string {
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
	b.WriteString(sel.Render("↑↓    ") + "  TASKS or PULL REQUESTS: pick one from the table on the right; esc comes back\n")
	b.WriteString(sel.Render("ctrl+s") + "  set a task's status (todo, in progress, in PR, blocked, done) or archive it\n")
	b.WriteString(sel.Render("ctrl+f") + "  search the tasks or PRs; ⌘⇧↑/⌘⇧↓ the previous or next one\n")
	b.WriteString(sel.Render("TASKS ") + "  the overview of every task: filters, search, what needs you\n")
	b.WriteString(sel.Render("ctrl+o") + "  archived tasks (in the TASKS overview)\n\n")
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

// contextLabel shows how much conversation each message re-sends, turning yellow then red as it
// grows so you know when /compact would save tokens.
func contextLabel(n int) string {
	s := "ctx " + tokens(n)
	switch {
	case n >= 150_000:
		return warn.Render(s + " · /compact or /new")
	case n >= 100_000:
		return notice.Render(s + " · /compact")
	default:
		return dim.Render(s)
	}
}

// focus names the area that has the keys: "tasks", "chat", "overview", "pr" or "shell".
func (m Model) focus() string {
	switch {
	case m.shellActive():
		return "shell"
	case m.current() == nil && m.onGroupRow() && !m.listFocus:
		return "overview"
	case m.selPR() != nil && !m.listFocus:
		return "pr"
	case m.listFocus || m.current() == nil:
		return "tasks"
	default:
		return "chat"
	}
}

// focusColors tie each area's highlight (task bar, chat marker, shell title) to its badge.
var focusColors = map[string]lipgloss.Color{"tasks": accent, "chat": accent, "overview": accent, "pr": accent, "shell": accent}

// focusBadge starts the bottom line with the focused area's name, so you always know where keys go.
func (m Model) focusBadge() string {
	f := m.focus()
	label, color := strings.ToUpper(f), focusColors[f]
	switch {
	case f == "shell":
		label = fmt.Sprintf("SHELL %d", m.shellFocus+1)
	case f == "chat":
		if m.tab != tabChat {
			label = strings.ToUpper(tabNames[m.tab])
		}
	}
	return lipgloss.NewStyle().Background(color).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render(label)
}
