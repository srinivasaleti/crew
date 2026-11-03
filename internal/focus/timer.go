package focus

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"crew/internal/notify"
)

// FOCUS paces your day at the Mac (docs/focus-prd.md): a focus timer (pomodoro or flow) that starts a
// break when it ends, a move break every 30 minutes, quiet eye breaks, and your screen time across the
// whole Mac, read from macOS's idle time, with a daily limit and an end of day. Timer is all of that:
// a UI ticks it (Tick), hands it your actions, and draws its fields. The data is in ~/.crew/focus.
//
// The break screen goes in three steps: time's up (start the break, or keep going, twice at most), the
// break counting down (ending it early asks first), then break done (start the next focus, or not
// yet). Set to "full", crew's app fills the screen with crew's window, above every app, for those
// steps (notify.Window); "crew" keeps them in crew's window; "off" only notifies.

const (
	// activeIdle is how long without input before the Mac counts you as away.
	activeIdle = time.Minute
	// awayAfter is the same without the Mac's idle time (not a Mac, or ioreg failed): how long
	// without a key or click in crew's window before crew stops counting you.
	awayAfter = 5 * time.Minute
	// EyeRest is how long an eye break lasts, and a pause long enough to count as one.
	EyeRest = 20 * time.Second
	// MaxExtra is how many times a focus session can be extended, or a move break put off.
	MaxExtra = 2
	// AutoBreak is how long time's up waits for an answer before the break starts (Auto).
	AutoBreak = 30 * time.Second
	// AutoFocus is how long after a break, with you back at the Mac, the next focus starts (Auto).
	AutoFocus = 15 * time.Second
	// HeadsUp is how long before a focus session ends that FOCUS says so, so you can extend it in time.
	HeadsUp = 2 * time.Minute
)

// Extras are how much "keep going" adds on time's up (1, 2, 3); ⌘⇧E adds the middle one.
var Extras = []time.Duration{5 * time.Minute, 15 * time.Minute, 25 * time.Minute}

// Env is what the timer needs from the UI running it.
type Env interface {
	On() bool            // FOCUS is turned on
	Blurred() bool       // crew's window isn't in front (for presence, when the Mac's idle time isn't known)
	Notice(text string)  // a short note for you (the terminal's help line)
	Screen(open bool)    // the break screen opens (time's up, a break) or closes
	Names(string) string // shortcut names (⌘⇧F) as you've bound them
}

// Timer is FOCUS's state. Its exported fields are for drawing; change it only with its methods. A
// method that returns true changed what's counting down, so a UI ticking slowly should tick again now.
type Timer struct {
	Set       Settings
	Store     Store
	Clock     Clock
	Day       Day
	Last      time.Time     // the last tick
	Active    time.Time     // your last key or click in crew (when the Mac's idle time isn't known)
	Idle      time.Duration // the Mac's idle time, as last read
	IdleAt    time.Time     // when it was read; zero when it couldn't be
	Eye, Move time.Duration // active time since the last eye and move break
	EyeUntil  time.Time     // the eye break shows until then
	Later     Clock         // a postponed break; it's offered again at Later.Start
	Screen    string        // the break screen's step: "ask", "break" or "back"; "" when it's closed
	Due       Clock         // the break "ask" offers (its Label and length)
	Why       string        // what "ask" says the break is for: "25m of focus done", "30m at the Mac"
	Delays    int           // how many times the move break offered was put off
	Ending    bool          // the break offered ends a focus session, which keeps going if you extend it
	Asked     time.Time     // when time's up was asked (Auto starts the break after AutoBreak)
	BackAt    time.Time     // since when break done has had you at the Mac (Auto starts focus after AutoFocus)
	Sure      bool          // "end early" was asked once on the break (the terminal's e, then y)
	away      time.Duration // time since you were last active in crew (no idle time)
	polled    time.Time     // when the idle time was last asked for
	polling   bool          // a read is on its way
	warned    bool          // the heads-up before the session's end was given
	opened    bool          // the first tick ran (OnOpen starts a session then)
	raised    bool          // crew's window was brought over every app (Screen "full")
	saved     time.Time     // when the day was last written
}

// New is FOCUS from its folder, as it was left.
func New(dir string) *Timer {
	st := Store{Dir: dir}
	now := time.Now()
	return &Timer{Set: st.Settings(), Store: st, Clock: st.Clock(), Day: st.Day(DateOf(now)), Active: now}
}

// Every is how often to tick: every second while something counts down, else every 15.
func (t *Timer) Every(now time.Time) time.Duration {
	if t.Clock.Running() || t.Screen != "" || now.Before(t.EyeUntil) {
		return time.Second
	}
	return 15 * time.Second
}

