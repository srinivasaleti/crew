package jira

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/browser"
	"crew/internal/section"
	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// The JIRA section, as crew's plugin: its row in the sidebar, reading the list on start and on its
// own clock (CREW_JIRA_EVERY) while acli is installed, installing and signing in through Setup's
// shell, and an issue opened in the main pane like a listed PR: its row under JIRA ("j:<key>"), and
// three tabs: Issue and Comments (the card) and Chat, a Claude chat about the issue. The chat is a
// task underneath, like a PR's (kind: jira, issue: <key>), so it has a task chat's /compact, /new,
// /model, usage and stopping; it runs in <dir>/<id>, where Claude reads the issue with acli.
// Working on an issue is a normal crew task made from it (issue: in the task file).

// chatKind marks an issue's chat in its task file.
const chatKind = "jira"

// Section is JIRA.
type Section struct {
	section.Base
	screen  *Screen
	cards   map[string]*Card // the issues opened in the main pane, by key
	pane    int              // the open tab of the issue on screen (PaneIssue…)
	chatDir string           // where an issue's chat runs, <task id>/
}

// NewSection makes the JIRA section; chats about issues run in chatDir.
func NewSection(ctx context.Context, chatDir string) *Section {
	return &Section{screen: New(ctx), cards: map[string]*Card{}, chatDir: chatDir}
}

// Def is JIRA on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "jira", Label: "JIRA", Letter: "j", About: "your open Jira issues; ⏎ works on one in a task, with Claude"}
}

// pollMsg is JIRA's clock: read the list now, and again after every.
type pollMsg struct{}

// every is how often JIRA reads your issues: CREW_JIRA_EVERY ("2m", "90s"), else 3 minutes, and
// never more often than every 30 seconds.
func every() time.Duration {
	d, err := time.ParseDuration(os.Getenv("CREW_JIRA_EVERY"))
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return max(d, 30*time.Second)
}

// pollTick is the next read of JIRA's clock.
func pollTick() tea.Cmd {
	return tea.Tick(every(), func(time.Time) tea.Msg { return pollMsg{} })
}

// Init starts JIRA's clock, with a read now.
func (s *Section) Init(section.Host) tea.Cmd { return func() tea.Msg { return pollMsg{} } }

// On reads the list when JIRA is turned on.
func (s *Section) On(h section.Host) tea.Cmd { return s.load(h) }

// ready reports whether Atlassian's CLI is installed and signed in: Setup says so, or the last
// read of the list worked (Setup's check can time out while crew starts).
func (s *Section) ready(h section.Host) bool { return h.Setup().Jira.OK() || s.screen.OK() }

// load reads your issues in the background while the section is on and acli is installed (the
// read tells whether it's signed in), unless a read is already running; the JIRA row spins until
// it's back.
func (s *Section) load(h section.Host) tea.Cmd {
	if !h.SectionOn("jira") || s.screen.Loading() {
		return nil
	}
	if _, err := exec.LookPath("acli"); err != nil {
		return nil
	}
	return tea.Batch(s.screen.Load(), h.Spin())
}

// SetupChanged reads the list as soon as acli is signed in, not at the next poll.
func (s *Section) SetupChanged(h section.Host, was setup.Status) tea.Cmd {
	if !(was.Jira.OK() || s.screen.OK()) && s.ready(h) {
		return s.load(h)
	}
	return nil
}

// Update takes the list, an issue's card and comments, a comment posted, an edit saved and
// Claude's drafts coming back, and the clock.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pollMsg:
		return tea.Batch(s.load(h), pollTick()), true
	case ListMsg:
		s.screen.Done(msg)
		if h.SelectedRow().Kind != "task" {
			h.Refresh()
		}
		return nil, true
	case CardMsg:
		if c := s.cards[msg.Key()]; c != nil {
			c.Done(msg)
			s.cardChanged(h, c)
		}
		return nil, true
	case PostedMsg:
		var cmd tea.Cmd
		if c := s.cards[msg.Key()]; c != nil {
			cmd = c.Posted(h.Ctx(), msg)
			s.cardChanged(h, c)
		}
		return cmd, true
	case SavedMsg: // an edit or a new status: read the issue and the list again
		var cmd tea.Cmd
		if c := s.cards[msg.Key()]; c != nil {
			cmd = c.Saved(h.Ctx(), msg)
			s.cardChanged(h, c)
		}
		return tea.Batch(cmd, s.load(h)), true
	case draftMsg:
		if c := s.cards[msg.key]; c != nil {
			c.SetDraft(msg.text, msg.err)
			s.cardChanged(h, c)
		}
		return nil, true
	}
	return nil, false
}

