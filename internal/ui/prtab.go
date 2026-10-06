package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/chat"
	"crew/internal/edit"
	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/task"
)

// PR tab: the task branch's pull request, read with the GitHub CLI (no agent tokens): state,
// checks, reviews, line comments and comments. ↑↓ pick an item; enter opens it (a line comment
// in the editor, a failed check's log, anything else in the browser); s sends it to the agent,
// a sends everything still open; P pushes the branch and creates the PR.

const prPollEvery = 2 * time.Minute // for the sidebar's CI marks

type prState struct {
	branch  string // the worktree's branch, which the PR is found by
	pr      *gh.PR
	err     string
	loading bool
	sel     int    // selected item
	log     string // a failed check's log, shown instead of the PR; esc goes back
	logName string
	armed   bool // P was pressed once
}

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
	prPollMsg struct{}
)

func (m Model) prOf(id string) *prState {
	if m.prs[id] == nil {
		m.prs[id] = &prState{}
	}
	return m.prs[id]
}

// loadPR reads the task's PR in the background.
func (m *Model) loadPR(t task.Task, threads bool) tea.Cmd {
	st := m.prOf(t.ID)
	if t.Worktree == "" || !gh.Installed() || st.loading {
		return nil
	}
	st.loading = true
	id, dir := t.ID, t.Worktree
	return func() tea.Msg {
		branch := taskBranch(dir, id)
		pr, err := gh.View(dir, branch, threads)
		return prMsg{id, branch, pr, err, threads}
	}
}

// taskBranch is the branch checked out in the task's worktree. It starts as the task id, but you
// may have switched it (e.g. to feat/…), and that's the branch the PR belongs to.
func taskBranch(dir, id string) string {
	if b := repo.Branch(dir); b != "" {
		return b
	}
	return id
}

func prPollTick() tea.Cmd {
	return tea.Tick(prPollEvery, func(time.Time) tea.Msg { return prPollMsg{} })
}

