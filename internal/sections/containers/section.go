package containers

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/section"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// DOCKER and KUBERNETES, as two of crew's sections from one package: each a row in the sidebar
// with a table per tab (⌘1…), read with its CLI on its own clock, so it costs no tokens. ⏎ opens a
// container or an object on its own page (Logs, Inspect or Describe, Chat). Changes (restart,
// remove, delete, cleanup) only run on your key, and ask first when they remove something or the
// Kubernetes context is production. Each thing's chat is Claude locked down to `crew ctr`, which
// reads and never changes anything.

// chatKind marks a chat about a container or Kubernetes object in its task file.
const chatKind = "containers"

// every is how often the section reads again while it's on screen; elsewhere it's slower.
const every = 5 * time.Second

// Section is DOCKER or KUBERNETES.
type Section struct {
	section.Base
	kube    bool   // KUBERNETES, else DOCKER
	dir     string // where its chats run, one folder each
	tab     int
	docker  dockerData
	kd      kubeData
	read    bool   // read at least once
	err     string // why the last read failed
	kctx    string // the context crew uses ("" until the first read picks kubectl's current one)
	loading bool
	missing bool // its CLI isn't on the PATH (looked up on each read)
	gen     int  // the clock's generation; an older clock's ticks are dropped
	ticks   int
	things  map[string]*thing
	pop     popup
}

// NewDocker makes DOCKER; chats about containers run in dir.
func NewDocker(dir string) *Section { return &Section{dir: dir, things: map[string]*thing{}} }

// NewKube makes KUBERNETES; chats about its objects run in dir.
func NewKube(dir string) *Section { return &Section{kube: true, dir: dir, things: map[string]*thing{}} }

// key is the section's key on home and in its rows.
func (s *Section) key() string {
	if s.kube {
		return "kube"
	}
	return "docker"
}

// cli is the command the section reads with.
func (s *Section) cli() string {
	if s.kube {
		return "kubectl"
	}
	return "docker"
}

// Def is DOCKER or KUBERNETES on home.
func (s *Section) Def() section.Def {
	if s.kube {
		return section.Def{Key: "kube", Label: "KUBERNETES", Letter: "k", About: "pods, deployments, logs and a shell, read with kubectl; cleanup of leftovers"}
	}
	return section.Def{Key: "docker", Label: "DOCKER", Letter: "c", About: "containers, images, volumes and networks, with logs, a shell and cleanup"}
}

// installed reports whether the section's CLI was on the PATH at the last read.
func (s *Section) installed() bool { return !s.missing }

// onScreen reports whether one of the section's rows is in the main pane.
func (s *Section) onScreen(h section.Host) bool { return h.SelectedRow().Section == s.key() }

// tickMsg is the section's clock.
type tickMsg struct {
	sec string
	gen int
}

// loadedMsg is what a read found.
type loadedMsg struct {
	sec    string
	docker dockerData
	kube   kubeData
	err    error
}

// thingMsg is a thing's logs or inspect output, read.
type thingMsg struct {
	key, out string
	logs     bool // out is its logs, else what inspect or describe says
	err      error
}

// leftoversMsg is what KUBERNETES' Cleanup reads, back.
type leftoversMsg struct{ l kubeLeftovers }

// tick is the next tick of the clock.
func (s *Section) tick() tea.Cmd {
	sec, gen := s.key(), s.gen
	return tea.Tick(every, func(time.Time) tea.Msg { return tickMsg{sec, gen} })
}

// Init starts the clock, with a read now.
func (s *Section) Init(h section.Host) tea.Cmd { return tea.Batch(s.load(h), s.tick()) }

// On reads now and starts a new clock when the section is turned on.
func (s *Section) On(h section.Host) tea.Cmd {
	s.gen++
	return tea.Batch(s.load(h), s.tick())
}

// load reads the section's CLI in the background.
func (s *Section) load(h section.Host) tea.Cmd {
	_, err := exec.LookPath(s.cli())
	if s.missing = err != nil; s.loading || !h.SectionOn(s.key()) || s.missing {
		return nil
	}
	s.loading = true
	ctx, sec, kctx, tab := h.Ctx(), s.key(), s.kctx, s.tab
	stats := s.onScreen(h) && s.tab == 0
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if sec == "kube" {
			d, err := loadKube(ctx, kctx, tab)
			return loadedMsg{sec: sec, kube: d, err: err}
		}
		d, err := loadDocker(ctx, stats)
		return loadedMsg{sec: sec, docker: d, err: err}
	}
}

