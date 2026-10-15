package requests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/look"
)

// A request's editor, laid out like Postman: the name and environment, the method and URL (with
// where it goes), then tabs: Params (the URL's query as rows, and the {{variables}} it uses, whose
// values live in the environment), Auth, Headers (rows, each can be turned off) and Body (JSON,
// with line numbers). Under the buttons, the last response, with its Body and Headers.
//
// Keys: tab/⇧tab move between cells (a row's Key, then its Value, then the next row), ↑↓ too
// (inside the body they move the cursor), ←/→ pick on a choice, the tabs or the buttons, space
// turns a header on or off, ⏎ presses a button. ⌘R sends, ⌘S saves, ⌘B beautifies the body,
// pgup/pgdn scroll the response, esc closes (asking once when there are unsaved changes). Typing in
// a table's empty last row adds a row; clearing a row removes it.

// The editor's tabs, and the response's.
var (
	editorTabs = []string{"Params", "Auth", "Headers", "Body"}
	respTabs   = []string{"Body", "Headers"}
)

const (
	tabParams = iota
	tabAuth
	tabHeaders
	tabBody
)

// kv is a row of a table: a query param, a variable or a header.
type kv struct {
	id       int
	on       bool // sent; only headers can be turned off
	key, val textinput.Model
	fixed    bool // a variable the request uses: its name comes from the request, not typed
	edited   bool // a variable's value was changed here, so it's saved to the environment
}

// empty reports whether nothing is typed in the row.
func (r *kv) empty() bool {
	return strings.TrimSpace(r.key.Value()) == "" && strings.TrimSpace(r.val.Value()) == ""
}

// hit is where a cell was drawn, for clicks: a line of the box's content and its columns.
type hit struct {
	line, x0, x1 int
	id           string
}

// editor is an open request.
type editor struct {
	s      *Screen
	app    string
	cur    *Request // the saved request it edits, once there is one
	keep   Request  // what the editor doesn't show and saving keeps: its folder, tags and file
	name   textinput.Model
	url    textinput.Model
	envs   []string
	env    int
	method int
	noAuth bool
	tab    int
	resTab int
	query  []*kv
	vars   []*kv
	heads  []*kv
	body   textarea.Model
	seq    int    // the last row id given
	focus  string // the cell with the keys: "name", "url", "q:<id>:k", "h:<id>:on", "btn:0"…
	scroll int    // of the response
	err    string
	note   string
	dirty  bool
	leave  bool // esc was pressed once with unsaved changes
	del    bool // Delete was pressed once
	closed bool
	hits   []hit
}

// input makes a one-line input.
func input(value, placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt, in.Placeholder, in.CharLimit = "", placeholder, 8000
	in.PlaceholderStyle = look.Dim
	in.SetValue(value)
	return in
}

// editor opens a request (a new one in app's folder when edit is nil).
func (s *Screen) editor(edit *Request, app, folder string) *editor {
	r := Request{App: app, Folder: folder, Method: "GET"}
	if edit != nil {
		r = *edit
	}
	e := &editor{s: s, app: app, cur: edit, keep: r, noAuth: r.NoAuth, focus: "name"}
	if edit != nil {
		e.focus = "btn:0" // on Send
	}
	e.name = input(r.Name, "e.g. Create subscription")
	e.url = input(r.Path(), "a path like /orgs?limit=20, or a full URL")
	for _, env := range s.envs[app] {
		e.envs = append(e.envs, env.Name)
		if env.Name == s.cur[app] {
			e.env = len(e.envs) - 1
		}
	}
	e.method = max(slices.Index(Methods, r.Method), 0)
	for _, h := range r.Headers {
		e.heads = append(e.heads, e.row(h[0], h[1], true))
	}
	for _, h := range r.Off {
		e.heads = append(e.heads, e.row(h[0], h[1], false))
	}
	e.body = textarea.New()
	e.body.ShowLineNumbers, e.body.Prompt, e.body.CharLimit = true, "", 0
	e.body.Placeholder = `{"name": "Acme"}`
	e.body.FocusedStyle.CursorLine = lipgloss.NewStyle()
	e.body.SetValue(pretty(r.Body))
	e.queryFromURL()
	e.syncVars()
	e.tidy()
	e.focusOn(e.focus)
	return e
}

