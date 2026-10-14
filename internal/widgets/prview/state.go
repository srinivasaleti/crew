// Package prview is a pull request's conversation, read with the GitHub CLI (no agent tokens): a
// task's PR tab, and a listed PR's Conversation pane. Checks come first, then the feedback grouped
// by reviewer: their review, the line comments they started and their comments, one row each.
// enter or a click opens a row in a popup with the whole conversation as Markdown; there (or on
// the row) you can ask Claude about it (c), have Claude fix it (s), reply on GitHub (r), resolve
// or reopen a line comment (x), jump to the code (g) or open it on GitHub (o).
package prview

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
)

// Host is what the PR view needs from crew.
type Host interface {
	Ref() Ref       // the PR on screen
	OnPRView() bool // the PR view is on screen (a task's PR tab, a listed PR's conversation)
	Viewport() *viewport.Model
	Input() *textarea.Model // the message box, which the popup asks and replies in
	Notice(string)
	Layout()
	FocusMain()           // the main pane gets the keys
	SetPopup(open bool)   // the popup is (or stops being) the open modal
	PopupOpen() bool      // the popup is open
	PaneSize() (w, h int) // the main pane's inside width and the frames' height
	Task(id string) *tasks.Task
	SetStatus(id, status string)
	Agent(create bool) *tasks.Task // the task whose agent works on the PR on screen (made with create)
	Chats() *chat.Chats
	Send(t tasks.Task, q chat.Outgoing) tea.Cmd
	ShowChat(t tasks.Task)                // shows the chat the agent answers in
	OpenAt(path string, line int) tea.Cmd // opens a file of the task in the editor at a line
	Changed(key string, old, cur *gh.PR)  // a PR was read again: tell you what changed
}

// View is every PR crew has read, by key, and the popup's text.
type View struct {
	states map[string]*prState
	Pop    viewport.Model // the popup's text
	md     *markdown.Renderer
}

// New makes the PR view; md draws comments.
func New(md *markdown.Renderer) *View {
	return &View{states: map[string]*prState{}, Pop: viewport.New(0, 0), md: md}
}

type prState struct {
	branch  string // the worktree's branch, which the PR is found by
	pr      *gh.PR
	err     string
	loading bool
	sel     int             // selected row, among the rows shown
	open    map[string]bool // groups you opened or closed, by key (see defaultOpen)
	spans   []prSpan        // where each selectable row was drawn, for clicks
	log     string          // a failed check's log, shown instead of the PR; esc goes back
	logName string
	armed   bool                  // P was pressed once
	talk    map[string][]talkTurn // what you asked Claude about each row
	todo    bool                  // t: only what still needs you

	popKey string // the row open in the popup
	input  string // "ask" or "reply" while the popup's message box is open
}

// talkTurn is one question you asked Claude about a row: where it sits in the task's chat.
type talkTurn struct {
	at    int // index of your message in the chat
	asked string
}

type prSpan struct{ first, last, pick int }

type (
	// LoadedMsg is a PR read with gh.
	LoadedMsg struct {
		id      string
		branch  string
		pr      *gh.PR
		err     error
		threads bool // a full read (line comments too), not the sidebar's poll
	}
	// LogMsg is a failed check's log.
	LogMsg struct {
		id, name, log string
		err           error
	}
	// CreatedMsg is a PR opened from a task (P twice).
	CreatedMsg struct {
		id, url string
		err     error
	}
	// PostedMsg is a reply, comment or resolve that went to GitHub.
	PostedMsg struct {
		id, done string
		err      error
	}
)

// Of is a PR's state, made the first time.
func (v *View) Of(id string) *prState {
	if v.states[id] == nil {
		v.states[id] = &prState{open: map[string]bool{}, talk: map[string][]talkTurn{}}
	}
	return v.states[id]
}

// Branch and PR are a task's PR as last read ("" and nil before it's read, or without one).
func (v *View) Branch(key string) (string, *gh.PR) {
	if st := v.states[key]; st != nil {
		return st.branch, st.pr
	}
	return "", nil
}

// Read reports whether a PR has been read (or tried), so it isn't read again on its own.
func (v *View) Read(key string) bool {
	st := v.Of(key)
	return st.pr != nil || st.loading || st.err != ""
}

// Loading reports whether a PR is being read.
func (v *View) Loading(key string) bool { return v.Of(key).loading }

// Typing reports whether the popup's message box is open (asking Claude, or replying).
func (v *View) Typing(key string) bool { return v.Of(key).input != "" }

// ShowTodo opens a PR on what still needs you (t).
func (v *View) ShowTodo(key string) { v.Of(key).todo = true }

// Drop forgets a PR (its task moved to another branch).
func (v *View) Drop(key string) { delete(v.states, key) }

// Placed remembers where a question asked about a row went in its chat, so its answer shows under
// the row (chat.Host's Placed).
func (v *View) Placed(key, row, asked string, at int) {
	st := v.Of(key)
	st.talk[row] = append(st.talk[row], talkTurn{at, asked})
}

// taskBranch is the branch checked out in the task's worktree. It starts as the task id, but you
// may have switched it (e.g. to feat/…), and that's the branch the PR belongs to.
func taskBranch(dir, id string) string {
	if b := repo.Branch(dir); b != "" {
		return b
	}
	return id
}

