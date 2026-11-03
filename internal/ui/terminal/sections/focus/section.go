package focus

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	fc "crew/internal/focus"
	"crew/internal/tasks"
	"crew/internal/ui/terminal/section"
	"crew/internal/ui/terminal/widgets/chat"
	"crew/internal/ui/terminal/widgets/keys"
	"crew/internal/ui/terminal/widgets/look"
)

// FOCUS in the terminal: the timer, breaks and screen time are the focus service's (fc.Timer); this
// ticks it with Bubble Tea, draws it (page.go: the help line's status, its row and its page; the break
// screen below) and hands it your keys. f on home turns all of it off.

// The service's types, by the names the drawing uses.
type (
	Settings = fc.Settings
	Clock    = fc.Clock
	Day      = fc.Day
	Store    = fc.Store
)

var (
	Dur       = fc.Dur
	Countdown = fc.Countdown
	DateOf    = fc.DateOf
)

// Section is FOCUS.
type Section struct {
	section.Base
	*fc.Timer
	form *Form // the settings popup, while it's open
	gen  int   // the ticker's generation; a tick from an older one is dropped
}

// NewSection makes FOCUS from its folder.
func NewSection(dir string) *Section { return &Section{Timer: fc.New(dir)} }

// Def is FOCUS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "focus", Label: "FOCUS", Letter: "f", About: "a focus timer, breaks, and your screen time on the Mac"}
}

// env is what the timer gets from crew's terminal.
type env struct {
	s *Section
	h section.Host
}

func (e env) On() bool              { return e.h.SectionOn("focus") }
func (e env) Blurred() bool         { return e.h.Blurred() }
func (e env) Notice(t string)       { e.h.Notice(t) }
func (e env) Names(t string) string { return keys.Names(t) }
func (e env) Screen(open bool) {
	switch {
	case open:
		e.h.SectionModal(e.s)
	case e.s.form == nil: // back to the settings form, if that was open
		e.h.SectionModal(nil)
	}
}

// do runs one of the timer's actions; when it changed what counts down, the ticker starts over.
func (s *Section) do(h section.Host, act func(fc.Env) bool) tea.Cmd {
	if act(env{s, h}) {
		return s.restart(h)
	}
	return nil
}

// tickMsg is the clock's tick, from ticker generation gen.
type tickMsg struct{ gen int }

// idleMsg is how long the Mac has had no keyboard or mouse input; ok is false when it couldn't be read.
type idleMsg struct {
	idle time.Duration
	ok   bool
}

// tick schedules the next tick: every second while something counts down on screen, else every 15.
func (s *Section) tick(h section.Host) tea.Cmd {
	if !h.SectionOn("focus") {
		return nil // turning FOCUS on starts it again (restart)
	}
	gen := s.gen
	return tea.Tick(s.Every(time.Now()), func(time.Time) tea.Msg { return tickMsg{gen} })
}

// restart starts a new ticker now (the old one's ticks are dropped), so a countdown you just
// started shows at once.
func (s *Section) restart(h section.Host) tea.Cmd {
	s.gen++
	s.Last = time.Now()
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
		s.Active = time.Now()
	case tickMsg:
		return s.onTick(h, msg), true
	case idleMsg:
		s.SetIdle(msg.idle, msg.ok, time.Now())
		return nil, true
	}
	return nil, false
}

// onTick counts the time since the last tick and fires what's due, then ticks again.
func (s *Section) onTick(h section.Host, msg tickMsg) tea.Cmd {
	if msg.gen != s.gen || !h.SectionOn("focus") {
		return nil
	}
	now := time.Now()
	var poll tea.Cmd
	if s.PollIdle(now) {
		poll = func() tea.Msg { idle, ok := fc.ReadIdle(); return idleMsg{idle, ok} }
	}
	s.Tick(env{s, h}, now)
	if onPage(h) {
		h.Refresh()
	}
	return tea.Batch(poll, s.restart(h))
}

// ModalOpen reports whether the break screen or the settings form is open.
func (s *Section) ModalOpen(section.Host) bool { return s.Screen != "" || s.form != nil }

