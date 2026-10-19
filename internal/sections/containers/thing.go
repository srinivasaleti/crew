package containers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
)

// A thing opened from a table (a container, an image, a pod…) on its own page: Logs that follow
// the end, Inspect or Describe with secret-looking values hidden, and a chat locked down to crew ctr.

// thing is a container, image, volume or network, or a Kubernetes object, opened on its own page.
type thing struct {
	key     string // its sidebar row's key: "d:Container:<id>", "k:Pod/<ns>/<name>"
	kind    string // Container, Image, Volume, Network; Pod, Deployment, Service…
	id      string // docker's id (a volume's name); the object's name on Kubernetes
	name    string
	ns      string // its namespace (Kubernetes)
	kctx    string // its context (Kubernetes)
	pane    int
	logs    string
	inspect string
	logErr  string // why its logs couldn't be read
	err     string // why inspect or describe couldn't be read
	follow  bool   // Logs reads again on the clock and stays at the bottom
	loaded  bool   // read at least once
	reading bool
}

// panes are the thing's tabs: Logs when it has any, Inspect (Describe on Kubernetes), Chat.
func (t *thing) panes() []string {
	look := "Inspect"
	if t.kctx != "" {
		look = "Describe"
	}
	if t.kind == "Container" || hasLogs(t.kind) {
		return []string{"Logs", look, "Chat"}
	}
	return []string{look, "Chat"}
}

// paneName is the open tab's name.
func (t *thing) paneName() string { return t.panes()[min(t.pane, len(t.panes())-1)] }

// openThing opens a thing on its own page (on Chat for a), reading its logs or inspect output.
func (s *Section) openThing(h section.Host, t *thing, action string) tea.Cmd {
	t.key = "d:" + t.kind + ":" + t.id
	if t.kctx != "" {
		t.key = "k:" + t.kctx + "/" + t.kind + "/" + t.ns + "/" + t.name
	}
	if old := s.things[t.key]; old != nil {
		t = old
	} else {
		t.follow = true
		s.things[t.key] = t
	}
	h.Select(t.key)
	h.FocusMain()
	pane := 0
	if action == Ask {
		pane = len(t.panes()) - 1
	}
	return s.setPane(h, t, pane)
}

// setPane opens one of a thing's tabs; Chat gets the message box, the others read again.
func (s *Section) setPane(h section.Host, t *thing, p int) tea.Cmd {
	t.pane = p
	in := h.Input()
	var cmd tea.Cmd
	if t.paneName() == "Chat" {
		c := s.ensureChat(h, t)
		h.MarkRead(c.ID)
		in.Placeholder = "Ask Claude about " + t.name + "…"
		cmd = in.Focus()
	} else {
		in.Blur()
		cmd = s.readThing(h, t)
	}
	h.ClearSuggestions()
	h.Layout()
	s.render(h)
	return cmd
}

// readThing reads the open tab of a thing: its logs, or what inspect or describe says.
func (s *Section) readThing(h section.Host, t *thing) tea.Cmd {
	t.reading = true
	ctx, logs := h.Ctx(), t.paneName() == "Logs"
	kind, id, ns, kctx, key := t.kind, t.id, t.ns, t.kctx, t.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var out string
		var err error
		switch {
		case kctx != "" && logs:
			out, err = kubectl(ctx, kctx, "-n", ns, "logs", strings.ToLower(kind)+"/"+id, "--all-containers", "--prefix", "--tail", "500")
		case kctx != "":
			out, err = kubectl(ctx, kctx, "-n", ns, "describe", strings.ToLower(kind), id)
		case logs:
			out, err = run(ctx, "docker", "logs", "--tail", "500", "--timestamps", id)
		default:
			out, err = run(ctx, "docker", strings.ToLower(kind), "inspect", id)
		}
		if logs {
			return thingMsg{key: key, out: out, logs: true, err: err}
		}
		return thingMsg{key: key, out: redact(out), err: err}
	}
}

// Frame is an open thing's frame title.
func (s *Section) Frame(_ section.Host, r section.Row) string {
	if t := s.thing(r); t != nil {
		return strings.ToLower(t.kind)
	}
	return ""
}

// Header is an open thing's first line: its name, kind, where it is and its state.
func (s *Section) Header(h section.Host, r section.Row, w int) string {
	t := s.thing(r)
	if t == nil {
		return ""
	}
	meta := " · " + strings.ToLower(t.kind)
	if t.kctx != "" {
		meta += " · " + t.ns + " · " + t.kctx
	}
	head := look.Bold.Render(t.name) + look.Dim.Render(meta)
	if isProd(t.kctx) {
		head += look.Warn.Render("  ⚠ production")
	}
	if t.paneName() == "Chat" {
		head += h.ChatMeta(s.chat(h, t))
	}
	return ansi.Truncate(head, w, "…")
}