// row makes a table row.
func (e *editor) row(key, val string, on bool) *kv {
	e.seq++
	return &kv{id: e.seq, on: on, key: input(key, "Key"), val: input(val, "Value")}
}

// pretty indents JSON two spaces; anything else stays as it is.
func pretty(body string) string {
	var b bytes.Buffer
	if json.Indent(&b, []byte(strings.TrimSpace(body)), "", "  ") == nil {
		return b.String()
	}
	return strings.TrimSpace(body)
}

// Closed reports whether the editor was closed.
func (e *editor) Closed() bool { return e.closed }

// envNow is the environment picked, or a zero Env when the application has none.
func (e *editor) envNow() Env {
	for _, env := range e.s.envs[e.app] {
		if e.env < len(e.envs) && env.Name == e.envs[e.env] {
			return env
		}
	}
	return Env{}
}

// ---- the URL's query and the variables ----

// splitURL is a URL's path and its query string.
func splitURL(u string) (path, query string) {
	path, query, _ = strings.Cut(u, "?")
	return path, query
}

// queryFromURL makes the query rows from the URL, as typed (variables stay as they are).
func (e *editor) queryFromURL() {
	_, q := splitURL(e.url.Value())
	e.query = nil
	for _, p := range strings.Split(q, "&") {
		if p == "" {
			continue
		}
		k, v, _ := strings.Cut(p, "=")
		e.query = append(e.query, e.row(k, v, true))
	}
}

// urlFromQuery writes the query rows back into the URL.
func (e *editor) urlFromQuery() {
	path, _ := splitURL(e.url.Value())
	var ps []string
	for _, r := range e.query {
		if k := strings.TrimSpace(r.key.Value()); k != "" {
			ps = append(ps, k+"="+strings.TrimSpace(r.val.Value()))
		}
	}
	if len(ps) > 0 {
		path += "?" + strings.Join(ps, "&")
	}
	pos := e.url.Position()
	e.url.SetValue(path)
	e.url.SetCursor(pos)
}

// used are the {{variables}} the request uses that the environment fills in from plain values:
// not baseUrl, and never a key (those stay in the Keychain).
func (e *editor) used() []string {
	env := e.envNow()
	text := e.url.Value() + "\n" + e.body.Value()
	for _, h := range e.heads {
		if h.on {
			text += "\n" + h.val.Value()
		}
	}
	var out []string
	for _, m := range varRef.FindAllStringSubmatch(text, -1) {
		n := m[1]
		if n != "baseUrl" && !slices.Contains(env.Keys, n) && n != env.Auth.Key && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// syncVars keeps a row for each variable the request uses, with the environment's value unless
// you changed it here; rows you added stay.
func (e *editor) syncVars() {
	env := e.envNow()
	used := e.used()
	var rows []*kv
	for _, r := range e.vars {
		if !r.fixed || slices.Contains(used, r.key.Value()) || e.inRow("v", r) {
			rows = append(rows, r)
		}
	}
	for _, n := range used {
		if !slices.ContainsFunc(rows, func(r *kv) bool { return strings.TrimSpace(r.key.Value()) == n }) {
			r := e.row(n, env.Vars[n], true)
			r.fixed = true
			r.val.Placeholder = "no value in " + env.Name
			rows = append(rows, r)
		}
	}
	for _, r := range rows {
		if r.fixed && !r.edited {
			r.val.SetValue(env.Vars[r.key.Value()])
			r.val.Placeholder = "no value in " + env.Name
		}
	}
	e.vars = rows
}

// inRow reports whether the focus is in row r of table t ("q", "v" or "h").
func (e *editor) inRow(t string, r *kv) bool {
	return strings.HasPrefix(e.focus, fmt.Sprintf("%s:%d:", t, r.id))
}

// tidy drops the empty rows you've left and keeps one empty row at the end of each table, to add to.
func (e *editor) tidy() {
	clean := func(t string, rows []*kv) []*kv {
		var out []*kv
		for i, r := range rows {
			if r.empty() && !r.fixed && i < len(rows)-1 && !e.inRow(t, r) {
				continue
			}
			out = append(out, r)
		}
		if len(out) == 0 || !out[len(out)-1].empty() || out[len(out)-1].fixed {
			out = append(out, e.row("", "", true))
		}
		return out
	}
	e.query, e.heads = clean("q", e.query), clean("h", e.heads)
	var free []*kv // the variables you added go after the request's own
	var fixed []*kv
	for _, r := range e.vars {
		if r.fixed {
			fixed = append(fixed, r)
		} else {
			free = append(free, r)
		}
	}
	e.vars = append(fixed, clean("v", free)...)
}

// ---- cells and focus ----

// cells are the cells tab moves through, in order.
func (e *editor) cells() []string {
	out := []string{"name"}
	if len(e.envs) > 0 {
		out = append(out, "env")
	}
	out = append(out, "method", "url", "tabs")
	switch e.tab {
	case tabParams:
		for _, r := range e.query {
			out = append(out, fmt.Sprintf("q:%d:k", r.id), fmt.Sprintf("q:%d:v", r.id))
		}
		for _, r := range e.vars {
			if !r.fixed {
				out = append(out, fmt.Sprintf("v:%d:k", r.id))
			}
			out = append(out, fmt.Sprintf("v:%d:v", r.id))
		}
	case tabAuth:
		out = append(out, "auth")
	case tabHeaders:
		for _, r := range e.heads {
			out = append(out, fmt.Sprintf("h:%d:on", r.id), fmt.Sprintf("h:%d:k", r.id), fmt.Sprintf("h:%d:v", r.id))
		}
	case tabBody:
		out = append(out, "body")
	}
	return append(out, "btn:0", "btn:1", "btn:2", "btn:3", "resp")
}

// cell finds a table cell: its row and which part ("k", "v" or "on"); nil when it isn't one.
func (e *editor) cell(id string) (*kv, string) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return nil, ""
	}
	n, _ := strconv.Atoi(parts[1])
	rows := map[string][]*kv{"q": e.query, "v": e.vars, "h": e.heads}[parts[0]]
	for _, r := range rows {
		if r.id == n {
			return r, parts[2]
		}
	}
	return nil, ""
}