// ModalView draws the break screen's step over the main pane, or the settings form.
func (s *Section) ModalView(h section.Host) string {
	if s.Screen == "" {
		return s.form.View()
	}
	w, ht := h.MainSize()
	now := time.Now()
	barW := min(40, w-10)
	var body string
	switch s.Screen {
	case "ask":
		d := s.Due.Ends.Sub(s.Due.Start)
		left := s.ExtrasLeft()
		more := look.Dim.Render("no more extensions")
		if left > 0 {
			more = "Keep going: "
			for i, x := range fc.Extras {
				more += " " + look.Dim.Render("[ ") + "+" + Dur(x) + " " + look.Dim.Render(strconv.Itoa(i+1)+" ]")
			}
		}
		advice := fc.Advice(s.Due.Label)
		body = look.Running.Render("●  "+s.Why) + "\n\n" +
			"Time for a " + Dur(d) + " " + strings.ToLower(s.Due.Label) + ": " + strings.ToLower(advice[:1]) + advice[1:] + ".\n\n\n" +
			look.Active.Render("[ Start the break ⏎ ]") + "     " + more
		var notes []string
		if left > 0 {
			notes = append(notes, fmt.Sprintf("you can keep going %d more time%s", left, look.Plural(left)))
		}
		if s.Set.Auto {
			notes = append(notes, "the break starts on its own in "+Countdown(max(fc.AutoBreak-now.Sub(s.Asked), 0)))
		}
		if len(notes) > 0 {
			body += "\n\n" + look.Dim.Render(strings.Join(notes, " · "))
		}
	case "break":
		c := s.Clock
		keys := look.KeyHints("e end early")
		if s.Sure {
			keys = look.Warn.Render("End the break early? y ends it · any other key keeps it")
		}
		body = look.Running.Render("◐  "+c.Label+"  "+Countdown(c.Left(now))) + "\n\n" +
			fc.Advice(c.Label) + "\n\n" +
			progress(c.Ends.Sub(c.Start)-c.Left(now), c.Ends.Sub(c.Start), barW) + "\n\n\n" + keys
	case "back":
		body = look.Ready.Render("✓  Break done") + "\n\n" + "Ready for the next one?" + "\n\n\n" +
			look.Active.Render("[ Start the next focus ⏎ ]") + "    " + look.Dim.Render("[ ") + "Not yet " + look.Dim.Render("esc ]")
		if s.Set.Auto {
			when := "once you're back at the Mac"
			if s.Here(env{s, h}, now) {
				when = "in " + Countdown(max(fc.AutoFocus-now.Sub(s.BackAt), 0))
			}
			body += "\n\n" + look.Dim.Render("the next focus starts on its own "+when)
		}
	}
	// The box's border and padding take 4 lines; it fills the main pane inside its frame.
	body = lipgloss.PlaceVertical(max(ht-6, lipgloss.Height(body)), lipgloss.Center, body)
	return look.MenuBox.Width(max(w-4, 20)).Align(lipgloss.Center).Render(body)
}

// ModalKey takes the break screen's keys (on time's up ⏎ starts the break and 1 2 3 keep going; e
// then y ends a break early; ⏎ or esc when it's done), else hands them to the form.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch s.Screen {
	case "ask":
		switch k {
		case "enter":
			return s.do(h, s.TakeBreak)
		case "1", "2", "3":
			d := fc.Extras[k[0]-'1']
			return s.do(h, func(e fc.Env) bool { return s.Extend(e, d) })
		}
		return nil
	case "break":
		switch {
		case s.Sure && k == "y":
			s.EndEarly()
			return s.restart(h)
		case k == "e":
			s.Sure = true
		default:
			s.Sure = false
		}
		return nil
	case "back":
		switch k {
		case "enter":
			return s.do(h, func(e fc.Env) bool { return s.StartFocus(e, 0) })
		case "esc":
			s.NotYet(env{s, h})
		}
		return nil
	}
	cmd, saved, closed := s.form.Key(msg)
	if saved != nil {
		if err := s.SaveSettings(*saved); err != nil {
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
			if arg == "" && s.Clock.Kind == "focus" {
				cmd := s.do(h, s.StopClock)
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
			cmd := s.do(h, func(e fc.Env) bool { return s.StartFocus(e, time.Duration(mins)*time.Minute) })
			h.Say(t.ID, chat.Tool, h.Note())
			return cmd
		}}}
}

// Actions are ⌘⇧F (focus) and a break, for the ⌘ shortcuts.
func (s *Section) Actions() []section.Action {
	return []section.Action{
		{Name: "focus", Help: "start a focus session, or stop the one running (FOCUS)", Run: func(h section.Host) tea.Cmd { return s.do(h, s.Toggle) }},
		{Name: "break", Help: "start a break now, or end the one running (FOCUS)", Run: func(h section.Host) tea.Cmd { return s.do(h, s.BreakNow) }},
		{Name: "extend", Help: "add 15 minutes to the focus session running, twice at most (FOCUS)", Run: func(h section.Host) tea.Cmd {
			return s.do(h, func(e fc.Env) bool { return s.Extend(e, fc.Extras[1]) })
		}},
	}
}
