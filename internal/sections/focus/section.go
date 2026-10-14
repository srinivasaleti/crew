package focus

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/notify"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
)

// FOCUS paces your day (docs/focus-prd.md): a focus timer (⌘⇧F, /focus) that starts a break when it
// ends, eye breaks every 20 minutes and a stand-up nudge after an hour, and your screen time: how
// long crew was in front while you were active, split by what you were in, and how long agents
// worked. A tick counts time and fires these; the data is in ~/.crew/focus. It's a section: f on
// home turns all of it off.

// awayAfter is how long without a key or the mouse before crew stops counting you as there, and
// a pause long enough to reset the stand-up nudge.
const awayAfter = 5 * time.Minute

// Section is FOCUS.
type Section struct {
	section.Base
	set      Settings
	store    Store
	clock    Clock
	day      Day
	last     time.Time     // the last tick
	active   time.Time     // your last key or mouse use
	eye, sit time.Duration // time in crew since the last eye break, and without a pause
	away     time.Duration // time since you were last active
	eyeUntil time.Time     // the eye break shows on the help line until then
	saved    time.Time     // when the day was last written
	gen      int           // the ticker's generation; a tick from an older one is dropped
}

// NewSection makes FOCUS from its folder.
func NewSection(dir string) *Section {
	st := Store{Dir: dir}
	now := time.Now()
	return &Section{set: LoadSettings(), store: st, clock: st.Clock(), day: st.Day(DateOf(now)), active: now}
}

// Def is FOCUS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "focus", Label: "FOCUS", Letter: "f", About: "a focus timer, eye and stand-up breaks, reminders and your screen time"}
}

type tickMsg struct{ gen int }

// tick schedules the next tick: every second while a countdown is on screen, else every 15.
func (s *Section) tick(h section.Host) tea.Cmd {
	if !h.SectionOn("focus") {
		return nil // turning FOCUS on starts it again (restart)
	}
	gen, every := s.gen, 15*time.Second
	if s.clock.Running() || time.Now().Before(s.eyeUntil) {
		every = time.Second
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return tickMsg{gen} })
}

// restart starts a new ticker now (the old one's ticks are dropped), so a countdown you just
// started shows at once.
func (s *Section) restart(h section.Host) tea.Cmd {
	s.gen++
	s.last = time.Now()
	return s.tick(h)
}

// Init starts the clock.
func (s *Section) Init(h section.Host) tea.Cmd { return s.restart(h) }

// On starts the clock again when FOCUS is turned on.
func (s *Section) On(h section.Host) tea.Cmd { return s.restart(h) }

// Update counts you as active on every key and click (it leaves them to crew), and takes the tick.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		s.active = time.Now() // you're here: screen time counts
	case tickMsg:
		return s.onTick(h, msg), true
	}
	return nil, false
}

// onTick counts the time since the last tick and fires what's due.
func (s *Section) onTick(h section.Host, msg tickMsg) tea.Cmd {
	if msg.gen != s.gen || !h.SectionOn("focus") {
		return nil
	}
	now := time.Now()
	dt := now.Sub(s.last)
	if s.last.IsZero() || dt > time.Minute {
		dt = 0 // the Mac slept, or this is the first tick
	}
	s.last = now
	s.countTime(h, now, dt)
	cmd := s.checkClock(h, now)
	s.checkBreaks(h, now)
	if now.Sub(s.saved) >= time.Minute {
		s.saveDay(now)
	}
	if onPage(h) {
		h.Refresh()
	}
	return tea.Batch(cmd, s.tick(h))
}

// countTime adds dt to today: to crew and what you're in when its window is in front and you've been
// active lately, and to agents while any of them works.
func (s *Section) countTime(h section.Host, now time.Time, dt time.Duration) {
	d := &s.day
	if d.Date != DateOf(now) { // past midnight: a new day
		s.saveDay(now)
		s.day = s.store.Day(DateOf(now))
		d = &s.day
	}
	if !h.Blurred() && now.Sub(s.active) < awayAfter {
		d.Crew += dt
		d.By[h.WhereNow()] += dt
		s.eye += dt
		s.sit += dt
		s.away = 0
	} else if s.away += dt; s.away >= awayAfter {
		s.eye, s.sit = 0, 0 // a real pause rests your eyes and legs
	}
	if s.clock.Kind == "focus" {
		d.Focus += dt
	}
	for _, c := range h.Chats().All() {
		if c.Busy {
			d.Agents += dt
			break
		}
	}
}