// input is the text input of a cell, or nil.
func (e *editor) input(id string) *textinput.Model {
	switch id {
	case "name":
		return &e.name
	case "url":
		return &e.url
	}
	r, part := e.cell(id)
	switch {
	case r == nil:
		return nil
	case part == "k":
		return &r.key
	case part == "v":
		return &r.val
	}
	return nil
}

// focusOn gives a cell the keys.
func (e *editor) focusOn(id string) {
	e.focus = id
	e.name.Blur()
	e.url.Blur()
	for _, rows := range [][]*kv{e.query, e.vars, e.heads} {
		for _, r := range rows {
			r.key.Blur()
			r.val.Blur()
		}
	}
	e.body.Blur()
	if in := e.input(id); in != nil {
		in.Focus()
	}
	if id == "body" {
		e.body.Focus()
	}
}

// move goes d cells on (round the end), then drops rows left empty.
func (e *editor) move(d int) {
	cells := e.cells()
	i := slices.Index(cells, e.focus)
	e.focusOn(cells[((i+d)%len(cells)+len(cells))%len(cells)])
	e.tidy()
	if !slices.Contains(e.cells(), e.focus) { // its row went
		e.focusOn("tabs")
	}
}

// edited notes a change: the URL and the query keep each other in step, and the variables follow.
func (e *editor) edited(id string) {
	e.dirty, e.leave, e.note, e.del = true, false, "", false
	switch {
	case id == "url":
		keep := e.query
		e.queryFromURL()
		if len(e.query) == len(keep) { // same rows: keep their ids, so the focus stays put
			for i := range keep {
				keep[i].key.SetValue(e.query[i].key.Value())
				keep[i].val.SetValue(e.query[i].val.Value())
			}
			e.query = keep
		}
	case strings.HasPrefix(id, "q:"):
		e.urlFromQuery()
	case strings.HasPrefix(id, "v:") && strings.HasSuffix(id, ":v"):
		if r, _ := e.cell(id); r != nil {
			r.edited = true
		}
	}
	e.syncVars()
	e.tidy()
}

// ---- keys ----

