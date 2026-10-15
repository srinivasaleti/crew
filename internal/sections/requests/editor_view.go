package requests

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/look"
)

// Drawing the request editor (editor.go), and its mouse.

// jsonErr checks a body: its line count and, when it isn't valid JSON, why and on which line.
func jsonErr(body string) (lines int, err string) {
	body = strings.TrimSpace(body)
	var v any
	switch jerr := json.Unmarshal([]byte(body), &v); {
	case body == "", jerr == nil:
		return strings.Count(body, "\n") + 1, ""
	default:
		var se *json.SyntaxError
		if errors.As(jerr, &se) {
			return 0, fmt.Sprintf("%s (line %d)", se.Error(), strings.Count(body[:min(int(se.Offset), len(body))], "\n")+1)
		}
		return 0, jerr.Error()
	}
}

// colorJSON draws a line of JSON: keys, strings, numbers and true/false/null each in a colour.
func colorJSON(line string) string {
	var b strings.Builder
	key := lipgloss.NewStyle().Foreground(look.Accent)
	str := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	lit := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(line))
			if strings.HasPrefix(strings.TrimSpace(line[j:]), ":") {
				b.WriteString(key.Render(line[i:j]))
			} else {
				b.WriteString(str.Render(line[i:j]))
			}
			i = j
		case c == '-' || c >= '0' && c <= '9' || strings.HasPrefix(line[i:], "true") || strings.HasPrefix(line[i:], "false") || strings.HasPrefix(line[i:], "null"):
			j := i + 1
			for j < len(line) && strings.IndexByte(",}] \t", line[j]) < 0 {
				j++
			}
			b.WriteString(lit.Render(line[i:j]))
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// chip draws a choice's option: picked, picked with the keys, or not.
func chip(label string, picked, focused bool) string {
	switch {
	case picked && focused:
		return look.FocusBar.Foreground(lipgloss.Color("15")).Render(" " + label + " ")
	case picked:
		return look.Sel.Render("‹" + label + "›")
	}
	return look.Dim.Render(" " + label + " ")
}

