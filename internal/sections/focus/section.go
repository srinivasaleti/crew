package focus

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/notify"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
)

// FOCUS paces your day at the Mac (docs/focus-prd.md): a focus timer (pomodoro or flow; ⌘⇧F,
// /focus) that starts a break when it ends, a move break every 30 minutes, quiet eye breaks on the
// help line, and your screen time across the whole Mac, read from macOS's idle time, with a daily
// limit and an end of day. A break can cover the main pane until it's over. A tick counts time and
// fires these; the data is in ~/.crew/focus. It's a section: f on home turns all of it off.
//
// The break screen goes in three steps: time's up (start the break, or 5 more minutes, twice at
// most), the break counting down (ending it early asks first), then break done (start the next
// focus, or not yet). Set to "full", crew's app fills the screen with crew's window, above every
// app, for those steps (notify.Window); "crew" keeps them in crew's window; "off" only notifies.

const (
	// activeIdle is how long without input before the Mac counts you as away.
	activeIdle = time.Minute
	// awayAfter is the same without the Mac's idle time (not a Mac, or ioreg failed): how long
	// without a key or click in crew's window before crew stops counting you.
	awayAfter = 5 * time.Minute
	// eyeRest is how long an eye break lasts, and a pause long enough to count as one.
	eyeRest = 20 * time.Second
	// postpone is how long p on the break screen puts a break off.
	postpone = 5 * time.Minute
	// maxDelays is how many times a break can be put off.
	maxDelays = 2
)

// Section is FOCUS.
type Section struct {
	section.Base
	set       Settings
	store     Store
	clock     Clock
	day       Day
	last      time.Time     // the last tick
	active    time.Time     // your last key or click in crew (when the Mac's idle time isn't known)
	away      time.Duration // time since you were last active in crew (the same)
	idle      time.Duration // the Mac's idle time, as last read
	idleAt    time.Time     // when it was read; zero when it couldn't be
	polled    time.Time     // when it was last asked for
	polling   bool          // a read is on its way
	eye, move time.Duration // active time since the last eye and move break
	eyeUntil  time.Time     // the eye break shows on the help line until then
	later     Clock         // a postponed break; it's offered again at later.Start
	screen    string        // the break screen's step: "ask", "break" or "back"; "" when it's closed
	due       Clock         // the break "ask" offers (its Label and length)
	why       string        // what "ask" says the break is for: "25m of focus done", "30m at the Mac"
	delays    int           // how many times the break offered was put off
	sure      bool          // "end early" was pressed once on the break
	raised    bool          // crew's window was brought over every app (Screen "full")
	form      *Form         // the settings popup, while it's open
	saved     time.Time     // when the day was last written
	gen       int           // the ticker's generation; a tick from an older one is dropped
}

// NewSection makes FOCUS from its folder.
func NewSection(dir string) *Section {
	st := Store{Dir: dir}
	now := time.Now()
	return &Section{set: st.Settings(), store: st, clock: st.Clock(), day: st.Day(DateOf(now)), active: now}
}

// Def is FOCUS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "focus", Label: "FOCUS", Letter: "f", About: "a focus timer, breaks, and your screen time on the Mac"}
}

// tickMsg is the clock's tick, from ticker generation gen.
type tickMsg struct{ gen int }

