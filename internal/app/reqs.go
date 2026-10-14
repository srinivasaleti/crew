package app

import (
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/requests"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// REQUESTS lives in its own package (requests: the client, its screens and forms). This is crew's
// side of it: the sidebar rows (REQUESTS, the application you opened, the chat), handing the screen
// keys, clicks, messages and its popup, and the section's Claude chat, which is crew's chat.

const reqChatKind = "requests"

// onReqsPage reports whether the main pane is the requests screen: REQUESTS, or an application.
func (m Model) onReqsPage() bool {
	r := m.selRow()
	return r.Kind == rowGroup && r.Section == "reqs" || r.Kind == rowApp
}

// showReqs points the screen at what the sidebar has selected.
func (m Model) showReqs() *requests.Screen {
	app := ""
	if r := m.selRow(); r.Kind == rowApp {
		app = r.Label
	}
	m.reqv.Show(app)
	m.reqv.Spin = m.spin
	return m.reqv
}

// afterReqs takes what the screen asks for after a key or click: its notice, its popup, opening an
// application, going back, or the chat.
func (m *Model) afterReqs(ev requests.Event) {
	if n := m.reqv.Notice; n != "" {
		m.notice, m.reqv.Notice = n, ""
	}
	switch {
	case m.reqv.Popup() != nil:
		m.modal = reqsModal
		m.input.Blur()
	case m.modal == reqsModal:
		m.modal = noModal
	}
	switch {
	case ev.Open != "":
		m.selKey = "a:" + ev.Open
		m.onSelect()
		m.focusOn(areaMain, -1)
	case ev.Back:
		m.goUp()
	case ev.Chat:
		m.openReqChat(ev.About)
	}
	m.refresh()
}

// updateReqsPage hands a key to the requests screen.
func (m Model) updateReqsPage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, ev, _ := m.showReqs().Key(msg)
	m.afterReqs(ev)
	return m, tea.Batch(cmd, m.reqsSpin())
}

// updateReqsPopup hands a key to the screen's popup.
func (m Model) updateReqsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if p := m.reqv.Popup(); p != nil {
		cmd = p.Key(msg)
	}
	m.afterReqs(requests.Event{})
	return m, tea.Batch(cmd, m.reqsSpin())
}

// reqsSpin starts the spinner while a request is being sent.
func (m *Model) reqsSpin() tea.Cmd {
	if m.reqv.Busy() {
		return m.startSpin()
	}
	return nil
}

// reqsPopupView draws the popup to fit the main pane.
func (m Model) reqsPopupView() string {
	p := m.reqv.Popup()
	if p == nil {
		return ""
	}
	m.reqv.Spin = m.spin
	return p.View(min(max(m.chatWidth()-4, 58), 104), m.bodyHeight()-2)
}

// clickReqsPopup handles a click on the popup (x, y on the screen).
func (m *Model) clickReqsPopup(x, y int) {
	box := m.reqsPopupView()
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	left, top := m.modalOrigin(bw, bh)
	if p := m.reqv.Popup(); p != nil && x >= left && x < left+bw && y >= top && y < top+bh {
		m.pending = tea.Batch(p.Click(x-left, y-top), m.reqsSpin())
		m.afterReqs(requests.Event{})
	}
}

// clickReqsPage handles a click on the requests screen (x, y inside the main pane's frame).
func (m *Model) clickReqsPage(x, y int) {
	s := m.showReqs()
	switch {
	case y == 2:
		s.ClickTabs(x)
	case y >= m.contentY():
		cmd, ev := s.Click(x, y-m.contentY()+m.vp.YOffset)
		m.pending = tea.Batch(cmd, m.reqsSpin())
		m.afterReqs(ev)
		return
	}
	m.refresh()
}

// renderReqsPage fills the main pane with the requests screen.
func (m *Model) renderReqsPage() {
	m.vp.SetContent(m.showReqs().Body(m.vp.Width, m.vp.Height, m.focus() == "overview"))
	m.vp.GotoTop() // the screen keeps its cursor in view itself
}

// reqAppLines draws an application's two sidebar lines.
func (m Model) reqAppLines(r row, selected bool) string {
	return m.itemLines(r, sel.Render("◆"), r.Label, m.reqv.AppLine(r.Label), selected)
}

// itemLines draws an item's two sidebar lines: mark and title, then what's going on with it.
func (m Model) itemLines(r row, mark, title, second string, selected bool) string {
	w, indent := m.sidebarWidth(), strings.Repeat("  ", r.Depth)
	title = ansi.Truncate(title, w-5-len(indent)-ansi.StringWidth(mark), "…")
	second = ansi.Truncate(second, w-4-len(indent), "…")
	pad := "    " + indent
	switch {
	case selected && m.listFocus:
		bar := focusBar.Width(w)
		return bar.Render("▶ "+indent+ansi.Strip(mark)+" "+title) + "\n" + bar.UnsetBold().Render(pad+second)
	case selected:
		return sel.Render("▶ ") + indent + mark + " " + bold.Render(title) + "\n" + pad + dim.Render(second)
	}
	return "  " + indent + mark + " " + title + "\n" + pad + dim.Render(second)
}