// View draws the editor w wide and at most h tall; the response gets the room left.
func (e *editor) View(w, h int) string {
	inner := w - 6
	e.hits = nil
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	on := func(id string) bool { return e.focus == id }
	// at notes where a cell was drawn on the line being built.
	at := func(x0, x1 int, id string) { e.hits = append(e.hits, hit{len(lines), x0, x1, id}) }

	// The title, and the ✕.
	title := e.app + " · new request"
	if e.cur != nil {
		title = e.app + " · " + folderLabel(e.keep.Folder) + " · " + e.cur.Name
	}
	if e.dirty {
		title += " · unsaved"
	}
	title = ansi.Truncate(look.Bold.Render(title), inner-3, "…")
	at(inner-2, inner+1, "close")
	add(title+strings.Repeat(" ", max(inner-1-ansi.StringWidth(title), 1))+look.Dim.Render("✕"), "")

	// Name and environment.
	label := func(s string, id string) string {
		if on(id) {
			return look.Bold.Render(s)
		}
		return look.Dim.Render(s)
	}
	e.name.Width = max(inner/2-8, 12)
	line := label("Name ", "name") + e.name.View()
	at(5, 5+e.name.Width+1, "name")
	x := ansi.StringWidth(line) + 3
	line += "   " + label("Env ", "env")
	x += 4
	if len(e.envs) == 0 {
		line += look.Warn.Render("none yet: add one in the Environments tab")
	}
	for i, n := range e.envs {
		c := chip(n, i == e.env, on("env"))
		at(x, x+ansi.StringWidth(c), "env="+strconv.Itoa(i))
		line += c
		x += ansi.StringWidth(c)
	}
	add(line)

	// Method and URL, and where it goes.
	m := chip(Methods[e.method], true, on("method"))
	if !on("method") {
		m = methodStyle(Methods[e.method]).Render("‹" + Methods[e.method] + "›")
	}
	at(0, ansi.StringWidth(m), "method")
	e.url.Width = max(inner-ansi.StringWidth(m)-2, 10)
	at(ansi.StringWidth(m)+1, inner, "url")
	add(m + " " + e.url.View())
	env := e.envNow()
	if q, err := e.request(); err == nil && env.Name != "" {
		filled := env
		filled.Vars = map[string]string{}
		for k, v := range env.Vars {
			filled.Vars[k] = v
		}
		for _, r := range e.vars {
			if n := strings.TrimSpace(r.key.Value()); n != "" && r.val.Value() != "" {
				filled.Vars[n] = r.val.Value()
			}
		}
		add(look.Dim.Render(ansi.Truncate("→ "+filled.Resolve(q.URL), inner, "…")))
	} else {
		add("")
	}
	add("")

	// The tabs.
	counts := []string{"", "", ""}
	n := 0
	for _, r := range e.query {
		if !r.empty() {
			n++
		}
	}
	for _, r := range e.vars {
		if r.fixed || !r.empty() {
			n++
		}
	}
	if n > 0 {
		counts[tabParams] = fmt.Sprintf(" (%d)", n)
	}
	n = 0
	for _, r := range e.heads {
		if r.on && !r.empty() {
			n++
		}
	}
	if n > 0 {
		counts[tabHeaders] = fmt.Sprintf(" (%d)", n)
	}
	if strings.TrimSpace(e.body.Value()) != "" {
		counts[tabBody] = " ●"
	}
	line, x = "", 0
	for i, t := range editorTabs {
		s := " " + t + counts[i] + look.Dim.Render(" ⌘"+strconv.Itoa(i+1)) + " "
		switch {
		case i == e.tab && on("tabs"):
			s = look.FocusBar.Foreground(lipgloss.Color("15")).Render(s)
		case i == e.tab:
			s = look.Active.Render(s)
		default:
			s = look.Dim.Render(s)
		}
		at(x, x+ansi.StringWidth(s), "tab="+strconv.Itoa(i))
		line += s + " "
		x += ansi.StringWidth(s) + 1
	}
	add(line)
	add(look.Dim.Render(strings.Repeat("─", inner)))

	// The tab's rows.
	kw := min(max(inner/3, 14), 30)
	vw := max(inner-kw-9, 10)
	table := func(t string, rows []*kv, checks bool) {
		for _, r := range rows {
			id := func(part string) string { return fmt.Sprintf("%s:%d:%s", t, r.id, part) }
			line, x := "  ", 2
			if checks {
				box := "[✓]"
				if !r.on {
					box = "[ ]"
				}
				if r.empty() {
					box = "   "
				}
				if on(id("on")) {
					box = look.FocusBar.Render(box)
				}
				at(2, 5, id("on"))
				line, x = line+box+" ", 6
			}
			r.key.Width, r.val.Width = kw-2, vw
			// cellView is an input as typed, or its placeholder, padded to w; the input itself
			// (with its cursor) while it has the keys.
			cellView := func(in textinput.Model, cell string, w int) string {
				v := in.View()
				if !on(cell) {
					v = ansi.Truncate(in.Value(), w-1, "…")
					if v == "" {
						v = look.Dim.Render(in.Placeholder)
					}
				}
				return v + strings.Repeat(" ", max(w-ansi.StringWidth(v), 1))
			}
			if r.fixed {
				line += look.Bold.Render(fmt.Sprintf("%-*s", kw, ansi.Truncate(r.key.Value(), kw-1, "…")))
			} else {
				line += cellView(r.key, id("k"), kw)
				at(x, x+kw, id("k"))
			}
			at(x+kw, inner, id("v"))
			v := cellView(r.val, id("v"), vw)
			if !r.on {
				v = look.Dim.Render(ansi.Strip(v))
			}
			add(strings.TrimRight(line+v, " "))
		}
	}
	switch e.tab {
	case tabParams:
		add(look.Bold.Render("Query Params") + look.Dim.Render("  in the URL after ?; type in the last row to add one"))
		table("q", e.query, false)
		add("", look.Bold.Render("Path Variables")+look.Dim.Render("  the {{names}} it uses; their values are kept in "+env.Name+" for every request"))
		table("v", e.vars, false)
	case tabHeaders:
		add(look.Bold.Render("Headers") + look.Dim.Render("  space turns one on or off; type in the last row to add one"))
		table("h", e.heads, true)
		if hd, v := env.AuthHeader(); hd != "" && !e.noAuth {
			add(look.Dim.Render("  auto  " + hd + ": " + v + "  (" + env.Name + "'s auth; its key stays in the Keychain)"))
		}
	case tabBody:
		count, jerr := jsonErr(e.body.Value())
		state := look.Ready.Render("✓ valid JSON")
		switch {
		case strings.TrimSpace(e.body.Value()) == "":
			state = look.Dim.Render("empty")
		case jerr != "":
			state = look.Warn.Render("✗ " + jerr)
		}
		add(look.Bold.Render("Body")+"  "+look.Sel.Render("JSON")+"  "+ansi.Truncate(state, inner-30, "…")+look.Dim.Render(" · "+KeyName("ctrl+b")+" beautify"), "")
		height := min(max(count, strings.Count(e.body.Value(), "\n")+1, 4)+1, max(h/3, 6))
		top := len(lines)
		if on("body") {
			e.body.SetWidth(inner)
			e.body.SetHeight(height)
			add(strings.Split(e.body.View(), "\n")...)
		} else {
			body := strings.Split(e.body.Value(), "\n")
			for i := 0; i < height; i++ {
				switch {
				case i < len(body):
					add(look.Dim.Render(fmt.Sprintf("%3d ", i+1)) + ansi.Truncate(colorJSON(body[i]), inner-4, "…"))
				case i == 0:
					add(look.Dim.Render("  1 " + e.body.Placeholder))
				default:
					add("")
				}
			}
		}
		for l := top; l < len(lines); l++ {
			e.hits = append(e.hits, hit{l, 0, inner, "body"})
		}
	}

	// Errors and notes, then the buttons.
	switch {
	case e.err != "":
		add("", look.Warn.Render(ansi.Truncate(e.err, inner, "…")))
	case e.note != "":
		add("", look.Ready.Render(e.note))
	}
	add("")
	line, x = "", 0
	for i, b := range []struct{ label, key string }{{"Send", "ctrl+r"}, {"Save", "ctrl+s"}, {"Delete", ""}, {"Close", "esc"}} {
		k := ""
		if b.key != "" {
			k = " " + KeyName(b.key)
		}
		s := look.Dim.Render("[") + " " + b.label + look.Dim.Render(k) + " " + look.Dim.Render("]")
		if on(fmt.Sprintf("btn:%d", i)) {
			s = look.Active.Render("[ " + b.label + k + " ]")
		}
		at(x, x+ansi.StringWidth(s), fmt.Sprintf("btn:%d", i))
		line += s + "  "
		x += ansi.StringWidth(s) + 2
	}
	add(line)

	// The response.
	if room := h - 4 - len(lines) - 2; room >= 3 { // the box's border and padding, a rule, its tabs
		add(look.Dim.Render(strings.Repeat("─", inner)))
		add(e.response(inner, room, at)...)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(look.Accent).Padding(1, 2).Width(w - 2).Render(strings.Join(lines, "\n"))
}

// response is the last response's tabs and status, then h lines of its body or headers from scroll.
func (e *editor) response(w, h int, at func(x0, x1 int, id string)) []string {
	r := e.s.runs[e.key()]
	switch {
	case r == nil:
		return []string{look.Dim.Render("Send (" + KeyName("ctrl+r") + ") shows the response here.")}
	case r.running:
		return []string{look.Sel.Render(look.SpinFrames[e.s.Spin%len(look.SpinFrames)]+" Sending") + look.Dim.Render(fmt.Sprintf(" to %s (%ds)…", r.env, int(time.Since(r.at).Seconds())))}
	case r.err != "":
		return strings.Split(look.Warn.Render(look.Wrap(r.err, w)), "\n")
	}
	text := Format(r.res, r.keys, false)
	_, rest, _ := strings.Cut(text, "\n")
	headers, body, _ := strings.Cut(rest, "\n\n")
	nh := 0
	if headers != "" {
		nh = strings.Count(headers, "\n") + 1
	}
	tabs, x := "", 0
	for i, t := range respTabs {
		if i == 1 {
			t += fmt.Sprintf(" (%d)", nh)
		}
		s := " " + t + " "
		switch {
		case i == e.resTab && e.focus == "resp":
			s = look.FocusBar.Foreground(lipgloss.Color("15")).Render(s)
		case i == e.resTab:
			s = look.Active.Render(s)
		default:
			s = look.Dim.Render(s)
		}
		at(x, x+ansi.StringWidth(s), "resp="+strconv.Itoa(i))
		tabs += s + " "
		x += ansi.StringWidth(s) + 1
	}
	status := statusStyle(r.res.Code).Render(r.res.Status) + look.Dim.Render(fmt.Sprintf(" · %d ms · %s · %s · %s", r.res.Took.Milliseconds(), size(len(r.res.Body)), r.env, look.Ago(r.at)))
	out := []string{tabs + strings.Repeat(" ", max(w-ansi.StringWidth(tabs)-ansi.StringWidth(status), 1)) + status}
	var lines []string
	if e.resTab == 1 {
		for _, l := range strings.Split(headers, "\n") {
			k, v, _ := strings.Cut(l, ":")
			lines = append(lines, look.Bold.Render(k)+look.Dim.Render(":")+ansi.Truncate(v, max(w-len(k)-1, 10), "…"))
		}
	} else {
		var v any
		isJSON := json.Unmarshal(r.res.Body, &v) == nil
		for _, l := range strings.Split(body, "\n") {
			if isJSON {
				lines = append(lines, ansi.Truncate(colorJSON(l), w, "…"))
			} else {
				lines = append(lines, strings.Split(look.Wrap(l, w), "\n")...)
			}
		}
	}
	h-- // the tabs' line
	e.scroll = min(e.scroll, max(len(lines)-h, 0))
	end := min(e.scroll+h, len(lines))
	shown := lines[e.scroll:end]
	if end < len(lines) && len(shown) > 0 {
		shown[len(shown)-1] = look.Dim.Render(fmt.Sprintf("… %d more lines · pgdn or the wheel scrolls", len(lines)-end))
	}
	return append(out, shown...)
}

// Click handles a click at (x, y) in the box View drew last.
func (e *editor) Click(x, y int) tea.Cmd {
	x, y = x-3, y-2 // inside the border and padding
	for _, hit := range e.hits {
		if hit.line != y || x < hit.x0 || x >= hit.x1 {
			continue
		}
		id, arg, _ := strings.Cut(hit.id, "=")
		i, _ := strconv.Atoi(arg)
		switch {
		case id == "close":
			e.close()
		case id == "env":
			e.focusOn("env")
			if e.env != i {
				e.env, e.dirty = i, true
				e.syncVars()
			}
		case id == "tab":
			e.tab = i
			e.focusOn("tabs")
		case id == "resp":
			e.resTab, e.scroll = i, 0
			e.focusOn("resp")
		case id == "method":
			e.focusOn("method")
		case strings.HasPrefix(id, "btn:"):
			e.focusOn(id)
			return e.press(id)
		case strings.HasSuffix(id, ":on"):
			e.focusOn(id)
			if r, _ := e.cell(id); r != nil && !r.empty() {
				r.on = !r.on
				e.edited(id)
			}
		default:
			e.focusOn(id)
		}
		e.tidy()
		return nil
	}
	return nil
}

// Wheel scrolls the response.
func (e *editor) Wheel(down bool) {
	if down {
		e.scroll += 3
	} else {
		e.scroll = max(e.scroll-3, 0)
	}
}
