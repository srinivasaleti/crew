package ui

import (
	"fmt"
	"os/exec"
	"regexp"
	"slices"
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
		if m.modal == prModal {
			m.refreshPopup(false)
		}
	}
}

// Markdown clean-up: GitHub comments (bots' especially) carry HTML that a terminal can't show.
var (
	mdHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	mdImage       = regexp.MustCompile(`(?is)<img[^>]*>`)
	mdLink        = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	mdSummary     = regexp.MustCompile(`(?is)<summary>(.*?)</summary>`)
	mdBreak       = regexp.MustCompile(`(?i)<br\s*/?>`)
	mdTag         = regexp.MustCompile(`(?i)</?(details|summary|p|div|sub|sup|b|strong|i|em|code|kbd|span|blockquote|table|thead|tbody|tr|td|th|picture|source)(\s[^>]*)?>`)
	mdAnyTag      = regexp.MustCompile(`<[^>]+>`)
	mdBotTag      = regexp.MustCompile(`(?m)^([ \t]*)</?[a-z][a-z _-]{0,30}>`) // e.g. <comment>, <file context>
	mdDetails     = regexp.MustCompile(`(?is)<details>\s*<summary>(.*?)</summary>.*?</details>`)
	mdAlert       = regexp.MustCompile(`(?m)^(>\s*)\[!(\w+)\]`)
	mdEmptyQuotes = regexp.MustCompile(`(?m)(^>[ \t]*\n){2,}`)
	mdBlankLines  = regexp.MustCompile(`\n{3,}`)
)

// cleanMD turns a GitHub comment into Markdown a terminal can render.
func cleanMD(s string) string {
	s = mdHTMLComment.ReplaceAllString(s, "")
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllStringFunc(s, func(a string) string {
		g := mdLink.FindStringSubmatch(a)
		if text := strings.TrimSpace(mdAnyTag.ReplaceAllString(g[2], "")); text != "" {
			return "[" + text + "](" + g[1] + ")"
		}
		return ""
	})
	s = mdSummary.ReplaceAllString(s, "**$1**\n")
	s = mdBotTag.ReplaceAllString(s, "$1")
	s = mdBreak.ReplaceAllString(s, "\n")
	s = mdTag.ReplaceAllString(s, "")
	s = mdAlert.ReplaceAllStringFunc(s, func(a string) string {
		g := mdAlert.FindStringSubmatch(a)
		return g[1] + "**" + strings.ToUpper(g[2][:1]) + strings.ToLower(g[2][1:]) + "**"
	})
	s = mdEmptyQuotes.ReplaceAllString(s, ">\n")
	s = mdBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// showMD is cleanMD for reading: <details> blocks (bots' long prompts and configs) fold to their
// title. Claude still gets them in full.
func showMD(s string) string {
	s = mdHTMLComment.ReplaceAllString(s, "")
	s = mdDetails.ReplaceAllStringFunc(s, func(d string) string {
		title := strings.TrimSpace(mdAnyTag.ReplaceAllString(mdDetails.FindStringSubmatch(d)[1], ""))
		return "\n*▸ " + title + " (folded · o shows it on GitHub)*\n"
	})
	return cleanMD(s)
}

// diffHunk colours the code a line comment is on, keeping the last lines (where the comment is).
func diffHunk(h string, w int) string {
	lines := strings.Split(strings.TrimRight(h, "\n"), "\n")
	lines = lines[max(len(lines)-12, 0):]
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "@@"):
			l = dim.Render(l)
		case strings.HasPrefix(l, "+"):
			l = green.Render(l)
		case strings.HasPrefix(l, "-"):
			l = warn.Render(l)
		}
		if i == len(lines)-1 {
			l = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(l) // the commented line
		}
		lines[i] = clip(l, w)
	}
	return strings.Join(lines, "\n")
}

// firstLine is the start of a comment for its row: the first line with words in it.
func firstLine(s string) string {
	for _, l := range strings.Split(showMD(s), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(l, ">#*-_ \t"))
		if l = strings.ReplaceAll(l, "**", ""); l != "" {
			return l
		}
	}
	return ""
}

func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(cleanMD(c.Body), "\n", "\n> ")
}

func isBot(login string) bool {
	l := strings.ToLower(login)
	return strings.Contains(l, "bot") || slices.Contains([]string{"coderabbitai", "copilot", "github-actions", "codecov", "sonarcloud", "vercel", "netlify"}, l)
}