// Update takes the clock, reads, a thing's logs, a change that ran, and Cleanup's read.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tickMsg:
		if msg.sec != s.key() {
			return nil, false
		}
		if msg.gen != s.gen || !h.SectionOn(s.key()) {
			return nil, true
		}
		s.ticks++
		var cmds []tea.Cmd
		if s.onScreen(h) || s.ticks%12 == 0 { // off screen, a read a minute keeps the row's count current
			cmds = append(cmds, s.load(h))
		}
		if t := s.thing(h.SelectedRow()); t != nil && t.follow && t.paneName() == "Logs" {
			cmds = append(cmds, s.readThing(h, t))
		}
		return tea.Batch(append(cmds, s.tick())...), true
	case loadedMsg:
		if msg.sec != s.key() {
			return nil, false
		}
		s.loading, s.read, s.err = false, true, ""
		if msg.err != nil {
			s.err = firstLine(msg.err.Error())
		}
		if s.kube {
			if msg.err == nil || s.kctx == "" {
				s.kd = msg.kube
			}
			if s.kctx == "" {
				s.kctx = msg.kube.kctx
			}
		} else if msg.err == nil {
			s.docker = msg.docker
		}
		if s.onScreen(h) {
			h.Refresh()
		}
		if s.kube && msg.err == nil && (msg.kube.tab != s.tab || msg.kube.kctx != s.kctx) {
			return s.load(h), true // you switched while it read
		}
		return nil, true
	case thingMsg:
		t := s.things[msg.key]
		if t == nil {
			return nil, false
		}
		t.loaded, t.reading = true, false
		errText := ""
		if msg.err != nil {
			errText = msg.err.Error()
		}
		if msg.logs {
			t.logs, t.logErr = msg.out, errText
		} else {
			t.inspect, t.err = msg.out, errText
		}
		if s.thing(h.SelectedRow()) == t {
			s.render(h)
		}
		return nil, true
	case doneMsg:
		if msg.sec != s.key() {
			return nil, false
		}
		if msg.err != nil {
			h.Notice("Didn't work: " + firstLine(msg.err.Error()))
		} else {
			h.Notice(msg.note)
		}
		return s.load(h), true
	case leftoversMsg:
		if !s.kube {
			return nil, false
		}
		s.open(h, kubeCleanup(h.Ctx(), msg.l))
		return nil, true
	}
	return nil, false
}

// firstLine is an error's first line, for the help line.
func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// Spin keeps the spinner going while the section reads.
func (s *Section) Spin(section.Host) bool { return s.loading && !s.read }

// Rows are the section's row, then the thing you opened.
func (s *Section) Rows(h section.Host) []section.Row {
	count := 0
	if s.kube {
		count = len(s.kd.pods)
	} else {
		for _, c := range s.docker.containers {
			if c.State.Running {
				count++
			}
		}
	}
	label := "DOCKER"
	if s.kube {
		label = "KUBERNETES"
	}
	rows := []section.Row{{Kind: section.Group, Section: s.key(), Label: label, Count: count}}
	if t := s.things[h.SelKey()]; t != nil {
		rows = append(rows, section.Row{Kind: "thing", Section: s.key(), Key: t.key, Depth: 1, Label: t.name})
	}
	return rows
}

// Title is the row: its count, a spinner before the first read, and what's wrong (crashed
// containers, unhealthy pods, the CLI missing); KUBERNETES adds its context.
func (s *Section) Title(h section.Host, r section.Row) (string, string) {
	title := section.CountTitle(r)
	switch {
	case !s.installed():
		return look.Bold.Render(r.Label), look.Dim.Render("no " + s.cli())
	case !s.read:
		return look.Bold.Render(r.Label), look.Sel.Render(look.SpinFrames[h.SpinFrame()%len(look.SpinFrames)])
	case s.err != "":
		return title, look.Warn.Render("✗")
	}
	bad := 0
	if s.kube {
		for _, p := range s.kd.pods {
			if !p.healthy() {
				bad++
			}
		}
	} else {
		for _, c := range s.docker.containers {
			if !c.State.Running && c.State.ExitCode != 0 {
				bad++
			}
		}
	}
	badge := ""
	if bad > 0 {
		badge = look.Warn.Render(fmt.Sprintf("✗%d", bad))
	}
	if s.kube {
		ctx := look.Dim.Render(" " + s.kd.kctx)
		if isProd(s.kd.kctx) {
			ctx = look.Warn.Render(" ⚠" + s.kd.kctx)
		}
		badge = ctx + " " + badge
	}
	return title, badge
}

// Lines draws an open thing's two sidebar lines.
func (s *Section) Lines(h section.Host, r section.Row, selected bool) string {
	t := s.things[r.Key]
	if t == nil {
		return ""
	}
	mark, second := look.Sel.Render("◆"), strings.ToLower(t.kind)
	if t.ns != "" {
		second += " · " + t.ns
	}
	return h.ItemLines(r, mark, t.name, second, selected)
}

// ListKey is ⏎ on the row when the CLI isn't installed: it says how to get it.
func (s *Section) ListKey(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "enter" && !s.installed() {
		h.Notice(s.howToGet())
		return nil, true
	}
	return nil, false
}

