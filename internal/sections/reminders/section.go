package reminders

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/notify"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// The REMINDERS section, as crew's plugin: its row with two tabs, List (the table, with the form as
// a popup) and Chat (Claude managing them with `crew remind`, locked down like the REQUESTS chat),
// and the tick that fires what's due.

// chatKind marks the REMINDERS chat in its task file.
const chatKind = "reminders"

// paneNames are REMINDERS' tabs.
var paneNames = []string{"List", "Chat"}

// alertFor is how long a reminder that fired stays on the help line, unless you open REMINDERS.
const alertFor = 10 * time.Minute

// Section is REMINDERS.
type Section struct {
	section.Base
	dir     string     // the reminders folder: reminders.json, and where its chat runs
	rems    []Reminder // as last read
	pane    int        // the tab: 0 List, 1 Chat
	form    *Form      // the new or edit popup, while it's open
	gen     int        // the ticker's generation; a tick from an older one is dropped
	alert   string     // the reminder that fired last, on the help line for a while
	alertAt time.Time
}

// NewSection makes REMINDERS from its folder, first moving in the reminders FOCUS used to keep
// (old, its reminders.json); err says why that move failed.
func NewSection(dir, old string) (*Section, error) {
	s := &Section{dir: dir}
	err := s.store().Import(old)
	s.load()
	return s, err
}

// Def is REMINDERS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "reminders", Label: "REMINDERS", Letter: "m", About: "reminders, once or repeating, and a Claude chat that manages them"}
}

// store is the reminders file.
func (s *Section) store() Store { return Store{Path: s.dir + "/reminders.json"} }

// load re-reads the reminders: the chat's Claude changes them with `crew remind`.
func (s *Section) load() { s.rems = s.store().Load() }

// onPage reports whether REMINDERS is in the main pane (either tab).
func onPage(h section.Host) bool {
	r := h.SelectedRow()
	return r.Kind == section.Group && r.Section == "reminders"
}

// change loads, changes and saves the reminders, then shows them.
func (s *Section) change(h section.Host, f func(rs []Reminder) ([]Reminder, error)) {
	if err := s.store().Change(f); err != nil {
		h.Notice(err.Error())
	}
	s.load()
	if onPage(h) {
		h.Refresh()
	}
}

// Rows is REMINDERS' row, with how many are active.
func (s *Section) Rows(section.Host) []section.Row {
	n := 0
	for _, r := range s.rems {
		if r.Active() {
			n++
		}
	}
	return []section.Row{{Kind: section.Group, Section: "reminders", Label: "REMINDERS", Count: n}}
}

// Title is REMINDERS' row with the next reminder's time, short so the count still fits: "4:00pm"
// today, else "Tue 4:00pm"; none when none is due.
func (s *Section) Title(_ section.Host, r section.Row) (string, string) {
	now := time.Now()
	for _, rm := range s.rems {
		if !rm.Active() {
			continue
		}
		if rm.At.YearDay() == now.YearDay() && rm.At.Year() == now.Year() {
			return section.CountTitle(r), look.Dim.Render(rm.At.Format("3:04pm"))
		}
		return section.CountTitle(r), look.Dim.Render(rm.At.Format("Mon 3:04pm"))
	}
	return section.CountTitle(r), ""
}

// Status is the reminder that fired last, for the start of the help line, so it can't slip by
// unseen when a notification is hidden; "" after alertFor.
func (s *Section) Status(section.Host) string {
	if s.alert == "" || time.Since(s.alertAt) > alertFor {
		return ""
	}
	return look.Warn.Render("◷ " + s.alert + " · " + s.alertAt.Format("3:04pm"))
}

// setPane switches REMINDERS' tab; the chat gets the message box.
func (s *Section) setPane(h section.Host, i int) tea.Cmd {
	s.pane = i
	if i == 1 {
		s.ensureChat(h)
	}
	h.Reselect()
	if i == 1 {
		h.FocusMain()
		return h.Input().Focus()
	}
	return nil
}

// ShowsTable is the List tab.
func (s *Section) ShowsTable(section.Host, section.Row) bool { return s.pane == 0 }

// Table is the reminders.
func (s *Section) Table(section.Host, section.Row) table.Table { return Table(s.rems, time.Now()) }

