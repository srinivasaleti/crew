package app

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/focus"
	"crew/internal/notify"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// FOCUS paces your day (docs/focus-prd.md): a focus timer (⌘⇧F, /focus) that starts a break when it
// ends, eye breaks every 20 minutes and a stand-up nudge after an hour, and your screen time: how long crew was in front while you were active, split by what you were
// in, and how long agents worked. A tick counts time and fires these; the data is the focus
// package's, in ~/.crew/focus. It's a section: f on home turns all of it off.

// focusState is FOCUS's state on the model.
type focusState struct {
	set      focus.Settings
	store    focus.Store
	clock    focus.Clock
	day      focus.Day
	last     time.Time     // the last tick
	active   time.Time     // your last key or mouse use
	eye, sit time.Duration // time in crew since the last eye break, and without a pause
	away     time.Duration // time since you were last active
	eyeUntil time.Time     // the eye break shows on the help line until then
	saved    time.Time     // when the day was last written
	gen      int           // the ticker's generation; a tick from an older one is dropped
}

// awayAfter is how long without a key or the mouse before crew stops counting you as there, and
// a pause long enough to reset the stand-up nudge.
const awayAfter = 5 * time.Minute

func newFocus(dir string) *focusState {
	st := focus.Store{Dir: dir}
	now := time.Now()
	return &focusState{set: focus.LoadSettings(), store: st, clock: st.Clock(), day: st.Day(focus.DateOf(now)), active: now}
}

type focusTickMsg struct{ gen int }

// focusTick schedules the next tick: every second while a countdown is on screen, else every 15.
func (m *Model) focusTick() tea.Cmd {
	if !m.sectionOn("focus") {
		return nil // turning FOCUS on starts it again (restartFocusTick)
	}
	gen, every := m.fc.gen, 15*time.Second
	if m.fc.clock.Running() || time.Now().Before(m.fc.eyeUntil) {
		every = time.Second
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return focusTickMsg{gen} })
}

// restartFocusTick starts a new ticker now (the old one's ticks are dropped), so a countdown you
// just started shows at once.
func (m *Model) restartFocusTick() tea.Cmd {
	m.fc.gen++
	m.fc.last = time.Now()
	return m.focusTick()
}

// onFocusTick counts the time since the last tick and fires what's due.
func (m *Model) onFocusTick(msg focusTickMsg) tea.Cmd {
	if msg.gen != m.fc.gen || !m.sectionOn("focus") {
		return nil
	}
	now := time.Now()
	dt := now.Sub(m.fc.last)
	if m.fc.last.IsZero() || dt > time.Minute {
		dt = 0 // the Mac slept, or this is the first tick
	}
	m.fc.last = now
	m.countTime(now, dt)
	cmd := m.checkClock(now)
	m.checkBreaks(now)
	if now.Sub(m.fc.saved) >= time.Minute {
		m.saveDay(now)
	}
	if m.onFocusPage() {
		m.refresh()
	}
	return tea.Batch(cmd, m.focusTick())
}

// countTime adds dt to today: to crew and what you're in when its window is in front and you've been
// active lately, and to agents while any of them works.
func (m *Model) countTime(now time.Time, dt time.Duration) {
	fc := &m.fc.day
	if fc.Date != focus.DateOf(now) { // past midnight: a new day
		m.saveDay(now)
		m.fc.day = m.fc.store.Day(focus.DateOf(now))
		fc = &m.fc.day
	}
	if !m.blurred && now.Sub(m.fc.active) < awayAfter {
		fc.Crew += dt
		fc.By[m.whereNow()] += dt
		m.fc.eye += dt
		m.fc.sit += dt
		m.fc.away = 0
	} else if m.fc.away += dt; m.fc.away >= awayAfter {
		m.fc.eye, m.fc.sit = 0, 0 // a real pause rests your eyes and legs
	}
	if m.fc.clock.Kind == "focus" {
		fc.Focus += dt
	}
	for _, c := range m.chat.All() {
		if c.Busy {
			fc.Agents += dt
			break
		}
	}
}

