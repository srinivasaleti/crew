package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/gh"
	"crew/internal/table"
)

// PULL REQUESTS: a sidebar section next to TASKS with the open PRs where your review is requested
// from you, and your own open PRs, in any repo your GitHub account can see (gh search; no tokens).
// A PR opens on three panes: its conversation (the same view as a task's PR tab), its diff, and a
// review by Claude that you edit and post (review.go).

const (
	paneConversation = iota
	paneChat
	paneDiff
	paneReview
)

var paneNames = []string{"Conversation", "Chat", "Diff", "Review"}

type (
	prListMsg struct {
		items []gh.Item
		err   error
	}
	prDiffMsg struct {
		url, diff string
		err       error
	}
)

// diffState is a listed PR's diff, read once.
type diffState struct {
	text    string
	err     string
	loading bool
}

// loadPRList reads both lists in the background: PRs waiting for your review, then yours.
func loadPRList() tea.Cmd {
	if !gh.Installed() {
		return nil
	}
	return func() tea.Msg {
		review, err1 := gh.ToReview()
		mine, err2 := gh.Mine()
		var items []gh.Item
		seen := map[string]bool{}
		for _, it := range append(review, mine...) {
			if !seen[it.URL] {
				seen[it.URL] = true
				items = append(items, it)
			}
		}
		// Waiting for you first, then yours; the most recently updated first in each.
		slices.SortStableFunc(items, func(a, b gh.Item) int {
			if a.Mine != b.Mine {
				if a.Mine {
					return 1
				}
				return -1
			}
			return b.Updated.Compare(a.Updated)
		})
		return prListMsg{items, errors.Join(err1, err2)}
	}
}

func (m *Model) onPRList(msg prListMsg) {
	m.prListErr, m.prListLoading = "", false
	if msg.err != nil {
		m.prListErr = msg.err.Error()
		if msg.items == nil {
			return // keep the last list
		}
	}
	m.prList = msg.items
	if m.current() == nil {
		m.refresh()
	}
}

// selPR is the PR selected in the sidebar, if a PR row is.
func (m Model) selPR() *gh.Item {
	if r := m.selRow(); r.kind == rowPR && r.task < len(m.prList) {
		return &m.prList[r.task]
	}
	return nil
}

// prRows are the PULL REQUESTS section's rows: its group row (there from the start, spinning while
// the list loads), then the PR you opened, if one is.
func (m Model) prRows() []row {
	if !gh.Installed() {
		return nil
	}
	rows := []row{{kind: rowGroup, group: "prs", label: "PULL REQUESTS", count: len(m.prList)}}
	if url, ok := strings.CutPrefix(m.selKey, "p:"); ok {
		if i := slices.IndexFunc(m.prList, func(it gh.Item) bool { return it.URL == url }); i >= 0 {
			rows = append(rows, row{kind: rowPR, group: "prs", depth: 1, task: i})
		}
	}
	return rows
}

// prMark is a listed PR's mark: waiting for your review, yours, or a draft.
func prMark(it gh.Item) string {
	switch {
	case it.Draft:
		return dim.Render("◌")
	case it.Mine:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("⇡")
	}
	return sel.Render("●")
}

// prLines draws a PR's two sidebar lines: its title, then repo · #n · who and how old.
func (m Model) prLines(r row, selected bool) string {
	it := m.prList[r.task]
	w := m.sidebarWidth()
	indent := strings.Repeat("  ", r.depth)
	title := ansi.Truncate(it.Title, w-5-len(indent), "…")
	who := "review"
	if it.Mine {
		who = "yours"
	} else {
		who = "@" + it.Author
	}
	second := ansi.Truncate(fmt.Sprintf("%s #%d · %s · %s", shortRepo(it.Repo), it.Number, who, ago(it.Updated)), w-5-len(indent), "…")
	pad := "    " + indent
	switch {
	case selected && m.listFocus:
		bar := focusBar.Width(w)
		return bar.Render("▶ "+indent+ansi.Strip(prMark(it))+" "+title) + "\n" + bar.UnsetBold().Render(pad+second)
	case selected:
		return sel.Render("▶ ") + indent + prMark(it) + " " + bold.Render(title) + "\n" + pad + dim.Render(second)
	}
	return "  " + indent + prMark(it) + " " + title + "\n" + pad + dim.Render(second)
}