// Spin keeps the spinner going while the list is being read.
func (s *Section) Spin(section.Host) bool { return s.screen.Loading() }

// Rows are JIRA's row, then the issue you opened.
func (s *Section) Rows(h section.Host) []section.Row {
	rows := []section.Row{{Kind: section.Group, Section: "jira", Label: "JIRA", Count: s.screen.Count()}}
	if key, ok := strings.CutPrefix(h.SelKey(), "j:"); ok && s.cards[key] != nil {
		rows = append(rows, section.Row{Kind: "issue", Section: "jira", Key: "j:" + key, Depth: 1, Label: key})
	}
	return rows
}

// Title is JIRA's row: a spinner while the list is read, and no count before the first one.
func (s *Section) Title(h section.Host, r section.Row) (string, string) {
	title, badge := section.CountTitle(r), ""
	if s.screen.Loading() {
		badge = look.Sel.Render(look.SpinFrames[h.SpinFrame()%len(look.SpinFrames)])
		if s.screen.Count() == 0 {
			title = look.Bold.Render(r.Label)
		}
	}
	return title, badge
}

// Lines draws an issue's two sidebar lines: its key and summary, then its status and type.
func (s *Section) Lines(h section.Host, r section.Row, selected bool) string {
	c := s.cards[r.Label]
	return h.ItemLines(r, look.Sel.Render("◆"), c.Issue.Key+" "+c.Issue.Summary, c.Issue.Status+" · "+c.Issue.Type, selected)
}

// Selected opens another issue on its Issue tab.
func (s *Section) Selected(_ section.Host, r section.Row, from string) {
	if r.Kind == "issue" && r.Key != from {
		s.pane = PaneIssue
	}
}

// ListKey is ⏎ on JIRA's row: it installs acli or signs in the first time you go in.
func (s *Section) ListKey(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "enter" && r.Kind == section.Group && h.Setup().Checked && !s.ready(h) {
		return s.signIn(h), true
	}
	return nil, false
}

// ShowsTable reports whether a row is JIRA's own, which shows the list.
func (s *Section) ShowsTable(_ section.Host, r section.Row) bool { return r.Kind == section.Group }

// Table is the list, with each issue's crew task and how it's doing.
func (s *Section) Table(h section.Host, r section.Row) table.Table {
	tasksFor := map[string]string{}
	for _, t := range h.Tasks() {
		switch {
		case t.Issue == "":
		case h.Chats().Conv(t.ID).Busy:
			tasksFor[t.Issue] = look.Running.Render("● working")
		default:
			tasksFor[t.Issue] = tasks.Tint[t.Status].Render(tasks.Icons[t.Status] + " " + tasks.Labels[t.Status])
		}
	}
	return s.screen.Table(s.ready(h), tasksFor)
}

// Open does what the table asks on an issue.
func (s *Section) Open(h section.Host, _ section.Row, key, action string) tea.Cmd {
	ev := s.screen.Act(s.ready(h), key, action)
	switch {
	case ev.SignIn:
		return s.signIn(h)
	case ev.Card != nil:
		return s.openIssue(h, *ev.Card, ev.Pane)
	case ev.URL != "":
		browser.Open(ev.URL)
	case ev.Work != nil:
		return s.workOn(h, *ev.Work)
	}
	return nil
}

// signIn installs Atlassian's CLI if it isn't, else signs it in (the browser opens), in a shell
// panel; Setup checks again when that shell exits, and the list loads once it's signed in.
func (s *Section) signIn(h section.Host) tea.Cmd {
	t := h.Setup().Jira
	if !t.Installed {
		return h.RunFix(t.Install, "then S signs in to Jira")
	}
	return h.SignIn(t)
}

