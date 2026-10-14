package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/agent"
	"crew/internal/gh"
	"crew/internal/repo"
)

// Reviewing a listed PR with Claude: one headless Claude run with the crew:review skill reads the
// diff (and, when the repo is on this machine, a read-only checkout of the PR for context) and
// answers in a fixed shape: a summary, risks, a verdict and comments on lines of the diff. You keep
// or drop each comment, edit any of it, pick approve, comment or request changes, and post it as
// one GitHub review from you. Nothing is posted until you press ⏎ twice.

type revComment struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
	keep     bool
	placed   bool // on a line the diff shows; else it goes in the review's text
}

type reviewState struct {
	running  bool
	started  time.Time
	err      string
	local    bool // Claude read a checkout of the PR, not just its diff
	Summary  string
	Risks    []string
	Verdict  string
	Comments []revComment
	sel      int    // selected comment
	selTop   int    // the content line it starts on, to keep it in view
	event    string // APPROVE, COMMENT or REQUEST_CHANGES
	armed    bool   // ⏎ was pressed once
	posted   bool
	edit     int // the comment being edited, -2 the summary, -1 none
}

type (
	reviewMsg struct {
		url string
		rs  *reviewState
		err error
	}
	reviewPostedMsg struct {
		url string
		err error
	}
)

const reviewSchema = `{"type":"object","properties":{"summary":{"type":"string"},"risks":{"type":"array","items":{"type":"string"}},` +
	`"verdict":{"type":"string","enum":["approve","comment","request_changes"]},"comments":{"type":"array","items":{"type":"object",` +
	`"properties":{"path":{"type":"string"},"line":{"type":"integer"},"severity":{"type":"string","enum":["must-fix","should-fix","nit"]},` +
	`"body":{"type":"string"}},"required":["path","line","body"]}}},"required":["summary","verdict","comments"]}`

func (m Model) review() *reviewState {
	if it := m.selPR(); it != nil {
		return m.reviews[it.URL]
	}
	return nil
}

// startReview has Claude review the selected PR in the background.
func (m *Model) startReview() tea.Cmd {
	it := m.selPR()
	if it == nil {
		return nil
	}
	if rs := m.reviews[it.URL]; rs != nil && rs.running {
		return nil
	}
	m.reviews[it.URL] = &reviewState{running: true, started: time.Now(), edit: -1}
	m.renderPRRow()
	item, repos, plugin := *it, m.cfg.Repos, ""
	if m.pluginOK() {
		plugin = m.cfg.Plugin
	}
	return tea.Batch(func() tea.Msg {
		rs, err := runReview(item, repos, plugin)
		return reviewMsg{item.URL, rs, err}
	}, m.startSpin())
}

// runReview is the Claude run: in a checkout of the PR when the repo is here, else beside its diff.
func runReview(it gh.Item, repos repo.Store, plugin string) (*reviewState, error) {
	diff, err := gh.Diff(it.URL)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "crew-review-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	diffPath := filepath.Join(tmp, "pr.diff")
	if err := os.WriteFile(diffPath, []byte(diff), 0o644); err != nil {
		return nil, err
	}
	rs := &reviewState{edit: -1}
	dir := tmp
	if local := repos.Find(it.Repo); local != "" {
		if wt, remove, err := repo.ReviewWorktree(local, it.Number); err == nil {
			defer remove() // a read-only checkout, gone once Claude's done
			dir, rs.local = wt, true
		}
	}
	prompt := fmt.Sprintf("PR %s (%s #%d %q by @%s). Diff %s.", it.URL, it.Repo, it.Number, it.Title, it.Author, diffPath)
	if !rs.local {
		prompt += " There's no checkout: review from the diff."
	}
	args := []string{"-p", "--output-format", "json", "--json-schema", reviewSchema, "--add-dir", tmp}
	if plugin != "" {
		prompt = "/crew:review " + prompt
		args = append(args, "--plugin-dir", plugin)
	} else {
		prompt = "Review this pull request: correctness, security, design, tests; comments on exact new lines of the diff, " +
			"each marked must-fix, should-fix or nit; a short summary, risks and a verdict. Change nothing. " + prompt
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	var res struct {
		Result     string          `json:"result"`
		Structured json.RawMessage `json:"structured_output"`
		IsError    bool            `json:"is_error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	raw := res.Structured
	if len(raw) == 0 || string(raw) == "null" { // the answer as text: take its JSON object
		i, j := strings.Index(res.Result, "{"), strings.LastIndex(res.Result, "}")
		if i < 0 || j < i {
			return nil, fmt.Errorf("Claude didn't answer in the review's shape: %s", firstLine(res.Result))
		}
		raw = json.RawMessage(res.Result[i : j+1])
	}
	if err := json.Unmarshal(raw, rs); err != nil {
		return nil, err
	}
	lines := diffLines(diff)
	for i := range rs.Comments {
		c := &rs.Comments[i]
		c.keep, c.placed = true, lines[c.Path][c.Line]
	}
	rs.event = map[string]string{"approve": "APPROVE", "request_changes": "REQUEST_CHANGES"}[rs.Verdict]
	if rs.event == "" || it.Mine {
		rs.event = "COMMENT" // GitHub doesn't let you approve or block your own PR
	}
	return rs, nil
}

var hunkHead = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)`)

// diffLines is the new-code lines a diff shows, by file: the lines a review comment can go on.
func diffLines(diff string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	var file string
	n := 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			file = strings.TrimPrefix(l, "+++ b/")
			out[file] = map[int]bool{}
		case strings.HasPrefix(l, "+++ "), strings.HasPrefix(l, "---"):
			file = ""
		case hunkHead.MatchString(l):
			n, _ = strconv.Atoi(hunkHead.FindStringSubmatch(l)[1])
		case file == "":
		case strings.HasPrefix(l, "+"), strings.HasPrefix(l, " "):
			out[file][n] = true
			n++
		}
	}
	return out
}

