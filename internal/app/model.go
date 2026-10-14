// Package app is the crew terminal UI: a sidebar (crew home + one chat per task) and a main pane.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/jira"
	"crew/internal/reminders"
	"crew/internal/repo"
	"crew/internal/requests"
	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/usage"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/files"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/markdown"
	"crew/internal/widgets/prview"
	"crew/internal/widgets/table"
	"crew/internal/widgets/term"
)

type Config struct {
	Ctx   context.Context // cancelled on exit; stops running agents
	Tasks tasks.Store
	Chats chat.Log
	Repos repo.Store
	// Plugin is crew's Claude Code plugin (the crew repo's plugin/ folder, set at build): skills
	// crew's agents get, like crew:plan. "" or missing runs without them.
	Plugin   string
	Plans    string // folder of the tasks' plans, <task id>.md
	PRChat   string // where a chat about a PR whose repo isn't on this machine runs, <task id>/
	JiraChat string // where a chat about a Jira issue runs, <task id>/
	Reqs     string // the REQUESTS section's folder: .http files, env/, and where its chat runs
	Keys     string // the ⌘ shortcuts file (/keys)
	// Sections is the file of sections turned off on home (sections.go).
	Sections  string
	Focus     string // FOCUS's folder: the timer and days (focus_section.go)
	Reminders string // REMINDERS' folder: reminders.json, and where its chat runs (reminders.go)
	Notify    string // where crew hands notifications to its app, and reads if they're allowed (notify.go)
}

const homeID = chat.Home // id of the home screen; task ids never contain "_"

type Model struct {
	jirav        *jira.Screen          // the JIRA section (its own package; jira.go is the glue)
	cards        map[string]*jira.Card // the Jira issues opened in the main pane, by key (jira_issue.go)
	sectionsOff  map[string]bool       // sections turned off on home (sections.go)
	fc           *focusState           // FOCUS: the timer, screen time, breaks (focus_section.go)
	rems         []reminders.Reminder  // REMINDERS, as last read (reminders.go)
	remPane      int                   // REMINDERS' tab: 0 List, 1 Chat
	remForm      *reminders.Form       // the new or edit popup, while it's open
	remGen       int                   // the reminders ticker's generation
	remAlert     string                // the reminder that fired last, on the help line for a while
	remAlertAt   time.Time
	cfg          Config
	tasks        []tasks.Task // all tasks, archived included; the sidebar groups and filters them
	loadErr      string
	selKey       string         // selected sidebar row: "home", "g:<group>", "t:<task id>" or "p:<PR url>"
	chat         *chat.Chats    // every chat's messages and turns (the chat package; chat.go is the glue)
	input        textarea.Model // message box; grows up to maxInputLines
	vp           viewport.Model
	w, h         int
	notice       string  // one-line feedback shown above the help line, cleared on the next key
	drag         textSel // a mouse selection inside one panel (select.go)
	pal          paletteState
	zoom         bool      // a panel is maximised (⌘⇧↵): the task list is hidden
	blurred      bool      // crew\'s window doesn\'t have focus (for notifications)
	spin         int       // the working spinner\'s frame
	spinning     bool      // its clock is running
	panelPct     int       // the shells' share of the main area, in percent (0: the default)
	resizing     bool      // dragging the border between the main pane and the shells
	sideW        int       // the task list width you dragged it to (0: it follows the window)
	resizingSide bool      // dragging the task list\'s border
	keys         *keys.Map // the app's ⌘ shortcuts (shortcuts.go)

	setup       setup.Status // git, Claude Code and gh: installed and signed in (setup.go)
	setupAt     time.Time    // when the last check started
	setupSel    int          // the tool picked in the Setup panel
	authFailed  string       // the agent whose login a turn failed on; cleared by signing in or a turn that works
	askedNotify bool         // ⏎ on Notifications asked macOS once; the next one opens System Settings

	tab    tab                    // which tab of the selected task the main pane shows
	usage  map[string]usage.Usage // per task, read from Claude's session logs
	unread map[string]bool        // tasks whose agent replied while you were looking elsewhere

	sideTop       int                     // first visible sidebar row when the list scrolls
	ovs           map[string]*table.State // per group row, what the overview is filtered to (overview.go)
	plans         map[string]*planView    // per task, its plan as last read (plan_tab.go)
	prList        []gh.Item               // PRs waiting for your review, then yours (prs.go)
	chats         []tasks.Task            // sections' chats: PRs' (pr: set) and REQUESTS' (kind:), never in TASKS
	reqv          *requests.Screen        // the REQUESTS section (its own package; reqs.go hands it keys and clicks)
	prListErr     string
	prListLoading bool                    // the PR list is being read (gh search)
	prPane        int                     // the open pane of a listed PR
	issuePane     int                     // the open tab of a Jira issue (jira.PaneIssue…)
	diffs         map[string]*diffState   // listed PRs' diffs, by URL
	reviews       map[string]*reviewState // Claude's reviews of listed PRs, by URL (review.go)
	menuIdx       int                     // highlighted row of the model menu
	pending       tea.Cmd                 // work started by a slash command, returned by submit

	// Terminal tab
	shells     map[string][]*term.Term // per task, the shells in the right-hand panel (max 4)
	shellFocus int                     // index of the shell that gets keys; -1 when the chat has them
	lastShell  int                     // the shell ⇧→ returns to

	md *markdown.Renderer // Markdown renderer for agent replies, plans and comments

	// suggestions in the message box (@ files, / commands)
	files       map[string][]string     // per task, from git ls-files
	fileTabs    map[string]*files.Tab   // per task, the Files tab: its tree and editor (files.go)
	editFocus   bool                    // keys go to the editor
	prv         *prview.View            // every PR read: a task's (PR tab, sidebar) and listed ones (pr.go is the glue)
	catalogs    map[string]catalogCache // per task: Claude Code commands, skills and agents
	sugg        []chat.Suggestion
	suggIdx     int
	argHint     string // arguments the inserted command expects, shown under the box
	argFor      string // the inserted command the hint belongs to
	listFocus   bool   // keys drive the task list (esc or ⇧← from the main pane); enter returns
	diffSummary string // e.g. "3 files changed, 10 insertions(+)"; "loading…" while git runs

	// modal windows
	modal      modal
	newTask    *tasks.Form // the new-task form, while it's open (tasks.go)
	statusMenu *tasks.Menu // a task's status menu, while it's open
	delID      string      // the task the delete prompt asks about
}