// Tabs is REMINDERS' tab bar.
func (s *Section) Tabs(_ section.Host, _ section.Row, w int) string {
	active := lipgloss.NewStyle().Background(look.Accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range paneNames {
		if i == s.pane {
			bar = append(bar, active.Render(section.TabLabel(n, i)))
		} else {
			bar = append(bar, look.Dim.Padding(0, 1).Render(section.TabLabel(n, i)))
		}
	}
	about := "   n new · ⏎ edit · c ask Claude"
	if s.pane == 1 {
		about = "   Claude manages your reminders with crew remind, and nothing else"
	}
	return ansi.Truncate(strings.Join(bar, " ")+look.Dim.Render(about), w, "…")
}

// TabsHere is REMINDERS' tabs.
func (s *Section) TabsHere(section.Host, section.Row) (int, int) { return s.pane, len(paneNames) }

// GotoTab opens one of REMINDERS' tabs.
func (s *Section) GotoTab(h section.Host, _ section.Row, i int) tea.Cmd {
	cmd := s.setPane(h, i)
	h.Refresh()
	return cmd
}

// Click on the tab bar opens that tab; the List's table takes its own clicks.
func (s *Section) Click(h section.Host, _ section.Row, x, y int) (tea.Cmd, bool) {
	if y != 2 {
		return nil, false
	}
	var cmd tea.Cmd
	if i := section.TabAt(paneNames, x); i >= 0 {
		cmd = s.setPane(h, i)
		h.Refresh()
	}
	return cmd, true
}

// Open does what the List table asks for on a reminder (its id, "" for a Global key).
func (s *Section) Open(h section.Host, _ section.Row, id, action string) tea.Cmd {
	n, _ := strconv.Atoi(id)
	switch action {
	case New:
		return s.openForm(h, Reminder{})
	case Chat:
		return s.setPane(h, 1)
	}
	r := Find(s.rems, n)
	if r == nil {
		return nil
	}
	switch action {
	case Edit:
		return s.openForm(h, *r)
	case Delete, Pause, Restore:
		var did string
		s.change(h, func(rs []Reminder) ([]Reminder, error) {
			if r := Find(rs, n); r != nil {
				switch {
				case action == Delete:
					r.Deleted, did = true, "Deleted "+strconv.Quote(r.Text)+" · u restores it (Deleted)"
				case action == Restore:
					r.Deleted, did = false, "Restored "+strconv.Quote(r.Text)
				case r.Paused:
					Resume(r, time.Now())
					did = "Resumed " + strconv.Quote(r.Text) + " · next " + When(r.At, time.Now())
				default:
					r.Paused, did = true, "Paused "+strconv.Quote(r.Text)+" · p resumes it"
				}
			}
			return rs, nil
		})
		if h.Note() == "" {
			h.Notice(did)
		}
	}
	return nil
}

// openForm opens the new or edit popup.
func (s *Section) openForm(h section.Host, r Reminder) tea.Cmd {
	var cmd tea.Cmd
	s.form, cmd = NewForm(r)
	h.SectionModal(true)
	h.Input().Blur()
	return cmd
}

// ModalOpen reports whether the new or edit popup is open.
func (s *Section) ModalOpen(section.Host) bool { return s.form != nil }

// ModalView draws the popup.
func (s *Section) ModalView(section.Host) string { return s.form.View() }

// ModalKey hands a key to the popup and saves what it makes.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	cmd, saved, closed := s.form.Key(msg)
	if !closed {
		return cmd
	}
	s.form = nil
	h.SectionModal(false)
	if saved != nil {
		r := *saved
		s.change(h, func(rs []Reminder) ([]Reminder, error) {
			if old := Find(rs, r.ID); old != nil {
				old.Text, old.At, old.Every, old.Done = r.Text, r.At, r.Every, false
				return rs, nil
			}
			r.ID = NextID(rs)
			return append(rs, r), nil
		})
		if h.Note() == "" {
			h.Notice("Saved · next " + When(r.At, time.Now()))
		}
	}
	h.Reselect()
	return nil
}

// playSound rings a reminder with one of macOS's sounds, whatever the notification settings:
// CREW_REMINDER_SOUND names it (Glass, Ping, Hero, Submarine… in /System/Library/Sounds), "off"
// for none.
func playSound() {
	name := os.Getenv("CREW_REMINDER_SOUND")
	switch name {
	case "off", "0", "none":
		return
	case "":
		name = "Glass"
	}
	go exec.Command("afplay", "/System/Library/Sounds/"+name+".aiff").Run()
}

// tickMsg is the reminders' clock.
type tickMsg struct{ gen int }

// tick fires due reminders every 15 seconds while REMINDERS is on.
func (s *Section) tick(h section.Host) tea.Cmd {
	if !h.SectionOn("reminders") {
		return nil // turning it on starts it again
	}
	gen := s.gen
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return tickMsg{gen} })
}

// Init fires what's due now, which starts the clock.
func (s *Section) Init(section.Host) tea.Cmd {
	gen := s.gen
	return func() tea.Msg { return tickMsg{gen} }
}

// On fires what's due now and starts a new ticker (an older one's ticks are dropped).
func (s *Section) On(section.Host) tea.Cmd {
	s.gen++
	gen := s.gen
	return func() tea.Msg { return tickMsg{gen} }
}

