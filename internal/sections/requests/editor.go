package requests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/widgets/look"
)

// A request's editor, laid out like Postman: the name and environment, the method and URL (with
// where it goes), then tabs: Params (the URL's query as rows, and the {{variables}} it uses, whose
// values live in the environment), Headers (rows, each can be turned off; the environment's auth
// header is added) and Body (JSON, with line numbers). Under the buttons, the last response, with
// its Body and Headers.
//
// Keys: ⌘1 ⌘2 ⌘3 switch the tabs (crew hands them over: Section.ModalGotoTab). tab/⇧tab move
// between cells (a row's Key, then its Value, then the next row), ↑↓ too (inside the body they move
// the cursor), ←/→ pick on a choice, the tabs or the buttons, space
// turns a header on or off, ⏎ presses a button. ⌘R sends, ⌘S saves, ⌘B beautifies the body,
// pgup/pgdn scroll the response, esc closes (asking once when there are unsaved changes). Typing in
// a table's empty last row adds a row; clearing a row removes it.

// The editor's tabs, and the response's.
var (
	editorTabs = []string{"Params", "Headers", "Body"}
	respTabs   = []string{"Body", "Headers"}
)

const (
	tabParams = iota
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
	noAuth bool // the request goes without the environment's auth ("# @auth none"), kept as it is
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

// Tab is the open tab, and how many there are (⌘1…, through crew).
func (e *editor) Tab() (cur, n int) { return e.tab, len(editorTabs) }

// SetTab opens tab i.
func (e *editor) SetTab(i int) {
	e.tab = i
	if !slices.Contains(e.cells(), e.focus) { // the cell it was in is on another tab
		e.focusOn("tabs")
	}
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