// prItem is one row of the PR view, or a heading.
type prItem struct {
	key     string
	head    bool   // a heading; can't be selected
	group   bool   // a row that folds the rows under it
	parent  string // the group it sits in
	summary string
	url     string
	check   *gh.Check
	thread  *gh.Thread
	review  *gh.Review
	comment *gh.Comment
	about   string // e.g. "@alice's comment on files.go:42"
	context string // the quoted feedback that goes with a question to Claude
}

// defaultOpen: people's feedback starts open; bots, passing checks and resolved comments folded.
func (st *prState) isOpen(key string) bool {
	if v, ok := st.open[key]; ok {
		return v
	}
	name, ok := strings.CutPrefix(key, "author:")
	return ok && !isBot(name)
}

// Status marks for feedback, so you can see what still needs work.
var (
	orange   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	todoMark = orange.Render("●")
	doneMark = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✔")
	oldMark  = dim.Render("◐")
	infoMark = dim.Render("·")
)

func threadMark(t *gh.Thread) string {
	switch {
	case t.Resolved:
		return doneMark
	case t.Outdated:
		return oldMark
	}
	return todoMark
}

func checkIcon(state string) string {
	switch state {
	case gh.Pass:
		return ready.Render("✓")
	case gh.Fail:
		return warn.Render("✗")
	case gh.Pending:
		return yellow.Render("●")
	}
	return dim.Render("-")
}

var (
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	claude = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
)

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

func reviewWhat(state string) (icon, what string) {
	switch state {
	case "APPROVED":
		return ready.Render("✔"), "approved"
	case "CHANGES_REQUESTED":
		return warn.Render("✎"), "requested changes"
	}
	return dim.Render("•"), "reviewed"
}

func where(t *gh.Thread) string { return fmt.Sprintf("%s:%d", t.Path, t.Line) }