// saveDay writes today.
func (m *Model) saveDay(now time.Time) {
	m.fc.saved = now
	m.fc.store.SaveDay(m.fc.day) // ponytail: a failed write loses a minute of counting, nothing else
}

// whereNow is what screen time counts you as in: the task (by title) or the section.
func (m Model) whereNow() string {
	r := m.selRow()
	switch {
	case m.current() != nil:
		return m.current().Title
	case r.Kind == rowPR:
		return "PULL REQUESTS"
	case r.Kind == rowReq || r.Kind == rowApp:
		return "REQUESTS"
	case r.Item() && m.owner(r) != nil:
		return m.owner(r).Def().Label
	case r.Kind == rowGroup:
		return r.Label
	}
	return "home"
}

// checkClock ends a session or break whose time is up. A focus session starts a break; one that
// ended long ago (crew was closed) just counts.
func (m *Model) checkClock(now time.Time) tea.Cmd {
	c := m.fc.clock
	if !c.Running() || now.Before(c.Ends) {
		return nil
	}
	stale := now.Sub(c.Ends) > time.Minute
	m.fc.clock = focus.Clock{}
	if c.Kind == "focus" {
		m.fc.day.Sessions++
		if !stale {
			m.fc.clock = focus.Clock{Kind: "break", Start: now, Ends: now.Add(m.fc.set.Break)}
			body := focus.Dur(c.Ends.Sub(c.Start)) + " of focus" + labelled(c.Label) + " · a " + focus.Dur(m.fc.set.Break) + " break starts now"
			notify.Send("Focus done", body)
			m.notice = "Focus done · " + focus.Dur(m.fc.set.Break) + " break · stand up, look away"
		}
	} else if !stale {
		notify.Send("Break over", "⌘⇧F starts the next focus session")
		m.notice = "Break over · ⌘⇧F starts the next focus session"
	}
	m.fc.store.SaveClock(m.fc.clock)
	m.saveDay(now)
	if c.Kind == "focus" {
		return m.runShortcut(m.fc.set.Off)
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
func (m *Model) checkBreaks(now time.Time) {
	if m.fc.clock.Kind == "break" {
		m.fc.eye, m.fc.sit = 0, 0
		return
	}
	if s := m.fc.set; s.Eye > 0 && m.fc.eye >= s.Eye {
		m.fc.eye, m.fc.eyeUntil = 0, now.Add(20*time.Second)
		notify.Send("Eye break", "Look at something 20 feet away for 20 seconds")
	}
	if s := m.fc.set; s.Sit > 0 && m.fc.sit >= s.Sit {
		m.fc.sit = 0
		notify.Send("Time to stand up", focus.Dur(s.Sit)+" in crew without a pause · stretch, or b on FOCUS starts a break")
		m.notice = "Time to stand up and stretch · " + focus.Dur(s.Sit) + " without a pause"
	}
}

// runShortcut runs a macOS Shortcut by name (Do Not Disturb on or off), in the background.
func (m *Model) runShortcut(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	return func() tea.Msg {
		exec.Command("shortcuts", "run", name).Run() // ponytail: a failed Shortcut is silent; run it once in Shortcuts to check
		return nil
	}
}

// startFocus starts a focus session of d (0: the default), named after label.
func (m *Model) startFocus(d time.Duration, label string) tea.Cmd {
	if !m.sectionOn("focus") {
		m.notice = "FOCUS is off · f on home turns it on"
		return nil
	}
	if d <= 0 {
		d = m.fc.set.Focus
	}
	now := time.Now()
	m.fc.clock = focus.Clock{Kind: "focus", Label: label, Start: now, Ends: now.Add(d)}
	m.fc.store.SaveClock(m.fc.clock)
	m.notice = "Focus for " + focus.Dur(d) + labelled(label) + " · ⌘⇧F stops it"
	return tea.Batch(m.runShortcut(m.fc.set.On), m.restartFocusTick())
}

// stopClock ends the session or break early.
func (m *Model) stopClock() tea.Cmd {
	c := m.fc.clock
	m.fc.clock = focus.Clock{}
	m.fc.store.SaveClock(m.fc.clock)
	if c.Kind != "focus" {
		m.notice = "Break ended"
		return m.restartFocusTick()
	}
	m.notice = "Focus stopped after " + focus.Dur(time.Since(c.Start))
	return tea.Batch(m.runShortcut(m.fc.set.Off), m.restartFocusTick())
}

// toggleFocus is ⌘⇧F: stop a session, or start one on the task you're in.
func (m *Model) toggleFocus() tea.Cmd {
	if m.fc.clock.Kind == "focus" {
		return m.stopClock()
	}
	label := ""
	if t := m.current(); t != nil {
		label = t.Title
	}
	return m.startFocus(0, label)
}

// startBreak starts a break now.
func (m *Model) startBreak() tea.Cmd {
	var cmd tea.Cmd
	if m.fc.clock.Kind == "focus" {
		cmd = m.runShortcut(m.fc.set.Off)
	}
	now := time.Now()
	m.fc.clock = focus.Clock{Kind: "break", Start: now, Ends: now.Add(m.fc.set.Break)}
	m.fc.store.SaveClock(m.fc.clock)
	m.notice = focus.Dur(m.fc.set.Break) + " break · stand up, look away"
	return tea.Batch(cmd, m.restartFocusTick())
}

// focusStatus is the timer or eye break, for the start of the help line; "" when neither runs.
func (m Model) focusStatus() string {
	if !m.sectionOn("focus") {
		return ""
	}
	now := time.Now()
	c := m.fc.clock
	switch {
	case now.Before(m.fc.eyeUntil):
		return fmt.Sprintf("◉ look at something 20 feet away · %ds", int(m.fc.eyeUntil.Sub(now).Seconds())+1)
	case c.Kind == "focus":
		return "● focus " + focus.Countdown(c.Left(now)) + labelled(c.Label)
	case c.Kind == "break":
		return "◐ break " + focus.Countdown(c.Left(now))
	}
	return ""
}

// focusRows is the FOCUS section's sidebar row.
func (m Model) focusRows() []row {
	if !m.sectionOn("focus") {
		return nil
	}
	return []row{{Kind: rowGroup, Section: "focus", Label: "FOCUS"}}
}

// onFocusPage reports whether the main pane shows FOCUS.
func (m Model) onFocusPage() bool { r := m.selRow(); return r.Kind == rowGroup && r.Section == "focus" }

// focusBadge is the FOCUS row's countdown, or "".
func (m Model) focusRowBadge() string {
	if c := m.fc.clock; c.Running() {
		return running.Render(focus.Countdown(c.Left(time.Now())))
	}
	return ""
}

// updateFocusPage handles keys on the FOCUS page: s focus, b break, +/− five minutes, esc home.
func (m Model) updateFocusPage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "s":
		if m.fc.clock.Running() {
			cmd = m.stopClock()
		} else {
			cmd = m.startFocus(0, "")
		}
	case "b":
		if m.fc.clock.Kind == "break" {
			cmd = m.stopClock()
		} else {
			cmd = m.startBreak()
		}
	case "+", "=", "-":
		if c := &m.fc.clock; c.Running() {
			d := 5 * time.Minute
			if msg.String() == "-" {
				d = -d
			}
			c.Ends = c.Ends.Add(d)
			if soon := time.Now().Add(time.Minute); c.Ends.Before(soon) {
				c.Ends = soon // at least a minute left
			}
			m.fc.store.SaveClock(*c)
		}
	case "esc":
		m.goUp()
		return m, nil
	}
	m.refresh()
	return m, cmd
}

