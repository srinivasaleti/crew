package focus

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/widgets/look"
)

// field is one row of the settings form: a toggle (opts) or a text field.
type field struct {
	label string
	opts  []string // a toggle's choices; nil for a text field
	pick  int      // the toggle's choice
	in    textinput.Model
}

// screens are the break screen's settings: crew's window full screen over every app, in crew only,
// or off (a notification, and the break starts on its own).
var screens = []string{"full", "crew", "off"}

// The form's rows, in order.
const (
	fMode = iota
	fFocus
	fBreak
	fLong
	fLongEvery
	fMove
	fMoveFor
	fEye
	fScreen
	fAuto
	fOnOpen
	fLimit
	fEnd
	fOn
	fOff
)

// onOff is a toggle's pick for a setting that's on (0) or off (1).
func onOff(on bool) int {
	if on {
		return 0
	}
	return 1
}

// Form is the settings popup (e on the FOCUS page): a row per setting, ⌘S saves, esc cancels.
type Form struct {
	fields []field
	at     int
	err    string
}

// NewForm opens the form on st.
func NewForm(st Settings) (*Form, tea.Cmd) {
	text := func(label, value, placeholder string) field {
		in := textinput.New()
		in.SetValue(value)
		in.Placeholder, in.Width, in.Prompt = placeholder, 24, ""
		return field{label: label, in: in}
	}
	mins := func(d time.Duration) string { return strconv.Itoa(int(d.Minutes())) }
	mode := 0
	if st.Mode == "flow" {
		mode = 1
	}
	screen := max(slices.Index(screens, st.Screen), 0)
	f := &Form{fields: []field{
		{label: "Timer", opts: []string{"pomodoro", "flow"}, pick: mode},
		text("Focus (min)", mins(st.Focus), "25"),
		text("Break (min)", mins(st.Break), "5"),
		text("Long break (min)", mins(st.Long), "15"),
		text("Long break every", strconv.Itoa(st.LongEvery), "4 sessions"),
		text("Move break every (min)", mins(st.Move), "30 · 0 off"),
		text("Move break for (min)", mins(st.MoveFor), "3"),
		text("Eye break every (min)", mins(st.Eye), "20 · 0 off"),
		{label: "Break screen", opts: screens, pick: screen},
		{label: "Auto start", opts: []string{"on", "off"}, pick: onOff(st.Auto)},
		{label: "Start when crew opens", opts: []string{"on", "off"}, pick: onOff(st.OnOpen)},
		text("Daily limit (hours)", strconv.FormatFloat(st.Limit.Hours(), 'f', -1, 64), "6 · 0 off"),
		text("End of day", st.EndOfDay, "19:00 · empty off"),
		text("Do Not Disturb on", st.On, "a Shortcut's name"),
		text("Do Not Disturb off", st.Off, "a Shortcut's name"),
	}}
	return f, f.focus(0)
}

// focus moves to row i.
func (f *Form) focus(i int) tea.Cmd {
	f.at = i
	var cmd tea.Cmd
	for j := range f.fields {
		if j == i && f.fields[j].opts == nil {
			cmd = f.fields[j].in.Focus()
		} else {
			f.fields[j].in.Blur()
		}
	}
	return cmd
}

// build is the settings the form describes, or what's wrong with them.
func (f *Form) build() (Settings, error) {
	val := func(i int) string { return strings.TrimSpace(f.fields[i].in.Value()) }
	var errs []error
	num := func(i int, low float64) float64 { // row i's number, at least low
		n, err := strconv.ParseFloat(val(i), 64)
		if err != nil || n < low {
			errs = append(errs, fmt.Errorf("%s: a number from %g", f.fields[i].label, low))
		}
		return n
	}
	mins := func(i int, low float64) time.Duration { return time.Duration(num(i, low) * float64(time.Minute)) }
	st := Settings{Mode: f.fields[fMode].opts[f.fields[fMode].pick], Screen: screens[f.fields[fScreen].pick]}
	st.Auto, st.OnOpen = f.fields[fAuto].pick == 0, f.fields[fOnOpen].pick == 0
	st.Focus, st.Break, st.Long = mins(fFocus, 1), mins(fBreak, 1), mins(fLong, 1)
	st.LongEvery = int(num(fLongEvery, 1))
	st.Move, st.MoveFor, st.Eye = mins(fMove, 0), mins(fMoveFor, 1), mins(fEye, 0)
	st.Limit = time.Duration(num(fLimit, 0) * float64(time.Hour))
	if st.EndOfDay = val(fEnd); st.EndOfDay != "" {
		if _, ok := st.EndOfDayAt(time.Now()); !ok {
			errs = append(errs, fmt.Errorf("End of day: a time like 19:00, or empty"))
		}
	}
	st.On, st.Off = val(fOn), val(fOff)
	if len(errs) > 0 {
		return st, errs[0]
	}
	return st, nil
}

// Key handles a key. saved is the settings to save (⌘S, or ⏎ on the last row); closed is set when
// the form is done, saved or cancelled.
func (f *Form) Key(msg tea.KeyMsg) (cmd tea.Cmd, saved *Settings, closed bool) {
	last := len(f.fields) - 1
	fl := &f.fields[f.at]
	switch k := msg.String(); {
	case k == "esc":
		return nil, nil, true
	case k == "up" || k == "shift+tab":
		return f.focus(max(f.at-1, 0)), nil, false
	case k == "down" || k == "tab" || (k == "enter" && f.at < last):
		return f.focus(min(f.at+1, last)), nil, false
	case fl.opts != nil && (k == "left" || k == "right" || k == " "):
		fl.pick = (fl.pick + 1) % len(fl.opts)
		return nil, nil, false
	case k == "ctrl+s" || k == "enter":
		st, err := f.build()
		if err != nil {
			f.err = err.Error()
			return nil, nil, false
		}
		return nil, &st, true
	}
	if fl.opts == nil {
		fl.in, cmd = fl.in.Update(msg)
		f.err = ""
	}
	return cmd, nil, false
}

// View draws the form.
func (f *Form) View() string {
	var b strings.Builder
	for i, fl := range f.fields {
		label := look.Bold.Render(fmt.Sprintf("  %-24s", fl.label))
		if i == f.at {
			label = look.Sel.Render(fmt.Sprintf("▶ %-24s", fl.label))
		}
		value := fl.in.View()
		if fl.opts != nil {
			var opts []string
			for j, o := range fl.opts {
				if j == fl.pick {
					opts = append(opts, look.Sel.Render(o))
				} else {
					opts = append(opts, look.Dim.Render(o))
				}
			}
			value = strings.Join(opts, look.Dim.Render(" / "))
		}
		b.WriteString(label + value + "\n")
		if i == fEye || i == fOnOpen || i == fEnd {
			b.WriteString("\n")
		}
	}
	status := look.Dim.Render("Break screen: full fills the screen over every app, crew keeps it in crew, off only notifies. Auto start: a break starts when you don't answer, the next focus when it's over and you're back. Eye breaks show quietly on the help line.")
	if f.err != "" {
		status = look.Warn.Render(f.err)
	}
	return look.MenuBox.Width(60).Render(look.Bold.Render("FOCUS settings") + "\n\n" + b.String() + "\n" + status + "\n\n" +
		look.Dim.Render("↑↓ field · ←/→ change · ⌘S saves · esc cancel"))
}