// saveDay writes today.
func (s *Section) saveDay(now time.Time) {
	s.saved = now
	s.store.SaveDay(s.day) // ponytail: a failed write loses a minute of counting, nothing else
}

// checkClock ends a session or break whose time is up. A focus session starts a break; one that
// ended long ago (crew was closed) just counts.
func (s *Section) checkClock(h section.Host, now time.Time) tea.Cmd {
	c := s.clock
	if !c.Running() || now.Before(c.Ends) {
		return nil
	}
	stale := now.Sub(c.Ends) > time.Minute
	s.clock = Clock{}
	if c.Kind == "focus" {
		s.day.Sessions++
		if !stale {
			s.clock = Clock{Kind: "break", Start: now, Ends: now.Add(s.set.Break)}
			body := Dur(c.Ends.Sub(c.Start)) + " of focus" + labelled(c.Label) + " · a " + Dur(s.set.Break) + " break starts now"
			notify.Send("Focus done", body)
			h.Notice("Focus done · " + Dur(s.set.Break) + " break · stand up, look away")
		}
	} else if !stale {
		notify.Send("Break over", "⌘⇧F starts the next focus session")
		h.Notice("Break over · ⌘⇧F starts the next focus session")
	}
	s.store.SaveClock(s.clock)
	s.saveDay(now)
	if c.Kind == "focus" {
		return runShortcut(s.set.Off)
	}
	return nil
}

// labelled is " on <label>", or "" for a session with no label.
func labelled(label string) string {
	if label == "" {
		return ""
	}
	return " on " + label
}

// checkBreaks nudges you to rest your eyes every so often in crew (20-20-20), and to stand up after a
// long stretch without a pause. Not during a break.
func (s *Section) checkBreaks(h section.Host, now time.Time) {
	if s.clock.Kind == "break" {
		s.eye, s.sit = 0, 0
		return
	}
	if st := s.set; st.Eye > 0 && s.eye >= st.Eye {
		s.eye, s.eyeUntil = 0, now.Add(20*time.Second)
		notify.Send("Eye break", "Look at something 20 feet away for 20 seconds")
	}
	if st := s.set; st.Sit > 0 && s.sit >= st.Sit {
		s.sit = 0
		notify.Send("Time to stand up", Dur(st.Sit)+" in crew without a pause · stretch, or b on FOCUS starts a break")
		h.Notice("Time to stand up and stretch · " + Dur(st.Sit) + " without a pause")
	}
}

// runShortcut runs a macOS Shortcut by name (Do Not Disturb on or off), in the background.
func runShortcut(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	return func() tea.Msg {
		exec.Command("shortcuts", "run", name).Run() // ponytail: a failed Shortcut is silent; run it once in Shortcuts to check
		return nil
	}
}

// startFocus starts a focus session of d (0: the default), named after label.
func (s *Section) startFocus(h section.Host, d time.Duration, label string) tea.Cmd {
	if !h.SectionOn("focus") {
		h.Notice("FOCUS is off · f on home turns it on")
		return nil
	}
	if d <= 0 {
		d = s.set.Focus
	}
	now := time.Now()
	s.clock = Clock{Kind: "focus", Label: label, Start: now, Ends: now.Add(d)}
	s.store.SaveClock(s.clock)
	h.Notice("Focus for " + Dur(d) + labelled(label) + " · ⌘⇧F stops it")
	return tea.Batch(runShortcut(s.set.On), s.restart(h))
}

// stopClock ends the session or break early.
func (s *Section) stopClock(h section.Host) tea.Cmd {
	c := s.clock
	s.clock = Clock{}
	s.store.SaveClock(s.clock)
	if c.Kind != "focus" {
		h.Notice("Break ended")
		return s.restart(h)
	}
	h.Notice("Focus stopped after " + Dur(time.Since(c.Start)))
	return tea.Batch(runShortcut(s.set.Off), s.restart(h))
}

// toggle is ⌘⇧F: stop a session, or start one on the task you're in.
func (s *Section) toggle(h section.Host) tea.Cmd {
	if s.clock.Kind == "focus" {
		return s.stopClock(h)
	}
	label := ""
	if t := h.Current(); t != nil {
		label = t.Title
	}
	return s.startFocus(h, 0, label)
}

// startBreak starts a break now.
func (s *Section) startBreak(h section.Host) tea.Cmd {
	var cmd tea.Cmd
	if s.clock.Kind == "focus" {
		cmd = runShortcut(s.set.Off)
	}
	now := time.Now()
	s.clock = Clock{Kind: "break", Start: now, Ends: now.Add(s.set.Break)}
	s.store.SaveClock(s.clock)
	h.Notice(Dur(s.set.Break) + " break · stand up, look away")
	return tea.Batch(cmd, s.restart(h))
}

