package prs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/browser"
	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
	"crew/internal/widgets/prview"
	"crew/internal/widgets/table"
)

// PULL REQUESTS, as crew's plugin: the open PRs where your review is requested (from you or a team
// you're in), your own open PRs in any repo your GitHub account can see, and the other open PRs in
// your attached repos, grouped by repo (gh search; no tokens). A PR opens on four
// panes: its conversation (crew's PR view, as on a task's PR tab), a chat with Claude about it
// (chat.go), its diff, and a review by Claude that you edit and post (review.go).

// The panes of a listed PR.
const (
	paneConversation = iota
	paneChat
	paneDiff
	paneReview
)

// paneNames are a listed PR's panes.
var paneNames = []string{"Conversation", "Chat", "Diff", "Review"}

// rowPR is a PR's row under PULL REQUESTS (Index indexes the list).
const rowPR = "pr"

// pollEvery is how often the list is read again.
const pollEvery = 2 * time.Minute

type (
	// listMsg is the list, read.
	listMsg struct {
		items []gh.Item
		err   error
	}
	// diffMsg is a listed PR's diff, read.
	diffMsg struct {
		url, diff string
		err       error
	}
	// pollMsg is the list's clock; a tick from an older clock (gen) is dropped.
	pollMsg struct{ gen int }
)

// diffState is a listed PR's diff, read once.
type diffState struct {
	text    string
	err     string
	loading bool
}

// Section is PULL REQUESTS.
type Section struct {
	section.Base
	repos   repo.Store // the attached repos: a PR's chat and review use a checkout when its repo is here
	chatDir string     // where a PR's chat runs when its repo isn't attached
	list    []gh.Item  // PRs waiting for your review, then yours, then the rest in your repos
	err     string     // why the list couldn't be read last time
	loading bool       // the list is being read (gh search)
	pane    int        // the open pane of a listed PR
	diffs   map[string]*diffState
	reviews map[string]*reviewState // Claude's reviews, by URL (review.go)
	gen     int
}

// NewSection makes PULL REQUESTS: repos are the attached repos, chatDir where a PR's chat runs
// when its repo isn't one of them.
func NewSection(repos repo.Store, chatDir string) *Section {
	return &Section{repos: repos, chatDir: chatDir, diffs: map[string]*diffState{}, reviews: map[string]*reviewState{}}
}

// Def is PULL REQUESTS on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "prs", Label: "PULL REQUESTS", Letter: "p", About: "PRs waiting for your review, yours, and the rest in your repos, from GitHub (gh)"}
}

// hint draws a hint line under the pane, with ⌘ for keys.
func hint(s string) string { return look.KeyHints(keys.Names(s)) }

// ---- the list ----

// loadList reads the lists in the background: PRs waiting for your review, yours, then the rest in
// your attached repos.
func loadList(repos repo.Store) tea.Cmd {
	if !gh.Installed() {
		return nil
	}
	return func() tea.Msg {
		review, err1 := gh.ToReview()
		mine, err2 := gh.Mine()
		others, err3 := gh.InRepos(repos.GitHub())
		var items []gh.Item
		seen := map[string]bool{}
		for _, it := range append(append(review, mine...), others...) {
			if !seen[it.URL] { // a PR in more than one list keeps the first: waiting for you, then yours
				seen[it.URL] = true
				items = append(items, it)
			}
		}
		// Waiting for you first, then yours, then the rest; the most recently updated first in each.
		slices.SortStableFunc(items, func(a, b gh.Item) int {
			if ra, rb := listRank(a), listRank(b); ra != rb {
				return ra - rb
			}
			return b.Updated.Compare(a.Updated)
		})
		return listMsg{items, errors.Join(err1, err2, err3)}
	}
}

// listRank orders the lists: waiting for your review, yours, the rest in your repos.
func listRank(it gh.Item) int {
	switch {
	case it.Review:
		return 0
	case it.Mine:
		return 1
	}
	return 2
}

// refresh reads the list again now, without waiting for the clock (r on the list).
func (s *Section) refresh(h section.Host) tea.Cmd {
	if s.loading || !gh.Installed() {
		return nil
	}
	s.loading = true
	h.Notice("Reading pull requests again…")
	return tea.Batch(loadList(s.repos), h.Spin())
}

