package focus

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	fc "crew/internal/focus"
	"crew/internal/ui/terminal/section"
	"crew/internal/ui/terminal/widgets/keys"
	"crew/internal/ui/terminal/widgets/look"
)

// FOCUS's drawing and keys: the help line's status, its sidebar row, and its page.

// Status is the start of the help line: the timer, then what's due (eyes, a move break, the
// limit, wrapping up); "" when there's nothing.
func (s *Section) Status(h section.Host) string {
	if !h.SectionOn("focus") {
		return ""
	}
	now := time.Now()
	var parts []string
	switch c := s.Clock; {
	case c.Kind == "break":
		parts = append(parts, look.Running.Render("◐ "+strings.ToLower(c.Label)+" "+Countdown(c.Left(now))))
	case c.Flow():
		parts = append(parts, look.Running.Render("● flow "+Countdown(now.Sub(c.Start))))
	case c.Kind == "focus":
		focus := "● focus " + Countdown(c.Left(now))
		if c.Left(now) <= fc.HeadsUp && c.Extra < fc.MaxExtra {
			focus += " · ⌘⇧E +15 min"
		}
		parts = append(parts, look.Running.Render(focus))
	case s.Later.Running():
		parts = append(parts, look.Dim.Render("◐ break in "+Countdown(s.Later.Start.Sub(now))))
	}
	if now.Before(s.EyeUntil) {
		parts = append(parts, look.Dim.Render(fmt.Sprintf("◉ look 20 ft away · %ds", int(s.EyeUntil.Sub(now).Seconds())+1)))
	}
	if s.MoveDue() && s.Clock.Kind != "break" {
		parts = append(parts, look.Ready.Render("stand up and move"))
	}
	if d, lim := s.Day, s.Set.Limit; lim > 0 && d.Warned >= 100 {
		parts = append(parts, look.Warn.Render("over "+Dur(lim)+" today"))
	} else if lim > 0 && d.Warned >= 80 {
		parts = append(parts, look.Ready.Render(Dur(d.Active)+" of "+Dur(lim)))
	}
	if s.Day.Wrapped {
		parts = append(parts, look.Ready.Render("wrap up"))
	}
	return keys.Names(strings.Join(parts, look.Dim.Render(" · ")))
}

// Rows is FOCUS's row.
func (s *Section) Rows(section.Host) []section.Row {
	return []section.Row{{Kind: section.Group, Section: "focus", Label: "FOCUS"}}
}

// Title is FOCUS's row: no count, and the timer when the clock runs.
func (s *Section) Title(_ section.Host, r section.Row) (string, string) {
	badge, now := "", time.Now()
	switch c := s.Clock; {
	case c.Flow():
		badge = look.Running.Render(Countdown(now.Sub(c.Start)))
	case c.Running():
		badge = look.Running.Render(Countdown(c.Left(now)))
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
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && strings.ContainsRune("sbe+=-", msg.Runes[0]) {
		h.FocusMain()
		return s.Key(h, r, msg)
	}
	return nil, false
}

// Key handles keys on the FOCUS page: s start or stop, b break, +/− five minutes, e settings, esc home.
func (s *Section) Key(h section.Host, _ section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg.String() {
	case "s":
		if s.Clock.Running() {
			cmd = s.do(h, s.StopClock)
		} else {
			cmd = s.do(h, func(e fc.Env) bool { return s.StartFocus(e, 0) })
		}
	case "b":
		cmd = s.do(h, s.BreakNow)
	case "e":
		s.form, cmd = NewForm(s.Set)
		h.SectionModal(s)
	case "+", "=", "-":
		d := 5 * time.Minute
		if msg.String() == "-" {
			d = -d
		}
		s.Nudge(d)
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
	start := "s start"
	if s.Clock.Running() {
		start = "s stop"
	}
	return look.KeyHints(keys.Names("  " + start + " · b break now · +/− 5 min · e settings · esc home"))
}

// Header is the FOCUS page's first line: FOCUS, and today's screen time on the right.
func (s *Section) Header(_ section.Host, _ section.Row, w int) string {
	today := "Mac today " + Dur(s.Day.Active)
	if !s.Whole(time.Now()) {
		today = "in crew today " + Dur(s.Day.Active)
	}
	if s.Set.Limit > 0 {
		today += " of " + Dur(s.Set.Limit)
	}
	left := look.Bold.Render("FOCUS")
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(today), 2)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+look.Dim.Render(today), w, "…")
}