// Status is the timer or eye break, for the start of the help line; "" when neither runs.
func (s *Section) Status(h section.Host) string {
	if !h.SectionOn("focus") {
		return ""
	}
	now := time.Now()
	c := s.clock
	text := ""
	switch {
	case now.Before(s.eyeUntil):
		text = fmt.Sprintf("◉ look at something 20 feet away · %ds", int(s.eyeUntil.Sub(now).Seconds())+1)
	case c.Kind == "focus":
		text = "● focus " + Countdown(c.Left(now)) + labelled(c.Label)
	case c.Kind == "break":
		text = "◐ break " + Countdown(c.Left(now))
	default:
		return ""
	}
	return look.Running.Render(keys.Names(text))
}

// Rows is FOCUS's row.
func (s *Section) Rows(section.Host) []section.Row {
	return []section.Row{{Kind: section.Group, Section: "focus", Label: "FOCUS"}}
}

// Title is FOCUS's row: no count, and the countdown when the clock runs.
func (s *Section) Title(_ section.Host, r section.Row) (string, string) {
	badge := ""
	if c := s.clock; c.Running() {
		badge = look.Running.Render(Countdown(c.Left(time.Now())))
	}
	return look.Bold.Render(r.Label), badge
}

// onPage reports whether the main pane shows FOCUS.
func onPage(h section.Host) bool {
	r := h.SelectedRow()
	return r.Kind == section.Group && r.Section == "focus"
}

// ListKey lets FOCUS's letters act on it from the sidebar too.
func (s *Section) ListKey(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && strings.ContainsRune("sb+=-", msg.Runes[0]) {
		h.FocusMain()
		return s.Key(h, r, msg)
	}
	return nil, false
}

// Key handles keys on the FOCUS page: s focus, b break, +/− five minutes, esc home.
func (s *Section) Key(h section.Host, _ section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg.String() {
	case "s":
		if s.clock.Running() {
			cmd = s.stopClock(h)
		} else {
			cmd = s.startFocus(h, 0, "")
		}
	case "b":
		if s.clock.Kind == "break" {
			cmd = s.stopClock(h)
		} else {
			cmd = s.startBreak(h)
		}
	case "+", "=", "-":
		if c := &s.clock; c.Running() {
			d := 5 * time.Minute
			if msg.String() == "-" {
				d = -d
			}
			c.Ends = c.Ends.Add(d)
			if soon := time.Now().Add(time.Minute); c.Ends.Before(soon) {
				c.Ends = soon // at least a minute left
			}
			s.store.SaveClock(*c)
		}
	case "esc":
		h.GoUp()
		return nil, true
	}
	h.Refresh()
	return cmd, true
}

// Hint is the line under the FOCUS page: its keys (the sidebar's hint while it has the keys).
func (s *Section) Hint(h section.Host, _ section.Row) string {
	if h.ListFocused() {
		return ""
	}
	return look.KeyHints(keys.Names("  s focus · b break · +/− 5 min · esc home"))
}

// Header is the FOCUS page's first line: today so far.
func (s *Section) Header(_ section.Host, _ section.Row, w int) string {
	d := s.day
	return ansi.Truncate(look.Bold.Render("FOCUS")+"  "+look.Dim.Render(fmt.Sprintf("today %s in crew · %d focus session(s)", Dur(d.Crew), d.Sessions)), w, "…")
}

// Render draws the FOCUS page: the timer, today, this week and breaks.
func (s *Section) Render(h section.Host, _ section.Row) {
	vp := h.Viewport()
	vp.SetContent(s.page(vp.Width))
}