// pollPRs refreshes the PR of every live task with a branch, for the sidebar.
func (m *Model) pollPRs() tea.Cmd {
	cmds := []tea.Cmd{prPollTick()}
	for _, t := range m.tasks {
		if !t.Archived && t.Status != task.Done && t.Worktree != "" {
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
		st.pr = msg.pr
	}
	if m.tab == tabPR && msg.id == m.currentID() {
		m.renderPR()
	}
}

// prItem is one row group of the PR view; head items can't be selected.
type prItem struct {
	lines  []string
	head   bool
	url    string
	check  *gh.Check
	thread *gh.Thread
	ask    string // what s sends to the agent
}

func checkIcon(state string) string {
	switch state {
	case gh.Pass:
		return ready.Render("✓")
	case gh.Fail:
		return warn.Render("✗")
	case gh.Pending:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("●")
	}
	return dim.Render("-")
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(strings.TrimSpace(c.Body), "\n", "\n> ")
}

func (m Model) prItems(pr *gh.PR) []prItem {
	w := max(m.vp.Width-6, 20)
	wrap := func(s string) []string {
		return strings.Split(lipgloss.NewStyle().Width(w).Render(strings.TrimSpace(s)), "\n")
	}
	body := func(lines []string) []string {
		for i := range lines {
			lines[i] = "    " + lines[i]
		}
		return lines
	}
	var items []prItem
	head := func(lines ...string) { items = append(items, prItem{lines: lines, head: true}) }

	state := strings.ToLower(pr.State)
	if pr.Draft {
		state += " · draft"
	}
	merge := ""
	switch {
	case pr.State != "OPEN":
	case pr.Mergeable == "CONFLICTING":
		merge = " · " + warn.Render("conflicts")
	case pr.MergeState == "BLOCKED":
		merge = " · blocked"
	case pr.MergeState == "CLEAN":
		merge = " · " + ready.Render("ready to merge")
	}
	head(bold.Render(fmt.Sprintf("#%d %s", pr.Number, pr.Title)),
		dim.Render(fmt.Sprintf("%s · into %s · +%d −%d", state, pr.Base, pr.Additions, pr.Deletions))+merge,
		dim.Render(pr.URL), "")

	// Checks, failing first.
	count := map[string]int{}
	for _, c := range pr.Checks {
		count[c.State]++
	}
	summary := []string{}
	for _, s := range []string{gh.Fail, gh.Pending, gh.Pass, gh.Skip} {
		if count[s] > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", count[s], map[string]string{gh.Fail: "failed", gh.Pending: "running", gh.Pass: "passed", gh.Skip: "skipped"}[s]))
		}
	}
	if len(pr.Checks) == 0 {
		summary = []string{"none"}
	}
	head(bold.Render("Checks") + "  " + dim.Render(strings.Join(summary, " · ")))
	for _, s := range []string{gh.Fail, gh.Pending, gh.Pass, gh.Skip} {
		for i := range pr.Checks {
			c := &pr.Checks[i]
			if c.State != s {
				continue
			}
			took := ""
			if !c.Done.IsZero() && !c.Started.IsZero() {
				took = dim.Render("  " + c.Done.Sub(c.Started).Round(time.Second).String())
			}
			it := prItem{lines: []string{checkIcon(c.State) + " " + c.Name + took}, url: c.URL, check: c}
			if c.State == gh.Fail {
				it.ask = fmt.Sprintf("The CI check %q failed on PR #%d. Find out why (gh run view, or %s) and fix it.", c.Name, pr.Number, c.URL)
			}
			items = append(items, it)
		}
	}

	// Reviews.
	if len(pr.Reviews) > 0 {
		decision := strings.ToLower(strings.ReplaceAll(pr.ReviewDecision, "_", " "))
		head("", bold.Render("Reviews")+"  "+dim.Render(decision))
		for _, r := range pr.Reviews {
			icon, what := dim.Render("•"), "commented"
			switch r.State {
			case "APPROVED":
				icon, what = ready.Render("✔"), "approved"
			case "CHANGES_REQUESTED":
				icon, what = warn.Render("✎"), "requested changes"
			}
			it := prItem{lines: []string{icon + " " + bold.Render(r.Author) + " " + what + dim.Render("  "+ago(r.At))}, url: pr.URL}
			if strings.TrimSpace(r.Body) != "" {
				it.lines = append(it.lines, body(wrap(r.Body))...)
			}
			if strings.TrimSpace(r.Body) != "" && r.State != "APPROVED" {
				it.ask = fmt.Sprintf("Please address this review on PR #%d:\n\n%s", pr.Number, quote(r.Comment))
			}
			items = append(items, it)
		}
	}

	// Line comments, open ones first.
	if len(pr.Threads) > 0 || pr.ThreadsErr != "" {
		open := 0
		for _, t := range pr.Threads {
			if !t.Resolved {
				open++
			}
		}
		head("", bold.Render("Line comments")+"  "+dim.Render(fmt.Sprintf("%d open · %d resolved", open, len(pr.Threads)-open)))
		if pr.ThreadsErr != "" {
			head(warn.Render("  couldn't read them: " + pr.ThreadsErr))
		}
		for _, resolved := range []bool{false, true} {
			for i := range pr.Threads {
				t := &pr.Threads[i]
				if t.Resolved != resolved || len(t.Comments) == 0 {
					continue
				}
				where := fmt.Sprintf("%s:%d", t.Path, t.Line)
				tag := ""
				switch {
				case t.Resolved:
					tag = dim.Render("  resolved")
				case t.Outdated:
					tag = dim.Render("  outdated")
				}
				it := prItem{lines: []string{sel.Render(where) + tag}, url: t.Comments[0].URL, thread: t}
				var quoted []string
				for _, c := range t.Comments {
					l := body(wrap(bold.Render(c.Author) + ": " + c.Body))
					if t.Resolved {
						for j := range l {
							l[j] = dim.Render(l[j])
						}
					}
					it.lines = append(it.lines, l...)
					quoted = append(quoted, quote(c))
				}
				it.ask = fmt.Sprintf("Please address this review comment on %s (PR #%d):\n\n%s", where, pr.Number, strings.Join(quoted, "\n"))
				items = append(items, it)
			}
		}
	}

	// Conversation.
	if len(pr.Comments) > 0 {
		head("", bold.Render("Comments"))
		for _, c := range pr.Comments {
			it := prItem{lines: []string{bold.Render(c.Author) + dim.Render("  "+ago(c.At))}, url: c.URL,
				ask: fmt.Sprintf("Please address this comment on PR #%d:\n\n%s", pr.Number, quote(c))}
			it.lines = append(it.lines, body(wrap(c.Body))...)
			items = append(items, it)
		}
	}
	return items
}