// reqRows are the section's rows: REQUESTS, then the application you opened (or the chat).
func (m Model) reqRows() []row {
	if !m.sectionOn("reqs") {
		return nil
	}
	rows := []row{{Kind: rowGroup, Section: "reqs", Label: "REQUESTS", Count: len(m.reqv.Apps())}}
	switch app := strings.TrimPrefix(m.selKey, "a:"); {
	case m.selKey == "q:":
		rows = append(rows, row{Kind: rowReq, Section: "reqs", Depth: 1, Index: -1})
	case strings.HasPrefix(m.selKey, "a:") && slices.Contains(m.reqv.Apps(), app):
		rows = append(rows, row{Kind: rowApp, Section: "reqs", Depth: 1, Label: app})
	}
	return rows
}

// ---- the chat ----

// reqItem reports whether the REQUESTS chat (the sidebar's "Claude" row) is selected.
func (m Model) reqItem() (req *requests.Request, ok bool) { return nil, m.selRow().Kind == rowReq }

// onReqChat reports whether the REQUESTS chat is on screen.
func (m Model) onReqChat() bool { return m.selRow().Kind == rowReq }

// reqChat is the REQUESTS chat, or nil before it's first opened.
func (m Model) reqChat() *tasks.Task {
	for i := range m.chats {
		if m.chats[i].Kind == reqChatKind {
			return &m.chats[i]
		}
	}
	return nil
}

func (m *Model) ensureReqChat() *tasks.Task {
	if t := m.reqChat(); t != nil {
		return t
	}
	m.chats = append(m.chats, tasks.Task{ID: "_requests", Title: "Requests", Kind: reqChatKind, Status: tasks.InProgress,
		Created: time.Now(), Body: requests.ChatBrief(section.CrewExe())})
	return &m.chats[len(m.chats)-1]
}

// openReqChat opens the REQUESTS chat; about a request, its id starts the message.
func (m *Model) openReqChat(about string) {
	m.ensureReqChat()
	m.selKey = "q:"
	m.onSelect()
	m.focusOn(areaMain, -1)
	if about != "" && m.input.Value() == "" {
		m.input.SetValue("About " + about + ": ")
		m.input.CursorEnd()
	}
}

// reqChatArgs lock the REQUESTS chat's Claude down (requests.ChatArgs).
func (m Model) reqChatArgs() []string {
	plugin := ""
	if m.pluginOK() {
		plugin = m.cfg.Plugin
	}
	return requests.ChatArgs(section.CrewExe(), plugin)
}

// startReqChat runs the chat's first turn in the requests folder.
func (m *Model) startReqChat(t tasks.Task, prompt string) tea.Cmd {
	dir := m.cfg.Reqs
	return func() tea.Msg {
		return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
	}
}

// reqTurnEnded reloads the requests after the chat's turn: Claude may have written some.
func (m *Model) reqTurnEnded(id string) {
	if t := m.find(id); t != nil && t.Kind == reqChatKind {
		m.reqv.Load()
	}
}

// reqHeader is the main pane's first line on the REQUESTS chat.
func (m Model) reqHeader(w int) string {
	return ansi.Truncate(bold.Render("Claude")+dim.Render(" · requests")+m.chatMeta(m.reqChat()), w, "…")
}

// reqTabs is the tab bar on the REQUESTS chat.
func (m Model) reqTabs(w int) string {
	active := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	return ansi.Truncate(active.Render("Chat")+dim.Render("   Claude writes requests and sends them with crew req; keys stay in crew"), w, "…")
}

// renderReqRow fills the main pane with the REQUESTS chat.
func (m *Model) renderReqRow() {
	w := max(m.vp.Width, 20)
	follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
	t := m.reqChat()
	if t == nil || len(m.chat.Conv(t.ID).Msgs) == 0 {
		m.vp.SetContent(dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude for requests: \"list the orgs in accounts\", " +
			"\"create an org then fetch it\", \"why does this 401 on staging?\". Claude writes them in your applications and " +
			"sends them with crew req. It never sees your keys: they stay in the Keychain, go only to the environment's own " +
			"hosts, and come back hidden.\n\n/compact, /new and /model work as in a task.")))
	} else {
		m.vp.SetContent(m.chat.Render(t.ID, w, m.spin))
	}
	if follow {
		m.vp.GotoBottom()
	}
}

// reqHint is the message box under the REQUESTS chat.
func (m Model) reqHint() string { return m.suggestionsView() + m.input.View() }

// updateReqRow hands the REQUESTS chat's keys to the chat.
func (m Model) updateReqRow(msg tea.KeyMsg) (tea.Model, tea.Cmd) { return m.updateChat(msg) }