// page is the FOCUS page, w wide.
func (s *Section) page(w int) string {
	now := time.Now()
	var b strings.Builder
	c := s.clock
	switch c.Kind {
	case "focus", "break":
		total := c.Ends.Sub(c.Start)
		name := map[string]string{"focus": "Focus", "break": "Break"}[c.Kind]
		b.WriteString(look.Running.Render("● "+name+" "+Countdown(c.Left(now))+" left") + look.Dim.Render(labelled(c.Label)) + "\n")
		b.WriteString("  " + progress(total-c.Left(now), total, min(40, w-4)) + "\n\n")
	default:
		b.WriteString(look.Bold.Render("No focus session") + look.Dim.Render(fmt.Sprintf("  s starts %s of focus, b a %s break, ⌘⇧F from anywhere", Dur(s.set.Focus), Dur(s.set.Break))) + "\n\n")
	}

	d := s.day
	b.WriteString(look.Bold.Render("Today") + look.Dim.Render(fmt.Sprintf("  %s in crew · agents worked %s · %d focus session(s), %s", Dur(d.Crew), Dur(d.Agents), d.Sessions, Dur(d.Focus))) + "\n")
	type item struct {
		name string
		t    time.Duration
	}
	var items []item
	for k, v := range d.By {
		if v >= time.Minute {
			items = append(items, item{k, v})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t > items[j].t })
	if len(items) == 0 {
		b.WriteString(look.Dim.Render("  nothing yet: time counts while crew is in front and you're using it") + "\n")
	}
	for _, it := range items[:min(len(items), 8)] {
		b.WriteString(fmt.Sprintf("  %-22s %7s  %s\n", ansi.Truncate(it.name, 22, "…"), Dur(it.t), bar(it.t, items[0].t, 24)))
	}

	b.WriteString("\n" + look.Bold.Render("This week") + "\n")
	week := s.store.Week(now)
	week[len(week)-1] = d // today as counted, not as last saved
	var most time.Duration
	for _, day := range week {
		most = max(most, day.Crew)
	}
	for _, day := range week {
		t, _ := time.Parse("2006-01-02", day.Date)
		b.WriteString(fmt.Sprintf("  %s %7s  %s\n", t.Format("Mon"), Dur(day.Crew), bar(day.Crew, most, 24)))
	}

	b.WriteString("\n" + look.Bold.Render("Breaks") + "\n")
	if s.set.Eye > 0 {
		b.WriteString(fmt.Sprintf("  eyes every %s in crew, 20-20-20 · next in %s\n", Dur(s.set.Eye), Dur(max(s.set.Eye-s.eye, 0))))
	} else {
		b.WriteString(look.Dim.Render("  eye breaks are off (CREW_EYE_BREAKS=0)") + "\n")
	}
	if s.set.Sit > 0 {
		b.WriteString(fmt.Sprintf("  stand up after %s without a pause · %s so far\n", Dur(s.set.Sit), Dur(s.sit)))
	} else {
		b.WriteString(look.Dim.Render("  stand-up nudges are off (CREW_SIT_MIN=0)") + "\n")
	}
	return b.String()
}

// bar draws v as a share of most, up to w cells.
func bar(v, most time.Duration, w int) string {
	if most <= 0 {
		return ""
	}
	return look.Running.Render(strings.Repeat("█", max(int(int64(w)*int64(v)/int64(most)), 0)))
}

// progress draws how far through a session you are.
func progress(done, total time.Duration, w int) string {
	if total <= 0 || w <= 0 {
		return ""
	}
	n := min(int(int64(w)*int64(done)/int64(total)), w)
	return look.Running.Render(strings.Repeat("█", n)) + look.Dim.Render(strings.Repeat("░", w-n))
}

// Commands is /focus [minutes]: start a session on this task, or stop the one running.
func (s *Section) Commands() []section.Command {
	return []section.Command{{Name: "/focus", Help: "start a focus session on this task (/focus 50 for 50 minutes), or stop it",
		Run: func(h section.Host, t *tasks.Task, arg string) tea.Cmd {
			h.Say(t.ID, chat.You, "/focus "+arg)
			if arg == "" && s.clock.Kind == "focus" {
				cmd := s.stopClock(h)
				h.Say(t.ID, chat.Tool, h.Note())
				return cmd
			}
			var mins int
			if arg != "" {
				if _, err := fmt.Sscanf(arg, "%d", &mins); err != nil || mins <= 0 {
					h.Say(t.ID, chat.Error, "/focus takes minutes, e.g. /focus 50")
					return nil
				}
			}
			cmd := s.startFocus(h, time.Duration(mins)*time.Minute, t.Title)
			h.Say(t.ID, chat.Tool, h.Note())
			return cmd
		}}}
}

// Actions are ⌘⇧F (focus) and a break, for the ⌘ shortcuts.
func (s *Section) Actions() []section.Action {
	return []section.Action{
		{Name: "focus", Help: "start a focus session on the task you're in, or stop the one running (FOCUS)", Run: s.toggle},
		{Name: "break", Help: "start a break now, or end the one running (FOCUS)", Run: func(h section.Host) tea.Cmd {
			if s.clock.Kind == "break" {
				return s.stopClock(h)
			}
			return s.startBreak(h)
		}},
	}
}