// selectable lists the indexes of the items you can pick.
func selectable(items []prItem) []int {
	var out []int
	for i, it := range items {
		if !it.head {
			out = append(out, i)
		}
	}
	return out
}

// renderPR fills the viewport with the PR (or the log being viewed) and keeps the selection in view.
func (m *Model) renderPR() {
	t, st := m.current(), m.prOf(m.currentID())
	switch {
	case st.log != "":
		m.vp.SetContent(clip(bold.Render("Failed log · "+st.logName)+dim.Render("   esc back to the PR")+"\n\n"+st.log, m.vp.Width))
		return
	case !gh.Installed():
		m.vp.SetContent(dim.Render("The PR tab uses the GitHub CLI. Install it with make setup (or brew install gh), then run gh auth login."))
		return
	case t.Worktree == "":
		m.vp.SetContent(dim.Render("No branch yet. Send a message in the Chat tab to start the task."))
		return
	case st.pr == nil:
		branch := st.branch
		if branch == "" {
			branch = t.ID
		}
		msg := "No pull request for branch " + branch + " yet.\n\nPress P to push the branch and open one (gh pr create --fill)."
		switch {
		case st.loading:
			msg = "Looking for the pull request…"
		case st.err != "":
			msg = warn.Render(st.err) + "\n\n" + dim.Render("r tries again")
		}
		m.vp.SetContent(dim.Render(msg))
		return
	}
	items := m.prItems(st.pr)
	picks := selectable(items)
	st.sel = max(min(st.sel, len(picks)-1), 0)
	var lines []string
	top, height := 0, 0
	for i, it := range items {
		selected := len(picks) > 0 && picks[st.sel] == i
		if selected {
			top, height = len(lines), len(it.lines)
		}
		for j, l := range it.lines {
			prefix := "  "
			if selected && j == 0 {
				prefix = sel.Render("▶ ")
			}
			if it.head {
				prefix = ""
			}
			lines = append(lines, prefix+l)
		}
	}
	if st.err != "" {
		lines = append(lines, "", warn.Render("refresh failed: "+st.err))
	}
	m.vp.SetContent(clip(strings.Join(lines, "\n"), m.vp.Width))
	switch {
	case top < m.vp.YOffset:
		m.vp.SetYOffset(top)
	case top+height > m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(top + min(height, m.vp.Height) - m.vp.Height)
	}
}

func (m Model) selectedPRItem() *prItem {
	st := m.prOf(m.currentID())
	if st.pr == nil {
		return nil
	}
	items := m.prItems(st.pr)
	if picks := selectable(items); st.sel < len(picks) {
		return &items[picks[st.sel]]
	}
	return nil
}

func openURL(u string) {
	if u != "" {
		if c := exec.Command("open", u); c.Start() == nil {
			go c.Wait()
		}
	}
}

