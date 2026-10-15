package requests

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/look"
)

// A Form is a modal of fields (text lines, masked or not, and choices picked with ←/→) and a row of
// buttons: the application, environment, folder and move forms (a request has its editor). It
// works with the keys and the mouse: click a field, an option, a button or the ✕.
// Keys: tab/↓ next (fields, then buttons), shift+tab/↑ previous, ←/→ pick an option or a button,
// ⏎ next field or press the button, a button's shortcut (ctrl+s…), esc close (asking once when
// there are unsaved changes).

// KeyName shows a key the way the app names it (crew's app sets it: ctrl+s reads ⌘S).
var KeyName = func(k string) string { return k }

type field struct {
	label, hint string
	in          textinput.Model
	choices     []string // set for a choice field
	labels      []string // how each choice reads; the choice itself when missing
	pick        int
	hidden      bool // skipped and not drawn right now
}

type button struct {
	label string
	key   string // the shortcut that presses it, e.g. "ctrl+s"; "" for none
	run   func(f *Form) tea.Cmd
}

// Form is an open form; Closed reports when it's done.
type Form struct {
	title   string
	fields  []field
	buttons []button
	at      int                  // the focused field, or len(fields)+i for button i
	err     string               // shown in red
	note    string               // shown in green: "Saved ✓"
	change  func(f *Form)        // after every edit: keep dependent fields right
	preview func(f *Form) string // shown under the fields
	save    func(f *Form) error
	dirty   bool // edited since it opened or was saved
	leaving bool // esc was pressed once with unsaved changes
	closed  bool
	lay     formLayout // where the last View drew things, for clicks
}

func text(label, hint, value, placeholder string) field {
	in := textinput.New()
	in.Prompt, in.Placeholder, in.CharLimit, in.Width = "", placeholder, 8000, 60
	in.PlaceholderStyle = look.Dim
	in.SetValue(value)
	return field{label: label, hint: hint, in: in}
}

func secretText(label, hint string) field {
	f := text(label, hint, "", "")
	f.in.EchoMode, f.in.EchoCharacter = textinput.EchoPassword, '•'
	return f
}

func choice(label, hint string, choices, labels []string, pick string) field {
	f := field{label: label, hint: hint, choices: choices, labels: labels}
	for i, c := range choices {
		if c == pick {
			f.pick = i
		}
	}
	return f
}

// newForm makes a form; one without buttons gets Save (save, then close) and Cancel.
func newForm(title string, fields []field, buttons ...button) *Form {
	f := &Form{title: title, fields: fields, buttons: buttons}
	if len(buttons) == 0 {
		f.buttons = []button{{"Save", "ctrl+s", saveClose}, {"Cancel", "esc", leave}}
	}
	return f
}

// start runs change once and focuses the first shown field.
func (f *Form) start() *Form {
	if f.change != nil {
		f.change(f)
	}
	for f.at < len(f.fields) && f.fields[f.at].hidden {
		f.at++
	}
	f.focus()
	return f
}

// Closed reports whether the form was saved and closed, or cancelled.
func (f *Form) Closed() bool { return f.closed }

func (f *Form) val(i int) string {
	fl := f.fields[i]
	if fl.choices != nil {
		if fl.pick < len(fl.choices) {
			return fl.choices[fl.pick]
		}
		return ""
	}
	if fl.in.EchoMode == textinput.EchoPassword {
		return fl.in.Value()
	}
	return strings.TrimSpace(fl.in.Value())
}

// saveClose is the Save button of a plain form.
func saveClose(f *Form) tea.Cmd {
	f.err = ""
	if err := f.save(f); err != nil {
		f.err = err.Error()
		return nil
	}
	f.closed = true
	return nil
}

// leave is Cancel / Close / esc / ✕: close, after asking once if there are unsaved changes.
func leave(f *Form) tea.Cmd {
	if f.dirty && !f.leaving {
		f.leaving, f.err = true, "Unsaved changes · press esc or Close again to discard them"
		return nil
	}
	f.closed = true
	return nil
}

func (f *Form) focus() {
	for i := range f.fields {
		if i == f.at && f.fields[i].choices == nil {
			f.fields[i].in.Focus()
		} else {
			f.fields[i].in.Blur()
		}
	}
}

// step moves to the next (1) or previous (-1) shown field or button, round the end.
func (f *Form) step(d int) {
	n := len(f.fields) + len(f.buttons)
	for i := 1; i <= n; i++ {
		at := ((f.at+d*i)%n + n) % n
		if at >= len(f.fields) || !f.fields[at].hidden {
			f.at = at
			break
		}
	}
	f.focus()
}

func (f *Form) edited() {
	f.dirty, f.leaving, f.note = true, false, ""
	if f.change != nil {
		f.change(f)
	}
}

