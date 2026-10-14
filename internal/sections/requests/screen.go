package requests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/look"
)

// Screen is the REQUESTS section's main pane, in two levels crew's sidebar mirrors:
//
//	REQUESTS      the applications (n adds one)
//	  accounts    an application: its Requests, grouped in folders (A to Z, then default), and
//	              its Environments; tab switches. ⏎ on a request opens it in a popup.
//
// Its section (section.go) hands it keys, clicks and messages, and draws what it returns; the
// screen keeps its own state (what's loaded, the cursor on each page, the open popup). It reports
// what crew should do (open an application, go back, open the chat) as an Event, and one-line
// feedback in Notice.
type Screen struct {
	dir string
	ctx context.Context

	apps   []string
	reqs   []Request
	envs   map[string][]Env // each application's environments
	cur    map[string]string
	keySet map[string]bool // "<app>/<env>": whether its key has a value
	folded map[string]map[string]bool
	runs   map[string]*sent // each request's last send, by id ("new:<app>" before it's saved)

	app    string         // the open application; "" is the list of applications
	pane   int            // the application's tab: paneRequests or paneEnvs
	cursor map[string]int // per page
	tag    map[string]string
	popup  *Form
	rowAt  map[int]int // the body's line -> index into the page's rows, from the last draw
	chipAt [][3]int    // the tag filters' columns and index, on the body's second line
	actAt  [][3]int    // the action buttons' columns and index, on the body's first line
	acts   []action    // those buttons
	tabAt  [][3]int    // the tab bar's columns and index

	Notice string // one line of feedback for crew to show; crew clears it
	Spin   int    // the spinner's frame, set by crew
	Err    string
}

const (
	paneRequests = iota
	paneEnvs
)

// Event is what the screen asks crew to do after a key or click.
type Event struct {
	Open  string // open this application (crew selects its sidebar row)
	Back  bool   // esc with nothing left to close: go up a level
	Chat  bool   // open the REQUESTS chat…
	About string
}

// DoneMsg is a request's send coming back.
type DoneMsg struct {
	id   string
	res  *Response
	keys map[string]string
	err  error
}

type sent struct {
	running bool
	env     string
	at      time.Time
	res     *Response
	keys    map[string]string // the values it used, to blank them out of what's shown
	err     string
}

// New makes the screen for the requests in dir.
func New(ctx context.Context, dir string) *Screen {
	s := &Screen{dir: dir, ctx: ctx, runs: map[string]*sent{}, cursor: map[string]int{}, tag: map[string]string{}}
	s.Load()
	return s
}

// Dir is the requests folder.
func (s *Screen) Dir() string { return s.dir }

// Load re-reads the applications, requests, environments, which keys are set and what's folded.
func (s *Screen) Load() {
	s.Err = ""
	if err := os.MkdirAll(s.dir, 0o755); err == nil {
		err = MoveLoose(s.dir)
		if err != nil {
			s.Err = err.Error()
		}
	}
	reqs, err := Load(s.dir)
	if err != nil {
		s.Err = err.Error()
	}
	s.reqs, s.apps = reqs, Apps(s.dir)
	s.envs, s.cur, s.keySet, s.folded = map[string][]Env{}, map[string]string{}, map[string]bool{}, map[string]map[string]bool{}
	for _, app := range s.apps {
		s.cur[app] = Current(s.dir, app)
		s.folded[app] = Folded(s.dir, app)
		for _, name := range Envs(s.dir, app) {
			e, _ := LoadEnv(s.dir, app, name)
			s.envs[app] = append(s.envs[app], e)
			if e.Auth.Key != "" {
				s.keySet[app+"/"+name] = HasKey(e, e.Auth.Key) // read once here, not on every frame
			}
		}
	}
	if s.app != "" && !slices.Contains(s.apps, s.app) {
		s.app = ""
	}
}

// Apps lists the applications.
func (s *Screen) Apps() []string { return s.apps }