func (m Model) prItems(pr *gh.PR) []prItem {
	var items []prItem
	head := func(s string) { items = append(items, prItem{head: true, summary: s}) }

	// Title and what's waiting on you.
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
	head(bold.Render(fmt.Sprintf("#%d %s", pr.Number, pr.Title)))
	head(dim.Render(fmt.Sprintf("%s · %s → %s · +%d −%d", state, m.prOf(m.currentID()).branch, pr.Base, pr.Additions, pr.Deletions)) + merge)
	count := map[string]int{}
	for _, c := range pr.Checks {
		count[c.State]++
	}
	openThreads := 0
	for _, t := range pr.Threads {
		if !t.Resolved {
			openThreads++
		}
	}
	var todo []string
	if count[gh.Fail] > 0 {
		todo = append(todo, warn.Render(fmt.Sprintf("%d failing check(s)", count[gh.Fail])))
	}
	if openThreads > 0 {
		todo = append(todo, yellow.Render(fmt.Sprintf("%d open comment(s)", openThreads)))
	}
	if pr.ReviewDecision == "CHANGES_REQUESTED" {
		todo = append(todo, warn.Render("changes requested"))
	}
	if count[gh.Pending] > 0 {
		todo = append(todo, yellow.Render(fmt.Sprintf("%d check(s) running", count[gh.Pending])))
	}
	if len(todo) == 0 {
		todo = []string{ready.Render("nothing waiting on you")}
	}
	head(strings.Join(todo, dim.Render(" · ")))

	// Checks: failing and running ones, the rest folded into one row.
	head("")
	head(bold.Render("Checks"))
	if len(pr.Checks) == 0 {
		head(dim.Render("  none"))
	}
	check := func(c *gh.Check, parent string) {
		took := ""
		if !c.Done.IsZero() && !c.Started.IsZero() {
			took = dim.Render("  " + c.Done.Sub(c.Started).Round(time.Second).String())
		}
		it := prItem{key: "check:" + c.Name, parent: parent, summary: checkIcon(c.State) + " " + c.Name + took, url: c.URL, check: c}
		if c.State == gh.Fail {
			it.about = "the failing check " + c.Name
			it.context = fmt.Sprintf("The CI check %q failed (%s).", c.Name, c.URL)
		}
		items = append(items, it)
	}
	for _, s := range []string{gh.Fail, gh.Pending} {
		for i := range pr.Checks {
			if pr.Checks[i].State == s {
				check(&pr.Checks[i], "")
			}
		}
	}
	if n := count[gh.Pass] + count[gh.Skip]; n > 0 {
		label := fmt.Sprintf("%d passed", count[gh.Pass])
		if count[gh.Skip] > 0 {
			label += fmt.Sprintf(" · %d skipped", count[gh.Skip])
		}
		items = append(items, prItem{key: "group:passed", group: true, summary: ready.Render("✓") + " " + label})
		for i := range pr.Checks {
			if s := pr.Checks[i].State; s == gh.Pass || s == gh.Skip {
				check(&pr.Checks[i], "group:passed")
			}
		}
	}

	// Feedback, by person: their review, the line comments they started, their comments.
	type person struct {
		name     string
		review   *gh.Review
		threads  []*gh.Thread
		comments []*gh.Comment
		open     int
	}
	people := map[string]*person{}
	who := func(name string) *person {
		if people[name] == nil {
			people[name] = &person{name: name}
		}
		return people[name]
	}
	for i := range pr.Reviews {
		r := &pr.Reviews[i]
		who(r.Author).review = r
		if r.State == "CHANGES_REQUESTED" {
			who(r.Author).open++
		}
	}
	for i := range pr.Threads {
		t := &pr.Threads[i]
		if len(t.Comments) > 0 {
			p := who(t.Comments[0].Author)
			p.threads = append(p.threads, t)
			if !t.Resolved {
				p.open++
			}
		}
	}
	for i := range pr.Comments {
		who(pr.Comments[i].Author).comments = append(who(pr.Comments[i].Author).comments, &pr.Comments[i])
	}
	order := make([]*person, 0, len(people))
	for _, p := range people {
		order = append(order, p)
	}
	slices.SortFunc(order, func(a, b *person) int { // people before bots, then most open first
		switch {
		case isBot(a.name) != isBot(b.name):
			if isBot(a.name) {
				return 1
			}
			return -1
		case a.open != b.open:
			return b.open - a.open
		}
		return strings.Compare(a.name, b.name)
	})
	if len(order) > 0 || pr.ThreadsErr != "" {
		todo, done := 0, 0
		for _, p := range order {
			todo += p.open
		}
		for _, t := range pr.Threads {
			if t.Resolved {
				done++
			}
		}
		head("")
		head(bold.Render("Feedback") + "  " + todoMark + orange.Render(fmt.Sprintf(" %d to do", todo)) + dim.Render("  ·  ") +
			doneMark + dim.Render(fmt.Sprintf(" %d resolved", done)) + dim.Render(map[bool]string{true: "   t shows everything", false: "   t shows only what's to do"}[m.prOf(m.currentID()).todo]))
	}
	if pr.ThreadsErr != "" {
		head(warn.Render("  couldn't read the line comments: " + pr.ThreadsErr))
	}
	for _, p := range order {
		key := "author:" + p.name
		n := len(p.threads) + len(p.comments)
		label := bold.Render(p.name)
		if isBot(p.name) {
			label += dim.Render(" (bot)")
		}
		if p.review != nil && p.review.State != "COMMENTED" {
			icon, what := reviewWhat(p.review.State)
			label += "  " + icon + " " + what
		}
		var stats []string
		if p.open > 0 {
			stats = append(stats, orange.Render(fmt.Sprintf("%d to do", p.open)))
		}
		if n > 0 {
			stats = append(stats, dim.Render(fmt.Sprintf("%d comment(s)", n)))
		}
		if len(stats) > 0 {
			label += "  " + strings.Join(stats, dim.Render(" · "))
		}
		items = append(items, prItem{key: key, group: true, summary: label})

		if r := p.review; r != nil && strings.TrimSpace(cleanMD(r.Body)) != "" {
			icon, what := reviewWhat(r.State)
			if r.State == "CHANGES_REQUESTED" {
				icon = todoMark + " " + icon
			} else {
				icon = infoMark + " " + icon
			}
			items = append(items, prItem{key: "review:" + r.Author, parent: key, url: pr.URL, review: r,
				summary: icon + " " + what + ": " + firstLine(r.Body) + dim.Render("  "+ago(r.At)),
				about:   fmt.Sprintf("@%s's review", r.Author), context: "Review:\n" + quote(r.Comment)})
		}
		slices.SortStableFunc(p.threads, func(a, b *gh.Thread) int { // open first
			switch {
			case a.Resolved == b.Resolved:
				return 0
			case b.Resolved:
				return -1
			}
			return 1
		})
		for _, t := range p.threads {
			icon, tag := threadMark(t), ""
			switch {
			case t.Resolved:
				tag = dim.Render(" resolved")
			case t.Outdated:
				tag = dim.Render(" outdated")
			}
			replies := ""
			if r := len(t.Comments) - 1; r > 0 {
				replies = dim.Render(fmt.Sprintf("  %d repl%s", r, map[bool]string{true: "y", false: "ies"}[r == 1]))
			}
			text := firstLine(t.Comments[0].Body)
			if t.Resolved {
				text = dim.Render(text)
			}
			var quoted []string
			for _, c := range t.Comments {
				quoted = append(quoted, quote(c))
			}
			items = append(items, prItem{key: "thread:" + t.ID, parent: key, url: t.Comments[0].URL, thread: t,
				summary: icon + " " + sel.Render(where(t)) + tag + "  " + text + replies,
				about:   fmt.Sprintf("@%s's comment on %s", t.Comments[0].Author, where(t)),
				context: fmt.Sprintf("Review comment on %s:\n%s", where(t), strings.Join(quoted, "\n"))})
		}
		for _, c := range p.comments {
			items = append(items, prItem{key: "comment:" + c.URL, parent: key, url: c.URL, comment: c,
				summary: infoMark + " " + firstLine(c.Body) + dim.Render("  "+ago(c.At)),
				about:   fmt.Sprintf("@%s's comment", c.Author), context: "PR comment:\n" + quote(*c)})
		}
	}
	return items
}

