package reminders

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/look"
)

// repeats are the form's choices for how a reminder repeats; "custom" takes what you type.
var repeats = []struct{ value, label string }{
	{"", "never"}, {"day", "every day"}, {"weekday", "every weekday"}, {"week", "every week"},
	{"month", "every month"}, {"year", "every year"}, {"custom", "custom"},
}

// Form is the new or edit popup: what, when, and how it repeats, with the next times it fires.
type Form struct {
	id     int // the reminder being edited; 0 for a new one
	field  int // 0 what, 1 when, 2 repeats, 3 custom repeat
	repeat int // index into repeats
	what   textinput.Model
	when   textinput.Model
	custom textinput.Model
	err    string
}

// NewForm opens the form for r; a zero r is a new reminder.
func NewForm(r Reminder) (*Form, tea.Cmd) {
	f := &Form{id: r.ID, what: textinput.New(), when: textinput.New(), custom: textinput.New()}
	f.what.Placeholder, f.what.CharLimit, f.what.Width = "stand-up", 200, 44
	f.when.Placeholder, f.when.Width = "4pm · 30m · tomorrow 9am · fri 5pm · nov 1 10am", 44
	f.custom.Placeholder, f.custom.Width = "2 weeks · mon,wed,fri · 3 months", 44
	if r.ID != 0 {
		f.what.SetValue(r.Text)
		f.when.SetValue(r.At.Format("2006-01-02 15:04"))
		f.repeat = slices.IndexFunc(repeats, func(c struct{ value, label string }) bool { return c.value == r.Every })
		if f.repeat < 0 {
			f.repeat = len(repeats) - 1
			f.custom.SetValue(r.Every)
		}
	}
	return f, f.focus(0)
}

func (f *Form) focus(i int) tea.Cmd {
	f.field = i
	f.what.Blur()
	f.when.Blur()
	f.custom.Blur()
	switch i {
	case 0:
		return f.what.Focus()
	case 1:
		return f.when.Focus()
	case 3:
		return f.custom.Focus()
	}
	return nil // the repeats row takes ←/→
}

// last is the form's last field: the custom repeat when it's picked, else the repeats row.
func (f *Form) last() int {
	if repeats[f.repeat].value == "custom" {
		return 3
	}
	return 2
}

// build is the reminder the form describes, or what's wrong with it.
func (f *Form) build(now time.Time) (Reminder, error) {
	every := repeats[f.repeat].value
	if every == "custom" {
		every = "every " + f.custom.Value()
	}
	r, err := build(Reminder{ID: f.id, Text: strings.TrimSpace(f.what.Value())}, f.when.Value(), every, now)
	if err == nil && r.Text == "" {
		err = errors.New("say what to remind you of")
	}
	return r, err
}

// Key handles a key. saved is the reminder to save (⏎ on the last field); closed is set when the
// form is done, saved or cancelled.
func (f *Form) Key(msg tea.KeyMsg) (cmd tea.Cmd, saved *Reminder, closed bool) {
	switch msg.String() {
	case "esc":
		return nil, nil, true
	case "up", "shift+tab":
		return f.focus(max(f.field-1, 0)), nil, false
	case "down", "tab":
		return f.focus(min(f.field+1, f.last())), nil, false
	case "left", "right":
		if f.field == 2 {
			d := map[string]int{"left": -1, "right": 1}[msg.String()]
			f.repeat = (f.repeat + d + len(repeats)) % len(repeats)
			return nil, nil, false
		}
	case "enter":
		if f.field < f.last() {
			return f.focus(f.field + 1), nil, false
		}
		r, err := f.build(time.Now())
		if err != nil {
			f.err = err.Error()
			return nil, nil, false
		}
		return nil, &r, true
	}
	switch f.field {
	case 0:
		f.what, cmd = f.what.Update(msg)
	case 1:
		f.when, cmd = f.when.Update(msg)
	case 3:
		f.custom, cmd = f.custom.Update(msg)
	}
	f.err = ""
	return cmd, nil, false
}

// View draws the form, with when it fires next (or what's wrong) as you type.
func (f *Form) View() string {
	label := func(i int, s string) string {
		if f.field == i {
			return look.Sel.Render(s)
		}
		return look.Bold.Render(s)
	}
	var choices []string
	for i, c := range repeats {
		if i == f.repeat {
			choices = append(choices, look.Sel.Render("▶ "+c.label))
		} else {
			choices = append(choices, look.Dim.Render("  "+c.label))
		}
	}
	body := label(0, "What") + "\n" + f.what.View() + "\n\n" +
		label(1, "When") + "\n" + f.when.View() + "\n\n" +
		label(2, "Repeats") + look.Dim.Render("  ←/→") + "\n" + strings.Join(choices[:4], " ") + "\n" + strings.Join(choices[4:], " ") + "\n"
	if repeats[f.repeat].value == "custom" {
		body += "\n" + label(3, "Every") + "\n" + f.custom.View() + "\n"
	}
	now := time.Now()
	preview := ""
	switch r, err := f.build(now); {
	case f.err != "":
		preview = look.Warn.Render(f.err)
	case err != nil && f.when.Value() != "":
		preview = look.Dim.Render(err.Error())
	case err == nil:
		preview = "Next: " + When(r.At, now)
		if r.Every != "" {
			preview += ", then " + Describe(r.Every, r.At) + " at " + r.At.Format("3:04pm")
		}
		preview = look.Green.Render(preview)
	}
	title := "New reminder"
	if f.id != 0 {
		title = "Edit reminder"
	}
	return look.MenuBox.Width(64).Render(look.Bold.Render(title) + "\n\n" + body + "\n" + preview + "\n\n" +
		look.Dim.Render("⏎ next field · ⏎ on the last saves · esc cancel"))
}