// Key handles a key.
func (f *Form) Key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	for _, b := range f.buttons {
		if b.key != "" && b.key == k {
			return b.run(f)
		}
	}
	f.leaving = false
	b := -1
	if f.at >= len(f.fields) {
		b = f.at - len(f.fields)
	}
	switch {
	case k == "tab" || k == "down":
		f.step(1)
	case k == "shift+tab" || k == "up":
		f.step(-1)
	case b >= 0 && (k == "left" || k == "right"):
		f.at = len(f.fields) + (b+map[string]int{"left": -1, "right": 1}[k]+len(f.buttons))%len(f.buttons)
	case b >= 0 && (k == "enter" || k == " "):
		return f.buttons[b].run(f)
	case b >= 0:
	case k == "enter":
		f.step(1)
	case f.fields[f.at].choices != nil:
		fl := &f.fields[f.at]
		if d, ok := map[string]int{"left": -1, "right": 1, " ": 1}[k]; ok && len(fl.choices) > 0 {
			fl.pick = (fl.pick + d + len(fl.choices)) % len(fl.choices)
			f.edited()
		}
	default:
		fl := &f.fields[f.at]
		before := fl.in.Value()
		var cmd tea.Cmd
		fl.in, cmd = fl.in.Update(msg)
		if fl.in.Value() != before {
			f.edited()
		}
		return cmd
	}
	return nil
}

// Wheel does nothing: a form fits on screen.
func (f *Form) Wheel(bool) {}

// formLayout is where a form's parts were drawn, in lines and columns of its content.
type formLayout struct {
	closeX  int
	fieldAt map[int]int      // line -> field
	pickAt  map[int][][3]int // line -> each option's start, end and index
	btnLine int
	btnAt   [][3]int // each button's start, end and index
}

// View draws the form w wide and at most h tall (what's below the buttons gets the room left).
func (f *Form) View(w, h int) string {
	w = min(w, 104) // a form reads better narrow; the request editor takes the room it's given
	lw := 0
	for _, fl := range f.fields {
		lw = max(lw, len(fl.label))
	}
	lw += 3
	inner := w - 6
	lay := formLayout{fieldAt: map[int]int{}, pickAt: map[int][][3]int{}}
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }

	title := ansi.Truncate(f.title, inner-3, "…")
	lay.closeX = inner - 1
	add(look.Bold.Render(title)+strings.Repeat(" ", max(inner-1-ansi.StringWidth(title), 1))+look.Dim.Render("✕"), "")
	for i := range f.fields {
		fl := &f.fields[i]
		if fl.hidden {
			continue
		}
		label := look.Dim.Render(fmt.Sprintf("  %-*s", lw, fl.label))
		if i == f.at {
			label = look.Sel.Render("▸ ") + look.Bold.Render(fmt.Sprintf("%-*s", lw, fl.label))
		}
		lay.fieldAt[len(lines)] = i
		if fl.choices == nil {
			fl.in.Width = max(inner-lw-4, 10)
			add(label + fl.in.View())
			continue
		}
		x, opts := lw+2, ""
		for j, c := range fl.choices {
			l := c
			if j < len(fl.labels) {
				l = fl.labels[j]
			}
			o := look.Dim.Render(" " + l + " ")
			switch {
			case j == fl.pick && i == f.at:
				o = look.FocusBar.Foreground(lipgloss.Color("15")).Render(" " + l + " ")
			case j == fl.pick:
				o = look.Sel.Render("‹" + l + "›")
			}
			ow := ansi.StringWidth(o)
			lay.pickAt[len(lines)] = append(lay.pickAt[len(lines)], [3]int{x, x + ow, j})
			opts += o
			x += ow
		}
		if len(fl.choices) == 0 {
			opts = look.Dim.Render("none yet")
		}
		add(label + ansi.Truncate(opts, inner-lw-2, "…"))
	}
	if f.at < len(f.fields) && f.fields[f.at].hint != "" {
		add("", look.Dim.Render(ansi.Truncate(f.fields[f.at].hint, inner, "…")))
	}
	if f.preview != nil {
		if p := f.preview(f); p != "" {
			add("")
			for _, l := range strings.Split(p, "\n") {
				add(ansi.Truncate(l, inner, "…"))
			}
		}
	}
	switch {
	case f.err != "":
		add("", look.Warn.Render(ansi.Truncate(f.err, inner, "…")))
	case f.note != "":
		add("", look.Ready.Render(f.note))
	}

	add("")
	lay.btnLine = len(lines)
	row, x := "", 0
	for i, b := range f.buttons {
		key := ""
		if b.key != "" {
			key = " " + KeyName(b.key)
		}
		s := look.Dim.Render("[") + " " + b.label + look.Dim.Render(key) + " " + look.Dim.Render("]")
		if f.at == len(f.fields)+i {
			s = look.Active.Render("[ " + b.label + key + " ]")
		}
		bw := ansi.StringWidth(s)
		lay.btnAt = append(lay.btnAt, [3]int{x, x + bw, i})
		row += s + "  "
		x += bw + 2
	}
	add(row)
	f.lay = lay
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(look.Accent).Padding(1, 2).Width(w - 2).Render(strings.Join(lines, "\n"))
}

// Click handles a click at (x, y) in the box View drew last: the ✕, a field, an option, a button.
func (f *Form) Click(x, y int) tea.Cmd {
	x, y = x-3, y-2 // inside the border and padding
	lay := f.lay
	switch {
	case y == 0 && x >= lay.closeX-1:
		return leave(f)
	case y == lay.btnLine:
		for _, b := range lay.btnAt {
			if x >= b[0] && x < b[1] {
				f.at = len(f.fields) + b[2]
				f.focus()
				return f.buttons[b[2]].run(f)
			}
		}
	}
	if i, ok := lay.fieldAt[y]; ok {
		f.at = i
		f.focus()
		for _, p := range lay.pickAt[y] {
			if x >= p[0] && x < p[1] && f.fields[i].pick != p[2] {
				f.fields[i].pick = p[2]
				f.edited()
			}
		}
	}
	return nil
}