// workOn opens the issue's crew task, or the new-task form filled in from it the first time.
func (s *Section) workOn(h section.Host, i Issue) tea.Cmd {
	return h.WorkOn(i.Key+" "+i.Summary, i.Key, i.Brief())
}

// card is the card of the issue on screen, if an issue's row is selected.
func (s *Section) card(r section.Row) *Card {
	if r.Kind == "issue" {
		return s.cards[r.Label]
	}
	return nil
}

// openIssue opens an issue in the main pane on a tab, reading it from Jira the first time.
func (s *Section) openIssue(h section.Host, i Issue, pane int) tea.Cmd {
	var cmd tea.Cmd
	if s.cards[i.Key] == nil {
		s.cards[i.Key], cmd = OpenCard(h.Ctx(), i, s.screen.Statuses(i.Project))
	}
	s.pane = PaneIssue
	h.Select("j:" + i.Key)
	h.FocusMain()
	s.setPane(h, pane)
	return cmd
}

// openLinked opens a linked issue or subtask in crew: from your list when it's there, else read
// from Jira by its key.
func (s *Section) openLinked(h section.Host, from *Card, key string) tea.Cmd {
	if i, ok := s.screen.Find(key); ok {
		return s.openIssue(h, i, PaneIssue)
	}
	project, _, _ := strings.Cut(key, "-")
	site := strings.TrimSuffix(from.Issue.URL, from.Issue.Key)
	return s.openIssue(h, Issue{Key: key, Project: project, Summary: key, URL: site + key}, PaneIssue)
}

// setPane opens one of an issue's tabs; Chat gets the keys for its message box.
func (s *Section) setPane(h section.Host, p int) {
	s.pane = p
	in := h.Input()
	if c := s.card(h.SelectedRow()); c != nil && p == PaneChat {
		t := s.ensureChat(h, c)
		h.MarkRead(t.ID)
		in.Placeholder = "Message " + chat.AgentName(*t) + " about " + c.Issue.Key + "…"
		in.Focus()
	} else {
		in.Blur()
	}
	h.ClearSuggestions()
	h.Layout()
	h.Viewport().GotoTop()
	s.render(h)
	if p == PaneChat {
		h.Viewport().GotoBottom()
	}
}

// Frame is an issue's frame title.
func (s *Section) Frame(_ section.Host, r section.Row) string {
	if r.Kind == "issue" {
		return "jira issue"
	}
	return ""
}

// Header is the main pane's first line for an issue.
func (s *Section) Header(h section.Host, r section.Row, w int) string {
	c := s.card(r)
	if c == nil {
		return ""
	}
	meta := ""
	if t := taskFor(h, c.Issue.Key); t != nil {
		meta = look.Dim.Render(" · ") + tasks.Tint[t.Status].Render(tasks.Icons[t.Status]+" task")
	}
	if c.Loading() {
		meta += look.Dim.Render(" · reading Jira…")
	}
	return ansi.Truncate(look.Sel.Render(c.Issue.Key)+" "+look.Bold.Render(c.Issue.Summary)+meta+h.ChatMeta(s.chat(h, c.Issue.Key)), w, "…")
}

// taskFor is the crew task working on an issue, if there is one.
func taskFor(h section.Host, key string) *tasks.Task {
	ts := h.Tasks()
	for i := range ts {
		if ts[i].Issue == key {
			return &ts[i]
		}
	}
	return nil
}

