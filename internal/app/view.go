package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	taskssec "crew/internal/sections/tasks"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/files"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
)

var (
	// The shared styles (look); short names because they're everywhere.
	dim    = look.Dim
	bold   = look.Bold
	accent = look.Accent
	sel    = look.Sel
	warn   = look.Warn
	notice = look.Notice
	green  = look.Green
	icons  = tasks.Icons // how each status reads (the tasks package)
	tint   = tasks.Tint
	// Agent activity, shown over the status icon while it applies.
	running = look.Running
	ready   = look.Ready
	plural  = look.Plural
	// tokens formats a count compactly: 950, 12.3k, 1.2M.
	tokens = look.Tokens
	claude = look.Claude
	wrap   = look.Wrap
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
	mainFocused := f == "chat" || f == "overview" || f == "pr" || f == "home" || m.modal != noModal

	var inner string
	if m.modal != noModal {
		inner = lipgloss.Place(cw, bodyH-2, lipgloss.Center, lipgloss.Center, m.modalView())
	} else {
		content := lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), " ", m.scrollbar())
		if m.tab == tabFiles && m.current() != nil {
			ft := m.filesOf(*m.current())
			if ft.Editing() {
				content = ft.EditorView(m.editFocus)
			}
			content = lipgloss.JoinHorizontal(lipgloss.Top, ft.TreeView(m.treeWidth(), m.vp.Height), content)
		}
		bottom := dim.Render(strings.Repeat("─", cw)) + "\n" + m.inputView()
		if m.boxedInput() { // the message box, framed like Claude Code's prompt
			border := lipgloss.Color("240")
			if f == "chat" {
				border = accent
			}
			bottom = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(cw - 2).Render(m.inputView())
		}
		top := m.header(cw) + "\n" + m.tabs(cw) + "\n"
		if m.current() != nil {
			bar, _ := m.taskBar(cw)
			top += bar + "\n"
		}
		inner = top + content + "\n" + bottom
	}
	title := "home"
	switch r := m.selRow(); {
	case m.current() != nil:
		title = strings.ToLower(tabNames[m.tab]) + " · " + chat.AgentName(*m.current())
	case m.owner(r) != nil && m.owner(r).Frame(&m, r) != "":
		title = m.owner(r).Frame(&m, r)
	case r.Kind == rowGroup:
		title = strings.ToLower(r.Label)
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
	return keys.Names(s)
}

// inputArea is the bottom of the chat frame before keyNames.
func (m Model) inputArea() string {
	if s, r, ok := m.page(); ok {
		if h := s.Hint(&m, r); h != "" {
			return h
		}
	}
	switch {
	case m.onGroupRow():
		if m.listFocus {
			return hint("  ⏎ or a click moves into this table")
		}
		return hint("  " + m.groupTable(m.selRow()).Hint)
	case m.current() == nil:
		return hint("  n new task · t/p/r turn TASKS, PULL REQUESTS, REQUESTS on or off · d setup (install, sign in) · ↑↓ scroll · esc the sidebar")
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
		return hint("  ⇧← / ⇧→ to come back to the chat")
	}
	ask, _ := m.askView(m.chatWidth() - 4)
	return ask + m.suggestionsView() + m.input.View()
}

// helpLine lists this screen's keys, then the shortcuts that work everywhere (from keyCommands).
func (m Model) helpLine() string {
	lead := ""
	for _, s := range m.secs {
		if st := s.Status(&m); st != "" {
			lead += st + "  " // what must stay in sight, like a reminder that fired
		}
	}
	if m.notice != "" {
		return lead + notice.Render(keys.Names(m.notice))
	}
	if b := m.setupBanner(); b != "" {
		return lead + warn.Render(keys.Names(b)) + "  " + look.KeyHints(keys.Names(m.helpKeys()))
	}
	return lead + look.KeyHints(keys.Names(m.helpKeys()))
}