// shortRepo is a repo's name without its owner.
func shortRepo(nameWithOwner string) string {
	_, name, ok := strings.Cut(nameWithOwner, "/")
	if !ok {
		return nameWithOwner
	}
	return name
}

// prTable is the PULL REQUESTS overview: every listed PR, filtered by chip, repo or search.
func (m Model) prTable(r row) table.Table {
	t := table.Table{
		Empty: "No open PRs here.", Facet: "repo",
		Cols:  []table.Col{{Name: "pull request", Width: 0}, {Name: "repo", Width: 20}, {Name: "author", Width: 14}, {Name: "list", Width: 10}, {Name: "opened", Width: 9}, {Name: "updated", Width: 9}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "review", Label: "To review"}, {ID: "mine", Label: "Mine"}, {ID: "draft", Label: "Drafts"}},
		Keys:  map[string]string{"v": "review", "d": "diff", "c": "chat"},
		Acts:  []table.Act{{Label: "Open", Key: "enter"}, {Label: "Chat", Key: "c"}, {Label: "Diff", Key: "d"}, {Label: "Review", Key: "v"}},
		Hint:  "⏎ opens · c chat with Claude about it · d its diff · v review it with Claude · tab filter · / search",
	}
	if m.prListLoading && len(m.prList) == 0 {
		t.Empty = "Loading pull requests…"
	}
	review := 0
	for _, it := range m.prList {
		tags, list := []string{"all"}, "review"
		if it.Mine {
			tags, list = append(tags, "mine"), "yours"
		} else {
			tags = append(tags, "review")
			review++
		}
		if it.Draft {
			tags = append(tags, "draft")
		}
		t.Rows = append(t.Rows, table.Row{
			ID: it.URL,
			Cells: []string{prMark(it) + " " + it.Title + dim.Render(fmt.Sprintf("  #%d", it.Number)), shortRepo(it.Repo), "@" + it.Author,
				list, dim.Render(ago(it.Created)), dim.Render(ago(it.Updated))},
			Tags: tags, Facet: shortRepo(it.Repo),
			Search: fmt.Sprintf("%s %s %s %d", it.Title, it.Repo, it.Author, it.Number),
		})
	}
	t.Summary = fmt.Sprintf("%d waiting for your review · %d of yours", review, len(m.prList)-review)
	if m.prListErr != "" {
		t.Summary += " · " + warn.Render("couldn't refresh: "+firstLine(m.prListErr))
	}
	return t
}

// openListedPR opens a PR from the overview, on its conversation, chat ("chat"), diff ("diff") or
// review ("review").
func openListedPR(m *Model, url, action string) tea.Cmd {
	m.selKey, m.prPane = "p:"+url, paneConversation
	m.onSelect()
	m.focusOn(areaMain, -1)
	m.setPRPane(map[string]int{"chat": paneChat, "diff": paneDiff, "review": paneReview}[action])
	if action == "review" {
		return m.startReview()
	}
	return nil
}

// prHeader is the main pane's first line for a listed PR.
func (m Model) prHeader(it gh.Item, w int) string {
	meta := fmt.Sprintf(" · %s #%d · @%s", it.Repo, it.Number, it.Author)
	if it.Draft {
		meta += " · draft"
	}
	return ansi.Truncate(bold.Render(it.Title)+dim.Render(meta)+m.prChatMeta(it.URL), w, "…")
}

// prPaneTabs is the tab bar for a listed PR: its panes.
func (m Model) prPaneTabs(w int) string {
	active := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	var bar []string
	for i, n := range paneNames {
		if i == m.prPane {
			bar = append(bar, active.Render(tabLabel(n, i)))
		} else {
			bar = append(bar, dim.Padding(0, 1).Render(tabLabel(n, i)))
		}
	}
	return ansi.Truncate(strings.Join(bar, " ")+dim.Render("   tab switches · o opens it on GitHub"), w, "…")
}