// Tabs is the tab bar for an issue: Issue, Comments, Chat.
func (s *Section) Tabs(_ section.Host, r section.Row, w int) string {
	c := s.card(r)
	if c == nil {
		return ""
	}
	active := lipgloss.NewStyle().Background(look.Accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range PaneNames {
		if i == s.pane {
			bar = append(bar, active.Render(section.TabLabel(n, i)))
		} else {
			bar = append(bar, look.Dim.Padding(0, 1).Render(section.TabLabel(n, i)))
		}
	}
	about := fmt.Sprintf("   %s · %s · %s", c.Issue.Type, c.Issue.Status, c.Issue.Priority)
	return ansi.Truncate(strings.Join(bar, " ")+look.Dim.Render(about), w, "…")
}

// TabsHere is an issue's tabs.
func (s *Section) TabsHere(_ section.Host, r section.Row) (int, int) {
	if s.card(r) == nil {
		return 0, 0
	}
	return s.pane, len(PaneNames)
}

// GotoTab opens one of an issue's tabs.
func (s *Section) GotoTab(h section.Host, r section.Row, i int) tea.Cmd {
	if s.card(r) != nil {
		s.setPane(h, i)
		h.Refresh()
	}
	return nil
}

// Render fills the main pane with the issue's open tab.
func (s *Section) Render(h section.Host, r section.Row) {
	if s.card(r) != nil {
		s.render(h)
	}
}

// render draws the issue on screen.
func (s *Section) render(h section.Host) {
	vp := h.Viewport()
	c, w := s.card(h.SelectedRow()), max(vp.Width, 20)
	if c == nil {
		return
	}
	if s.pane != PaneChat {
		vp.SetContent(c.View(s.pane, w, vp.Height))
		if s.pane == PaneComments && c.Typing() {
			vp.GotoBottom() // the comment box is at the bottom
		}
		return
	}
	follow := vp.AtBottom() || vp.TotalLineCount() <= vp.Height
	if t := s.chat(h, c.Issue.Key); t != nil && len(h.Chats().Conv(t.ID).Msgs) > 0 {
		vp.SetContent(h.Chats().Render(t.ID, w, h.SpinFrame()))
	} else {
		vp.SetContent(look.Dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude anything about " + c.Issue.Key +
			": how to approach it, what to ask the reporter, a plan, a better description. Claude reads the issue, its comments " +
			"and linked issues with acli, and changes nothing in Jira unless you ask it to.\n\n/compact, /new and /model work as in a task.")))
	}
	if follow {
		vp.GotoBottom()
	}
}

// Hint is the line under an issue's tab, or the message box on Chat.
func (s *Section) Hint(h section.Host, r section.Row) string {
	c := s.card(r)
	if c == nil {
		return ""
	}
	if s.pane == PaneChat {
		return h.ChatBox()
	}
	return look.KeyHints(keys.Names("  " + c.Hint(s.pane)))
}

// Key handles keys on an issue: the card's keys on Issue and Comments, the message box on Chat,
// tab/shift+tab to switch tabs, esc back to the list, and ↑↓ to scroll.
func (s *Section) Key(h section.Host, r section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	c := s.card(r)
	if c == nil {
		return nil, false
	}
	k := msg.String()
	switching := k == "tab" || k == "shift+tab"
	if s.pane == PaneChat && (!switching || h.ChatTyping()) {
		return h.ChatKey(msg), true // the message box, as in a task's chat
	}
	if s.pane != PaneChat {
		ev, cmd, ok := c.KeyMsg(h.Ctx(), s.pane, msg)
		if ok {
			return tea.Batch(cmd, s.onCardEvent(h, c, ev)), true
		}
	}
	if switching {
		d := map[string]int{"shift+tab": -1, "tab": 1}[k]
		s.setPane(h, (s.pane+d+len(PaneNames))%len(PaneNames))
		return nil, true
	}
	if k == "esc" {
		h.GoUp()
		return nil, true
	}
	var cmd tea.Cmd
	vp := h.Viewport()
	*vp, cmd = vp.Update(msg) // ↑↓ pgup pgdn scroll
	return cmd, true
}

// Click on an issue's tab bar opens that tab.
func (s *Section) Click(h section.Host, r section.Row, x, y int) (tea.Cmd, bool) {
	if s.card(r) == nil {
		return nil, false
	}
	if y == 2 {
		if i := section.TabAt(PaneNames, x); i >= 0 {
			s.setPane(h, i)
		}
	}
	return nil, true
}

// Focus is the chat on the Chat tab, the issue's own keys on the others.
func (s *Section) Focus(_ section.Host, r section.Row) string {
	switch {
	case s.card(r) == nil:
		return "overview"
	case s.pane == PaneChat:
		return "chat"
	}
	return "pr"
}