// poll reads the list now (the row spins until it's back), and starts the clock that reads it again.
func (s *Section) poll(h section.Host) tea.Cmd {
	gen := s.gen
	cmds := []tea.Cmd{tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{gen} })}
	if gh.Installed() && h.SectionOn("prs") {
		s.loading = true
		cmds = append(cmds, loadList(s.repos), h.Spin())
	}
	return tea.Batch(cmds...)
}

// Init reads the list, through Update (Init's host is a copy that doesn't last).
func (s *Section) Init(section.Host) tea.Cmd {
	gen := s.gen
	return func() tea.Msg { return pollMsg{gen} }
}

// On reads the list again, on a new clock (an older one's ticks are dropped).
func (s *Section) On(h section.Host) tea.Cmd {
	s.gen++
	return s.poll(h)
}

// Update takes the list, a diff, a review, a posted review, and the clock.
func (s *Section) Update(h section.Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pollMsg:
		if msg.gen != s.gen {
			return nil, true
		}
		return s.poll(h), true
	case listMsg:
		s.onList(h, msg)
	case diffMsg:
		s.onDiff(h, msg)
	case reviewMsg:
		s.onReview(h, msg)
	case reviewPostedMsg:
		return s.onReviewPosted(h, msg), true
	default:
		return nil, false
	}
	return nil, true
}

// onList takes the list; a failed read keeps the last one.
func (s *Section) onList(h section.Host, msg listMsg) {
	s.err, s.loading = "", false
	if msg.err != nil {
		s.err = msg.err.Error()
		if msg.items == nil {
			return // keep the last list
		}
	}
	s.list = msg.items
	if h.Current() == nil {
		h.Refresh()
	}
}

// Spin keeps the spinner going while the list loads or Claude reviews, and redraws the review
// that's on screen.
func (s *Section) Spin(h section.Host) bool {
	if rs := s.review(h); rs != nil && rs.running {
		s.render(h)
	}
	busy := s.loading
	for _, rs := range s.reviews {
		busy = busy || rs.running
	}
	return busy
}

// selected is the PR selected in the sidebar, if a PR row is.
func (s *Section) selected(h section.Host) *gh.Item {
	if r := h.SelectedRow(); r.Kind == rowPR && r.Index < len(s.list) {
		return &s.list[r.Index]
	}
	return nil
}

// Rows are PULL REQUESTS (there from the start, spinning while the list loads), then the PR you
// opened, if one is.
func (s *Section) Rows(h section.Host) []section.Row {
	if !gh.Installed() {
		return nil
	}
	rows := []section.Row{{Kind: section.Group, Section: "prs", Label: "PULL REQUESTS", Count: len(s.list)}}
	if url, ok := strings.CutPrefix(h.SelKey(), "p:"); ok {
		if i := slices.IndexFunc(s.list, func(it gh.Item) bool { return it.URL == url }); i >= 0 {
			rows = append(rows, section.Row{Kind: rowPR, Section: "prs", Key: "p:" + url, Depth: 1, Index: i})
		}
	}
	return rows
}

// Title is PULL REQUESTS' row: a spinner while the list is read, and no count before the first one.
func (s *Section) Title(h section.Host, r section.Row) (string, string) {
	if !s.loading {
		return section.CountTitle(r), ""
	}
	title := section.CountTitle(r)
	if len(s.list) == 0 {
		title = look.Bold.Render(r.Label) // no count until the first list is in
	}
	return title, look.Sel.Render(look.SpinFrames[h.SpinFrame()%len(look.SpinFrames)])
}

// mark is a listed PR's mark: waiting for your review, yours, someone else's, or a draft.
func mark(it gh.Item) string {
	switch {
	case it.Draft:
		return look.Dim.Render("◌")
	case it.Review:
		return look.Sel.Render("●")
	case it.Mine:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("⇡")
	}
	return look.Dim.Render("○")
}

// Lines draws a PR's two sidebar lines: its title, then repo · #n · who and how old.
func (s *Section) Lines(h section.Host, r section.Row, selected bool) string {
	it := s.list[r.Index]
	who := "@" + it.Author
	if it.Mine && !it.Review {
		who = "yours"
	}
	return h.ItemLines(r, mark(it), it.Title, fmt.Sprintf("%s #%d · %s · %s", shortRepo(it.Repo), it.Number, who, look.Ago(it.Updated)), selected)
}

// shortRepo is a repo's name without its owner.
func shortRepo(nameWithOwner string) string {
	_, name, ok := strings.Cut(nameWithOwner, "/")
	if !ok {
		return nameWithOwner
	}
	return name
}