// Key handles a key.
func (e *editor) Key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k != "esc" {
		e.leave = false
	}
	switch k {
	case "ctrl+r":
		return e.sendNow()
	case "ctrl+s":
		e.saveNow()
		return nil
	case "ctrl+b":
		e.beautify()
		return nil
	case "esc":
		e.close()
		return nil
	case "pgdown":
		e.scroll += 10
		return nil
	case "pgup":
		e.scroll = max(e.scroll-10, 0)
		return nil
	case "tab":
		e.move(1)
		return nil
	case "shift+tab":
		e.move(-1)
		return nil
	}
	if e.focus == "body" {
		switch {
		case k == "up" && e.body.Line() == 0, k == "down" && e.body.Line() >= e.body.LineCount()-1:
			e.move(map[string]int{"up": -1, "down": 1}[k])
			return nil
		}
		before := e.body.Value()
		var cmd tea.Cmd
		e.body, cmd = e.body.Update(msg)
		if e.body.Value() != before {
			e.edited("body")
		}
		return cmd
	}
	if k == "up" || k == "down" {
		e.move(map[string]int{"up": -1, "down": 1}[k])
		return nil
	}
	if in := e.input(e.focus); in != nil {
		if k == "enter" {
			e.move(1)
			return nil
		}
		before := in.Value()
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		if in.Value() != before {
			e.edited(e.focus)
		}
		return cmd
	}
	step := map[string]int{"left": -1, "right": 1, " ": 1}[k]
	switch r, part := e.cell(e.focus); {
	case part == "on" && (k == " " || k == "enter"):
		r.on = !r.on
		e.edited(e.focus)
	case e.focus == "env" && step != 0 && len(e.envs) > 0:
		e.env = (e.env + step + len(e.envs)) % len(e.envs)
		e.dirty = true
		e.syncVars()
	case e.focus == "method" && step != 0:
		e.method = (e.method + step + len(Methods)) % len(Methods)
		e.edited("method")
	case e.focus == "auth" && step != 0:
		e.noAuth = !e.noAuth
		e.edited("auth")
	case e.focus == "tabs" && step != 0 && k != " ":
		e.tab = (e.tab + step + len(editorTabs)) % len(editorTabs)
	case e.focus == "resp" && step != 0 && k != " ":
		e.resTab = (e.resTab + 1) % len(respTabs)
		e.scroll = 0
	case strings.HasPrefix(e.focus, "btn:") && (k == "left" || k == "right"):
		b, _ := strconv.Atoi(strings.TrimPrefix(e.focus, "btn:"))
		e.focusOn(fmt.Sprintf("btn:%d", (b+step+4)%4))
	case strings.HasPrefix(e.focus, "btn:") && (k == "enter" || k == " "):
		return e.press(e.focus)
	case k == "enter":
		e.move(1)
	}
	return nil
}

// press runs a button: Send, Save, Delete or Close.
func (e *editor) press(id string) tea.Cmd {
	switch id {
	case "btn:0":
		return e.sendNow()
	case "btn:1":
		e.saveNow()
	case "btn:2":
		e.deleteNow()
	case "btn:3":
		e.close()
	}
	return nil
}

// close closes, after asking once when there are unsaved changes.
func (e *editor) close() {
	if e.dirty && !e.leave {
		e.leave, e.err = true, "Unsaved changes · press esc or Close again to discard them"
		return
	}
	e.closed = true
}

// beautify indents the body's JSON (⌘B).
func (e *editor) beautify() {
	if _, err := jsonErr(e.body.Value()); err != "" {
		e.err = "The body isn't valid JSON: " + err
		return
	}
	if p := pretty(e.body.Value()); p != e.body.Value() {
		e.body.SetValue(p)
		e.edited("body")
	}
	e.tab = tabBody
}

// ---- the request it makes ----

// request is the request as the editor has it.
func (e *editor) request() (Request, error) {
	q := e.keep
	q.App, q.Name, q.Method, q.NoAuth = e.app, strings.TrimSpace(e.name.Value()), Methods[e.method], e.noAuth
	q.Body = strings.TrimSpace(e.body.Value())
	q.Headers, q.Off = nil, nil
	for _, h := range e.heads {
		k := strings.TrimSpace(h.key.Value())
		if k == "" {
			continue
		}
		if h.on {
			q.Headers = append(q.Headers, [2]string{k, strings.TrimSpace(h.val.Value())})
		} else {
			q.Off = append(q.Off, [2]string{k, strings.TrimSpace(h.val.Value())})
		}
	}
	base := e.envNow().BaseURL()
	switch u := strings.TrimSpace(e.url.Value()); {
	case u == "":
		return q, errors.New("Type the URL: a path like /orgs, or a full URL")
	case strings.HasPrefix(u, "{{"):
		q.URL = u
	case base != "" && strings.HasPrefix(u, base):
		q.URL = "{{baseUrl}}" + strings.TrimPrefix(u, base) // a pasted full URL becomes a path
	case strings.Contains(u, "://"):
		q.URL = u
	default:
		q.URL = "{{baseUrl}}/" + strings.TrimPrefix(u, "/")
	}
	if q.Name == "" {
		q.Name = q.Method + " " + q.Path()
	}
	return q, nil
}