// updatePR handles keys on the PR tab; ok is false for keys it leaves to the generic tab keys.
func (m Model) updatePR(msg tea.KeyMsg) (_ Model, _ tea.Cmd, ok bool) {
	t, st := m.current(), m.prOf(m.currentID())
	k := msg.String()
	if k != "P" {
		st.armed = false
	}
	if st.log != "" {
		if k == "esc" || k == "q" {
			st.log = ""
			m.renderPR()
			return m, nil, true
		}
		return m, nil, false // scrolling, tab
	}
	switch k {
	case "up", "down":
		if k == "up" {
			st.sel--
		} else {
			st.sel++
		}
		m.renderPR()
	case "o":
		if it := m.selectedPRItem(); it != nil {
			openURL(it.url)
		} else if st.pr != nil {
			openURL(st.pr.URL)
		}
	case "enter", "l":
		it := m.selectedPRItem()
		switch {
		case it == nil:
		case it.thread != nil && k == "enter":
			return m, m.openAt(it.thread.Path, it.thread.Line), true
		case it.check != nil && it.check.State == gh.Fail:
			m.notice = "Reading the failed log…"
			id, dir, name, u := t.ID, t.Worktree, it.check.Name, it.check.URL
			return m, func() tea.Msg {
				log, err := gh.FailedLog(dir, u)
				return prLogMsg{id, name, log, err}
			}, true
		default:
			openURL(it.url)
		}
	case "s":
		if it := m.selectedPRItem(); it != nil && it.ask != "" {
			return m, m.askAgent(it.ask), true
		}
		m.notice = "Nothing to send for this item"
	case "a":
		if st.pr == nil {
			break
		}
		var asks []string
		for _, it := range m.prItems(st.pr) {
			open := it.thread == nil || !it.thread.Resolved && !it.thread.Outdated
			if it.ask != "" && open && (it.check != nil || it.thread != nil || strings.Contains(it.ask, "review on PR")) {
				asks = append(asks, fmt.Sprintf("%d. %s", len(asks)+1, it.ask))
			}
		}
		if len(asks) == 0 {
			m.notice = "No failing checks, open line comments or reviews to send"
			break
		}
		return m, m.askAgent(fmt.Sprintf("Please work through this feedback on PR #%d, then summarise what you changed:\n\n%s",
			st.pr.Number, strings.Join(asks, "\n\n"))), true
	case "P":
		switch {
		case t.Worktree == "" || !gh.Installed():
			m.notice = "Needs the task's branch and the GitHub CLI (make setup)"
		case st.pr != nil && st.pr.State == "OPEN":
			m.notice = "This branch already has an open PR"
		case !st.armed:
			st.armed = true
			m.notice = "Press P again to push " + taskBranch(t.Worktree, t.ID) + " to origin and open a PR"
		default:
			st.armed, st.loading = false, true
			m.notice = "Pushing and opening a PR…"
			id, dir := t.ID, t.Worktree
			return m, func() tea.Msg {
				u, err := gh.Create(dir, taskBranch(dir, id))
				return prCreatedMsg{id, u, err}
			}, true
		}
	default:
		return m, nil, false
	}
	return m, nil, true
}

// openAt opens a file of the task in the editor at a line.
func (m *Model) openAt(path string, line int) tea.Cmd {
	cmd := m.setTab(tabFiles)
	m.openInEditor(path)
	if ed := m.editors[m.currentID()]; ed != nil && ed.doc().path == path {
		ed.doc().buf.MoveTo(edit.Pos{Row: max(line-1, 0)}, false)
		ed.center()
	}
	return cmd
}

// askAgent sends text to the task's agent as your message and shows the chat.
func (m *Model) askAgent(text string) tea.Cmd {
	t := m.current()
	if m.conv(t.ID).busy {
		m.notice = "The agent is busy · send it when this turn ends"
		return nil
	}
	m.setTab(tabChat)
	m.add(t.ID, chat.You, text)
	if t.Session == "" {
		return m.start(*t, brief(*t)+"\n\n"+text)
	}
	return m.run(*t, text, true)
}

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