// renderPRRow fills the main pane for a listed PR's open pane.
func (m *Model) renderPRRow() {
	it := m.selPR()
	switch m.prPane {
	case paneConversation:
		m.renderPR()
	case paneDiff:
		d := m.diffs[it.URL]
		switch {
		case d == nil || d.loading:
			m.vp.SetContent(dim.Render("Reading the diff…"))
		case d.err != "":
			m.vp.SetContent(warn.Render(d.err))
		default:
			m.vp.SetContent(colorDiff(d.text, m.vp.Width))
		}
	case paneReview:
		m.vp.SetContent(m.reviewView(*it))
	case paneChat:
		follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
		m.vp.SetContent(m.prChatView(*it))
		if follow { // follows new output unless you've scrolled up to read
			m.vp.GotoBottom()
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
			out = append(out, "", sel.Render("━━ "+b))
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "index "),
			strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"):
		case strings.HasPrefix(l, "@@"):
			out = append(out, dim.Render(l))
		case strings.HasPrefix(l, "+"):
			out = append(out, green.Render(ansi.Truncate(l, w, "…")))
		case strings.HasPrefix(l, "-"):
			out = append(out, warn.Render(ansi.Truncate(l, w, "…")))
		default:
			out = append(out, ansi.Truncate(l, w, "…"))
		}
	}
	return strings.Join(out, "\n")
}

// autoLoad starts what the screen is waiting for: the selected listed PR and its diff. It runs
// after every Update, so a PR loads however it got selected.
func (m *Model) autoLoad() tea.Cmd {
	it := m.selPR()
	if it == nil {
		return nil
	}
	var cmds []tea.Cmd
	if st := m.prOf("pr:" + it.URL); st.pr == nil && !st.loading && st.err == "" {
		cmds = append(cmds, m.loadPRRef(prRef{key: "pr:" + it.URL, url: it.URL}, true))
	}
	if d := m.diffs[it.URL]; m.prPane == paneDiff && d == nil {
		m.diffs[it.URL] = &diffState{loading: true}
		u := it.URL
		cmds = append(cmds, func() tea.Msg {
			diff, err := gh.Diff(u)
			return prDiffMsg{u, diff, err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) onPRDiff(msg prDiffMsg) {
	d := &diffState{text: msg.diff}
	if msg.err != nil {
		d.err = msg.err.Error()
	}
	m.diffs[msg.url] = d
	if it := m.selPR(); it != nil && it.URL == msg.url && m.prPane == paneDiff {
		m.renderPRRow()
	}
}

// updatePRRow handles keys on a listed PR: tab/shift+tab switch panes; each pane has its own keys.
func (m Model) updatePRRow(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	it := m.selPR()
	k := msg.String()
	if m.prPane == paneReview && m.revEditing() { // typing in the review's edit box
		return m.updateReviewEdit(msg)
	}
	switching := k == "tab" || k == "shift+tab"
	if m.prPane == paneChat && (!switching || m.input.Value() != "" || len(m.sugg) > 0) {
		return m.updateChat(msg) // the message box, as in a task's chat
	}
	if switching {
		d := map[string]int{"shift+tab": -1, "tab": 1}[k]
		m.setPRPane((m.prPane + d + len(paneNames)) % len(paneNames))
		return m, nil
	}
	switch m.prPane {
	case paneConversation: // the PR view's own keys first (its rows, popups, the failed log)
		if k == "v" {
			m.setPRPane(paneReview)
			return m, m.startReview()
		}
		if m, cmd, ok := m.updatePR(msg); ok {
			return m, cmd
		}
	case paneReview:
		if k != "esc" && k != "o" {
			return m.updateReview(msg)
		}
	}
	switch k {
	case "o":
		openURL(it.URL)
		return m, nil
	case "esc":
		m.goUp()
		return m, nil
	case "v":
		m.setPRPane(paneReview)
		return m, m.startReview()
	case "c":
		m.setPRPane(paneChat)
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // ↑↓ pgup pgdn scroll
	return m, cmd
}

// prRowHint is the line under a listed PR's pane.
func (m Model) prRowHint() string {
	switch m.prPane {
	case paneDiff:
		return hint("  ↑↓ pgup/pgdn scroll · c chat with Claude · v review it with Claude · tab panes · o GitHub")
	case paneReview:
		return m.reviewHint()
	case paneChat:
		return m.suggestionsView() + m.input.View()
	}
	return hint("  ⏎ opens a comment · c ask Claude about it · r reply · x resolve · a Claude on all feedback · v review · tab panes · o GitHub")
}