func New(cfg Config) Model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "> "
		}
		return "  "
	})
	in.FocusedStyle.CursorLine = lipgloss.NewStyle()
	in.FocusedStyle.Base = lipgloss.NewStyle()
	in.BlurredStyle.Base = lipgloss.NewStyle()
	// Terminals send Shift+Enter as plain Enter, so new lines use keys that arrive distinctly.
	// Enter itself sends (handled in updateChat); a trailing \ then Enter also inserts a new line.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	km := &keys.Map{Path: cfg.Keys}
	keys.Use(km)
	requests.KeyName, table.KeyName = keys.Names, keys.Names
	m := Model{cfg: cfg, keys: km, selKey: "home", shells: map[string][]*term.Term{}, shellFocus: -1, md: &markdown.Renderer{}, usage: map[string]usage.Usage{}, unread: map[string]bool{}, files: map[string][]string{}, fileTabs: map[string]*files.Tab{}, ovs: map[string]*table.State{}, plans: map[string]*planView{}, diffs: map[string]*diffState{}, reviews: map[string]*reviewState{}, reqv: requests.New(cfg.Ctx, cfg.Reqs), catalogs: map[string]catalogCache{}, input: in, vp: viewport.New(0, 0)}
	os.MkdirAll(cfg.Plans, 0o755) // agents get it with --add-dir, so it must exist
	if !m.pluginOK() {
		m.notice = "crew's skills (plugin/) weren't found, so agents run without them · run make install from the crew repo"
	}
	m.chat = chat.New(cfg.Ctx, cfg.Chats, m.md)
	m.prv = prview.New(m.md)
	m.sectionsOff = loadSectionsOff(cfg.Sections)
	m.jirav, m.cards = jira.New(cfg.Ctx), map[string]*jira.Card{}
	notifyDir = cfg.Notify
	m.fc = newFocus(cfg.Focus)
	if err := m.remStore().Import(filepath.Join(cfg.Focus, "reminders.json")); err != nil {
		m.notice = "couldn't move FOCUS's reminders to REMINDERS: " + err.Error()
	}
	m.loadReminders()
	m.reload()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, func() tea.Msg { return prPollMsg{} }, func() tea.Msg { return jiraPollMsg{} }, m.checkSetup(false), m.restartFocusTick(), func() tea.Msg { return remTickMsg{m.remGen} })
}

// reload re-reads tasks from disk, archiving old done tasks; the selection (a row key) is kept.
func (m *Model) reload() {
	all, err := m.cfg.Tasks.Load()
	m.loadErr = ""
	if err != nil {
		m.loadErr = err.Error()
	}
	if n := tasks.AutoArchive(m.cfg.Tasks, all); n > 0 {
		m.notice = fmt.Sprintf("Archived %d done task(s) untouched for %d days", n, tasks.ArchiveDays())
	}
	m.tasks, m.chats = nil, nil
	for _, t := range all {
		if t.Section() {
			m.chats = append(m.chats, t)
		} else {
			m.tasks = append(m.tasks, t)
		}
	}
	for _, t := range all {
		m.loadUsage(t)
		m.loadPlan(t.ID)
	}
	m.reqv.Load()
	m.onSelect()
}

func (m *Model) onSelect() {
	delete(m.unread, m.currentID())
	m.sugg = nil
	if t := m.chatTask(); t != nil {
		m.input.Placeholder = "Message " + chat.AgentName(*t) + " about " + t.Title + "…"
		if !m.listFocus {
			m.input.Focus()
		}
	} else {
		m.input.Blur()
	}
	m.layout()
	m.ensureVisible()
	m.refresh()
	if m.chatTask() != nil {
		m.vp.GotoBottom() // a chat opens at its latest message
	}
}

// current is the selected task, or nil when home or a group row is selected.
func (m Model) current() *tasks.Task {
	r := m.selRow()
	if r.Kind != rowTask {
		return nil
	}
	return &m.tasks[r.Index]
}

// currentID is the id of the chat on screen (a task's or a PR's), or homeID.
func (m Model) currentID() string {
	if t := m.chatTask(); t != nil {
		return t.ID
	}
	return homeID
}

func (m *Model) find(id string) *tasks.Task {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			return &m.tasks[i]
		}
	}
	for i := range m.chats {
		if m.chats[i].ID == id {
			return &m.chats[i]
		}
	}
	return nil
}

func (m *Model) setStatus(id, status string) {
	if t := m.find(id); t != nil && t.Status != status {
		t.Status = status
		m.cfg.Tasks.Save(*t)
	}
}
