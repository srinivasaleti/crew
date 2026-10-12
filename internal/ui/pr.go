package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/look"
	"crew/internal/repo"
	"crew/internal/tasks"
)

// PR tab: the task branch's pull request, read with the GitHub CLI (no agent tokens). Checks come
// first, then the feedback grouped by reviewer: their review, the line comments they started and
// their comments, one row each. enter or a click opens a row in a popup with the whole
// conversation as Markdown; there (or on the row) you can ask Claude about it (c), have Claude fix
// it (s), reply on GitHub (r), resolve or reopen a line comment (x), jump to the code (g) or open
// it on GitHub (o).

const prPollEvery = 2 * time.Minute // for the sidebar's CI marks

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
	prMsg struct {
		id      string
		branch  string
		pr      *gh.PR
		err     error
		threads bool // a full read (line comments too), not the sidebar's poll
	}
	prLogMsg struct {
		id, name, log string
		err           error
	}
	prCreatedMsg struct {
		id, url string
		err     error
	}
	prPostedMsg struct { // a reply, comment or resolve went to GitHub
		id, done string
		err      error
	}
	prPollMsg struct{}
)

func (m Model) prOf(id string) *prState {
	if m.prs[id] == nil {
		m.prs[id] = &prState{open: map[string]bool{}, talk: map[string][]talkTurn{}}
	}
	return m.prs[id]
}

// taskBranch is the branch checked out in the task's worktree. It starts as the task id, but you
// may have switched it (e.g. to feat/…), and that's the branch the PR belongs to.
func taskBranch(dir, id string) string {
	if b := repo.Branch(dir); b != "" {
		return b
	}
	return id
}

// prRef is the PR a view is about: a task's (found by the branch in its worktree), or one from the
// PULL REQUESTS list (found by its URL, with no checkout needed).
type prRef struct {
	key  string      // prs map key: the task id, or "pr:" + URL
	dir  string      // where gh runs: the task's worktree, or "" for a listed PR
	url  string      // a listed PR's URL
	task *tasks.Task // the task, for what needs its agent or worktree; nil for a listed PR
}

// prRef is the PR on screen: the selected task's, or the selected PR in the list.
func (m Model) prRef() prRef {
	if t := m.current(); t != nil {
		return prRef{key: t.ID, dir: t.Worktree, task: t}
	}
	if it := m.selPR(); it != nil {
		return prRef{key: "pr:" + it.URL, url: it.URL}
	}
	return prRef{}
}

func (m Model) prKey() string { return m.prRef().key }

// refFor is the reference for a prs key, for messages that come back about it.
func (m Model) refFor(key string) prRef {
	if u, ok := strings.CutPrefix(key, "pr:"); ok {
		return prRef{key: key, url: u}
	}
	if t := m.find(key); t != nil {
		return prRef{key: key, dir: t.Worktree, task: t}
	}
	return prRef{key: key}
}

// onPRView says whether the PR view is on screen: a task's PR tab, or a listed PR's conversation.
func (m Model) onPRView() bool {
	return m.current() != nil && m.tab == tabPR || m.selPR() != nil && m.prPane == paneConversation
}

func (m Model) showingPR(key string) bool { return m.onPRView() && m.prKey() == key }

// loadPR reads the task's PR in the background.
func (m *Model) loadPR(t tasks.Task, threads bool) tea.Cmd {
	return m.loadPRRef(prRef{key: t.ID, dir: t.Worktree, task: &t}, threads)
}

// loadPRRef reads a PR in the background: a task's by its branch, a listed one by its URL.
func (m *Model) loadPRRef(ref prRef, threads bool) tea.Cmd {
	st := m.prOf(ref.key)
	if ref.key == "" || ref.task != nil && ref.task.Worktree == "" || !gh.Installed() || st.loading {
		return nil
	}
	st.loading = true
	return func() tea.Msg {
		target, branch := ref.url, ""
		if ref.task != nil {
			branch = taskBranch(ref.dir, ref.task.ID)
			target = branch
		}
		pr, err := gh.View(ref.dir, target, threads)
		return prMsg{ref.key, branch, pr, err, threads}
	}
}

func prPollTick() tea.Cmd {
	return tea.Tick(prPollEvery, func(time.Time) tea.Msg { return prPollMsg{} })
}

// pollPRs refreshes the PULL REQUESTS list and the PR of every live task with a branch, for each
// section that's on.
func (m *Model) pollPRs() tea.Cmd {
	cmds := []tea.Cmd{prPollTick()}
	if gh.Installed() && m.sectionOn("prs") {
		m.prListLoading = true // the PULL REQUESTS row spins until the list is back
		cmds = append(cmds, loadPRList(), m.startSpin())
	}
	for _, t := range m.tasks {
		if !m.sectionOn("tasks") {
			break
		}
		if !t.Archived && t.Status != tasks.Done && t.Worktree != "" {
			cmds = append(cmds, m.loadPR(t, false))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) onPR(msg prMsg) {
	st := m.prOf(msg.id)
	st.loading, st.err, st.branch = false, "", msg.branch
	if msg.err != nil {
		st.err = msg.err.Error()
	} else {
		if !msg.threads && msg.pr != nil && st.pr != nil && st.pr.Number == msg.pr.Number {
			msg.pr.Threads, msg.pr.ThreadsErr = st.pr.Threads, st.pr.ThreadsErr // the poll doesn't read them
		}
		m.notifyPR(msg.id, st.pr, msg.pr)
		st.pr = msg.pr
	}
	if m.showingPR(msg.id) {
		m.renderPR()
		if m.modal == prModal {
			m.refreshPopup(false)
		}
	}
}

var ago = look.Ago

// prBadge is the sidebar's mark for a task's PR: CI state, and ! when changes were requested.
func (m Model) prBadge(id string) string {
	st := m.prs[id]
	if st == nil || st.pr == nil {
		return ""
	}
	pr := st.pr
	switch pr.State {
	case "MERGED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render("merged")
	case "CLOSED":
		return dim.Render("closed")
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
		b += warn.Render("!")
	} else if pr.ReviewDecision == "APPROVED" {
		b += ready.Render("✔")
	}
	return b
}