// Show opens an application ("" for the list of applications).
func (s *Screen) Show(app string) {
	if app != s.app && app != "" {
		s.pane = paneRequests
	}
	if app == "" && s.app != "" { // back on the list, with the cursor on the one you left
		s.cursor[""] = max(slices.Index(s.apps, s.app), 0)
	}
	s.app = app
}

// AppLine is an application's second line in the sidebar.
func (s *Screen) AppLine(app string) string {
	n := len(s.appReqs(app))
	return fmt.Sprintf("%d request%s · %s", n, look.Plural(n), s.cur[app])
}

// Busy reports whether a request is being sent (crew keeps the spinner going).
func (s *Screen) Busy() bool {
	for _, r := range s.runs {
		if r.running {
			return true
		}
	}
	return false
}

// Popup is the open popup, or nil.
func (s *Screen) Popup() *Form {
	if s.popup != nil && s.popup.closed {
		s.popup = nil
	}
	return s.popup
}

// Done takes a request's send coming back.
func (s *Screen) Done(msg DoneMsg) {
	r := s.runs[msg.id]
	if r == nil {
		return
	}
	r.running, r.res, r.keys, r.at, r.err = false, msg.res, msg.keys, time.Now(), ""
	if msg.err != nil {
		r.err = Redact(msg.err.Error(), msg.keys, false)
	}
}

func (s *Screen) appReqs(app string) []Request {
	var out []Request
	for _, q := range s.reqs {
		if q.App == app {
			out = append(out, q)
		}
	}
	return out
}

func (s *Screen) envOf(app string) Env {
	for _, e := range s.envs[app] {
		if e.Name == s.cur[app] {
			return e
		}
	}
	return Env{App: app, Vars: map[string]string{}}
}

func (s *Screen) setEnv(app, env string) {
	if err := SetCurrent(s.dir, app, env); err != nil {
		s.Notice = err.Error()
		return
	}
	s.cur[app] = env
}

func (s *Screen) nextEnv(app string) {
	envs := s.envs[app]
	if len(envs) == 0 {
		s.Notice = app + " has no environments yet · its Environments tab adds one"
		return
	}
	i := slices.IndexFunc(envs, func(e Env) bool { return e.Name == s.cur[app] })
	next := envs[(i+1)%len(envs)]
	s.setEnv(app, next.Name)
	s.Notice = fmt.Sprintf("%s now goes to %s · %s", app, next.Name, next.BaseURL())
}

// send sends a request with its application's current environment; the response goes in runs[key].
func (s *Screen) send(q Request, key string) tea.Cmd {
	env, ctx := s.envOf(q.App), s.ctx
	s.runs[key] = &sent{running: true, env: env.Name, at: time.Now()}
	return func() tea.Msg {
		if env.Name == "" {
			return DoneMsg{id: key, err: fmt.Errorf("%s has no environment yet: add one in its Environments tab", q.App)}
		}
		keys, err := KeysFor(q, env)
		if err != nil {
			return DoneMsg{id: key, err: err}
		}
		res, err := Send(ctx, q, env, keys)
		return DoneMsg{key, res, keys, err}
	}
}

// ---- the pages' rows ----

// row is a line of the open page: an application, a folder, a request (index into s.reqs) or an
// environment.
type row struct {
	app, folder, env string
	req              int // -1 unless a request
}

func (s *Screen) pageKey() string {
	if s.app != "" && s.pane == paneEnvs {
		return s.app + "/envs"
	}
	return s.app
}

// folderNames is an application's folders: on disk A to Z, then default when it has requests.
func (s *Screen) folderNames(app string) []string {
	out := Folders(s.dir, app)
	if slices.ContainsFunc(s.reqs, func(q Request) bool { return q.App == app && q.Folder == "" }) {
		out = append(out, "")
	}
	return out
}