// Selected opens another PR on its conversation.
func (s *Section) Selected(_ section.Host, r section.Row, from string) {
	if r.Kind == rowPR && r.Key != from {
		s.pane = paneConversation
	}
}

// ShowsTable is PULL REQUESTS' own row.
func (s *Section) ShowsTable(_ section.Host, r section.Row) bool { return r.Kind == section.Group }

// Table is every listed PR under its repo's heading, filtered by chip or search.
func (s *Section) Table(section.Host, section.Row) table.Table {
	t := table.Table{
		Empty: "No open PRs here.", Groups: map[string]string{},
		Cols:  []table.Col{{Name: "pull request", Width: 0}, {Name: "author", Width: 14}, {Name: "list", Width: 11}, {Name: "opened", Width: 9}, {Name: "updated", Width: 9}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "review", Label: "To review"}, {ID: "mine", Label: "Mine"}, {ID: "others", Label: "Your repos"}, {ID: "draft", Label: "Drafts"}},
		Keys:  map[string]string{"v": "review", "d": "diff", "c": "chat", "r": "refresh"}, Global: map[string]bool{"r": true},
		Acts: []table.Act{{Label: "Open", Key: "enter"}, {Label: "Chat", Key: "c"}, {Label: "Diff", Key: "d"}, {Label: "Review", Key: "v"},
			{Label: "+ Refresh", Key: "r"}},
		Hint: "⏎ opens · c chat with Claude about it · d its diff · v review it with Claude · r refresh · ←/→ fold a repo · tab filter · / search",
	}
	if s.loading && len(s.list) == 0 {
		t.Empty = "Loading pull requests…"
	}
	review, mine := 0, 0
	for _, it := range s.list {
		tags, list := []string{"all"}, look.Dim.Render("in your repo")
		switch {
		case it.Review && it.Team:
			tags, list = append(tags, "review"), "team review"
			review++
		case it.Review:
			tags, list = append(tags, "review"), "review"
			review++
		case it.Mine:
			tags, list = append(tags, "mine"), "yours"
			mine++
		default:
			tags = append(tags, "others")
		}
		if it.Draft {
			tags = append(tags, "draft")
		}
		t.Groups[it.Repo] = shortRepo(it.Repo)
		t.Rows = append(t.Rows, table.Row{
			ID: it.URL,
			Cells: []string{mark(it) + " " + it.Title + look.Dim.Render(fmt.Sprintf("  #%d", it.Number)), "@" + it.Author,
				list, look.Dim.Render(look.Ago(it.Created)), look.Dim.Render(look.Ago(it.Updated))},
			Tags: tags, Group: it.Repo,
			Search: fmt.Sprintf("%s %s %s %d", it.Title, it.Repo, it.Author, it.Number),
		})
	}
	t.Summary = fmt.Sprintf("%d waiting for your review · %d of yours · %d more in your repos", review, mine, len(s.list)-review-mine)
	if s.err != "" {
		t.Summary += " · " + look.Warn.Render("couldn't refresh: "+markdown.FirstLine(s.err))
	}
	return t
}

// Open opens a PR from the table, on its conversation, chat ("chat"), diff ("diff") or review
// ("review"); "refresh" reads the list again.
func (s *Section) Open(h section.Host, _ section.Row, url, action string) tea.Cmd {
	if action == "refresh" {
		return s.refresh(h)
	}
	s.pane = paneConversation
	h.Select("p:" + url)
	h.FocusMain()
	s.setPane(h, map[string]int{"chat": paneChat, "diff": paneDiff, "review": paneReview}[action])
	if action == "review" {
		return s.startReview(h)
	}
	return nil
}

// ---- a listed PR ----

// setPane opens one of a listed PR's panes; the Chat pane gets the keys for its message box.
func (s *Section) setPane(h section.Host, p int) {
	s.pane = p
	in := h.Input()
	if it := s.selected(h); it != nil && p == paneChat {
		t := s.ensureChat(h, *it)
		h.MarkRead(t.ID)
		in.Placeholder = "Message " + chat.AgentName(*t) + " about this PR…"
		in.Focus()
	} else {
		in.Blur()
	}
	h.ClearSuggestions()
	h.Layout()
	vp := h.Viewport()
	vp.GotoTop()
	s.render(h)
	if p == paneChat {
		vp.GotoBottom()
	}
}