// helpKeys is the help line's keys for the screen you're on, as plain text (helpLine colours it).
func (m Model) helpKeys() string {
	screen := "↑↓ switch"
	switch {
	case m.listFocus:
		screen = "↑↓ choose  ⏎ into it  ← up to its section  esc back"
	case m.shellActive():
		screen = fmt.Sprintf("shell %d has the keys  ⇧← chat  ⇧↑/⇧↓ other shell  ctrl+g next  click to focus", m.shellFocus+1)
	case m.editActive():
		return files.EditorKeys // crew's own shortcuts don't apply while editing
	case m.tab == tabFiles && m.current() != nil && m.filesOf(*m.current()).Finding():
		screen = "type to find a file  ↑↓ choose  ⏎ open  esc close"
	case m.tab == tabFiles && m.current() != nil:
		screen = "↑↓ move  ←/→ fold/unfold  ⏎ open folder or edit  / find  c changed only  ⇧→ editor  ctrl+e VS Code  pgup/pgdn scroll file  tab next tab  esc chat  r refresh"
	case m.modal == prModal && m.prv.Typing(m.prKey()):
		screen = "⏎ send  alt+⏎ or \\⏎ new line  esc cancel"
	case m.modal == prModal:
		screen = "↑↓ pgup/pgdn scroll  c ask Claude  s fix it  r reply  x resolve  g code  o GitHub  n next open  esc close"
	case m.tab == tabPlan && m.current() != nil:
		screen = "↑↓ pick  ⏎ answer a question  s work on the step  n next step  x change its mark  e edit the plan  r refresh  tab next tab  esc chat"
		if m.planOf(m.currentID()).answering > 0 {
			screen = "⏎ send the answer  a, b, … picks an option  esc cancel"
		}
	case m.tab == tabPR && m.current() != nil:
		screen = "↑↓ pick  ←/→ fold/unfold  ⏎ open or fold  t to do only  c ask Claude  s fix it  r reply  x resolve  n next open  a fix all  o GitHub  P create PR  R refresh  tab next tab  esc chat"
	case m.tab != tabChat && m.current() != nil:
		screen = "tab next tab  esc back to chat  ↑↓ pgup/pgdn home/end scroll  r refresh"
	case m.chatTask() != nil:
		send := "⏎ send"
		if m.chat.Conv(m.currentID()).Busy {
			send = "⏎ send now (stops the current step)  esc stop"
		}
		arrows, back := "tab tabs", "esc all tasks"
		if _, _, ok := m.page(); ok {
			arrows, back = "tab panes", "esc back"
		}
		screen = send + "  ↑↓ scroll  " + arrows + "  ⌘⌥←/→ panels  @ file  " + strings.ReplaceAll(m.slashNames(), ",", "") +
			"  alt+⏎ or \\⏎ new line  pgup/pgdn page  " + back
	}
	// ponytail: ⌘⌥T is named as bound by default; read the bindings if people start rebinding it.
	return screen + "  ⌘⌥T next panel  ⌘⇧H home  ⌘⇧P all commands  " + m.keyHelp()
}

func (m Model) modalView() string {
	if m.modal == prModal {
		return m.prv.PopupView(&m)
	}
	if m.modal == paletteModal {
		return m.paletteView()
	}
	if m.modal == statusModal {
		return m.statusMenu.View()
	}
	if m.modal == modelModal {
		return m.modelMenuView()
	}
	if m.modal == setupModal {
		return m.setupView()
	}
	if m.modal == secModal {
		if s := m.modalSec(); s != nil {
			return s.ModalView(&m)
		}
	}
	if m.modal == quitModal {
		return m.quitView()
	}
	if t := m.find(m.delID); m.modal == deleteModal && t != nil {
		return taskssec.DeleteView(*t)
	}
	return ""
}

// tabs is the second header line: the Chat / Changes / Usage tab bar for a task, a plain rule on home.
func (m Model) tabs(w int) string {
	if r := m.selRow(); m.owner(r) != nil {
		if t := m.owner(r).Tabs(&m, r, w); t != "" {
			return t
		}
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
			bar = append(bar, active.Render(tabLabel(name, i)))
		} else {
			bar = append(bar, inactive.Render(tabLabel(name, i)))
		}
	}
	extra := "tab switches"
	switch {
	case m.tab == tabFiles && m.filesOf(*m.current()).OnlyChanged():
		extra = "changed files only · " + m.filesOf(*m.current()).File()
	case m.tab == tabFiles:
		extra = m.filesOf(*m.current()).File()
	case m.tab == tabChanges && m.diffSummary != "":
		extra = m.diffSummary
	case m.tab == tabUsage:
		extra = "from Claude Code's session log"
	case m.tab == tabPlan:
		extra = m.planSummary()
	case m.tab == tabPR && m.prv.Loading(m.currentID()):
		extra = "loading…"
	case m.tab == tabPR:
		extra = "via gh"
	}
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render("   "+extra), w, "…")
}