// focusHint is the line under the FOCUS page: its keys.
func (m Model) focusHint() string {
	return hint("  s focus · b break · +/− 5 min · esc home")
}

// focusPage draws the FOCUS page: the timer, today, this week and breaks.
func (m Model) focusPage(w int) string {
	now, fc := time.Now(), m.fc
	var b strings.Builder
	c := fc.clock
	switch c.Kind {
	case "focus", "break":
		total := c.Ends.Sub(c.Start)
		name := map[string]string{"focus": "Focus", "break": "Break"}[c.Kind]
		b.WriteString(running.Render("● "+name+" "+focus.Countdown(c.Left(now))+" left") + dim.Render(labelled(c.Label)) + "\n")
		b.WriteString("  " + progress(total-c.Left(now), total, min(40, w-4)) + "\n\n")
	default:
		b.WriteString(bold.Render("No focus session") + dim.Render(fmt.Sprintf("  s starts %s of focus, b a %s break, ⌘⇧F from anywhere", focus.Dur(fc.set.Focus), focus.Dur(fc.set.Break))) + "\n\n")
	}

	d := fc.day
	b.WriteString(bold.Render("Today") + dim.Render(fmt.Sprintf("  %s in crew · agents worked %s · %d focus session(s), %s", focus.Dur(d.Crew), focus.Dur(d.Agents), d.Sessions, focus.Dur(d.Focus))) + "\n")
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
		b.WriteString(dim.Render("  nothing yet: time counts while crew is in front and you're using it") + "\n")
	}
	for _, it := range items[:min(len(items), 8)] {
		b.WriteString(fmt.Sprintf("  %-22s %7s  %s\n", truncate(it.name, 22), focus.Dur(it.t), bar(it.t, items[0].t, 24)))
	}

	b.WriteString("\n" + bold.Render("This week") + "\n")
	week := fc.store.Week(now)
	week[len(week)-1] = d // today as counted, not as last saved
	var most time.Duration
	for _, day := range week {
		most = max(most, day.Crew)
	}
	for _, day := range week {
		t, _ := time.Parse("2006-01-02", day.Date)
		b.WriteString(fmt.Sprintf("  %s %7s  %s\n", t.Format("Mon"), focus.Dur(day.Crew), bar(day.Crew, most, 24)))
	}

	b.WriteString("\n" + bold.Render("Breaks") + "\n")
	if fc.set.Eye > 0 {
		b.WriteString(fmt.Sprintf("  eyes every %s in crew, 20-20-20 · next in %s\n", focus.Dur(fc.set.Eye), focus.Dur(max(fc.set.Eye-fc.eye, 0))))
	} else {
		b.WriteString(dim.Render("  eye breaks are off (CREW_EYE_BREAKS=0)") + "\n")
	}
	if fc.set.Sit > 0 {
		b.WriteString(fmt.Sprintf("  stand up after %s without a pause · %s so far\n", focus.Dur(fc.set.Sit), focus.Dur(fc.sit)))
	} else {
		b.WriteString(dim.Render("  stand-up nudges are off (CREW_SIT_MIN=0)") + "\n")
	}
	return b.String()
}