// Frame is the main pane's title on a PR.
func (s *Section) Frame(_ section.Host, r section.Row) string {
	if r.Kind == rowPR {
		return "pull request"
	}
	return ""
}

// Header is a PR's first line: its title, repo, number and author, and its chat's state.
func (s *Section) Header(h section.Host, _ section.Row, w int) string {
	it := s.selected(h)
	if it == nil {
		return ""
	}
	meta := fmt.Sprintf(" · %s #%d · @%s", it.Repo, it.Number, it.Author)
	if it.Draft {
		meta += " · draft"
	}
	return ansi.Truncate(look.Bold.Render(it.Title)+look.Dim.Render(meta)+h.ChatMeta(s.chat(h, it.URL)), w, "…")
}

// Tabs is a PR's panes.
func (s *Section) Tabs(_ section.Host, r section.Row, w int) string {
	if r.Kind != rowPR {
		return ""
	}
	active := lipgloss.NewStyle().Background(look.Accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range paneNames {
		if i == s.pane {
			bar = append(bar, active.Render(section.TabLabel(n, i)))
		} else {
			bar = append(bar, look.Dim.Padding(0, 1).Render(section.TabLabel(n, i)))
		}
	}
	return ansi.Truncate(strings.Join(bar, " ")+look.Dim.Render("   tab switches · o opens it on GitHub"), w, "…")
}

// TabsHere is a PR's panes.
func (s *Section) TabsHere(_ section.Host, r section.Row) (int, int) {
	if r.Kind != rowPR {
		return 0, 0
	}
	return s.pane, len(paneNames)
}

// GotoTab opens one of a PR's panes.
func (s *Section) GotoTab(h section.Host, _ section.Row, i int) tea.Cmd {
	s.setPane(h, i)
	h.Refresh()
	return nil
}

// Render fills the main pane with a PR's open pane.
func (s *Section) Render(h section.Host, _ section.Row) { s.render(h) }

// render draws the selected PR's open pane.
func (s *Section) render(h section.Host) {
	it := s.selected(h)
	if it == nil {
		return
	}
	vp := h.Viewport()
	switch s.pane {
	case paneConversation:
		v, ph := h.PR()
		v.Render(ph)
	case paneDiff:
		d := s.diffs[it.URL]
		switch {
		case d == nil || d.loading:
			vp.SetContent(look.Dim.Render("Reading the diff…"))
		case d.err != "":
			vp.SetContent(look.Warn.Render(d.err))
		default:
			vp.SetContent(colorDiff(d.text, vp.Width))
		}
	case paneReview:
		vp.SetContent(s.reviewView(h, *it))
	case paneChat:
		follow := vp.AtBottom() || vp.TotalLineCount() <= vp.Height
		vp.SetContent(s.chatView(h, *it))
		if follow { // follows new output unless you've scrolled up to read
			vp.GotoBottom()
		}
	}
}

// colorDiff draws a unified diff: a header per file, additions green, removals red.
func colorDiff(diff string, w int) string {
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	var out []string
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "diff --git"):
			_, b, _ := strings.Cut(l, " b/")
			out = append(out, "", look.Sel.Render("━━ "+b))
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "index "),
			strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"):
		case strings.HasPrefix(l, "@@"):
			out = append(out, look.Dim.Render(l))
		case strings.HasPrefix(l, "+"):
			out = append(out, green.Render(ansi.Truncate(l, w, "…")))
		case strings.HasPrefix(l, "-"):
			out = append(out, look.Warn.Render(ansi.Truncate(l, w, "…")))
		default:
			out = append(out, ansi.Truncate(l, w, "…"))
		}
	}
	return strings.Join(out, "\n")
}

// AutoLoad starts what the screen is waiting for: the selected PR and its diff.
func (s *Section) AutoLoad(h section.Host) tea.Cmd {
	it := s.selected(h)
	if it == nil {
		return nil
	}
	var cmds []tea.Cmd
	if v, _ := h.PR(); !v.Read("pr:" + it.URL) {
		cmds = append(cmds, v.Load(prview.ListedRef(it.URL), true))
	}
	if d := s.diffs[it.URL]; s.pane == paneDiff && d == nil {
		s.diffs[it.URL] = &diffState{loading: true}
		u := it.URL
		cmds = append(cmds, func() tea.Msg {
			diff, err := gh.Diff(u)
			return diffMsg{u, diff, err}
		})
	}
	return tea.Batch(cmds...)
}