// tick schedules the next tick: every second while something counts down on screen, else every 15.
func (s *Section) tick(h section.Host) tea.Cmd {
	if !h.SectionOn("focus") {
		return nil // turning FOCUS on starts it again (restart)
	}
	gen, every := s.gen, 15*time.Second
	if s.clock.Running() || s.screen != "" || time.Now().Before(s.eyeUntil) {
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

// Update counts you as active on every key and click (it leaves them to crew), and takes the tick
// and the Mac's idle time.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		s.active = time.Now()
	case tickMsg:
		return s.onTick(h, msg), true
	case idleMsg:
		s.polling, s.idle, s.idleAt = false, msg.idle, time.Time{}
		if msg.ok {
			s.idleAt = time.Now()
		}
		return nil, true
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
	var poll tea.Cmd
	if !s.polling && now.Sub(s.polled) >= 10*time.Second {
		s.polled = now
		if poll = readIdle(); poll != nil {
			s.polling = true
		}
	}
	s.countTime(h, now, dt)
	gen := s.gen
	cmd := s.checkClock(h, now)
	if cmd == nil {
		cmd = s.checkBreaks(h, now)
	}
	s.checkDay(h, now)
	if now.Sub(s.saved) >= time.Minute {
		s.saveDay(now)
	}
	if onPage(h) {
		h.Refresh()
	}
	if s.gen != gen {
		return tea.Batch(poll, cmd) // a break started or ended: its restart ticks
	}
	return tea.Batch(poll, cmd, s.tick(h))
}

// presence reports whether you're at the Mac, and how long you've been away. It reads the Mac's
// idle time when it has a fresh one, else crew's own keys and clicks while its window is in front.
func (s *Section) presence(h section.Host, now time.Time, dt time.Duration) (bool, time.Duration) {
	if s.whole(now) {
		return s.idle < activeIdle, s.idle
	}
	if !h.Blurred() && now.Sub(s.active) < awayAfter {
		s.away = 0
		return true, 0
	}
	s.away += dt
	return false, s.away
}

// whole reports whether screen time counts the whole Mac (its idle time was read lately).
func (s *Section) whole(now time.Time) bool {
	return !s.idleAt.IsZero() && now.Sub(s.idleAt) < time.Minute
}

// countTime adds dt to today while you're active, and to the eye and move counters. A pause as long
// as the break that's due counts as that break.
func (s *Section) countTime(h section.Host, now time.Time, dt time.Duration) {
	if s.day.Date != DateOf(now) { // past midnight: a new day
		s.saveDay(now)
		s.day = s.store.Day(DateOf(now))
	}
	active, away := s.presence(h, now, dt)
	if active {
		s.day.Active += dt
		s.eye += dt
		s.move += dt
	}
	if s.clock.Kind == "focus" {
		s.day.Focus += dt
	}
	if s.clock.Kind == "break" {
		return // the break counts when it ends
	}
	if away >= eyeRest {
		s.eye = 0
	}
	if away >= s.set.MoveFor && s.move > 0 {
		s.move = 0
		s.day.Breaks++
	}
}

// saveDay writes today.
func (s *Section) saveDay(now time.Time) {
	s.saved = now
	s.store.SaveDay(s.day) // ponytail: a failed write loses a minute of counting, nothing else
}

// checkClock ends a session or break whose time is up. A pomodoro starts a short or long break; one
// that ended long ago (crew was closed) just counts.
func (s *Section) checkClock(h section.Host, now time.Time) tea.Cmd {
	c := s.clock
	if !c.Running() || c.Flow() || now.Before(c.Ends) {
		return nil
	}
	stale := now.Sub(c.Ends) > time.Minute
	if c.Kind == "break" {
		s.endBreak(h, true)
		switch {
		case stale:
			s.closeScreen(h)
		case s.screen != "":
			s.screen = "back" // break done: start the next focus, or not yet
			notify.Send("Break over", "Ready? ⏎ starts the next focus session")
		default:
			notify.Send("Break over", keys.Names("⌘⇧F starts the next focus session"))
			h.Notice("Break over · ⌘⇧F starts the next focus session")
		}
		return s.restart(h)
	}
	s.day.Sessions++
	done, name, d := c.Done+1, "Short break", s.set.Break
	if done >= s.set.LongEvery {
		done, name, d = 0, "Long break", s.set.Long
	}
	s.clock = Clock{Done: done}
	s.store.SaveClock(s.clock)
	s.saveDay(now)
	if stale {
		return runShortcut(s.set.Off)
	}
	s.delays = 0
	return tea.Batch(runShortcut(s.set.Off), s.offerBreak(h, name, d, Dur(c.Ends.Sub(c.Start))+" of focus done"))
}

// checkBreaks starts a postponed break when it's time, a move break after a stretch without one
// (after the focus session, if one runs), and shows the eye break on the help line. Not during a break.
func (s *Section) checkBreaks(h section.Host, now time.Time) tea.Cmd {
	if s.clock.Kind == "break" {
		s.eye, s.move = 0, 0
		return nil
	}
	if s.later.Running() {
		if now.Before(s.later.Start) {
			return nil
		}
		return s.offerBreak(h, s.later.Label, s.later.Ends.Sub(s.later.Start), "5 more minutes are up")
	}
	if st := s.set; st.Eye > 0 && s.eye >= st.Eye {
		s.eye, s.eyeUntil = 0, now.Add(eyeRest)
	}
	if st := s.set; st.Move > 0 && s.move >= st.Move && s.clock.Kind != "focus" && s.screen == "" {
		s.delays = 0
		return s.offerBreak(h, "Move break", st.MoveFor, Dur(st.Move)+" at the Mac")
	}
	return nil
}

// moveDue reports whether a move break is due (it waits for the focus session to end).
func (s *Section) moveDue() bool { return s.set.Move > 0 && s.move >= s.set.Move }

// checkDay warns at 80% and 100% of the daily limit, and says to wrap up at the end of the day.
// Nothing is ever locked.
func (s *Section) checkDay(h section.Host, now time.Time) {
	d, lim := &s.day, s.set.Limit
	switch {
	case lim > 0 && d.Active >= lim && d.Warned < 100:
		d.Warned = 100
		notify.Send("Screen time", Dur(lim)+" at the Mac today · your limit")
		h.Notice("Screen time: " + Dur(lim) + " today, your limit")
	case lim > 0 && d.Active >= lim*8/10 && d.Warned < 80:
		d.Warned = 80
		notify.Send("Screen time", Dur(d.Active)+" of "+Dur(lim)+" today · "+Dur(lim-d.Active)+" left")
		h.Notice("Screen time: " + Dur(d.Active) + " of " + Dur(lim) + " today")
	}
	if end, ok := s.set.EndOfDayAt(now); ok && !now.Before(end) && !d.Wrapped {
		d.Wrapped = true
		notify.Send("Wrap up", "It's "+s.set.EndOfDay+" · time to finish for the day")
		h.Notice("It's " + s.set.EndOfDay + " · time to wrap up")
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

// startFocus starts a focus session of d; 0 is the settings' (a flow session in flow mode).
func (s *Section) startFocus(h section.Host, d time.Duration) tea.Cmd {
	if !h.SectionOn("focus") {
		h.Notice("FOCUS is off · f on home turns it on")
		return nil
	}
	if s.clock.Kind == "break" {
		s.endBreak(h, false)
	}
	s.closeScreen(h)
	now := time.Now()
	s.clock = Clock{Kind: "focus", Start: now, Done: s.clock.Done}
	switch {
	case d <= 0 && s.set.Mode == "flow":
		h.Notice("Flow session · ⌘⇧F stops it and starts a break")
	default:
		if d <= 0 {
			d = s.set.Focus
		}
		s.clock.Ends = now.Add(d)
		h.Notice("Focus for " + Dur(d) + " · ⌘⇧F stops it")
	}
	s.later = Clock{}
	s.store.SaveClock(s.clock)
	return tea.Batch(runShortcut(s.set.On), s.restart(h))
}

// stopClock ends the session or break early. A flow session counts and starts a break of a fifth
// of it; a pomodoro stopped early doesn't count; a break ended early is skipped.
func (s *Section) stopClock(h section.Host) tea.Cmd {
	c := s.clock
	if c.Kind == "break" {
		s.endBreak(h, false)
		s.closeScreen(h)
		h.Notice("Break skipped")
		return s.restart(h)
	}
	s.clock = Clock{Done: c.Done}
	s.store.SaveClock(s.clock)
	worked := time.Since(c.Start)
	if c.Flow() && worked >= time.Minute {
		s.day.Sessions++
		return tea.Batch(runShortcut(s.set.Off), s.startBreak(h, "Break", max(worked/5, time.Minute)))
	}
	h.Notice("Focus stopped after " + Dur(worked))
	return tea.Batch(runShortcut(s.set.Off), s.restart(h))
}

// toggle is ⌘⇧F: stop a session, or start one.
func (s *Section) toggle(h section.Host) tea.Cmd {
	if s.clock.Kind == "focus" {
		return s.stopClock(h)
	}
	return s.startFocus(h, 0)
}

// breakNow is b: a short break now, or the end of the one running.
func (s *Section) breakNow(h section.Host) tea.Cmd {
	if s.clock.Kind == "break" {
		return s.stopClock(h)
	}
	var off tea.Cmd
	if s.clock.Kind == "focus" {
		off = runShortcut(s.set.Off)
	}
	return tea.Batch(off, s.startBreak(h, "Break", s.set.Break))
}

// offerBreak is time for a break: on the break screen, it asks first (start it, or 5 more minutes);
// with the break screen off, the break just starts.
func (s *Section) offerBreak(h section.Host, name string, d time.Duration, why string) tea.Cmd {
	s.later = Clock{}
	if s.set.Screen == "off" {
		notify.Send(why, strings.ToLower(name)+" for "+Dur(d))
		return s.startBreak(h, name, d)
	}
	now := time.Now()
	s.due, s.why, s.screen = Clock{Kind: "break", Label: name, Start: now, Ends: now.Add(d)}, why, "ask"
	s.openScreen(h)
	notify.Send("Time for a break", why+" · "+strings.ToLower(name)+" for "+Dur(d))
	return s.restart(h)
}

// startBreak starts a break called name for d, counting down on the break screen when it's on.
func (s *Section) startBreak(h section.Host, name string, d time.Duration) tea.Cmd {
	now := time.Now()
	s.clock = Clock{Kind: "break", Label: name, Start: now, Ends: now.Add(d), Done: s.clock.Done}
	s.later, s.move, s.eye, s.sure = Clock{}, 0, 0, false
	s.store.SaveClock(s.clock)
	h.Notice(name + " · " + Dur(d) + " · " + advice(name))
	if s.set.Screen != "off" {
		s.screen = "break"
		s.openScreen(h)
	} else {
		notify.Send(name, Dur(d)+" · "+advice(name))
	}
	return s.restart(h)
}

// advice is what to do on a break.
func advice(name string) string {
	switch name {
	case "Move break":
		return "Stand up · walk a little · look far away"
	case "Long break":
		return "Get up · get some water · go outside if you can"
	}
	return "Stand up · stretch · look far away"
}

// endBreak ends the break running, as taken or skipped. The break screen stays for its last step.
func (s *Section) endBreak(h section.Host, taken bool) {
	if taken {
		s.day.Breaks++
	} else {
		s.day.Skipped++
	}
	s.clock = Clock{Done: s.clock.Done}
	s.store.SaveClock(s.clock)
	s.saveDay(time.Now())
}

// openScreen shows the break screen as FOCUS's popup, and with Screen "full" has crew's app bring
// its window over every app.
func (s *Section) openScreen(h section.Host) {
	h.SectionModal(s)
	if s.set.Screen == "full" && !s.raised {
		s.raised = true
		notify.Window("front")
	}
}

// closeScreen closes the break screen (back to the settings form, if that was open) and puts crew's
// window back as it was.
func (s *Section) closeScreen(h section.Host) {
	if s.raised {
		s.raised = false
		notify.Window("normal")
	}
	if s.screen == "" {
		return
	}
	s.screen, s.sure = "", false
	if s.form == nil {
		h.SectionModal(nil)
	}
}

// ModalOpen reports whether the break screen or the settings form is open.
func (s *Section) ModalOpen(section.Host) bool { return s.screen != "" || s.form != nil }

// ModalView draws the break screen's step over the main pane, or the settings form.
func (s *Section) ModalView(h section.Host) string {
	if s.screen == "" {
		return s.form.View()
	}
	w, ht := h.MainSize()
	now := time.Now()
	barW := min(40, w-10)
	var body string
	switch s.screen {
	case "ask":
		d := s.due.Ends.Sub(s.due.Start)
		more := look.Dim.Render("[ ") + "5 more minutes " + look.Dim.Render("p ]")
		if s.delays >= maxDelays {
			more = look.Dim.Render("no more delays")
		}
		body = look.Running.Render("●  "+s.why) + "\n\n" +
			"Time for a " + Dur(d) + " " + strings.ToLower(s.due.Label) + ": " + strings.ToLower(advice(s.due.Label)[:1]) + advice(s.due.Label)[1:] + ".\n\n\n" +
			look.Active.Render("[ Start the break ⏎ ]") + "    " + more
		if s.delays < maxDelays {
			n := maxDelays - s.delays
			body += "\n\n" + look.Dim.Render(fmt.Sprintf("you can put it off %d more time%s", n, look.Plural(n)))
		}
	case "break":
		c := s.clock
		keys := look.KeyHints("e end early")
		if s.sure {
			keys = look.Warn.Render("End the break early? y ends it · any other key keeps it")
		}
		body = look.Running.Render("◐  "+c.Label+"  "+Countdown(c.Left(now))) + "\n\n" +
			advice(c.Label) + "\n\n" +
			progress(c.Ends.Sub(c.Start)-c.Left(now), c.Ends.Sub(c.Start), barW) + "\n\n\n" + keys
	case "back":
		body = look.Ready.Render("✓  Break done") + "\n\n" + "Ready for the next one?" + "\n\n\n" +
			look.Active.Render("[ Start the next focus ⏎ ]") + "    " + look.Dim.Render("[ ") + "Not yet " + look.Dim.Render("esc ]")
	}
	// The box's border and padding take 4 lines; it fills the main pane inside its frame.
	body = lipgloss.PlaceVertical(max(ht-6, lipgloss.Height(body)), lipgloss.Center, body)
	return look.MenuBox.Width(max(w-4, 20)).Align(lipgloss.Center).Render(body)
}

// ModalKey takes the break screen's keys (⏎ and p when asked; e then y to end a break early; ⏎ or
// esc when it's done), else hands them to the form.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch s.screen {
	case "ask":
		switch k {
		case "enter":
			return s.startBreak(h, s.due.Label, s.due.Ends.Sub(s.due.Start))
		case "p":
			if s.delays >= maxDelays {
				h.Notice("No more delays · ⏎ starts the break")
				return nil
			}
			s.delays++
			start := time.Now().Add(postpone)
			s.later = Clock{Kind: "break", Label: s.due.Label, Start: start, Ends: start.Add(s.due.Ends.Sub(s.due.Start))}
			s.closeScreen(h)
			h.Notice(s.due.Label + " in " + Dur(postpone))
			return s.restart(h)
		}
		return nil
	case "break":
		switch {
		case s.sure && k == "y":
			s.endBreak(h, false)
			s.screen, s.sure = "back", false
			return s.restart(h)
		case k == "e":
			s.sure = true
		default:
			s.sure = false
		}
		return nil
	case "back":
		switch k {
		case "enter":
			return s.startFocus(h, 0)
		case "esc":
			s.closeScreen(h)
			h.Notice(keys.Names("Welcome back · ⌘⇧F starts the next focus session"))
		}
		return nil
	}
	cmd, saved, closed := s.form.Key(msg)
	if saved != nil {
		s.set = saved.fix()
		if err := s.store.SaveSettings(s.set); err != nil {
			h.Notice("Couldn't save FOCUS's settings: " + err.Error())
		} else {
			h.Notice("FOCUS settings saved")
		}
	}
	if closed {
		s.form = nil
		h.SectionModal(nil)
		h.Refresh()
	}
	return cmd
}

// Commands is /focus [minutes]: start a focus session, or stop the one running.
func (s *Section) Commands() []section.Command {
	return []section.Command{{Name: "/focus", Help: "start a focus session (/focus 50 for 50 minutes), or stop it",
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
			cmd := s.startFocus(h, time.Duration(mins)*time.Minute)
			h.Say(t.ID, chat.Tool, h.Note())
			return cmd
		}}}
}

// Actions are ⌘⇧F (focus) and a break, for the ⌘ shortcuts.
func (s *Section) Actions() []section.Action {
	return []section.Action{
		{Name: "focus", Help: "start a focus session, or stop the one running (FOCUS)", Run: s.toggle},
		{Name: "break", Help: "start a break now, or end the one running (FOCUS)", Run: s.breakNow},
	}
}