// PollIdle reports whether it's time to read the Mac's idle time (ReadIdle, then SetIdle).
func (t *Timer) PollIdle(now time.Time) bool {
	if t.polling || now.Sub(t.polled) < 10*time.Second {
		return false
	}
	t.polled, t.polling = now, true
	return true
}

// SetIdle takes the Mac's idle time as read at now.
func (t *Timer) SetIdle(idle time.Duration, ok bool, now time.Time) {
	t.polling, t.Idle, t.IdleAt = false, idle, time.Time{}
	if ok {
		t.IdleAt = now
	}
}

// Tick counts the time since the last tick and fires what's due.
func (t *Timer) Tick(e Env, now time.Time) {
	dt := now.Sub(t.Last)
	if t.Last.IsZero() || dt > time.Minute {
		dt = 0 // the Mac slept, or this is the first tick
	}
	t.Last = now
	if !t.opened {
		t.opened = true
		if t.Set.OnOpen && !t.Clock.Running() && t.Screen == "" {
			t.StartFocus(e, 0) // opening crew starts a session
			return
		}
	}
	t.countTime(e, now, dt)
	t.headsUp(e, now)
	if !t.auto(e, now) && !t.checkClock(e, now) {
		t.checkBreaks(e, now)
	}
	t.checkDay(e, now)
	if now.Sub(t.saved) >= time.Minute {
		t.saveDay(now)
	}
}

// presence reports whether you're at the Mac, and how long you've been away. It reads the Mac's
// idle time when it has a fresh one, else crew's own keys and clicks while its window is in front.
func (t *Timer) presence(e Env, now time.Time, dt time.Duration) (bool, time.Duration) {
	if t.Whole(now) {
		return t.Idle < activeIdle, t.Idle
	}
	if !e.Blurred() && now.Sub(t.Active) < awayAfter {
		t.away = 0
		return true, 0
	}
	t.away += dt
	return false, t.away
}

// Whole reports whether screen time counts the whole Mac (its idle time was read lately).
func (t *Timer) Whole(now time.Time) bool {
	return !t.IdleAt.IsZero() && now.Sub(t.IdleAt) < time.Minute
}

// countTime adds dt to today while you're active, and to the eye and move counters. A pause as long
// as the break that's due counts as that break.
func (t *Timer) countTime(e Env, now time.Time, dt time.Duration) {
	if t.Day.Date != DateOf(now) { // past midnight: a new day
		t.saveDay(now)
		t.Day = t.Store.Day(DateOf(now))
	}
	active, away := t.presence(e, now, dt)
	if active {
		t.Day.Active += dt
		t.Eye += dt
		t.Move += dt
	}
	if c := t.Clock; c.Kind == "focus" && (c.Flow() || now.Before(c.Ends)) { // not while time's up waits
		t.Day.Focus += dt
	}
	if t.Clock.Kind == "break" {
		return // the break counts when it ends
	}
	if away >= EyeRest {
		t.Eye = 0
	}
	if away >= t.Set.MoveFor && t.Move > 0 {
		t.Move = 0
		t.Day.Breaks++
	}
}

// saveDay writes today.
func (t *Timer) saveDay(now time.Time) {
	t.saved = now
	t.Store.SaveDay(t.Day) // ponytail: a failed write loses a minute of counting, nothing else
}

// checkClock ends a session or break whose time is up. A pomodoro starts a short or long break; one
// that ended long ago (crew was closed) just counts.
func (t *Timer) checkClock(e Env, now time.Time) bool {
	c := t.Clock
	if !c.Running() || c.Flow() || now.Before(c.Ends) {
		return false
	}
	stale := now.Sub(c.Ends) > time.Minute
	if c.Kind == "break" {
		t.endBreak(true)
		switch {
		case stale:
			t.closeScreen(e)
		case t.Screen != "":
			t.Screen, t.BackAt = "back", now // break done: start the next focus, or not yet
			notify.Send("Break over", "Ready? ⏎ starts the next focus session")
		default:
			notify.Send("Break over", e.Names("⌘⇧F starts the next focus session"))
			e.Notice("Break over · ⌘⇧F starts the next focus session")
		}
		return true
	}
	if t.Screen == "ask" {
		return false // time's up waits for your answer (or Auto)
	}
	name, d := t.nextBreak()
	why := Dur(c.Ends.Sub(c.Start)) + " of focus done"
	if stale || t.Set.Screen == "off" {
		t.finishFocus()
		runShortcut(t.Set.Off)
		if stale {
			return true
		}
		return t.offerBreak(e, name, d, why)
	}
	t.Ending = true // the session stays on, so extending it carries on with it
	return t.offerBreak(e, name, d, why)
}