// shown is the rows on screen: headings and every row whose group is open.
func (st *prState) shown(items []prItem) []prItem {
	busy := map[string]bool{} // groups with something to do in them
	for _, it := range items {
		if needsYou(it) {
			busy[it.parent] = true
		}
	}
	var out []prItem
	for _, it := range items {
		switch {
		case st.todo && !it.head && !needsYou(it) && !(it.group && busy[it.key]):
		case it.parent == "" || st.isOpen(it.parent):
			out = append(out, it)
		}
	}
	return out
}

// picks lists the indexes of the rows you can select.
func picks(items []prItem) []int {
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
	st.spans = nil
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
			msg = warn.Render(st.err) + "\n\n" + dim.Render("R tries again")
		}
		m.vp.SetContent(dim.Render(msg))
		return
	}
	items := st.shown(m.prItems(st.pr))
	pk := picks(items)
	st.sel = max(min(st.sel, len(pk)-1), 0)
	var lines []string
	top, n := 0, 0
	for i, it := range items {
		if it.head {
			lines = append(lines, it.summary)
			continue
		}
		marker := "  "
		if pk[st.sel] == i {
			marker, top = sel.Render("▶ "), len(lines)
		}
		indent := ""
		if it.parent != "" {
			indent = "    "
		}
		row := it.summary
		if it.group {
			fold := "▸ "
			if st.isOpen(it.key) {
				fold = "▾ "
			}
			row = dim.Render(fold) + row
		}
		if len(st.talk[it.key]) > 0 {
			row += claude.Render("  ◆ Claude")
		}
		st.spans = append(st.spans, prSpan{len(lines), len(lines), n})
		lines = append(lines, marker+indent+row)
		n++
	}
	if st.err != "" {
		lines = append(lines, "", warn.Render("refresh failed: "+st.err))
	}
	m.vp.SetContent(clip(strings.Join(lines, "\n"), m.vp.Width))
	switch {
	case top < m.vp.YOffset:
		m.vp.SetYOffset(top)
	case top >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(top - m.vp.Height + 1)
	}
}

func (m Model) selectedPRItem() *prItem {
	st := m.prOf(m.currentID())
	if st.pr == nil {
		return nil
	}
	items := st.shown(m.prItems(st.pr))
	if pk := picks(items); st.sel < len(pk) {
		return &items[pk[st.sel]]
	}
	return nil
}

// prItemByKey finds a row by key, shown or not.
func (m Model) prItemByKey(key string) *prItem {
	st := m.prOf(m.currentID())
	if st.pr == nil {
		return nil
	}
	for _, it := range m.prItems(st.pr) {
		if it.key == key {
			return &it
		}
	}
	return nil
}