// Render draws the FOCUS page: the timer, what's next, today and this week.
func (s *Section) Render(h section.Host, _ section.Row) {
	vp := h.Viewport()
	vp.SetContent(s.page(vp.Width))
}

// page is the FOCUS page, w wide.
func (s *Section) page(w int) string {
	now := time.Now()
	var b strings.Builder
	c, st := s.Clock, s.Set
	line, barW := "", min(25, w/3)
	switch {
	case c.Flow():
		line = look.Running.Render("● Flow "+Countdown(now.Sub(c.Start))) + look.Dim.Render(fmt.Sprintf(" · s stops it and starts a %s break", Dur(max(now.Sub(c.Start)/5, time.Minute))))
	case c.Kind == "focus":
		line = look.Running.Render("● Pomodoro "+Countdown(c.Left(now))+" left") + look.Dim.Render(fmt.Sprintf(" · %d of %d", c.Done+1, st.LongEvery))
	case c.Kind == "break":
		line = look.Running.Render("◐ " + c.Label + " " + Countdown(c.Left(now)) + " left")
	case st.Mode == "flow":
		line = look.Bold.Render("No session") + look.Dim.Render("  s starts a flow session, b a break, ⌘⇧F from anywhere")
	default:
		line = look.Bold.Render("No session") + look.Dim.Render(fmt.Sprintf("  s starts %s of focus, b a break, ⌘⇧F from anywhere", Dur(st.Focus)))
	}
	if c.Running() && !c.Flow() && w-lipgloss.Width(line) > barW+4 {
		total := c.Ends.Sub(c.Start)
		line += strings.Repeat(" ", w-lipgloss.Width(line)-barW-1) + progress(total-c.Left(now), total, barW)
	}
	b.WriteString(keys.Names(line) + "\n\n")

	var next []string
	if s.Later.Running() {
		next = append(next, s.Later.Label+" in "+Dur(s.Later.Start.Sub(now)))
	}
	if st.Move > 0 {
		if s.MoveDue() {
			next = append(next, "move now")
		} else {
			next = append(next, "move in "+Dur(st.Move-s.Move))
		}
	}
	if st.Eye > 0 {
		next = append(next, "eyes in "+Dur(max(st.Eye-s.Eye, 0)))
	}
	if end, ok := st.EndOfDayAt(now); ok {
		if now.Before(end) {
			next = append(next, fmt.Sprintf("end of day %s (%s)", st.EndOfDay, Dur(end.Sub(now))))
		} else {
			next = append(next, "end of day "+st.EndOfDay+" · wrap up")
		}
	}
	if len(next) == 0 {
		next = append(next, "no breaks set · e settings")
	}
	d := s.Day
	b.WriteString(look.Bold.Render("Next      ") + strings.Join(next, " · ") + "\n")
	b.WriteString(look.Bold.Render("Today     ") + fmt.Sprintf("%s active · %d sessions · %d breaks taken, %d skipped", Dur(d.Active), d.Sessions, d.Breaks, d.Skipped) + "\n")
	if !s.Whole(now) {
		b.WriteString(look.Dim.Render("          counts while crew is in front (the Mac's idle time isn't readable)") + "\n")
	}

	b.WriteString("\n" + look.Bold.Render("This week") + "\n")
	week := s.Store.Week(now)
	week[len(week)-1] = d // today as counted, not as last saved
	most := st.Limit
	for _, day := range week {
		most = max(most, day.Active)
	}
	for _, day := range week {
		t, _ := time.Parse("2006-01-02", day.Date)
		over := ""
		if st.Limit > 0 && day.Active > st.Limit {
			over = look.Warn.Render("  over")
		}
		b.WriteString(fmt.Sprintf("  %s %7s %s%s\n", t.Format("Mon"), Dur(day.Active), bar(day.Active, most, min(30, max(w-24, 4))), over))
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

// progress draws how far through a session or break you are.
func progress(done, total time.Duration, w int) string {
	if total <= 0 || w <= 0 {
		return ""
	}
	n := min(max(int(int64(w)*int64(done)/int64(total)), 0), w)
	return look.Running.Render(strings.Repeat("█", n)) + look.Dim.Render(strings.Repeat("░", w-n))
}