// Ref is the PR a view is about: a task's (found by the branch in its worktree), or one from the
// PULL REQUESTS list (found by its URL, with no checkout needed).
type Ref struct {
	Key  string      // the PR's key: the task id, or "pr:" + URL
	Dir  string      // where gh runs: the task's worktree, or "" for a listed PR
	URL  string      // a listed PR's URL
	Task *tasks.Task // the task, for what needs its agent or worktree; nil for a listed PR
}

// TaskRef is a task's PR.
func TaskRef(t *tasks.Task) Ref { return Ref{Key: t.ID, Dir: t.Worktree, Task: t} }

// ListedRef is a PR from the PULL REQUESTS list.
func ListedRef(url string) Ref { return Ref{Key: "pr:" + url, URL: url} }

// refFor is the reference for a key, for messages that come back about it.
func (v *View) refFor(h Host, key string) Ref {
	if u, ok := strings.CutPrefix(key, "pr:"); ok {
		return Ref{Key: key, URL: u}
	}
	if t := h.Task(key); t != nil {
		return Ref{Key: key, Dir: t.Worktree, Task: t}
	}
	return Ref{Key: key}
}

// showing says whether a PR is on screen in the PR view.
func (v *View) showing(h Host, key string) bool { return h.OnPRView() && h.Ref().Key == key }

// Load reads a PR in the background: a task's by its branch, a listed one by its URL.
func (v *View) Load(ref Ref, threads bool) tea.Cmd {
	st := v.Of(ref.Key)
	if ref.Key == "" || ref.Task != nil && ref.Task.Worktree == "" || !gh.Installed() || st.loading {
		return nil
	}
	st.loading = true
	return func() tea.Msg {
		target, branch := ref.URL, ""
		if ref.Task != nil {
			branch = taskBranch(ref.Dir, ref.Task.ID)
			target = branch
		}
		pr, err := gh.View(ref.Dir, target, threads)
		return LoadedMsg{ref.Key, branch, pr, err, threads}
	}
}

// Update takes what comes back from GitHub; ok is false for other messages.
func (v *View) Update(h Host, msg tea.Msg) (_ tea.Cmd, ok bool) {
	switch msg := msg.(type) {
	case LoadedMsg:
		st := v.Of(msg.id)
		st.loading, st.err, st.branch = false, "", msg.branch
		if msg.err != nil {
			st.err = msg.err.Error()
		} else {
			if !msg.threads && msg.pr != nil && st.pr != nil && st.pr.Number == msg.pr.Number {
				msg.pr.Threads, msg.pr.ThreadsErr = st.pr.Threads, st.pr.ThreadsErr // the poll doesn't read them
			}
			h.Changed(msg.id, st.pr, msg.pr)
			st.pr = msg.pr
		}
		if v.showing(h, msg.id) {
			v.Render(h)
			if h.PopupOpen() {
				v.RefreshPopup(h, false)
			}
		}
		return nil, true

	case LogMsg:
		h.Notice("")
		switch {
		case msg.err != nil:
			h.Notice("couldn't read the log: " + msg.err.Error())
		case msg.log == "":
			h.Notice("The check has no failed log lines · o opens it in the browser")
		default:
			st := v.Of(msg.id)
			st.log, st.logName = msg.log, msg.name
			if v.showing(h, msg.id) {
				v.Render(h)
				h.Viewport().GotoBottom() // the error is usually at the end
			}
		}
		return nil, true

	case PostedMsg:
		h.Notice(msg.done)
		if msg.err != nil {
			h.Notice("GitHub said: " + msg.err.Error())
		}
		return v.Load(v.refFor(h, msg.id), true), true // shows the reply, or undoes a resolve that failed

	case CreatedMsg:
		st := v.Of(msg.id)
		st.loading = false
		if msg.err != nil {
			h.Notice("couldn't open the PR: " + msg.err.Error())
			return nil, true
		}
		h.Notice("Opened " + msg.url)
		h.SetStatus(msg.id, tasks.InPR)
		if t := h.Task(msg.id); t != nil {
			tc := *t // the read runs in the background, on its own copy
			return v.Load(TaskRef(&tc), true), true
		}
		return nil, true
	}
	return nil, false
}

// Badge is the sidebar's mark for a task's PR: CI state, and ! when changes were requested.
func (v *View) Badge(id string) string {
	st := v.states[id]
	if st == nil || st.pr == nil {
		return ""
	}
	pr := st.pr
	switch pr.State {
	case "MERGED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render("merged")
	case "CLOSED":
		return look.Dim.Render("closed")
	}
	state := gh.Pass
	for _, c := range pr.Checks {
		if c.State == gh.Fail {
			state = gh.Fail
			break
		}
		if c.State == gh.Pending {
			state = gh.Pending
		}
	}
	b := "PR"
	if len(pr.Checks) > 0 {
		b += checkIcon(state)
	}
	if pr.ReviewDecision == "CHANGES_REQUESTED" {
		b += look.Warn.Render("!")
	} else if pr.ReviewDecision == "APPROVED" {
		b += look.Ready.Render("✔")
	}
	return b
}