// saveVars keeps the variables' values you changed in the picked environment.
func (e *editor) saveVars() error {
	env := e.envNow()
	if env.Name == "" {
		return nil
	}
	changed := false
	for _, r := range e.vars {
		n := strings.TrimSpace(r.key.Value())
		if n == "" || n == "baseUrl" || slices.Contains(env.Keys, n) || (r.fixed && !r.edited) {
			continue
		}
		if !ValidName(n) {
			return fmt.Errorf("%q isn't a variable name: one word, like businessId", n)
		}
		env.Vars[n] = strings.TrimSpace(r.val.Value())
		r.edited, changed = false, true
	}
	if !changed {
		return nil
	}
	if err := SaveEnv(e.s.dir, env); err != nil {
		return err
	}
	e.s.Load()
	return nil
}

// key is where the request's response is kept: its id, or the application's new request.
func (e *editor) key() string {
	if e.cur != nil {
		return e.cur.ID()
	}
	return "new:" + e.app
}

// sendNow sends the request (⌘R), with the variables' values as they are here.
func (e *editor) sendNow() tea.Cmd {
	e.err, e.scroll = "", 0
	q, err := e.request()
	if err == nil {
		err = e.saveVars()
	}
	if err != nil {
		e.err = err.Error()
		return nil
	}
	if env := e.envNow(); env.Name != "" {
		e.s.setEnv(e.app, env.Name) // sending with an environment makes it the one in use
	}
	return e.s.send(q, e.key())
}

// saveNow saves the request, and the variables' values in the environment (⌘S).
func (e *editor) saveNow() {
	e.err = ""
	q, err := e.request()
	if err == nil {
		err = e.saveVars()
	}
	if err != nil {
		e.err = err.Error()
		return
	}
	old := e.key()
	saved, err := Save(e.s.dir, q)
	if err != nil {
		e.err = err.Error()
		return
	}
	if r := e.s.runs[old]; r != nil {
		e.s.runs[saved.ID()] = r // the response stays with it
	}
	e.cur, e.keep = &saved, saved
	e.s.Load()
	e.dirty, e.note = false, "Saved ✓"
}

// deleteNow deletes the request, on a second press.
func (e *editor) deleteNow() {
	if e.cur == nil {
		e.closed = true
		return
	}
	if !e.del {
		e.del, e.err = true, "Delete "+e.cur.Name+"? Press Delete again"
		return
	}
	if err := Delete(e.s.dir, *e.cur); err != nil {
		e.err = err.Error()
		return
	}
	e.s.Notice = "Deleted " + e.cur.Name
	e.s.Load()
	e.closed = true
}

// ---- drawing ----

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
	counts := []string{"", "", "", ""}
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
	if hd, _ := env.AuthHeader(); hd != "" && !e.noAuth {
		counts[tabAuth] = " ●"
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
		s := " " + t + counts[i] + " "
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
	add(line + look.Dim.Render(" ←/→ switch"))
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
	case tabAuth:
		lbl := "the environment's (" + env.AuthLabel() + ")"
		line := "  " + chip(lbl, !e.noAuth, on("auth") && !e.noAuth) + " " + chip("none", e.noAuth, on("auth") && e.noAuth)
		at(2, ansi.StringWidth(line), "auth")
		add(line)
		if hd, v := env.AuthHeader(); hd != "" && !e.noAuth {
			add("", look.Dim.Render("  Every send adds "+hd+": "+v+" · the key stays in the Keychain and goes only to "+strings.Join(env.AllHosts(), ", ")))
		}
	case tabHeaders:
		add(look.Bold.Render("Headers") + look.Dim.Render("  space turns one on or off; type in the last row to add one"))
		table("h", e.heads, true)
		if hd, v := env.AuthHeader(); hd != "" && !e.noAuth {
			add(look.Dim.Render("  auto  " + hd + ": " + v + "  (" + env.Name + "'s auth · Auth tab)"))
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

// ---- the mouse ----

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
		case id == "auth":
			e.focusOn("auth")
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