// bar draws v as a share of most, up to w cells.
func bar(v, most time.Duration, w int) string {
	if most <= 0 {
		return ""
	}
	return running.Render(strings.Repeat("█", max(int(int64(w)*int64(v)/int64(most)), 0)))
}

// progress draws how far through a session you are.
func progress(done, total time.Duration, w int) string {
	if total <= 0 || w <= 0 {
		return ""
	}
	n := min(int(int64(w)*int64(done)/int64(total)), w)
	return running.Render(strings.Repeat("█", n)) + dim.Render(strings.Repeat("░", w-n))
}

// runFocusCmd is /focus [minutes]: start a session on this task, or stop the one running.
func runFocusCmd(m *Model, t *tasks.Task, arg string) {
	m.add(t.ID, chat.You, "/focus "+arg)
	if arg == "" && m.fc.clock.Kind == "focus" {
		m.pending = m.stopClock()
		m.add(t.ID, chat.Tool, m.notice)
		return
	}
	var mins int
	if arg != "" {
		if _, err := fmt.Sscanf(arg, "%d", &mins); err != nil || mins <= 0 {
			m.add(t.ID, chat.Error, "/focus takes minutes, e.g. /focus 50")
			return
		}
	}
	m.pending = m.startFocus(time.Duration(mins)*time.Minute, t.Title)
	m.add(t.ID, chat.Tool, m.notice)
}
