package prview

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/browser"
	"crew/internal/gh"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
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
	orange   = look.Orange
	todoMark = orange.Render("●")
	doneMark = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✔")
	oldMark  = look.Dim.Render("◐")
	infoMark = look.Dim.Render("·")
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
		return look.Ready.Render("✓")
	case gh.Fail:
		return look.Warn.Render("✗")
	case gh.Pending:
		return yellow.Render("●")
	}
	return look.Dim.Render("-")
}

var yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

func reviewWhat(state string) (icon, what string) {
	switch state {
	case "APPROVED":
		return look.Ready.Render("✔"), "approved"
	case "CHANGES_REQUESTED":
		return look.Warn.Render("✎"), "requested changes"
	}
	return look.Dim.Render("•"), "reviewed"
}

func where(t *gh.Thread) string { return fmt.Sprintf("%s:%d", t.Path, t.Line) }

func (v *View) items(h Host, pr *gh.PR) []prItem {
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
		merge = " · " + look.Warn.Render("conflicts")
	case pr.MergeState == "BLOCKED":
		merge = " · blocked"
	case pr.MergeState == "CLEAN":
		merge = " · " + look.Ready.Render("ready to merge")
	}
	head(look.Bold.Render(fmt.Sprintf("#%d %s", pr.Number, pr.Title)))
	from := v.Of(h.Ref().Key).branch
	if from == "" { // a listed PR: its repo instead
		from = repoOf(pr.URL)
	}
	head(look.Dim.Render(fmt.Sprintf("%s · %s → %s · +%d −%d", state, from, pr.Base, pr.Additions, pr.Deletions)) + merge)
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
		todo = append(todo, look.Warn.Render(fmt.Sprintf("%d failing check(s)", count[gh.Fail])))
	}
	if openThreads > 0 {
		todo = append(todo, yellow.Render(fmt.Sprintf("%d open comment(s)", openThreads)))
	}
	if pr.ReviewDecision == "CHANGES_REQUESTED" {
		todo = append(todo, look.Warn.Render("changes requested"))
	}
	if count[gh.Pending] > 0 {
		todo = append(todo, yellow.Render(fmt.Sprintf("%d check(s) running", count[gh.Pending])))
	}
	if len(todo) == 0 {
		todo = []string{look.Ready.Render("nothing waiting on you")}
	}
	head(strings.Join(todo, look.Dim.Render(" · ")))

	// Checks: failing and running ones, the rest folded into one row.
	head("")
	head(look.Bold.Render("Checks"))
	if len(pr.Checks) == 0 {
		head(look.Dim.Render("  none"))
	}
	check := func(c *gh.Check, parent string) {
		took := ""
		if !c.Done.IsZero() && !c.Started.IsZero() {
			took = look.Dim.Render("  " + c.Done.Sub(c.Started).Round(time.Second).String())
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
		items = append(items, prItem{key: "group:passed", group: true, summary: look.Ready.Render("✓") + " " + label})
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
		head(look.Bold.Render("Feedback") + "  " + todoMark + orange.Render(fmt.Sprintf(" %d to do", todo)) + look.Dim.Render("  ·  ") +
			doneMark + look.Dim.Render(fmt.Sprintf(" %d resolved", done)) + look.Dim.Render(map[bool]string{true: "   t shows everything", false: "   t shows only what's to do"}[v.Of(h.Ref().Key).todo]))
	}
	if pr.ThreadsErr != "" {
		head(look.Warn.Render("  couldn't read the line comments: " + pr.ThreadsErr))
	}
	for _, p := range order {
		key := "author:" + p.name
		n := len(p.threads) + len(p.comments)
		label := look.Bold.Render(p.name)
		if isBot(p.name) {
			label += look.Dim.Render(" (bot)")
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
			stats = append(stats, look.Dim.Render(fmt.Sprintf("%d comment(s)", n)))
		}
		if len(stats) > 0 {
			label += "  " + strings.Join(stats, look.Dim.Render(" · "))
		}
		items = append(items, prItem{key: key, group: true, summary: label})

		if r := p.review; r != nil && strings.TrimSpace(markdown.Clean(r.Body)) != "" {
			icon, what := reviewWhat(r.State)
			if r.State == "CHANGES_REQUESTED" {
				icon = todoMark + " " + icon
			} else {
				icon = infoMark + " " + icon
			}
			items = append(items, prItem{key: "review:" + r.Author, parent: key, url: pr.URL, review: r,
				summary: icon + " " + what + ": " + markdown.FirstLine(r.Body) + look.Dim.Render("  "+look.Ago(r.At)),
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
				tag = look.Dim.Render(" resolved")
			case t.Outdated:
				tag = look.Dim.Render(" outdated")
			}
			replies := ""
			if r := len(t.Comments) - 1; r > 0 {
				replies = look.Dim.Render(fmt.Sprintf("  %d repl%s", r, map[bool]string{true: "y", false: "ies"}[r == 1]))
			}
			text := markdown.FirstLine(t.Comments[0].Body)
			if t.Resolved {
				text = look.Dim.Render(text)
			}
			var quoted []string
			for _, c := range t.Comments {
				quoted = append(quoted, quote(c))
			}
			items = append(items, prItem{key: "thread:" + t.ID, parent: key, url: t.Comments[0].URL, thread: t,
				summary: icon + " " + look.Sel.Render(where(t)) + tag + "  " + text + replies,
				about:   fmt.Sprintf("@%s's comment on %s", t.Comments[0].Author, where(t)),
				context: fmt.Sprintf("Review comment on %s:\n%s", where(t), strings.Join(quoted, "\n")) + hunkContext(t.DiffHunk)})
		}
		for _, c := range p.comments {
			items = append(items, prItem{key: "comment:" + c.URL, parent: key, url: c.URL, comment: c,
				summary: infoMark + " " + markdown.FirstLine(c.Body) + look.Dim.Render("  "+look.Ago(c.At)),
				about:   fmt.Sprintf("@%s's comment", c.Author), context: "PR comment:\n" + quote(*c)})
		}
	}
	if drafts := v.Of(h.Ref().Key).drafts; len(drafts) > 0 {
		for i := range items {
			if _, ok := drafts[items[i].key]; ok {
				items[i].summary += "  " + look.Claude.Render("✎ draft reply")
			}
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

// Render fills the viewport with the PR (or the log being viewed) and keeps the selection in view.
func (v *View) Render(h Host) {
	vp := h.Viewport()
	ref, st := h.Ref(), v.Of(h.Ref().Key)
	st.spans = nil
	switch {
	case st.log != "":
		vp.SetContent(look.Clip(look.Bold.Render("Failed log · "+st.logName)+look.Dim.Render("   esc back to the PR")+"\n\n"+st.log, vp.Width))
		return
	case !gh.Installed():
		vp.SetContent(look.Dim.Render("The PR tab uses the GitHub CLI. Install it with make setup (or brew install gh), then run gh auth login."))
		return
	case ref.Task != nil && ref.Task.Worktree == "":
		vp.SetContent(look.Dim.Render("No branch yet. Send a message in the Chat tab to start the task."))
		return
	case st.pr == nil:
		msg := "Reading the pull request…"
		if ref.Task != nil {
			branch := st.branch
			if branch == "" {
				branch = ref.Task.ID
			}
			msg = "No pull request for branch " + branch + " yet.\n\nPress P to push the branch and open one (gh pr create --fill)."
		}
		switch {
		case st.loading:
			msg = "Looking for the pull request…"
		case st.err != "":
			msg = look.Warn.Render(st.err) + "\n\n" + look.Dim.Render("R tries again")
		}
		vp.SetContent(look.Dim.Render(msg))
		return
	}
	items := st.shown(v.items(h, st.pr))
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
			marker, top = look.Sel.Render("▶ "), len(lines)
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
			row = look.Dim.Render(fold) + row
		}
		if len(st.talk[it.key]) > 0 {
			row += look.Claude.Render("  ◆ Claude")
		}
		st.spans = append(st.spans, prSpan{len(lines), len(lines), n})
		lines = append(lines, marker+indent+row)
		n++
	}
	if st.err != "" {
		lines = append(lines, "", look.Warn.Render("refresh failed: "+st.err))
	}
	vp.SetContent(look.Clip(strings.Join(lines, "\n"), vp.Width))
	switch {
	case top < vp.YOffset:
		vp.SetYOffset(top)
	case top >= vp.YOffset+vp.Height:
		vp.SetYOffset(top - vp.Height + 1)
	}
}

func (v *View) selected(h Host) *prItem {
	st := v.Of(h.Ref().Key)
	if st.pr == nil {
		return nil
	}
	items := st.shown(v.items(h, st.pr))
	if pk := picks(items); st.sel < len(pk) {
		return &items[pk[st.sel]]
	}
	return nil
}

// prItemByKey finds a row by key, shown or not.
func (v *View) itemByKey(h Host, key string) *prItem {
	st := v.Of(h.Ref().Key)
	if st.pr == nil {
		return nil
	}
	for _, it := range v.items(h, st.pr) {
		if it.key == key {
			return &it
		}
	}
	return nil
}

// selectKey selects a row by key, opening its group if needed.
func (v *View) selectKey(h Host, key string) {
	st := v.Of(h.Ref().Key)
	if it := v.itemByKey(h, key); it != nil && it.parent != "" {
		st.open[it.parent] = true
	}
	items := st.shown(v.items(h, st.pr))
	for j, i := range picks(items) {
		if items[i].key == key {
			st.sel = j
		}
	}
	v.Render(h)
}

// Key handles keys on the PR view; ok is false for keys it leaves to the generic tab keys.
func (v *View) Key(h Host, msg tea.KeyMsg) (_ tea.Cmd, ok bool) {
	ref, st := h.Ref(), v.Of(h.Ref().Key)
	k := msg.String()
	if k != "P" {
		st.armed = false
	}
	if k != "D" {
		st.armD = false
	}
	if st.log != "" {
		if k == "esc" || k == "q" {
			st.log = ""
			v.Render(h)
			return nil, true
		}
		return nil, false // scrolling, tab
	}
	it := v.selected(h)
	switch k {
	case "up", "down", "k", "j":
		if k == "up" || k == "k" {
			st.sel--
		} else {
			st.sel++
		}
		v.Render(h)
	case "right": // unfold a group
		if it != nil && it.group && !st.isOpen(it.key) {
			st.open[it.key] = true
			v.Render(h)
		}
	case "left": // fold a group, or go up to the group a row is in
		switch {
		case it != nil && it.group && st.isOpen(it.key):
			st.open[it.key] = false
		case it != nil && it.parent != "":
			items := st.shown(v.items(h, st.pr))
			for i, j := range picks(items) {
				if items[j].key == it.parent {
					st.sel = i
				}
			}
		}
		v.Render(h)
	case "n": // the next row that needs you
		v.nextOpen(h)
	case "t":
		st.todo = !st.todo
		v.Render(h)
	case "enter", " ":
		if it != nil {
			return v.activate(h, *it), true
		}
	case "o":
		if it != nil && it.url != "" {
			browser.Open(it.url)
		} else if st.pr != nil {
			browser.Open(st.pr.URL)
		}
	case "c", "s", "r", "x", "g":
		if it == nil || it.group {
			h.Notice("Pick a check, comment or review first")
			break
		}
		v.openPopup(h, it.key)
		return v.PopupKey(h, msg)
	case "a":
		return v.askAll(h), true
	case "P":
		if ref.Task == nil {
			h.Notice("This PR is already open")
			break
		}
		return v.create(h, *ref.Task), true
	case "D":
		switch {
		case len(st.drafts) == 0:
			h.Notice("No drafted replies · /fix-review has the agent draft them")
		case !st.armD:
			st.armD = true
			h.Notice(fmt.Sprintf("D again posts %d drafted replies to GitHub as you · any other key cancels", len(st.drafts)))
		default:
			st.armD = false
			return v.postDrafts(h, ref), true
		}
	case "R":
		return v.Load(ref, true), true
	default:
		return nil, false
	}
	return nil, true
}

// activate is enter or a click on a row: fold a group, show a failed check's log, open a
// passing check on GitHub, or open feedback in the popup.
func (v *View) activate(h Host, it prItem) tea.Cmd {
	st := v.Of(h.Ref().Key)
	switch {
	case it.group:
		st.open[it.key] = !st.isOpen(it.key)
		v.Render(h)
	case it.check != nil && it.check.State == gh.Fail:
		return v.readLog(h, h.Ref(), it.check)
	case it.check != nil:
		browser.Open(it.url)
	default:
		v.openPopup(h, it.key)
	}
	return nil
}

// needsYou reports whether a row is waiting on you.
func needsYou(it prItem) bool {
	return it.check != nil && it.check.State == gh.Fail || it.thread != nil && !it.thread.Resolved ||
		it.review != nil && it.review.State == "CHANGES_REQUESTED"
}

// nextOpen selects the next row after the selection that needs you, wrapping around.
func (v *View) nextOpen(h Host) bool {
	st := v.Of(h.Ref().Key)
	if st.pr == nil {
		return false
	}
	items := v.items(h, st.pr)
	cur := 0
	if it := v.selected(h); it != nil {
		for i := range items {
			if items[i].key == it.key {
				cur = i
			}
		}
	}
	for i := 1; i <= len(items); i++ {
		if it := items[(cur+i)%len(items)]; !it.head && needsYou(it) {
			v.selectKey(h, it.key)
			return true
		}
	}
	h.Notice("Nothing waiting on you")
	return false
}

// Mouse handles a click on the PR list, whose content starts at (x0, y0) on the screen: it selects
// the row and opens it (see activate), and returns what opening needs to run.
func (v *View) Mouse(h Host, msg tea.MouseMsg, x0, y0 int) (tea.Cmd, bool) {
	if !h.OnPRView() || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	st, vp := v.Of(h.Ref().Key), h.Viewport()
	if st.log != "" || msg.X < x0 || msg.X >= x0+vp.Width || msg.Y < y0 || msg.Y >= y0+vp.Height {
		return nil, false
	}
	h.FocusMain()
	line := vp.YOffset + msg.Y - y0
	for _, sp := range st.spans {
		if line >= sp.first && line <= sp.last {
			st.sel = sp.pick
			var cmd tea.Cmd
			if it := v.selected(h); it != nil {
				cmd = v.activate(h, *it)
			}
			v.Render(h)
			return cmd, true
		}
	}
	return nil, false
}

// hunkContext is the code a line comment is on, for Claude: the end of its diff hunk.
func hunkContext(h string) string {
	if h == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(h, "\n"), "\n")
	return "\n\nThe code it's on (the last line is the one commented):\n```diff\n" + strings.Join(lines[max(len(lines)-15, 0):], "\n") + "\n```"
}

// repoOf is owner/name from a PR's URL.
func repoOf(prURL string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(prURL, "https://"), "http://"), "/")
	if len(parts) >= 3 {
		return parts[1] + "/" + parts[2]
	}
	return prURL
}