// howToGet says how to get the section's CLI.
func (s *Section) howToGet() string {
	if s.kube {
		return "kubectl isn't installed · brew install kubectl, then sign in to your cluster as usual"
	}
	return "docker isn't installed · install Docker Desktop (brew install --cask docker), or colima"
}

// ShowsTable is the section's own row.
func (s *Section) ShowsTable(_ section.Host, r section.Row) bool { return r.Kind == section.Group }

// Table is the open tab's table.
func (s *Section) Table(section.Host, section.Row) table.Table {
	err := s.err
	switch {
	case !s.installed():
		err = s.howToGet()
	case !s.read:
		err = "Reading " + s.cli() + "…"
	}
	if s.kube {
		return kubeTable(s.kd, s.tab, err)
	}
	return dockerTable(s.docker, s.tab, err)
}

// tabs are the open row's tab names.
func (s *Section) tabs(r section.Row) []string {
	if t := s.thing(r); t != nil {
		return t.panes()
	}
	if s.kube {
		return kubeTabs
	}
	return dockerTabs
}

// Tabs is the tab bar: the section's kinds, or an open thing's tabs.
func (s *Section) Tabs(_ section.Host, r section.Row, w int) string {
	cur, _ := s.TabsHere(nil, r)
	active := lipgloss.NewStyle().Background(look.Accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range s.tabs(r) {
		if i == cur {
			bar = append(bar, active.Render(section.TabLabel(n, i)))
		} else {
			bar = append(bar, look.Dim.Padding(0, 1).Render(section.TabLabel(n, i)))
		}
	}
	return ansi.Truncate(strings.Join(bar, " "), w, "…")
}

// TabsHere is the open tab and how many there are.
func (s *Section) TabsHere(_ section.Host, r section.Row) (int, int) {
	if t := s.thing(r); t != nil {
		return t.pane, len(t.panes())
	}
	return s.tab, len(s.tabs(r))
}

// GotoTab opens tab i: another kind, which reads it now, or one of a thing's tabs.
func (s *Section) GotoTab(h section.Host, r section.Row, i int) tea.Cmd {
	if t := s.thing(r); t != nil {
		return s.setPane(h, t, i)
	}
	s.tab = i
	h.Refresh()
	return s.load(h)
}

// Click on the tab bar opens that tab; the table takes its own clicks.
func (s *Section) Click(h section.Host, r section.Row, x, y int) (tea.Cmd, bool) {
	if y != 2 {
		return nil, s.thing(r) != nil
	}
	if i := section.TabAt(s.tabs(r), x); i >= 0 {
		return s.GotoTab(h, r, i), true
	}
	return nil, true
}

// thing is the thing on a row, or nil.
func (s *Section) thing(r section.Row) *thing {
	if r.Kind == "thing" {
		return s.things[r.Key]
	}
	return nil
}

// Open does what the table asks on a row.
func (s *Section) Open(h section.Host, _ section.Row, id, action string) tea.Cmd {
	switch action {
	case Cleanup:
		if s.kube {
			ctx, kctx := h.Ctx(), s.kd.kctx
			h.Notice("Reading what's left over in " + kctx + "…")
			return func() tea.Msg { return leftoversMsg{loadLeftovers(ctx, kctx)} }
		}
		s.open(h, dockerCleanup(h.Ctx(), s.docker))
		return nil
	case Context:
		s.open(h, &menu{items: s.kd.contexts, sel: max(slices.Index(s.kd.contexts, s.kd.kctx), 0), pick: func(c string) tea.Cmd {
			s.kctx, s.kd.kctx = c, c
			h.Notice("Context " + c + " · crew's own choice; your kubeconfig doesn't change")
			return s.load(h)
		}})
		return nil
	}
	if s.kube {
		return s.kubeAct(h, id, action)
	}
	return s.dockerAct(h, id, action)
}

// open shows a popup.
func (s *Section) open(h section.Host, p popup) {
	s.pop = p
	h.SectionModal(s)
	h.Input().Blur()
}

// ModalOpen reports whether a popup is open.
func (s *Section) ModalOpen(section.Host) bool { return s.pop != nil }

// ModalView draws it.
func (s *Section) ModalView(section.Host) string { return s.pop.View() }

// ModalKey hands it a key, and closes it when it's done.
func (s *Section) ModalKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	cmd, closed := s.pop.Key(msg)
	if closed {
		s.pop = nil
		h.SectionModal(nil)
		h.Reselect()
	}
	return cmd
}

// ask runs a change after you say yes in a popup.
func (s *Section) ask(h section.Host, text string, run tea.Cmd) tea.Cmd {
	s.open(h, &confirm{text: text, run: run})
	return nil
}
