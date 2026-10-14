package requests

import (
	"context"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
)

// The REQUESTS section, as crew's plugin: its rows (REQUESTS, the application you opened, the
// chat), handing the screen keys, clicks, messages and its popup, and the section's Claude chat,
// which is crew's chat.

// chatKind marks the REQUESTS chat in its task file.
const chatKind = "requests"

// The kinds of REQUESTS' rows under its own.
const (
	rowChat = "req" // the REQUESTS chat (the sidebar's "Claude" row)
	rowApp  = "app" // an application (Label is its name)
)

// Section is REQUESTS.
type Section struct {
	section.Base
	dir    string  // the requests folder: applications, and where the chat runs
	screen *Screen // the list of applications, or the one that's open
	popup  bool    // the screen's popup is crew's open modal
}

// NewSection makes REQUESTS from its folder.
func NewSection(ctx context.Context, dir string) *Section {
	return &Section{dir: dir, screen: New(ctx, dir)}
}

// Def is REQUESTS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "reqs", Label: "REQUESTS", Letter: "r", About: "an HTTP client for your APIs, with a locked-down Claude chat"}
}

// Reload re-reads the applications.
func (s *Section) Reload(section.Host) { s.screen.Load() }

// show points the screen at what the sidebar has selected.
func (s *Section) show(h section.Host) *Screen {
	app := ""
	if r := h.SelectedRow(); r.Kind == rowApp {
		app = r.Label
	}
	s.screen.Show(app)
	s.screen.Spin = h.SpinFrame()
	return s.screen
}

// onScreen reports whether a row shows the requests screen: REQUESTS, or an application.
func onScreen(r section.Row) bool { return r.Kind == section.Group || r.Kind == rowApp }

// after takes what the screen asks for after a key or click: its notice, its popup, opening an
// application, going back, or the chat.
func (s *Section) after(h section.Host, ev Event) {
	if n := s.screen.Notice; n != "" {
		s.screen.Notice = ""
		h.Notice(n)
	}
	switch {
	case s.screen.Popup() != nil:
		s.popup = true
		h.SectionModal(s)
		h.Input().Blur()
	case s.popup:
		s.popup = false
		h.SectionModal(nil)
	}
	switch {
	case ev.Open != "":
		h.Select("a:" + ev.Open)
		h.FocusMain()
	case ev.Back:
		h.GoUp()
	case ev.Chat:
		s.openChat(h, ev.About)
	}
	h.Refresh()
}

// spin starts the spinner while a request is being sent.
func (s *Section) spin(h section.Host) tea.Cmd {
	if s.screen.Busy() {
		return h.Spin()
	}
	return nil
}

// Rows are REQUESTS, then the application you opened (or the chat).
func (s *Section) Rows(h section.Host) []section.Row {
	rows := []section.Row{{Kind: section.Group, Section: "reqs", Label: "REQUESTS", Count: len(s.screen.Apps())}}
	switch key := h.SelKey(); {
	case key == "q:":
		rows = append(rows, section.Row{Kind: rowChat, Section: "reqs", Key: "q:", Depth: 1, Index: -1})
	case strings.HasPrefix(key, "a:") && slices.Contains(s.screen.Apps(), key[2:]):
		rows = append(rows, section.Row{Kind: rowApp, Section: "reqs", Key: key, Depth: 1, Label: key[2:]})
	}
	return rows
}

// Title is REQUESTS' row, marked while its chat works or has a reply ready.
func (s *Section) Title(h section.Host, r section.Row) (string, string) {
	t := s.chat(h)
	switch {
	case t == nil:
	case h.Chats().Conv(t.ID).Busy:
		return section.CountTitle(r), look.Running.Render("●")
	case h.Unread(t.ID):
		return section.CountTitle(r), look.Ready.Render("◐")
	}
	return section.CountTitle(r), ""
}

// Lines draws an application's or the chat's two sidebar lines.
func (s *Section) Lines(h section.Host, r section.Row, selected bool) string {
	if r.Kind == rowChat {
		return h.ItemLines(r, look.Claude.Render("✦"), "Claude", "makes and sends requests", selected)
	}
	return h.ItemLines(r, look.Sel.Render("◆"), r.Label, s.screen.AppLine(r.Label), selected)
}