// nextBreak is the break the focus session running ends with: a long one every LongEvery sessions.
func (t *Timer) nextBreak() (string, time.Duration) {
	if t.Clock.Done+1 >= t.Set.LongEvery {
		return "Long break", t.Set.Long
	}
	return "Short break", t.Set.Break
}

// finishFocus counts the focus session as done and stops its clock.
func (t *Timer) finishFocus() {
	t.Day.Sessions++
	done := t.Clock.Done + 1
	if done >= t.Set.LongEvery {
		done = 0
	}
	t.Clock, t.Ending = Clock{Done: done}, false
	t.Store.SaveClock(t.Clock)
	t.saveDay(time.Now())
}

// TakeBreak starts the break time's up offered, first counting the focus session it ends.
func (t *Timer) TakeBreak(e Env) bool {
	if !t.Ending {
		return t.startBreak(e, t.Due.Label, t.Due.Ends.Sub(t.Due.Start))
	}
	name, d := t.nextBreak()
	t.finishFocus()
	runShortcut(t.Set.Off)
	return t.startBreak(e, name, d)
}

// Extend adds d: to the focus session at its end (time's up closes, the session carries on), to the
// one running (⌘⇧E), or puts off the move break offered. Twice at most each.
func (t *Timer) Extend(e Env, d time.Duration) bool {
	now := time.Now()
	c := &t.Clock
	switch {
	case t.Screen == "ask" && !t.Ending:
		if t.Delays >= MaxExtra {
			e.Notice("No more delays · ⏎ starts the break")
			return false
		}
		t.Delays++
		start := now.Add(d)
		t.Later = Clock{Kind: "break", Label: t.Due.Label, Start: start, Ends: start.Add(t.Due.Ends.Sub(t.Due.Start))}
		t.closeScreen(e)
		e.Notice(t.Due.Label + " in " + Dur(d))
		return true
	case c.Kind != "focus":
		e.Notice(e.Names("No focus session to extend · ⌘⇧F starts one"))
		return false
	case c.Flow():
		e.Notice("A flow session has no end to extend · ⌘⇧F stops it when you're done")
		return false
	case c.Extra >= MaxExtra:
		e.Notice("No more extensions · take the break first")
		return false
	}
	c.Extra++
	if t.Screen == "ask" { // time's up: the session carries on from now
		c.Ends, t.Ending = now.Add(d), false
		t.closeScreen(e)
	} else {
		c.Ends = c.Ends.Add(d)
	}
	t.warned = false
	t.Store.SaveClock(*c)
	left := MaxExtra - c.Extra
	e.Notice(fmt.Sprintf("Focus +%s · until %s · %d more extension%s", Dur(d), c.Ends.Format("3:04pm"), left, plural(left)))
	return true
}

// ExtrasLeft is how many more times "keep going" works on the break screen's time's up.
func (t *Timer) ExtrasLeft() int {
	if t.Ending {
		return MaxExtra - t.Clock.Extra
	}
	return MaxExtra - t.Delays
}