// Render fills the main pane with the open thing's tab.
func (s *Section) Render(h section.Host, r section.Row) {
	if s.thing(r) != nil {
		s.render(h)
	}
}

// render draws the thing on screen: its logs (following the end), inspect or describe, or chat.
func (s *Section) render(h section.Host) {
	t := s.thing(h.SelectedRow())
	if t == nil {
		return
	}
	vp := h.Viewport()
	w := max(vp.Width, 20)
	follow := vp.AtBottom() || vp.TotalLineCount() <= vp.Height
	switch t.paneName() {
	case "Chat":
		if c := s.chat(h, t); c != nil && len(h.Chats().Conv(c.ID).Msgs) > 0 {
			vp.SetContent(h.Chats().Render(c.ID, w, h.SpinFrame()))
		} else {
			vp.SetContent(look.Dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude about " + t.name + ": why it crashed, " +
				"what's using the disk, what an error means. Claude reads logs, inspect and describe output with crew ctr, and " +
				"can't change anything: a fix comes back as a command for you to run.\n\n/compact, /new and /model work as in a task.")))
		}
	case "Logs":
		text := t.logs
		switch {
		case t.logErr != "":
			text = look.Warn.Render(t.logErr)
		case !t.loaded:
			text = look.Dim.Render("Reading logs…")
		case text == "":
			text = look.Dim.Render("No logs yet.")
		default:
			text = highlight(text)
		}
		vp.SetContent(clip(text, w))
		if t.follow {
			vp.GotoBottom()
			return
		}
	default:
		text := t.inspect
		switch {
		case t.err != "":
			text = look.Warn.Render(t.err)
		case text == "":
			text = look.Dim.Render("Reading…")
		}
		vp.SetContent(clip(text, w))
		return
	}
	if follow {
		vp.GotoBottom()
	}
}