// ListKey lets the screen's letters act on it from the sidebar too.
func (s *Section) ListKey(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if !onScreen(r) || msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || !strings.ContainsRune("nfxmsEtc", msg.Runes[0]) {
		return nil, false
	}
	h.FocusMain()
	return s.Key(h, r, msg)
}

// Spin redraws a request's "sending…" in its row while one is sent; the popup redraws on its own.
func (s *Section) Spin(h section.Host) bool {
	busy := s.screen.Busy()
	if r := h.SelectedRow(); busy && r.Section == "reqs" && onScreen(r) {
		h.Refresh()
	}
	return busy
}

// Update takes a request's send coming back.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	done, ok := msg.(DoneMsg)
	if !ok {
		return nil, false
	}
	s.screen.Done(done)
	h.Refresh()
	return nil, true
}

// Frame is the main pane's title on an application or the chat.
func (s *Section) Frame(_ section.Host, r section.Row) string {
	switch r.Kind {
	case rowChat:
		return "requests chat"
	case rowApp:
		return "application"
	}
	return ""
}

// Header is the main pane's first line.
func (s *Section) Header(h section.Host, r section.Row, w int) string {
	if r.Kind == rowChat {
		return ansi.Truncate(look.Bold.Render("Claude")+look.Dim.Render(" · requests")+h.ChatMeta(s.chat(h)), w, "…")
	}
	return s.show(h).Header(w)
}

// Tabs is the tab bar: an application's tabs, or the chat's one.
func (s *Section) Tabs(h section.Host, r section.Row, w int) string {
	if r.Kind == rowChat {
		active := lipgloss.NewStyle().Background(look.Accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
		return ansi.Truncate(active.Render("Chat")+look.Dim.Render("   Claude writes requests and sends them with crew req; keys stay in crew"), w, "…")
	}
	return s.show(h).Tabs(w)
}

// TabsHere is an application's tabs (none on the list of applications or the chat).
func (s *Section) TabsHere(h section.Host, r section.Row) (int, int) {
	if r.Kind == rowChat {
		return 0, 0
	}
	return s.show(h).Pane()
}

// GotoTab opens one of an application's tabs.
func (s *Section) GotoTab(h section.Host, _ section.Row, i int) tea.Cmd {
	s.screen.SetPane(i)
	h.Refresh()
	return nil
}

// Render fills the main pane with the screen or the chat.
func (s *Section) Render(h section.Host, r section.Row) {
	vp := h.Viewport()
	if r.Kind != rowChat {
		vp.SetContent(s.show(h).Body(vp.Width, vp.Height, h.HasKeys() == "overview"))
		vp.GotoTop() // the screen keeps its cursor in view itself
		return
	}
	w := max(vp.Width, 20)
	follow := vp.AtBottom() || vp.TotalLineCount() <= vp.Height
	t := s.chat(h)
	if t == nil || len(h.Chats().Conv(t.ID).Msgs) == 0 {
		vp.SetContent(look.Dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude for requests: \"list the orgs in accounts\", " +
			"\"create an org then fetch it\", \"why does this 401 on staging?\". Claude writes them in your applications and " +
			"sends them with crew req. It never sees your keys: they stay in the Keychain, go only to the environment's own " +
			"hosts, and come back hidden.\n\n/compact, /new and /model work as in a task.")))
	} else {
		vp.SetContent(h.Chats().Render(t.ID, w, h.SpinFrame()))
	}
	if follow {
		vp.GotoBottom()
	}
}

// Hint is the screen's hint, or the chat's message box.
func (s *Section) Hint(h section.Host, r section.Row) string {
	switch {
	case r.Kind == rowChat:
		return h.ChatBox()
	case h.ListFocused():
		return ""
	}
	return s.show(h).Hint()
}

// Key hands a key to the screen, or the chat's message box.
func (s *Section) Key(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if r.Kind == rowChat {
		return h.ChatKey(msg), true
	}
	cmd, ev, _ := s.show(h).Key(msg)
	s.after(h, ev)
	return tea.Batch(cmd, s.spin(h)), true
}

// Click handles a click on the screen's tabs or body.
func (s *Section) Click(h section.Host, r section.Row, x, y int) (tea.Cmd, bool) {
	if r.Kind == rowChat {
		return nil, false
	}
	sc := s.show(h)
	switch {
	case y == 2:
		sc.ClickTabs(x)
	case y >= h.ContentY():
		cmd, ev := sc.Click(x, y-h.ContentY()+h.Viewport().YOffset)
		cmd = tea.Batch(cmd, s.spin(h))
		s.after(h, ev)
		return cmd, true
	}
	h.Refresh()
	return nil, true
}

