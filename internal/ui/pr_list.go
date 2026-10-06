package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
)

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