// selectKey selects a row by key, opening its group if needed.
func (m *Model) selectKey(key string) {
	st := m.prOf(m.currentID())
	if it := m.prItemByKey(key); it != nil && it.parent != "" {
		st.open[it.parent] = true
	}
	items := st.shown(m.prItems(st.pr))
	for j, i := range picks(items) {
		if items[i].key == key {
			st.sel = j
		}
	}
	m.renderPR()
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
	it := m.selectedPRItem()
	switch k {
	case "up", "down", "k", "j":
		if k == "up" || k == "k" {
			st.sel--
		} else {
			st.sel++
		}
		m.renderPR()
	case "n": // the next row that needs you
		m.nextOpenPR()
	case "t":
		st.todo = !st.todo
		m.renderPR()
	case "right", "left":
		if it != nil && it.group {
			st.open[it.key] = k == "right"
			m.renderPR()
		}
	case "enter", " ":
		if it != nil {
			return m, m.activatePR(*it), true
		}
	case "o":
		if it != nil && it.url != "" {
			openURL(it.url)
		} else if st.pr != nil {
			openURL(st.pr.URL)
		}
	case "c", "s", "r", "x", "g":
		if it == nil || it.group {
			m.notice = "Pick a check, comment or review first"
			break
		}
		m.openPopup(it.key)
		return m.updatePopup(msg)
	case "a":
		return m, m.askAllPR(), true
	case "P":
		return m, m.createPR(*t), true
	case "R":
		return m, m.loadPR(*t, true), true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// activatePR is enter or a click on a row: fold a group, show a failed check's log, open a
// passing check on GitHub, or open feedback in the popup.
func (m *Model) activatePR(it prItem) tea.Cmd {
	st := m.prOf(m.currentID())
	switch {
	case it.group:
		st.open[it.key] = !st.isOpen(it.key)
		m.renderPR()
	case it.check != nil && it.check.State == gh.Fail:
		return m.readLog(*m.current(), it.check)
	case it.check != nil:
		openURL(it.url)
	default:
		m.openPopup(it.key)
	}
	return nil
}

// needsYou reports whether a row is waiting on you.
func needsYou(it prItem) bool {
	return it.check != nil && it.check.State == gh.Fail || it.thread != nil && !it.thread.Resolved ||
		it.review != nil && it.review.State == "CHANGES_REQUESTED"
}

// nextOpenPR selects the next row after the selection that needs you, wrapping around.
func (m *Model) nextOpenPR() bool {
	st := m.prOf(m.currentID())
	if st.pr == nil {
		return false
	}
	items := m.prItems(st.pr)
	cur := 0
	if it := m.selectedPRItem(); it != nil {
		for i := range items {
			if items[i].key == it.key {
				cur = i
			}
		}
	}
	for i := 1; i <= len(items); i++ {
		if it := items[(cur+i)%len(items)]; !it.head && needsYou(it) {
			m.selectKey(it.key)
			return true
		}
	}
	m.notice = "Nothing waiting on you"
	return false
}

// askAboutPR sends your question with the row's feedback to the agent; the answer shows in the popup.
func (m *Model) askAboutPR(it prItem, question string) tea.Cmd {
	t, st := m.current(), m.prOf(m.currentID())
	c := m.conv(t.ID)
	if c.busy {
		m.notice = "Claude is busy · ask again when this turn ends (ctrl+x stops it)"
		return nil
	}
	text := fmt.Sprintf("About %s on PR #%d:\n\n%s\n\n%s", it.about, st.pr.Number, it.context, question)
	st.talk[it.key] = append(st.talk[it.key], talkTurn{len(c.msgs), question})
	m.add(t.ID, chat.You, text)
	var cmd tea.Cmd
	if t.Session == "" {
		cmd = m.start(*t, brief(*t)+"\n\n"+text)
	} else {
		cmd = m.run(*t, text, true)
	}
	m.renderPR()
	return cmd
}

// askAllPR sends every failing check, open line comment and change request to the agent at once,
// and shows the chat.
func (m *Model) askAllPR() tea.Cmd {
	st := m.prOf(m.currentID())
	if st.pr == nil {
		return nil
	}
	var asks []string
	for _, it := range m.prItems(st.pr) {
		if !it.head && needsYou(it) && it.context != "" && (it.thread == nil || !it.thread.Outdated) {
			asks = append(asks, fmt.Sprintf("%d. %s", len(asks)+1, it.context))
		}
	}
	if len(asks) == 0 {
		m.notice = "Nothing waiting on you: no failing checks, open comments or change requests"
		return nil
	}
	t := m.current()
	if m.conv(t.ID).busy {
		m.notice = "Claude is busy · try again when this turn ends"
		return nil
	}
	text := fmt.Sprintf("Please work through this feedback on PR #%d, then summarise what you changed:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
	m.setTab(tabChat)
	m.add(t.ID, chat.You, text)
	if t.Session == "" {
		return m.start(*t, brief(*t)+"\n\n"+text)
	}
	return m.run(*t, text, true)
}

// replyPR posts your reply on GitHub. A line comment gets it in its thread. GitHub has no replies
// for reviews and conversation comments, so those get a new PR comment that quotes them.
func (m *Model) replyPR(t task.Task, it prItem, text string) tea.Cmd {
	st := m.prOf(t.ID)
	dir, prURL, id := t.Worktree, st.pr.URL, t.ID
	m.notice = "Posting your reply…"
	var post func() error
	switch {
	case it.thread != nil:
		threadID := it.thread.ID
		post = func() error { return gh.ReplyThread(dir, prURL, threadID, text) }
	case it.review != nil:
		body := quote(it.review.Comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	default:
		body := quote(*it.comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	}
	return func() tea.Msg { return prPostedMsg{id, "Reply posted", post()} }
}

// toggleResolved resolves or reopens a line-comment thread, showing the change right away.
func (m *Model) toggleResolved(t task.Task, th *gh.Thread) tea.Cmd {
	st := m.prOf(t.ID)
	resolve := !th.Resolved
	for i := range st.pr.Threads {
		if st.pr.Threads[i].ID == th.ID {
			st.pr.Threads[i].Resolved = resolve
		}
	}
	m.renderPR()
	done := "Resolved"
	if !resolve {
		done = "Reopened"
	}
	dir, prURL, id, threadID := t.Worktree, st.pr.URL, t.ID, th.ID
	return func() tea.Msg { return prPostedMsg{id, done, gh.SetResolved(dir, prURL, threadID, resolve)} }
}

func (m *Model) readLog(t task.Task, c *gh.Check) tea.Cmd {
	m.notice = "Reading the failed log…"
	id, dir, name, u := t.ID, t.Worktree, c.Name, c.URL
	return func() tea.Msg {
		log, err := gh.FailedLog(dir, u)
		return prLogMsg{id, name, log, err}
	}
}

// createPR pushes the branch and opens a PR, on the second P.
func (m *Model) createPR(t task.Task) tea.Cmd {
	st := m.prOf(t.ID)
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
		return func() tea.Msg {
			u, err := gh.Create(dir, taskBranch(dir, id))
			return prCreatedMsg{id, u, err}
		}
	}
	return nil
}

// mousePR handles a click on the PR list: it selects the row and opens it (see activatePR).
// What opening needs to run is left in m.pending.
func (m *Model) mousePR(msg tea.MouseMsg) bool {
	if m.tab != tabPR || m.current() == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	st := m.prOf(m.currentID())
	x0 := sideOuter + 1
	if st.log != "" || msg.X < x0 || msg.X >= x0+m.vp.Width || msg.Y < contentY || msg.Y >= contentY+m.vp.Height {
		return false
	}
	line := m.vp.YOffset + msg.Y - contentY
	for _, sp := range st.spans {
		if line >= sp.first && line <= sp.last {
			st.sel = sp.pick
			if it := m.selectedPRItem(); it != nil {
				m.pending = m.activatePR(*it)
			}
			m.renderPR()
			return true
		}
	}
	return false
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

// ---- The popup: one row's whole conversation, rendered as Markdown, with its actions. ----

// popupSize is the popup's outer size, inside the main pane.
func (m Model) popupSize() (w, h int) { return max(m.chatWidth()-4, 40), max(m.bodyHeight()-4, 12) }

// popupInner is the width of the popup's text.
func (m Model) popupInner() int { w, _ := m.popupSize(); return w - 4 }

func (m *Model) openPopup(key string) {
	st := m.prOf(m.currentID())
	m.selectKey(key)
	st.popKey, st.input = key, ""
	m.modal = prModal
	m.refreshPopup(true)
}

func (m *Model) closePopup() {
	st := m.prOf(m.currentID())
	st.popKey, st.input = "", ""
	m.modal = noModal
	m.input.Reset()
	m.input.Blur()
	m.layout()
	m.renderPR()
}

// refreshPopup sizes the popup's viewport and redraws its text; top scrolls to the start, otherwise
// it stays put (or follows Claude's answer if you're at the bottom).
func (m *Model) refreshPopup(top bool) {
	st := m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		m.closePopup()
		return
	}
	_, h := m.popupSize()
	inputH := 0
	if st.input != "" {
		m.input.SetWidth(m.popupInner())
		inputH = m.input.Height() + 1
	}
	follow := m.pop.AtBottom()
	m.pop.Width, m.pop.Height = m.popupInner(), max(h-2-3-inputH, 3) // border, title, rule, hint line
	m.pop.SetContent(m.popupBody(*it, m.popupInner()))
	switch {
	case top:
		m.pop.GotoTop()
	case follow:
		m.pop.GotoBottom()
	}
}

// popupBody is the conversation: every comment as Markdown, then what you asked Claude about it.
func (m *Model) popupBody(it prItem, w int) string {
	var b []string
	say := func(c gh.Comment) {
		b = append(b, bold.Render(c.Author)+dim.Render("  "+ago(c.At)), m.markdown(showMD(c.Body), w), "")
	}
	switch {
	case it.thread != nil:
		if h := it.thread.DiffHunk; h != "" {
			b = append(b, sel.Render(where(it.thread)), diffHunk(h, w), "")
		}
		for _, c := range it.thread.Comments {
			say(c)
		}
	case it.review != nil:
		say(it.review.Comment)
	case it.comment != nil:
		say(*it.comment)
	case it.check != nil:
		b = append(b, checkIcon(it.check.State)+" "+it.check.Name, dim.Render(it.check.URL), "",
			dim.Render("Close this and press ⏎ on the check to read its failed log."), "")
	}
	turns := m.prOf(m.currentID()).talk[it.key]
	if len(turns) > 0 {
		c := m.conv(m.currentID())
		b = append(b, claude.Render("── with Claude ──"), "")
		for i, tt := range turns {
			b = append(b, bold.Render("You")+dim.Render(": ")+tt.asked, "")
			actions := 0
			for _, msg := range c.msgs[min(tt.at+1, len(c.msgs)):] {
				if msg.Role == chat.You {
					break
				}
				switch msg.Role {
				case chat.Agent:
					b = append(b, m.markdown(msg.Text, w))
				case chat.Tool:
					actions++
				case chat.Error:
					b = append(b, warn.Render(msg.Text))
				}
			}
			if actions > 0 {
				b = append(b, dim.Render(fmt.Sprintf("(%d actions · the Chat tab lists them)", actions)))
			}
			if i == len(turns)-1 && c.busy {
				b = append(b, claude.Render("Claude is working…"))
			}
			b = append(b, "")
		}
	}
	return strings.Join(b, "\n")
}

// popupKeys is what the popup offers for a row.
func popupKeys(it prItem) string {
	var keys []string
	if it.context != "" {
		keys = append(keys, "c ask Claude", "s fix it")
	}
	if it.thread != nil {
		keys = append(keys, "r reply in thread")
		if it.thread.Resolved {
			keys = append(keys, "x reopen")
		} else {
			keys = append(keys, "x resolve")
		}
		keys = append(keys, "g go to code")
	} else if it.review != nil || it.comment != nil {
		keys = append(keys, "r reply")
	}
	return strings.Join(append(keys, "o GitHub", "n next open", "esc close"), " · ")
}

func (m Model) popupView() string {
	st := m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		return ""
	}
	w, h := m.popupSize()
	inner := m.popupInner()
	title := it.about
	if title == "" {
		title = it.check.Name
	}
	status := ""
	switch {
	case it.thread != nil && it.thread.Resolved:
		status = "  " + doneMark + dim.Render(" resolved")
	case it.thread != nil && it.thread.Outdated:
		status = "  " + oldMark + dim.Render(" outdated (the code changed since)")
	case needsYou(*it):
		status = "  " + todoMark + orange.Render(" to do")
	}
	parts := []string{clip(bold.Render(title)+status, inner), dim.Render(strings.Repeat("─", inner)), m.pop.View()}
	switch st.input {
	case "ask":
		parts = append(parts, claude.Render("↳ Ask Claude")+dim.Render(" · ⏎ send · esc cancel"), m.input.View())
	case "reply":
		label := yellow.Render("↳ Reply in this thread on GitHub") + dim.Render(" · ⏎ posts it · esc cancel")
		if it.thread == nil {
			label = yellow.Render("↳ New PR comment quoting @"+strings.TrimPrefix(strings.Split(it.about, "'")[0], "@")) +
				dim.Render(" (GitHub can't reply in place here) · ⏎ posts it · esc cancel")
		}
		parts = append(parts, clip(label, inner), m.input.View())
	}
	parts = append(parts, clip(dim.Render(popupKeys(*it)), inner))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("141")).Padding(0, 1).Width(w - 2).Height(h - 2)
	return box.Render(strings.Join(parts, "\n"))
}

// updatePopup handles keys while the popup is open.
func (m Model) updatePopup(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	t, st := m.current(), m.prOf(m.currentID())
	it := m.prItemByKey(st.popKey)
	if it == nil {
		m.closePopup()
		return m, nil, true
	}
	k := msg.String()
	if st.input != "" {
		switch k {
		case "esc":
			st.input = ""
			m.input.Reset()
			m.input.Blur()
			m.refreshPopup(false)
			return m, nil, true
		case "enter":
			v := m.input.Value()
			if strings.HasSuffix(v, "\\") { // \⏎ adds a line, as in the chat
				m.input.SetValue(strings.TrimSuffix(v, "\\") + "\n")
				m.refreshPopup(false)
				return m, nil, true
			}
			text := strings.TrimSpace(v)
			if text == "" {
				return m, nil, true
			}
			mode := st.input
			st.input = ""
			m.input.Reset()
			m.input.Blur()
			var cmd tea.Cmd
			if mode == "ask" {
				cmd = m.askAboutPR(*it, text)
			} else {
				cmd = m.replyPR(*t, *it, text)
			}
			m.refreshPopup(false)
			m.pop.GotoBottom()
			return m, cmd, true
		}
		m.input.Focus()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.refreshPopup(false)
		return m, cmd, true
	}
	switch k {
	case "esc", "q":
		m.closePopup()
	case "up", "down", "pgup", "pgdown", "k", "j":
		var cmd tea.Cmd
		m.pop, cmd = m.pop.Update(msg)
		return m, cmd, true
	case "home":
		m.pop.GotoTop()
	case "end":
		m.pop.GotoBottom()
	case "c", "r":
		if k == "c" && it.context == "" || k == "r" && it.thread == nil && it.review == nil && it.comment == nil {
			break
		}
		st.input = map[string]string{"c": "ask", "r": "reply"}[k]
		m.input.Reset()
		m.input.Placeholder = "e.g. “is this right?” or “fix it but keep the API”…"
		if k == "r" {
			m.input.Placeholder = "Your reply…"
		}
		m.input.SetHeight(3)
		m.input.Focus()
		m.refreshPopup(false)
	case "s":
		if it.context != "" {
			cmd := m.askAboutPR(*it, "Please fix this. Keep the change small, then tell me what you changed.")
			m.refreshPopup(false)
			m.pop.GotoBottom()
			return m, cmd, true
		}
	case "x":
		if it.thread != nil {
			cmd := m.toggleResolved(*t, it.thread)
			m.refreshPopup(false)
			return m, cmd, true
		}
	case "g":
		if it.thread != nil {
			m.closePopup()
			return m, m.openAt(it.thread.Path, it.thread.Line), true
		}
	case "o":
		openURL(it.url)
	case "n":
		if m.nextOpenPR() {
			m.openPopup(m.selectedPRItem().key)
		}
	}
	return m, nil, true
}

// mousePopup: the wheel scrolls the popup, a click outside it closes it.
func (m *Model) mousePopup(msg tea.MouseMsg) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		m.pop, _ = m.pop.Update(msg)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		w, h := m.popupSize()
		left, top := sideOuter+1+(m.chatWidth()-w)/2, 1+(m.bodyHeight()-2-h)/2
		if msg.X < left || msg.X >= left+w || msg.Y < top || msg.Y >= top+h {
			m.closePopup()
		}
	}
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