// Nudge moves the end of the session or break running by d (the page's +/− 5 min), leaving at least
// a minute.
func (t *Timer) Nudge(d time.Duration) {
	c := &t.Clock
	if !c.Running() || c.Flow() {
		return
	}
	c.Ends = c.Ends.Add(d)
	if soon := time.Now().Add(time.Minute); c.Ends.Before(soon) {
		c.Ends = soon
	}
	t.Store.SaveClock(*c)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// headsUp says a focus session ends soon, while it can still be extended.
func (t *Timer) headsUp(e Env, now time.Time) {
	c := t.Clock
	if c.Kind != "focus" || c.Flow() || t.warned || c.Left(now) > HeadsUp || c.Left(now) <= 0 {
		return
	}
	t.warned = true
	if c.Extra < MaxExtra {
		notify.Send(Dur(c.Left(now))+" of focus left", e.Names("⌘⇧E adds 15 min to this session"))
	}
}

// Here reports whether you're at the Mac now: its idle time when known, else crew's own keys.
func (t *Timer) Here(e Env, now time.Time) bool {
	if t.Whole(now) {
		return t.Idle < activeIdle
	}
	return !e.Blurred() && now.Sub(t.Active) < awayAfter
}

// auto runs the cycle on its own (Auto): the break when time's up goes unanswered, and the next focus
// once a break is done and you've been back a little while.
func (t *Timer) auto(e Env, now time.Time) bool {
	if !t.Set.Auto {
		return false
	}
	switch {
	case t.Screen == "ask" && now.Sub(t.Asked) >= AutoBreak:
		return t.TakeBreak(e)
	case t.Screen == "back" && !t.Here(e, now):
		t.BackAt = now // it counts from when you're back
	case t.Screen == "back" && now.Sub(t.BackAt) >= AutoFocus:
		return t.StartFocus(e, 0)
	}
	return false
}

// checkBreaks starts a postponed break when it's time, a move break after a stretch without one
// (after the focus session, if one runs), and shows the eye break. Not during a break.
func (t *Timer) checkBreaks(e Env, now time.Time) bool {
	if t.Clock.Kind == "break" {
		t.Eye, t.Move = 0, 0
		return false
	}
	if t.Later.Running() {
		if now.Before(t.Later.Start) || t.Screen != "" {
			return false
		}
		return t.offerBreak(e, t.Later.Label, t.Later.Ends.Sub(t.Later.Start), "Your extra time is up")
	}
	if st := t.Set; st.Eye > 0 && t.Eye >= st.Eye {
		t.Eye, t.EyeUntil = 0, now.Add(EyeRest)
	}
	if st := t.Set; st.Move > 0 && t.Move >= st.Move && t.Clock.Kind != "focus" && t.Screen == "" {
		t.Delays = 0
		return t.offerBreak(e, "Move break", st.MoveFor, Dur(st.Move)+" at the Mac")
	}
	return false
}

// MoveDue reports whether a move break is due (it waits for the focus session to end).
func (t *Timer) MoveDue() bool { return t.Set.Move > 0 && t.Move >= t.Set.Move }

// checkDay warns at 80% and 100% of the daily limit, and says to wrap up at the end of the day.
// Nothing is ever locked.
func (t *Timer) checkDay(e Env, now time.Time) {
	d, lim := &t.Day, t.Set.Limit
	switch {
	case lim > 0 && d.Active >= lim && d.Warned < 100:
		d.Warned = 100
		notify.Send("Screen time", Dur(lim)+" at the Mac today · your limit")
		e.Notice("Screen time: " + Dur(lim) + " today, your limit")
	case lim > 0 && d.Active >= lim*8/10 && d.Warned < 80:
		d.Warned = 80
		notify.Send("Screen time", Dur(d.Active)+" of "+Dur(lim)+" today · "+Dur(lim-d.Active)+" left")
		e.Notice("Screen time: " + Dur(d.Active) + " of " + Dur(lim) + " today")
	}
	if end, ok := t.Set.EndOfDayAt(now); ok && !now.Before(end) && !d.Wrapped {
		d.Wrapped = true
		notify.Send("Wrap up", "It's "+t.Set.EndOfDay+" · time to finish for the day")
		e.Notice("It's " + t.Set.EndOfDay + " · time to wrap up")
	}
}

// runShortcut runs a macOS Shortcut by name (Do Not Disturb on or off), in the background.
func runShortcut(name string) {
	if name == "" {
		return
	}
	go exec.Command("shortcuts", "run", name).Run() // ponytail: a failed Shortcut is silent; run it once in Shortcuts to check
}

// StartFocus starts a focus session of d; 0 is the settings' (a flow session in flow mode).
func (t *Timer) StartFocus(e Env, d time.Duration) bool {
	if !e.On() {
		e.Notice("FOCUS is off · f on home turns it on")
		return false
	}
	if t.Clock.Kind == "break" {
		t.endBreak(false)
	}
	if t.Ending { // time's up was open: that session ran its full time
		t.finishFocus()
	}
	t.closeScreen(e)
	now := time.Now()
	t.Clock, t.warned, t.Delays = Clock{Kind: "focus", Start: now, Done: t.Clock.Done}, false, 0
	switch {
	case d <= 0 && t.Set.Mode == "flow":
		e.Notice("Flow session · ⌘⇧F stops it and starts a break")
	default:
		if d <= 0 {
			d = t.Set.Focus
		}
		t.Clock.Ends = now.Add(d)
		e.Notice("Focus for " + Dur(d) + " · ⌘⇧F stops it")
	}
	t.Later = Clock{}
	t.Store.SaveClock(t.Clock)
	runShortcut(t.Set.On)
	return true
}

// StopClock ends the session or break early. A flow session counts and starts a break of a fifth
// of it; a pomodoro stopped early doesn't count; a break ended early is skipped.
func (t *Timer) StopClock(e Env) bool {
	c := t.Clock
	if t.Ending { // time's up was open: the session ran its full time
		t.finishFocus()
		t.closeScreen(e)
		e.Notice(e.Names("Focus done · ⌘⇧F starts the next one"))
		runShortcut(t.Set.Off)
		return true
	}
	if c.Kind == "break" {
		t.endBreak(false)
		t.closeScreen(e)
		e.Notice("Break skipped")
		return true
	}
	t.Clock = Clock{Done: c.Done}
	t.Store.SaveClock(t.Clock)
	worked := time.Since(c.Start)
	runShortcut(t.Set.Off)
	if c.Flow() && worked >= time.Minute {
		t.Day.Sessions++
		return t.startBreak(e, "Break", max(worked/5, time.Minute))
	}
	e.Notice("Focus stopped after " + Dur(worked))
	return true
}

// Toggle is ⌘⇧F: stop a session, or start one.
func (t *Timer) Toggle(e Env) bool {
	if t.Clock.Kind == "focus" {
		return t.StopClock(e)
	}
	return t.StartFocus(e, 0)
}

// BreakNow is a short break now, or the end of the one running.
func (t *Timer) BreakNow(e Env) bool {
	if t.Clock.Kind == "break" {
		return t.StopClock(e)
	}
	if t.Ending { // time's up was open: its break
		return t.TakeBreak(e)
	}
	if t.Clock.Kind == "focus" {
		runShortcut(t.Set.Off)
	}
	return t.startBreak(e, "Break", t.Set.Break)
}

// offerBreak is time for a break: on the break screen, it asks first (start it, or keep going);
// with the break screen off, the break just starts.
func (t *Timer) offerBreak(e Env, name string, d time.Duration, why string) bool {
	t.Later = Clock{}
	if t.Set.Screen == "off" {
		notify.Send(why, strings.ToLower(name)+" for "+Dur(d))
		return t.startBreak(e, name, d)
	}
	now := time.Now()
	t.Due, t.Why, t.Screen, t.Asked = Clock{Kind: "break", Label: name, Start: now, Ends: now.Add(d)}, why, "ask", now
	t.openScreen(e)
	notify.Send("Time for a break", why+" · "+strings.ToLower(name)+" for "+Dur(d))
	return true
}

// startBreak starts a break called name for d, counting down on the break screen when it's on.
func (t *Timer) startBreak(e Env, name string, d time.Duration) bool {
	now := time.Now()
	t.Clock = Clock{Kind: "break", Label: name, Start: now, Ends: now.Add(d), Done: t.Clock.Done}
	t.Later, t.Move, t.Eye, t.Sure = Clock{}, 0, 0, false
	t.Store.SaveClock(t.Clock)
	e.Notice(name + " · " + Dur(d) + " · " + Advice(name))
	if t.Set.Screen != "off" {
		t.Screen = "break"
		t.openScreen(e)
	} else {
		notify.Send(name, Dur(d)+" · "+Advice(name))
	}
	return true
}

// Advice is what to do on a break.
func Advice(name string) string {
	switch name {
	case "Move break":
		return "Stand up · walk a little · look far away"
	case "Long break":
		return "Get up · get some water · go outside if you can"
	}
	return "Stand up · stretch · look far away"
}

// endBreak ends the break running, as taken or skipped. The break screen stays for its last step.
func (t *Timer) endBreak(taken bool) {
	if taken {
		t.Day.Breaks++
	} else {
		t.Day.Skipped++
	}
	t.Clock = Clock{Done: t.Clock.Done}
	t.Store.SaveClock(t.Clock)
	t.saveDay(time.Now())
}

// EndEarly ends the break running before its time (it counts as skipped) and goes to break done.
func (t *Timer) EndEarly() bool {
	t.endBreak(false)
	t.Screen, t.Sure, t.BackAt = "back", false, time.Now()
	return true
}

// NotYet closes break done without starting the next focus.
func (t *Timer) NotYet(e Env) {
	t.closeScreen(e)
	e.Notice(e.Names("Welcome back · ⌘⇧F starts the next focus session"))
}

// SaveSettings keeps new settings, made usable first.
func (t *Timer) SaveSettings(st Settings) error {
	t.Set = st.Fix()
	return t.Store.SaveSettings(t.Set)
}

// openScreen shows the break screen, and with Screen "full" has crew's app bring its window over
// every app.
func (t *Timer) openScreen(e Env) {
	e.Screen(true)
	if t.Set.Screen == "full" && !t.raised {
		t.raised = true
		notify.Window("front")
	}
}

// closeScreen closes the break screen and puts crew's window back as it was.
func (t *Timer) closeScreen(e Env) {
	if t.raised {
		t.raised = false
		notify.Window("normal")
	}
	if t.Screen == "" {
		return
	}
	t.Screen, t.Sure = "", false
	e.Screen(false)
}