func (m Model) header(w int) string {
	busy := ""
	if m.chat.Conv(m.currentID()).Busy {
		busy = running.Render("  ● working…")
	}
	t := m.current()
	if s, r, ok := m.page(); ok {
		return s.Header(&m, r, w)
	}
	if r := m.selRow(); m.onGroupRow() {
		return ansi.Truncate(bold.Render(r.Label)+"  "+dim.Render(m.groupTable(r).Summary), w, "…")
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
	return ansi.Truncate(bold.Render(t.Title)+dim.Render(m.headerMeta(*t))+tasks.Chip(t.Status)+dim.Render(spent)+ctx+busy, w, "…")
}

// refresh re-renders the selected chat into the viewport and scrolls to the latest message.
func (m *Model) refresh() {
	if s, r, ok := m.page(); ok {
		s.Render(m, r)
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
	if m.selRow().Kind != rowHome { // home scrolls with ↑↓; selectRow puts a new row at the top
		m.vp.GotoTop() // the overview pages itself to fit
	}
}

func (m *Model) render() string {
	w := max(m.vp.Width, 20)
	msgs := m.chat.Conv(m.currentID()).Msgs
	t := m.current()
	if len(msgs) == 0 {
		if r := m.selRow(); m.onGroupRow() {
			s, _ := m.overview(r)
			return s
		}
		if t == nil {
			return m.homeView()
		}
		return dim.Render(lipgloss.NewStyle().Width(w).Render(
			"Send a message to start this task. " + chat.AgentName(*t) + " gets the task below plus your message.\n\n" + chat.Brief(*t)))
	}
	if t == nil {
		return m.homeView()
	}
	return m.chat.Render(t.ID, w, m.spin)
}

// headerMeta is the plain text between the task title and its status chip.
func (m Model) headerMeta(t tasks.Task) string {
	model := t.Model
	if model == "" {
		model = m.usage[t.ID].LastModel // what Claude actually answered with
	}
	if model == "" {
		model = chat.AgentName(t)
	}
	return " · " + filepath.Base(t.Repo) + " · " + model + " · "
}

func (m Model) homeView() string { return keys.Names(m.homeText()) }

func (m Model) homeText() string {
	counts := map[string]int{}
	busy, unread, archived := 0, len(m.unread), 0
	for _, t := range m.tasks {
		if t.Archived {
			archived++
			continue
		}
		counts[t.Status]++
		if m.chat.Conv(t.ID).Busy {
			busy++
		}
	}
	var b strings.Builder
	b.WriteString(m.getStarted())
	sections, _ := m.sectionsHome()
	b.WriteString(sections)
	b.WriteString(bold.Render("Welcome to crew") + "\n\n")
	b.WriteString(sel.Render("n     ") + "  in TASKS, create a task: give it a name and pick a repo folder\n")
	b.WriteString(sel.Render("↑↓    ") + "  TASKS, PULL REQUESTS or REQUESTS: pick one from the table on the right; esc comes back\n")
	b.WriteString(sel.Render("s     ") + "  in TASKS (or ⌘S on a task), set its status (todo, in progress, in PR, blocked, done) or archive it; x deletes it\n")
	b.WriteString(sel.Render("⌘F    ") + "  search the tasks or PRs; ⌘⇧↑/⌘⇧↓ the previous or next one\n")
	b.WriteString(sel.Render("TASKS ") + "  the overview of every task: filters, search, what needs you\n")
	b.WriteString(sel.Render("⌘O    ") + "  archived tasks (in the TASKS overview)\n\n")
	b.WriteString(bold.Render("Tasks") + "  " + strconv.Itoa(len(m.tasks)-archived) + " active")
	if archived > 0 {
		b.WriteString(dim.Render(fmt.Sprintf(" · %d archived", archived)))
	}
	b.WriteString("\n  ")
	for _, s := range tasks.Statuses {
		b.WriteString(tint[s].Render(fmt.Sprintf("%s %d %s", icons[s], counts[s], tasks.Labels[s])) + "   ")
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

// cost is what a task's sessions cost, in dollars.
var cost = look.Cost

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
	case m.pageFocus() != "" && !m.listFocus:
		return m.pageFocus()
	case m.selRow().Kind == rowHome && !m.listFocus && m.chatTask() == nil:
		return "home" // the main pane can have the keys on home too, like on any other row
	case m.listFocus || m.current() == nil:
		return "tasks"
	default:
		return "chat"
	}
}

// focusColors tie each area's highlight (task bar, chat marker, shell title) to its badge.
var focusColors = map[string]lipgloss.Color{"tasks": accent, "chat": accent, "overview": accent, "pr": accent, "home": accent, "shell": accent}

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

// hint draws a hint line under the pane: its keys in the accent (as ⌘ keys), the rest dim.
func hint(s string) string { return look.KeyHints(keys.Names(s)) }