// ChatOpen reports whether an issue's Chat tab is open.
func (s *Section) ChatOpen(_ section.Host, r section.Row) bool {
	return s.card(r) != nil && s.pane == PaneChat
}

// Chat is the issue's chat on its Chat tab.
func (s *Section) Chat(h section.Host, r section.Row) *tasks.Task {
	if c := s.card(r); c != nil && s.pane == PaneChat {
		return s.chat(h, c.Issue.Key)
	}
	return nil
}

// EnsureChat makes the issue's chat when it isn't there.
func (s *Section) EnsureChat(h section.Host, r section.Row) *tasks.Task {
	if c := s.card(r); c != nil {
		return s.ensureChat(h, c)
	}
	return nil
}

// onCardEvent does what a key on the card asks of crew.
func (s *Section) onCardEvent(h section.Host, c *Card, ev CardEvent) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case ev.URL != "":
		browser.Open(ev.URL)
	case ev.Work:
		return s.workOn(h, c.Issue)
	case ev.Open != "":
		return s.openLinked(h, c, ev.Open)
	case ev.Draft:
		cmd = draftComment(h.Ctx(), c.Issue.Key, c.Text())
	}
	if ev.Comments && s.pane != PaneComments {
		s.setPane(h, PaneComments)
	}
	s.render(h)
	return cmd
}

// cardChanged redraws the issue on screen when its card took a message.
func (s *Section) cardChanged(h section.Host, c *Card) {
	if c != nil && s.card(h.SelectedRow()) == c {
		s.render(h)
	}
}

// chat is the chat about an issue, or nil before you open its Chat tab.
func (s *Section) chat(h section.Host, key string) *tasks.Task {
	return h.FindChat(func(t tasks.Task) bool { return t.Kind == chatKind && t.Issue == key })
}

// ensureChat makes the chat about an issue if there isn't one yet (in memory; the first message
// saves it).
func (s *Section) ensureChat(h section.Host, c *Card) *tasks.Task {
	if t := s.chat(h, c.Issue.Key); t != nil {
		return t
	}
	i := c.Issue
	return h.AddChat(tasks.Task{
		ID:    tasks.Slugify("jira " + i.Key),
		Title: i.Key + " " + i.Summary,
		Kind:  chatKind, Issue: i.Key, Status: tasks.InProgress, Created: time.Now(),
		Body: fmt.Sprintf("You're helping me with Jira issue %s. Atlassian's CLI (acli) is signed in: read the issue with "+
			"`acli jira workitem view %[1]s --fields '*all' --json` and its comments with `acli jira workitem comment list --key %[1]s`. "+
			"Don't edit, comment on or move it in Jira unless I ask; when I do, use acli and say what you changed.\n\n%s",
			i.Key, i.Brief()),
	})
}

// Where runs an issue chat's first turn in its own folder (stable, so Claude can resume the
// session there).
func (s *Section) Where(_ section.Host, t tasks.Task, prompt string) (tea.Cmd, bool) {
	if t.Kind != chatKind {
		return nil, false
	}
	dir := filepath.Join(s.chatDir, t.ID)
	return func() tea.Msg {
		return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
	}, true
}

// draftMsg is Claude's draft of a comment coming back.
type draftMsg struct {
	key, text string
	err       error
}

// draftComment has Claude write a comment for an issue from its card, in one headless run with no
// tools; the draft goes in the card's comment box, and only your ⏎ posts it.
func draftComment(ctx context.Context, key, card string) tea.Cmd {
	prompt := "Draft one comment for this Jira issue, as the person it's assigned to: short, specific and " +
		"useful (a status update, a question or the next step), in plain text. Reply with the comment only.\n\n" + card
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "text", "--tools", "")
		cmd.Dir = os.TempDir() // no repo: the card is all it needs
		out, err := cmd.Output()
		if err != nil && strings.TrimSpace(string(out)) == "" {
			return draftMsg{key: key, err: err}
		}
		return draftMsg{key: key, text: string(out)}
	}
}