func (s *Screen) rows() []row {
	var out []row
	switch {
	case s.app == "":
		for _, a := range s.apps {
			out = append(out, row{app: a, req: -1})
		}
	case s.pane == paneEnvs:
		for _, e := range s.envs[s.app] {
			out = append(out, row{app: s.app, env: e.Name, req: -1})
		}
	default:
		tag := s.tag[s.app]
		for _, f := range s.folderNames(s.app) {
			var in []row
			for i, q := range s.reqs {
				if q.App == s.app && q.Folder == f && (tag == "" || slices.Contains(q.Tags, tag)) {
					in = append(in, row{app: s.app, folder: f, req: i})
				}
			}
			if tag != "" && len(in) == 0 {
				continue // filtering: only folders with matches
			}
			out = append(out, row{app: s.app, folder: f, req: -1})
			if !s.folded[s.app][f] {
				out = append(out, in...)
			}
		}
	}
	return out
}

func (s *Screen) selected() (row, bool) {
	rows := s.rows()
	if len(rows) == 0 {
		return row{req: -1}, false
	}
	i := min(max(s.cursor[s.pageKey()], 0), len(rows)-1)
	s.cursor[s.pageKey()] = i
	return rows[i], true
}

func (s *Screen) moveCursor(d int) {
	n := len(s.rows())
	s.cursor[s.pageKey()] = min(max(s.cursor[s.pageKey()]+d, 0), max(n-1, 0))
}

func folderLabel(f string) string {
	if f == "" {
		return DefaultFolder
	}
	return f
}

func (s *Screen) toggleFold(app, folder string, fold bool) {
	if s.folded[app] == nil {
		s.folded[app] = map[string]bool{}
	}
	s.folded[app][folder] = fold
	if err := SetFolded(s.dir, app, s.folded[app]); err != nil {
		s.Notice = err.Error()
	}
}