// Wheel moves the screen's cursor.
func (s *Section) Wheel(h section.Host, r section.Row, down bool) bool {
	if r.Kind == rowChat {
		return false
	}
	s.show(h).Wheel(down)
	h.Refresh()
	return true
}

// Focus is the chat on its row, the screen on the others.
func (s *Section) Focus(_ section.Host, r section.Row) string {
	if r.Kind == rowChat {
		return "chat"
	}
	return "overview"
}

// ModalOpen reports whether the screen's popup is open.
func (s *Section) ModalOpen(section.Host) bool { return s.screen.Popup() != nil }

// ModalView draws the popup to fit the main pane.
func (s *Section) ModalView(h section.Host) string {
	p := s.screen.Popup()
	if p == nil {
		return ""
	}
	s.screen.Spin = h.SpinFrame()
	w, ht := h.MainSize()
	return p.View(min(max(w-4, 58), 104), ht-2)
}

// ModalKey hands a key to the popup.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	if p := s.screen.Popup(); p != nil {
		cmd = p.Key(msg)
	}
	s.after(h, Event{})
	return tea.Batch(cmd, s.spin(h))
}

// ModalClick handles a click on the popup's fields, options, buttons and ✕.
func (s *Section) ModalClick(h section.Host, x, y int) tea.Cmd {
	p := s.screen.Popup()
	if p == nil {
		return nil
	}
	cmd := tea.Batch(p.Click(x, y), s.spin(h))
	s.after(h, Event{})
	return cmd
}

// ModalWheel scrolls the popup.
func (s *Section) ModalWheel(_ section.Host, down bool) {
	if p := s.screen.Popup(); p != nil {
		p.Wheel(down)
	}
}

// ---- the chat ----

// chat is the REQUESTS chat, or nil before it's first opened.
func (s *Section) chat(h section.Host) *tasks.Task {
	return h.FindChat(func(t tasks.Task) bool { return t.Kind == chatKind })
}

// ensureChat makes the REQUESTS chat if there isn't one yet.
func (s *Section) ensureChat(h section.Host) *tasks.Task {
	if t := s.chat(h); t != nil {
		return t
	}
	return h.AddChat(tasks.Task{ID: "_requests", Title: "Requests", Kind: chatKind, Status: tasks.InProgress,
		Created: time.Now(), Body: ChatBrief(section.CrewExe())})
}

// openChat opens the REQUESTS chat; about a request, its id starts the message.
func (s *Section) openChat(h section.Host, about string) {
	s.ensureChat(h)
	h.Select("q:")
	h.FocusMain()
	if in := h.Input(); about != "" && in.Value() == "" {
		in.SetValue("About " + about + ": ")
		in.CursorEnd()
	}
}

// ChatOpen reports whether the chat's row is selected.
func (s *Section) ChatOpen(_ section.Host, r section.Row) bool { return r.Kind == rowChat }

// Chat is the REQUESTS chat on its row.
func (s *Section) Chat(h section.Host, r section.Row) *tasks.Task {
	if r.Kind == rowChat {
		return s.chat(h)
	}
	return nil
}

// EnsureChat makes the chat when a reload dropped it.
func (s *Section) EnsureChat(h section.Host, r section.Row) *tasks.Task {
	if r.Kind == rowChat {
		return s.ensureChat(h)
	}
	return nil
}

// Where runs the chat's first turn in the requests folder.
func (s *Section) Where(_ section.Host, t tasks.Task, prompt string) (tea.Cmd, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	dir := s.dir
	return func() tea.Msg {
		return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
	}, true
}

// Args locks the chat's Claude down: keys must never reach it.
func (s *Section) Args(h section.Host, t tasks.Task) ([]string, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	return ChatArgs(section.CrewExe(), h.Plugin()), true
}

// TurnEnded reloads the requests after the chat's turn: Claude may have written some.
func (s *Section) TurnEnded(h section.Host, id string) {
	if t := h.Task(id); t != nil && t.Kind == chatKind {
		s.screen.Load()
	}
}