// Update takes the clock's tick: it re-reads the reminders and tells you about the ones that are due.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	tm, ok := msg.(tickMsg)
	if !ok {
		return nil, false
	}
	if tm.gen != s.gen || !h.SectionOn("reminders") {
		return nil, true
	}
	rs := s.store().Load()
	fired, changed := Fire(rs, time.Now())
	if changed {
		s.change(h, func([]Reminder) ([]Reminder, error) { return rs, nil })
	} else {
		s.rems = rs // the chat or a terminal may have changed them
	}
	if len(fired) > 0 {
		playSound()
	}
	for _, r := range fired {
		notify.Send("Reminder", r.Text)
		s.alert, s.alertAt = r.Text, time.Now()
		note := "◷ Reminder: " + r.Text
		if r.Every != "" {
			note += " · next " + When(Find(s.rems, r.ID).At, time.Now())
		}
		h.Notice(note)
	}
	return s.tick(h), true
}

// chat is the REMINDERS chat, or nil before it's first opened.
func (s *Section) chat(h section.Host) *tasks.Task {
	return h.FindChat(func(t tasks.Task) bool { return t.Kind == chatKind })
}

// ensureChat makes the REMINDERS chat if there isn't one yet.
func (s *Section) ensureChat(h section.Host) *tasks.Task {
	if t := s.chat(h); t != nil {
		return t
	}
	return h.AddChat(tasks.Task{ID: "_reminders", Title: "Reminders", Kind: chatKind, Status: tasks.InProgress,
		Created: time.Now(), Body: ChatBrief(section.CrewExe(), time.Now())})
}

// ChatOpen reports whether the Chat tab is open.
func (s *Section) ChatOpen(section.Host, section.Row) bool { return s.pane == 1 }

// Chat is the REMINDERS chat on its tab.
func (s *Section) Chat(h section.Host, _ section.Row) *tasks.Task {
	if s.pane == 1 {
		return s.chat(h)
	}
	return nil
}

// EnsureChat makes the chat when a reload dropped it.
func (s *Section) EnsureChat(h section.Host, _ section.Row) *tasks.Task { return s.ensureChat(h) }

// Where runs the chat's first turn in the reminders folder.
func (s *Section) Where(_ section.Host, t tasks.Task, prompt string) (tea.Cmd, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	dir := s.dir
	return func() tea.Msg {
		return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
	}, true
}

// Args locks the chat's Claude down to crew remind.
func (s *Section) Args(_ section.Host, t tasks.Task) ([]string, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	return ChatArgs(section.CrewExe()), true
}

// TurnEnded shows what the chat's Claude changed.
func (s *Section) TurnEnded(h section.Host, id string) {
	if t := h.Task(id); t != nil && t.Kind == chatKind {
		s.load()
	}
}

// Header is the Chat tab's first line.
func (s *Section) Header(h section.Host, _ section.Row, w int) string {
	return ansi.Truncate(look.Bold.Render("Claude")+look.Dim.Render(" · reminders")+h.ChatMeta(s.chat(h)), w, "…")
}

// Render fills the main pane with the chat.
func (s *Section) Render(h section.Host, _ section.Row) {
	vp := h.Viewport()
	w := max(vp.Width, 20)
	follow := vp.AtBottom() || vp.TotalLineCount() <= vp.Height
	t := s.chat(h)
	if t == nil || len(h.Chats().Conv(t.ID).Msgs) == 0 {
		vp.SetContent(look.Dim.Render(lipgloss.NewStyle().Width(w).Render("Tell Claude what to remember: \"stand-up every weekday at 4pm\", " +
			"\"move the timesheet reminder to Fridays 5:30\", \"pause everything next week, I'm on leave\", \"what's coming up tomorrow?\". " +
			"Claude changes your reminders with crew remind and nothing else, and says what it changed. The List tab (⌘1) shows them.\n\n" +
			"/compact, /new and /model work as in a task.")))
	} else {
		vp.SetContent(h.Chats().Render(t.ID, w, h.SpinFrame()))
	}
	if follow {
		vp.GotoBottom()
	}
}

// Hint is the message box under the chat.
func (s *Section) Hint(h section.Host, _ section.Row) string { return h.ChatBox() }

// Key is the chat's keys: tab goes back to the List (unless it picks a suggestion), the rest go to
// the message box.
func (s *Section) Key(h section.Host, _ section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if k := msg.String(); k == "tab" && !h.Suggesting() || k == "shift+tab" {
		return s.setPane(h, 0), true // back to the List
	}
	return h.ChatKey(msg), true
}

// Focus is the chat.
func (s *Section) Focus(section.Host, section.Row) string { return "chat" }