// highlight colours error and warning lines in logs.
func highlight(logs string) string {
	lines := strings.Split(logs, "\n")
	for i, l := range lines {
		switch u := strings.ToUpper(l); {
		case strings.Contains(u, "ERROR") || strings.Contains(u, "PANIC") || strings.Contains(u, "FATAL") || strings.Contains(u, "EXCEPTION"):
			lines[i] = look.Warn.Render(l)
		case strings.Contains(u, "WARN"):
			lines[i] = look.Orange.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// clip cuts each line to w columns, so long log lines don't wrap the pane.
func clip(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return strings.Join(lines, "\n")
}

// Hint is the line under a thing's tab, or the message box on Chat.
func (s *Section) Hint(h section.Host, r section.Row) string {
	t := s.thing(r)
	if t == nil {
		return ""
	}
	switch t.paneName() {
	case "Chat":
		return h.ChatBox()
	case "Logs":
		f := "f stops following"
		if !t.follow {
			f = "f follows"
		}
		hint := "  " + f + " · r reads again · a asks Claude about these logs · n makes a task from them"
		if t.kind == "Container" || t.kind == "Pod" {
			hint += " · e shell"
		}
		return look.KeyHints(keys.Names(hint + " · esc back"))
	}
	return look.KeyHints(keys.Names("  r reads again · a asks Claude · esc back"))
}

// Key handles keys on an open thing: the message box on Chat, tab to switch tabs, f, r, a, n and
// e on Logs, esc back to the table, the arrows scroll.
func (s *Section) Key(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	t := s.thing(r)
	if t == nil {
		return nil, false
	}
	k := msg.String()
	switching := k == "tab" || k == "shift+tab"
	if t.paneName() == "Chat" && (!switching || h.ChatTyping()) {
		return h.ChatKey(msg), true
	}
	vp := h.Viewport()
	switch {
	case switching:
		n := len(t.panes())
		return s.setPane(h, t, (t.pane+map[string]int{"tab": 1, "shift+tab": -1}[k]+n)%n), true
	case k == "esc":
		h.GoUp()
		return nil, true
	case k == "r":
		return s.readThing(h, t), true
	case k == "a":
		var cmd tea.Cmd
		if t.paneName() == "Logs" && t.logs != "" {
			c := s.ensureChat(h, t)
			cmd = h.Send(*c, chat.Outgoing{Text: "What's going wrong here? The last lines of its logs:\n\n```\n" + tail(t.logs, 80) + "\n```"})
		}
		return tea.Batch(s.setPane(h, t, len(t.panes())-1), cmd), true
	case k == "f" && t.paneName() == "Logs":
		t.follow = !t.follow
		s.render(h)
		return nil, true
	case k == "n" && t.paneName() == "Logs":
		brief := "Fix what's going wrong in " + strings.ToLower(t.kind) + " " + t.name
		if t.ns != "" {
			brief += " (namespace " + t.ns + ", context " + t.kctx + ")"
		}
		return h.WorkOn("Fix "+t.name, "", brief+". The last lines of its logs:\n\n```\n"+tail(t.logs, 60)+"\n```"), true
	case k == "e" && (t.kind == "Container" || t.kind == "Pod"):
		cmd := "docker exec -it " + t.id + " " + anyShell
		if t.kctx != "" {
			cmd = "kubectl --context " + quote(t.kctx) + " -n " + quote(t.ns) + " exec -it " + quote(t.id) + " -- " + anyShell
			if isProd(t.kctx) {
				return s.ask(h, look.Warn.Render("⚠ "+t.kctx+" is production.")+"\nOpen a shell in "+look.Bold.Render(t.name)+"?", h.RunShell(cmd, "a shell in "+t.name)), true
			}
		}
		return h.RunShell(cmd, "a shell in "+t.name), true
	case k == "up" || k == "down" || k == "pgup" || k == "pgdown" || k == "home" || k == "end":
		t.follow = t.follow && (k == "down" || k == "end" || k == "pgdown") // scrolling up stops following
	}
	var cmd tea.Cmd
	*vp, cmd = vp.Update(msg)
	return cmd, true
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

// Focus is the chat on Chat, the thing's own keys on its other tabs, else the table.
func (s *Section) Focus(_ section.Host, r section.Row) string {
	switch t := s.thing(r); {
	case t == nil:
		return "overview"
	case t.paneName() == "Chat":
		return "chat"
	}
	return "pr"
}

// ChatOpen reports whether a thing's Chat tab is open.
func (s *Section) ChatOpen(_ section.Host, r section.Row) bool {
	t := s.thing(r)
	return t != nil && t.paneName() == "Chat"
}

// Chat is the thing's chat on its Chat tab.
func (s *Section) Chat(h section.Host, r section.Row) *tasks.Task {
	if t := s.thing(r); t != nil && t.paneName() == "Chat" {
		return s.chat(h, t)
	}
	return nil
}

// EnsureChat makes the thing's chat when it isn't there.
func (s *Section) EnsureChat(h section.Host, r section.Row) *tasks.Task {
	if t := s.thing(r); t != nil {
		return s.ensureChat(h, t)
	}
	return nil
}

// chatID is the id of a thing's chat, the same each time it's opened, so its talk is kept.
func chatID(t *thing) string { return tasks.Slugify("ctr " + t.key) }

// chat is a thing's chat, or nil before its Chat tab is opened.
func (s *Section) chat(h section.Host, t *thing) *tasks.Task {
	id := chatID(t)
	return h.FindChat(func(c tasks.Task) bool { return c.Kind == chatKind && c.ID == id })
}

// ensureChat makes a thing's chat (in memory until its first message saves it).
func (s *Section) ensureChat(h section.Host, t *thing) *tasks.Task {
	if c := s.chat(h, t); c != nil {
		return c
	}
	return h.AddChat(tasks.Task{ID: chatID(t), Title: t.name, Kind: chatKind, Status: tasks.InProgress, Created: time.Now(),
		Body: ChatBrief(section.CrewExe(), t.kind, t.id, t.name, t.ns, t.kctx)})
}

// Where runs a thing's chat in its own folder, so Claude can resume it there.
func (s *Section) Where(_ section.Host, t tasks.Task, prompt string) (tea.Cmd, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	dir := filepath.Join(s.dir, t.ID)
	return func() tea.Msg {
		return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
	}, true
}

// Args locks a thing's chat down to crew ctr, which only reads.
func (s *Section) Args(_ section.Host, t tasks.Task) ([]string, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	return ChatArgs(section.CrewExe()), true
}

// TurnEnded redraws a thing's chat when its turn ends.
func (s *Section) TurnEnded(h section.Host, id string) {
	if t := s.thing(h.SelectedRow()); t != nil && t.paneName() == "Chat" {
		s.render(h)
	}
}

// AutoLoad reads a thing that's on screen and hasn't been read, however it was selected.
func (s *Section) AutoLoad(h section.Host) tea.Cmd {
	if t := s.thing(h.SelectedRow()); t != nil && !t.loaded && !t.reading && t.paneName() != "Chat" {
		return s.readThing(h, t)
	}
	return nil
}