// onDiff takes a PR's diff.
func (s *Section) onDiff(h section.Host, msg diffMsg) {
	d := &diffState{text: msg.diff}
	if msg.err != nil {
		d.err = msg.err.Error()
	}
	s.diffs[msg.url] = d
	if it := s.selected(h); it != nil && it.URL == msg.url && s.pane == paneDiff {
		s.render(h)
	}
}

// Key handles keys on a listed PR: tab/shift+tab switch panes; each pane has its own keys.
func (s *Section) Key(h section.Host, _ section.Row, msg tea.KeyMsg) (tea.Cmd, bool) {
	it := s.selected(h)
	if it == nil {
		return nil, false
	}
	k := msg.String()
	if s.pane == paneReview && s.editing(h) { // typing in the review's edit box
		return s.reviewEditKey(h, msg), true
	}
	switching := k == "tab" || k == "shift+tab"
	if s.pane == paneChat && (!switching || h.ChatTyping()) {
		return h.ChatKey(msg), true // the message box, as in a task's chat
	}
	if switching {
		d := map[string]int{"shift+tab": -1, "tab": 1}[k]
		s.setPane(h, (s.pane+d+len(paneNames))%len(paneNames))
		return nil, true
	}
	switch s.pane {
	case paneConversation: // the PR view's own keys first (its rows, popups, the failed log)
		if k == "v" {
			s.setPane(h, paneReview)
			return s.startReview(h), true
		}
		v, ph := h.PR()
		if cmd, ok := v.Key(ph, msg); ok {
			return cmd, true
		}
	case paneReview:
		if k != "esc" && k != "o" {
			return s.reviewKey(h, msg), true
		}
	}
	switch k {
	case "o":
		browser.Open(it.URL)
		return nil, true
	case "esc":
		h.GoUp()
		return nil, true
	case "v":
		s.setPane(h, paneReview)
		return s.startReview(h), true
	case "c":
		s.setPane(h, paneChat)
		return nil, true
	}
	vp := h.Viewport()
	var cmd tea.Cmd
	*vp, cmd = vp.Update(msg) // ↑↓ pgup pgdn scroll
	return cmd, true
}

// Click on a PR's tab bar opens that pane.
func (s *Section) Click(h section.Host, r section.Row, x, y int) (tea.Cmd, bool) {
	if r.Kind != rowPR || y != 2 {
		return nil, false
	}
	if i := section.TabAt(paneNames, x); i >= 0 {
		s.setPane(h, i)
		h.Refresh()
	}
	return nil, true
}

// Hint is the line under a listed PR's pane.
func (s *Section) Hint(h section.Host, _ section.Row) string {
	switch s.pane {
	case paneDiff:
		return hint("  ↑↓ pgup/pgdn scroll · c chat with Claude · v review it with Claude · tab panes · o GitHub")
	case paneReview:
		return s.reviewHint(h)
	case paneChat:
		return h.ChatBox()
	}
	return hint("  ⏎ opens a comment · c ask Claude about it · r reply · x resolve · a Claude on all feedback · D post drafted replies · v review · tab panes · o GitHub")
}

// EditBox is the review pane while you edit a comment or the summary.
func (s *Section) EditBox(h section.Host, _ section.Row) bool { return s.editing(h) }

// Focus is the chat on the Chat pane, the PR's keys on the others.
func (s *Section) Focus(section.Host, section.Row) string {
	if s.pane == paneChat {
		return "chat"
	}
	return "pr"
}

// PR is the selected PR, and whether its conversation is showing (section.PRPage).
func (s *Section) PR(h section.Host, _ section.Row) (prview.Ref, bool) {
	it := s.selected(h)
	if it == nil {
		return prview.Ref{}, false
	}
	return prview.ListedRef(it.URL), s.pane == paneConversation
}

// PRAgent is the selected PR's chat, made with create (section.PRPage).
func (s *Section) PRAgent(h section.Host, _ section.Row, create bool) *tasks.Task {
	it := s.selected(h)
	switch {
	case it == nil:
		return nil
	case create:
		return s.ensureChat(h, *it)
	}
	return s.chat(h, it.URL)
}

// ShowPRChat opens the Chat pane (section.PRPage).
func (s *Section) ShowPRChat(h section.Host, _ section.Row) { s.setPane(h, paneChat) }