// tags is an application's tags, A to Z.
func (s *Screen) tags(app string) []string {
	var out []string
	for _, q := range s.appReqs(app) {
		for _, t := range q.Tags {
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	slices.Sort(out)
	return out
}

// ---- drawing ----

// Header is the main pane's first line.
func (s *Screen) Header(w int) string {
	if s.app == "" {
		return ansi.Truncate(look.Bold.Render("REQUESTS")+look.Dim.Render(fmt.Sprintf("  %d application%s", len(s.apps), look.Plural(len(s.apps)))), w, "…")
	}
	env := s.envOf(s.app)
	where := look.Warn.Render("no environment yet: tab, then n")
	if env.Name != "" {
		where = look.Sel.Render(env.Name) + look.Dim.Render(" "+env.BaseURL())
	}
	n := len(s.appReqs(s.app))
	return ansi.Truncate(look.Bold.Render(s.app)+look.Dim.Render(fmt.Sprintf("  %d request%s · ", n, look.Plural(n)))+where, w, "…")
}

// Tabs is the main pane's second line: an application's tabs, or a rule on the list.
func (s *Screen) Tabs(w int) string {
	s.tabAt = nil
	if s.app == "" {
		return look.Dim.Render(strings.Repeat("─", w))
	}
	line, x := "", 0
	for i, n := range []string{"Requests", "Environments"} {
		n = fmt.Sprintf("%s ⌘%d", n, i+1) // ⌘1, ⌘2 jump to it, as on every tab bar
		t := look.Dim.Padding(0, 1).Render(n)
		if i == s.pane {
			t = look.Active.Padding(0, 1).Render(n)
		}
		s.tabAt = append(s.tabAt, [3]int{x, x + ansi.StringWidth(t), i})
		line += t + " "
		x += ansi.StringWidth(t) + 1
	}
	return ansi.Truncate(line+look.Dim.Render("  tab switches"), w, "…")
}

// Hint is the line under the main pane.
func (s *Screen) Hint() string {
	switch {
	case s.app == "":
		return look.KeyHints("  ⏎ opens · n new application · E next environment · x delete · c ask Claude")
	case s.pane == paneEnvs:
		return look.KeyHints("  ⏎ edits · space uses it · n new environment · x delete · tab Requests · esc back")
	}
	return look.KeyHints("  ⏎ opens a request or folds a folder · n new request · f new folder · m move · s send · x delete · E env · t tag · c Claude")
}

// columns of a page: widths, the first takes what's left.
func widths(w int, fixed ...int) []int {
	used := 2
	for _, f := range fixed {
		used += f + 1
	}
	return append([]int{max(w-used, 16)}, fixed...)
}

func cell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + " "
}

// Body draws the open page w wide and h tall, with the cursor's row in view.
func (s *Screen) Body(w, h int, focused bool) string {
	rows := s.rows()
	s.rowAt, s.chipAt = map[int]int{}, nil
	lines := []string{s.actionBar(w)}
	var head string
	var cols []int
	switch {
	case s.app == "":
		cols = widths(w, 9, 34, 12)
		head = cell("application", cols[0]) + cell("requests", cols[1]) + cell("environments", cols[2]) + cell("key", cols[3])
		if len(rows) == 0 {
			return strings.Join(append(lines, "", look.Dim.Render("  No applications yet: an application (accounts, billing…) has environments and requests")), "\n")
		}
	case s.pane == paneEnvs:
		cols = widths(w, 14, 22, 12, 8)
		cols[0], cols[1] = cols[1], cols[0] // the name first, the URL takes the room
		head = cell("environment", cols[0]) + cell("base URL", cols[1]) + cell("auth", cols[2]) + cell("key", cols[3]) + cell("", cols[4])
		if len(rows) == 0 {
			return strings.Join(append(lines, "", look.Dim.Render("  No environments yet: dev, staging, prod, each with its base URL and key")), "\n")
		}
	default:
		if tags := s.tags(s.app); len(tags) > 0 {
			line, x := " ", 1
			for i, t := range tags {
				c := look.Dim.Render(" #" + t + " ")
				if s.tag[s.app] == t {
					c = look.FocusBar.Foreground(lipgloss.Color("15")).Render(" #" + t + " ")
				}
				s.chipAt = append(s.chipAt, [3]int{x, x + ansi.StringWidth(c), i})
				line += c + " "
				x += ansi.StringWidth(c) + 1
			}
			lines = append(lines, "", line+look.Dim.Render(" t filters by tag"))
		}
		cols = widths(w, 7, 16, 18, 9)
		head = cell("request", cols[0]) + cell("method", cols[1]) + cell("tags", cols[2]) + cell("last", cols[3]) + cell("sent", cols[4])
		if len(rows) == 0 {
			return strings.Join(append(lines, "", look.Dim.Render("  No requests yet: make one, or a folder to group them in")), "\n")
		}
	}
	lines = append(lines, "", look.Dim.Render("  "+head))
	cur := min(max(s.cursor[s.pageKey()], 0), len(rows)-1)
	room := max(h-len(lines)-2, 3)
	from := max(min(cur-room/2, len(rows)-room), 0)
	to := min(from+room, len(rows))
	for i := from; i < to; i++ {
		body := s.rowLine(rows[i], cols)
		line := "  " + body
		switch {
		case i == cur && focused:
			line = look.FocusBar.Width(w).Render("▶ " + ansi.Strip(body))
		case i == cur:
			line = look.Sel.Render("▶ ") + body
		}
		s.rowAt[len(lines)] = i
		lines = append(lines, line)
	}
	if len(rows) > room {
		lines = append(lines, "", look.Dim.Render(fmt.Sprintf("  %d–%d of %d", from+1, to, len(rows))))
	}
	return strings.Join(lines, "\n")
}

// rowLine draws a row's cells.
func (s *Screen) rowLine(r row, cols []int) string {
	switch {
	case s.app == "":
		var envs []string
		for _, e := range s.envs[r.app] {
			if e.Name == s.cur[r.app] {
				envs = append(envs, look.Sel.Render("●"+e.Name))
			} else {
				envs = append(envs, look.Dim.Render(e.Name))
			}
		}
		if len(envs) == 0 {
			envs = []string{look.Warn.Render("none yet")}
		}
		return cell(look.Bold.Render(r.app), cols[0]) + cell(fmt.Sprint(len(s.appReqs(r.app))), cols[1]) + cell(strings.Join(envs, " "), cols[2]) + cell(s.keyCell(r.app, s.cur[r.app]), cols[3])
	case r.env != "":
		var e Env
		for _, x := range s.envs[r.app] {
			if x.Name == r.env {
				e = x
			}
		}
		use := ""
		if e.Name == s.cur[r.app] {
			use = look.Sel.Render("● in use")
		}
		return cell(look.Bold.Render(e.Name), cols[0]) + cell(e.BaseURL(), cols[1]) + cell(e.AuthLabel(), cols[2]) + cell(s.keyCell(r.app, e.Name), cols[3]) + cell(use, cols[4])
	case r.req < 0: // a folder
		n := 0
		for _, q := range s.reqs {
			if q.App == r.app && q.Folder == r.folder {
				n++
			}
		}
		arrow := "▾"
		if s.folded[r.app][r.folder] {
			arrow = "▸"
		}
		return look.Bold.Render(arrow+" "+folderLabel(r.folder)) + look.Dim.Render(fmt.Sprintf("  %d request%s", n, look.Plural(n)))
	}
	q := s.reqs[r.req]
	last, when := look.Dim.Render("–"), look.Dim.Render("–")
	if rn := s.runs[q.ID()]; rn != nil {
		when = look.Dim.Render(look.Ago(rn.at))
		switch {
		case rn.running:
			last = look.Sel.Render("sending…")
		case rn.err != "":
			last = look.Warn.Render("✗ " + firstLine(rn.err))
		default:
			last = statusStyle(rn.res.Code).Render(rn.res.Status) + look.Dim.Render(fmt.Sprintf(" %dms", rn.res.Took.Milliseconds()))
		}
	}
	return cell("    "+q.Name+look.Dim.Render("  "+q.Path()), cols[0]) + cell(methodStyle(q.Method).Render(q.Method), cols[1]) +
		cell(look.Dim.Render(strings.Join(q.Tags, " ")), cols[2]) + cell(last, cols[3]) + cell(when, cols[4])
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func (s *Screen) keyCell(app, env string) string {
	set, has := s.keySet[app+"/"+env]
	switch {
	case env == "":
		return look.Dim.Render("–")
	case !has:
		return look.Dim.Render("no auth")
	case set:
		return look.Ready.Render("✓ set")
	}
	return look.Warn.Render("✗ not set")
}

func methodStyle(method string) lipgloss.Style {
	c := map[string]string{"GET": "10", "POST": "12", "PUT": "11", "PATCH": "11", "DELETE": "9"}[method]
	if c == "" {
		c = "245"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true)
}

func statusStyle(code int) lipgloss.Style {
	switch {
	case code >= 500:
		return look.Warn
	case code >= 400:
		return look.Orange
	}
	return look.Ready
}

// ---- keys and clicks ----

// Key handles a key on the page; handled is false for keys crew should handle (ctrl+…, ⇧+arrows).
func (s *Screen) Key(msg tea.KeyMsg) (cmd tea.Cmd, ev Event, handled bool) {
	k := msg.String()
	r, ok := s.selected()
	handled = true
	switch k {
	case "up", "k":
		s.moveCursor(-1)
	case "down", "j":
		s.moveCursor(1)
	case "pgup":
		s.moveCursor(-10)
	case "pgdown":
		s.moveCursor(10)
	case "home":
		s.cursor[s.pageKey()] = 0
	case "end":
		s.moveCursor(1 << 20)
	case "tab", "shift+tab":
		if s.app != "" {
			s.pane = 1 - s.pane
		}
	case "esc":
		ev.Back = true
	case "c":
		ev.Chat = true
		if ok && r.req >= 0 {
			ev.About = s.reqs[r.req].ID()
		}
	case "E":
		app := s.app
		if app == "" && ok {
			app = r.app
		}
		if app != "" {
			s.nextEnv(app)
		}
	case "n":
		switch {
		case s.app == "":
			s.popup = s.appForm()
		case s.pane == paneEnvs:
			s.popup = s.envForm(s.app, "")
		default:
			folder := ""
			if ok {
				folder = r.folder
			}
			s.popup = s.reqPopup(nil, s.app, folder)
		}
	case "f":
		if s.app != "" && s.pane == paneRequests {
			s.popup = s.folderForm(s.app)
		}
	case "t":
		if s.app != "" && s.pane == paneRequests {
			tags := append([]string{""}, s.tags(s.app)...)
			s.tag[s.app] = tags[(slices.Index(tags, s.tag[s.app])+1)%len(tags)]
			s.cursor[s.pageKey()] = 0
		}
	case "x":
		if ok {
			s.delete(r)
		}
	default:
		if !ok {
			return nil, ev, false
		}
		return s.rowKey(k, r)
	}
	return nil, ev, handled
}

// rowKey handles the keys that act on the selected row.
func (s *Screen) rowKey(k string, r row) (tea.Cmd, Event, bool) {
	var ev Event
	switch {
	case s.app == "" && k == "enter":
		ev.Open = r.app
	case r.env != "" && k == "enter":
		s.popup = s.envForm(r.app, r.env)
	case r.env != "" && k == " ":
		s.setEnv(r.app, r.env)
	case r.req < 0: // a folder
		switch k {
		case "enter", " ":
			s.toggleFold(r.app, r.folder, !s.folded[r.app][r.folder])
		case "left":
			s.toggleFold(r.app, r.folder, true)
		case "right":
			s.toggleFold(r.app, r.folder, false)
		default:
			return nil, ev, false
		}
	case r.req >= 0:
		q := s.reqs[r.req]
		switch k {
		case "enter":
			s.popup = s.reqPopup(&q, q.App, q.Folder)
		case "s":
			s.popup = s.reqPopup(&q, q.App, q.Folder)
			return s.popup.buttons[0].run(s.popup), ev, true // Send
		case "m":
			s.popup = s.moveForm(q)
		case "left": // to its folder
			rows := s.rows()
			for i := s.cursor[s.pageKey()]; i >= 0; i-- {
				if rows[i].req < 0 {
					s.cursor[s.pageKey()] = i
					break
				}
			}
		default:
			return nil, ev, false
		}
	default:
		return nil, ev, false
	}
	return nil, ev, true
}

// delete deletes the selected application, folder, request or environment, on a second x.
func (s *Screen) delete(r row) {
	var what, id string
	switch {
	case s.app == "":
		what, id = fmt.Sprintf("%s: its %d requests and %d environments (and their keys)", r.app, len(s.appReqs(r.app)), len(s.envs[r.app])), "a:"+r.app
	case r.env != "":
		what, id = r.env+": its key", "e:"+r.app+"/"+r.env
	case r.req >= 0:
		what, id = s.reqs[r.req].Name, "q:"+s.reqs[r.req].ID()
	case r.folder != "":
		n := 0
		for _, q := range s.reqs {
			if q.App == r.app && q.Folder == r.folder {
				n++
			}
		}
		what, id = fmt.Sprintf("the folder %s: its %d request%s", r.folder, n, look.Plural(n)), "f:"+r.app+"/"+r.folder
	default:
		s.Notice = "The default folder goes when its requests do"
		return
	}
	name, detail, _ := strings.Cut(what, ":")
	f := newForm("Delete "+name+"?", nil,
		button{"Delete", "", func(f *Form) tea.Cmd { s.deleteNow(r, id, name); f.closed = true; return nil }},
		button{"Cancel", "esc", leave})
	f.preview = func(*Form) string {
		if detail == "" {
			return look.Warn.Render("This can't be undone.")
		}
		return look.Warn.Render("With it go" + detail + ". This can't be undone.")
	}
	s.popup = f.start()
	s.popup.at = 1 // on Cancel, so ⏎ is safe
}

// deleteNow deletes what delete asked about.
func (s *Screen) deleteNow(r row, id, name string) {
	var err error
	switch id[0] {
	case 'a':
		for _, e := range s.envs[r.app] {
			DeleteEnv(s.dir, e)
		}
		err = os.RemoveAll(filepath.Join(s.dir, r.app))
	case 'e':
		for _, e := range s.envs[r.app] {
			if e.Name == r.env {
				err = DeleteEnv(s.dir, e)
			}
		}
	case 'q':
		err = Delete(s.dir, s.reqs[r.req])
	case 'f':
		err = DeleteFolder(s.dir, r.app, r.folder)
	}
	if err != nil {
		s.Notice = err.Error()
	} else {
		s.Notice = "Deleted " + name
	}
	s.Load()
	s.moveCursor(0)
}

// Click handles a click at (x, y) in the body (as Body drew it last).
func (s *Screen) Click(x, y int) (tea.Cmd, Event) {
	if y == 0 {
		for _, a := range s.actAt {
			if x >= a[0] && x < a[1] {
				cmd, ev, _ := s.Key(keyMsg(s.acts[a[2]].key))
				return cmd, ev
			}
		}
		return nil, Event{}
	}
	if y == 2 {
		for _, c := range s.chipAt {
			if x >= c[0] && x < c[1] {
				t := s.tags(s.app)[c[2]]
				if s.tag[s.app] == t {
					t = ""
				}
				s.tag[s.app], s.cursor[s.pageKey()] = t, 0
				return nil, Event{}
			}
		}
	}
	i, ok := s.rowAt[y]
	if !ok {
		return nil, Event{}
	}
	s.cursor[s.pageKey()] = i
	r, _ := s.selected()
	if r.env != "" {
		return nil, Event{} // a click picks it; ⏎ edits
	}
	cmd, ev, _ := s.rowKey("enter", r)
	return cmd, ev
}

// action is a button above a page's list: what it says and the key it presses.
type action struct{ label, key string }

// actions are the buttons for the open page and what's selected on it.
func (s *Screen) actions() []action {
	r, ok := s.selected()
	switch {
	case s.app == "":
		out := []action{{"+ Application", "n"}}
		if ok {
			out = append(out, action{"Open", "enter"}, action{"Next environment", "E"}, action{"Delete", "x"})
		}
		return append(out, action{"Ask Claude", "c"})
	case s.pane == paneEnvs:
		out := []action{{"+ Environment", "n"}}
		if ok {
			out = append(out, action{"Edit", "enter"}, action{"Use it", "space"}, action{"Delete", "x"})
		}
		return out
	}
	out := []action{{"+ Request", "n"}, {"+ Folder", "f"}}
	switch {
	case ok && r.req >= 0:
		out = append(out, action{"Open", "enter"}, action{"Send", "s"}, action{"Move", "m"}, action{"Delete", "x"})
	case ok && r.folder != "":
		fold := "Fold"
		if s.folded[r.app][r.folder] {
			fold = "Unfold"
		}
		out = append(out, action{fold, "enter"}, action{"Delete folder", "x"})
	}
	return append(out, action{"Next environment", "E"}, action{"Ask Claude", "c"})
}

// actionBar draws the page's buttons and remembers where they are.
func (s *Screen) actionBar(w int) string {
	s.acts, s.actAt = s.actions(), nil
	line, x := " ", 1
	for i, a := range s.acts {
		b := look.Button(a.label, KeyName(a.key))
		bw := ansi.StringWidth(b)
		if x+bw > w {
			break
		}
		s.actAt = append(s.actAt, [3]int{x, x + bw, i})
		line += b + " "
		x += bw + 1
	}
	return line
}

// keyMsg is the key a name stands for: "enter", "space", or a letter.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// Pane is the open application's tab and how many it has (0 on the list of applications).
func (s *Screen) Pane() (cur, n int) {
	if s.app == "" {
		return 0, 0
	}
	return s.pane, 2
}

// SetPane opens one of the application's tabs.
func (s *Screen) SetPane(i int) { s.pane = i }

// ClickTabs handles a click at column x of the tab bar.
func (s *Screen) ClickTabs(x int) {
	for _, t := range s.tabAt {
		if x >= t[0] && x < t[1] {
			s.pane = t[2]
		}
	}
}

// Wheel moves the cursor.
func (s *Screen) Wheel(down bool) {
	if down {
		s.moveCursor(3)
	} else {
		s.moveCursor(-3)
	}
}