func (m *Model) onReview(msg reviewMsg) {
	rs := msg.rs
	if msg.err != nil {
		rs = &reviewState{err: msg.err.Error(), edit: -1}
		if agent.Claude.AuthError(rs.err) {
			m.authFailed = agent.Claude.Name // the help line offers ⌘L
			rs.err = "Claude is signed out · ⌘L signs in, then review again\n\n" + rs.err
		}
	}
	m.reviews[msg.url] = rs
	if it := m.selPR(); it != nil && it.URL == msg.url && m.prPane == paneReview {
		m.vp.GotoTop()
		m.renderPRRow()
	}
	if it := m.selPR(); it == nil || it.URL != msg.url || m.blurred {
		notify("Review ready", fmt.Sprintf("Claude reviewed %s", msg.url))
	}
}

var (
	sevStyle = map[string]lipgloss.Style{
		"must-fix": warn, "should-fix": yellow, "nit": dim,
	}
	eventNames = []struct{ key, event, label string }{
		{"a", "APPROVE", "approve"}, {"c", "COMMENT", "comment"}, {"r", "REQUEST_CHANGES", "request changes"},
	}
)

// reviewView draws the review pane: Claude's review to go over, keep, edit and post.
func (m Model) reviewView(it gh.Item) string {
	rs := m.reviews[it.URL]
	w := m.vp.Width
	switch {
	case rs == nil:
		return dim.Render(wrap("Press v to have Claude review this PR: a summary, the risks, and comments on lines of the diff. "+
			"If the repo is on this machine, Claude reads a checkout of the PR for context; otherwise the diff. "+
			"You go over it here, and nothing is posted until you choose to.", w))
	case rs.running:
		where := "from its diff"
		if r := m.cfg.Repos.Find(it.Repo); r != "" {
			where = "with a checkout of " + shortRepo(it.Repo)
		}
		return sel.Render(spinFrames[m.spin%len(spinFrames)]+" Claude is reviewing it") +
			dim.Render(fmt.Sprintf(" %s (%ds)…", where, int(time.Since(rs.started).Seconds())))
	case rs.err != "":
		return warn.Render(wrap("The review failed: "+rs.err, w)) + "\n\n" + dim.Render("v tries again")
	}
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	verdict := map[string]string{"approve": ready.Render("approve"), "request_changes": warn.Render("request changes"), "comment": "comment"}[rs.Verdict]
	add(bold.Render("Claude's review")+dim.Render("  verdict: ")+verdict, "")
	for _, l := range strings.Split(wrap(rs.Summary, w), "\n") {
		add(l)
	}
	if len(rs.Risks) > 0 {
		add("", bold.Render("Risks"))
		for _, r := range rs.Risks {
			add(bulleted(dim.Render("•"), wrap(r, w-2), ""))
		}
	}
	kept := 0
	for _, c := range rs.Comments {
		if c.keep {
			kept++
		}
	}
	add("", bold.Render(fmt.Sprintf("Comments  %d of %d kept", kept, len(rs.Comments))))
	if len(rs.Comments) == 0 {
		add(dim.Render("  none"))
	}
	for i, c := range rs.Comments {
		box := "[ ]"
		if c.keep {
			box = "[" + ready.Render("✓") + "]"
		}
		where := sel.Render(fmt.Sprintf("%s:%d", c.Path, c.Line))
		if !c.placed {
			where += dim.Render("  not on a changed line: goes in the review's text")
		}
		head := box + " " + where + "  " + sevStyle[c.Severity].Render(c.Severity)
		if i == rs.sel {
			rs.selTop = len(lines)
			add(focusBar.Width(w).Render("▶ " + ansi.Strip(head)))
		} else {
			add("  " + head)
		}
		body := wrap(c.Body, w-6)
		if !c.keep {
			body = dim.Render(body)
		}
		for _, l := range strings.Split(body, "\n") {
			add("      " + l)
		}
	}
	add("")
	if rs.posted {
		add(ready.Render("✓ Posted to GitHub"))
		return strings.Join(lines, "\n")
	}
	var opts []string
	for _, e := range eventNames {
		label := e.key + " " + e.label
		if e.event == rs.event {
			label = focusBar.Render(" " + label + " ")
		} else {
			label = dim.Render(" " + label + " ")
		}
		opts = append(opts, label)
	}
	add("Post as  " + strings.Join(opts, " "))
	if rs.armed {
		add("", sel.Render("⏎ again posts this review to GitHub as you · any other key cancels"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) reviewHint() string {
	rs := m.review()
	switch {
	case rs != nil && rs.edit != -1:
		what := "the summary"
		if rs.edit >= 0 {
			what = "the comment"
		}
		return sel.Render("  ↳ Editing "+what) + dim.Render(" · ⏎ saves · alt+⏎ new line · esc cancels") + "\n" + m.input.View()
	case rs == nil || rs.running || rs.err != "":
		return hint("  v review it with Claude · tab panes · o GitHub")
	case rs.posted:
		return hint("  posted · v reviews again · tab panes · o GitHub")
	}
	return hint("  ↑↓ pick a comment · space keep/drop · e edit it · E edit the summary · a/c/r post as · ⏎ post · v again")
}

func (m Model) revEditing() bool {
	rs := m.review()
	return rs != nil && rs.edit != -1
}

// updateReview handles keys on the review pane.
func (m Model) updateReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	it, rs := m.selPR(), m.review()
	k := msg.String()
	if k == "v" && (rs == nil || !rs.running) {
		return m, m.startReview()
	}
	if rs == nil || rs.running || rs.err != "" || rs.posted {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	if k != "enter" {
		rs.armed = false
	}
	switch k {
	case "up":
		rs.sel = max(rs.sel-1, 0)
	case "down":
		rs.sel = min(rs.sel+1, max(len(rs.Comments)-1, 0))
	case " ":
		if rs.sel < len(rs.Comments) {
			rs.Comments[rs.sel].keep = !rs.Comments[rs.sel].keep
		}
	case "e", "E":
		rs.edit = -2
		text := rs.Summary
		if k == "e" && rs.sel < len(rs.Comments) {
			rs.edit, text = rs.sel, rs.Comments[rs.sel].Body
		}
		m.input.Reset()
		m.input.SetValue(text)
		m.input.Focus()
		m.layout()
	case "a", "c", "r":
		for _, e := range eventNames {
			if e.key == k {
				rs.event = e.event
			}
		}
	case "enter":
		if !rs.armed {
			rs.armed = true
			break
		}
		rs.armed = false
		m.notice = "Posting your review…"
		return m, m.postReview(*it, rs)
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg) // pgup/pgdn
		return m, cmd
	}
	m.renderPRRow()
	m.keepReviewSelInView(rs)
	return m, nil
}

// keepReviewSelInView scrolls the review so the selected comment is on screen.
func (m *Model) keepReviewSelInView(rs *reviewState) {
	switch {
	case rs.selTop < m.vp.YOffset:
		m.vp.SetYOffset(rs.selTop)
	case rs.selTop >= m.vp.YOffset+m.vp.Height-3:
		m.vp.SetYOffset(rs.selTop - m.vp.Height + 4)
	}
}

// updateReviewEdit handles keys while editing a comment or the summary.
func (m Model) updateReviewEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rs := m.review()
	switch msg.String() {
	case "esc":
		rs.edit = -1
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if rs.edit >= 0 && rs.edit < len(rs.Comments) {
			rs.Comments[rs.edit].Body = text
		} else {
			rs.Summary = text
		}
		rs.edit = -1
	default:
		m.input.Focus()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.layout()
		return m, cmd
	}
	m.input.Reset()
	m.input.Blur()
	m.layout()
	m.renderPRRow()
	return m, nil
}

// postReview posts the review as you: the kept comments on their lines, the summary, the risks and
// any kept comment that isn't on a changed line in its text.
func (m *Model) postReview(it gh.Item, rs *reviewState) tea.Cmd {
	body := rs.Summary
	if len(rs.Risks) > 0 {
		body += "\n\n**Risks**\n- " + strings.Join(rs.Risks, "\n- ")
	}
	var comments []gh.ReviewComment
	var also []string
	for _, c := range rs.Comments {
		switch {
		case !c.keep:
		case c.placed:
			comments = append(comments, gh.ReviewComment{Path: c.Path, Line: c.Line, Body: c.Body})
		default:
			also = append(also, fmt.Sprintf("`%s:%d` %s", c.Path, c.Line, c.Body))
		}
	}
	if len(also) > 0 {
		body += "\n\n**Also**\n- " + strings.Join(also, "\n- ")
	}
	url, event := it.URL, rs.event
	return func() tea.Msg { return reviewPostedMsg{url, gh.PostReview(url, event, body, comments)} }
}

func (m *Model) onReviewPosted(msg reviewPostedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = "GitHub said: " + msg.err.Error()
		return nil
	}
	m.notice = "Review posted"
	if rs := m.reviews[msg.url]; rs != nil {
		rs.posted = true
	}
	m.renderPRRow()
	return tea.Batch(m.loadPRRef(prRef{key: "pr:" + msg.url, url: msg.url}, true), loadPRList())
}
