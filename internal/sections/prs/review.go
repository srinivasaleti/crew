package prs

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

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/agent"
	"crew/internal/gh"
	"crew/internal/notify"
	"crew/internal/repo"
	"crew/internal/section"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
	"crew/internal/widgets/prview"
)

// Reviewing a listed PR with Claude: one headless Claude run with the crew:review skill reads the
// diff (and, when the repo is on this machine, a read-only checkout of the PR for context) and
// answers in a fixed shape: a summary, risks, a verdict and comments on lines of the diff. You keep
// or drop each comment, edit any of it, pick approve, comment or request changes, and post it as
// one GitHub review from you. Nothing is posted until you press ⏎ twice.

// revComment is one of Claude's comments on a line of the diff, and whether you keep it.
type revComment struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
	keep     bool
	placed   bool // on a line the diff shows; else it goes in the review's text
}

// reviewState is Claude's review of a PR, and what you've done with it.
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
	// reviewMsg is Claude's review, back.
	reviewMsg struct {
		url string
		rs  *reviewState
		err error
	}
	// reviewPostedMsg is the review, posted to GitHub (or why not).
	reviewPostedMsg struct {
		url string
		err error
	}
)

// reviewSchema is the shape Claude answers a review in.
const reviewSchema = `{"type":"object","properties":{"summary":{"type":"string"},"risks":{"type":"array","items":{"type":"string"}},` +
	`"verdict":{"type":"string","enum":["approve","comment","request_changes"]},"comments":{"type":"array","items":{"type":"object",` +
	`"properties":{"path":{"type":"string"},"line":{"type":"integer"},"severity":{"type":"string","enum":["must-fix","should-fix","nit"]},` +
	`"body":{"type":"string"}},"required":["path","line","body"]}}},"required":["summary","verdict","comments"]}`

// review is the selected PR's review, or nil.
func (s *Section) review(h section.Host) *reviewState {
	if it := s.selected(h); it != nil {
		return s.reviews[it.URL]
	}
	return nil
}

// startReview has Claude review the selected PR in the background.
func (s *Section) startReview(h section.Host) tea.Cmd {
	it := s.selected(h)
	if it == nil {
		return nil
	}
	if rs := s.reviews[it.URL]; rs != nil && rs.running {
		return nil
	}
	s.reviews[it.URL] = &reviewState{running: true, started: time.Now(), edit: -1}
	s.render(h)
	item, repos, plugin := *it, s.repos, h.Plugin()
	return tea.Batch(func() tea.Msg {
		rs, err := runReview(item, repos, plugin)
		return reviewMsg{item.URL, rs, err}
	}, h.Spin())
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
			return nil, fmt.Errorf("Claude didn't answer in the review's shape: %s", markdown.FirstLine(res.Result))
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

// hunkHead is a diff's hunk header, with the new side's first line.
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

// onReview takes Claude's review, and tells you it's ready when you're elsewhere.
func (s *Section) onReview(h section.Host, msg reviewMsg) {
	rs := msg.rs
	if msg.err != nil {
		rs = &reviewState{err: msg.err.Error(), edit: -1}
		if agent.Claude.AuthError(rs.err) {
			h.SignedOut(agent.Claude.Name) // the help line offers ⌘L
			rs.err = "Claude is signed out · ⌘L signs in, then review again\n\n" + rs.err
		}
	}
	s.reviews[msg.url] = rs
	if it := s.selected(h); it != nil && it.URL == msg.url && s.pane == paneReview {
		h.Viewport().GotoTop()
		s.render(h)
	}
	if it := s.selected(h); it == nil || it.URL != msg.url || h.Blurred() {
		notify.Send("Review ready", fmt.Sprintf("Claude reviewed %s", msg.url))
	}
}

var (
	sevStyle = map[string]lipgloss.Style{
		"must-fix": look.Warn, "should-fix": lipgloss.NewStyle().Foreground(lipgloss.Color("11")), "nit": look.Dim,
	}
	eventNames = []struct{ key, event, label string }{
		{"a", "APPROVE", "approve"}, {"c", "COMMENT", "comment"}, {"r", "REQUEST_CHANGES", "request changes"},
	}
)

// reviewView draws the review pane: Claude's review to go over, keep, edit and post.
func (s *Section) reviewView(h section.Host, it gh.Item) string {
	rs := s.reviews[it.URL]
	w := h.Viewport().Width
	switch {
	case rs == nil:
		return look.Dim.Render(look.Wrap("Press v to have Claude review this PR: a summary, the risks, and comments on lines of the diff. "+
			"If the repo is on this machine, Claude reads a checkout of the PR for context; otherwise the diff. "+
			"You go over it here, and nothing is posted until you choose to.", w))
	case rs.running:
		where := "from its diff"
		if r := s.repos.Find(it.Repo); r != "" {
			where = "with a checkout of " + shortRepo(it.Repo)
		}
		return look.Sel.Render(look.SpinFrames[h.SpinFrame()%len(look.SpinFrames)]+" Claude is reviewing it") +
			look.Dim.Render(fmt.Sprintf(" %s (%ds)…", where, int(time.Since(rs.started).Seconds())))
	case rs.err != "":
		return look.Warn.Render(look.Wrap("The review failed: "+rs.err, w)) + "\n\n" + look.Dim.Render("v tries again")
	}
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	verdict := map[string]string{"approve": look.Ready.Render("approve"), "request_changes": look.Warn.Render("request changes"), "comment": "comment"}[rs.Verdict]
	add(look.Bold.Render("Claude's review")+look.Dim.Render("  verdict: ")+verdict, "")
	for _, l := range strings.Split(look.Wrap(rs.Summary, w), "\n") {
		add(l)
	}
	if len(rs.Risks) > 0 {
		add("", look.Bold.Render("Risks"))
		for _, r := range rs.Risks {
			add(chat.Bulleted(look.Dim.Render("•"), look.Wrap(r, w-2), ""))
		}
	}
	kept := 0
	for _, c := range rs.Comments {
		if c.keep {
			kept++
		}
	}
	add("", look.Bold.Render(fmt.Sprintf("Comments  %d of %d kept", kept, len(rs.Comments))))
	if len(rs.Comments) == 0 {
		add(look.Dim.Render("  none"))
	}
	for i, c := range rs.Comments {
		box := "[ ]"
		if c.keep {
			box = "[" + look.Ready.Render("✓") + "]"
		}
		where := look.Sel.Render(fmt.Sprintf("%s:%d", c.Path, c.Line))
		if !c.placed {
			where += look.Dim.Render("  not on a changed line: goes in the review's text")
		}
		head := box + " " + where + "  " + sevStyle[c.Severity].Render(c.Severity)
		if i == rs.sel {
			rs.selTop = len(lines)
			add(look.FocusBar.Width(w).Render("▶ " + ansi.Strip(head)))
		} else {
			add("  " + head)
		}
		body := look.Wrap(c.Body, w-6)
		if !c.keep {
			body = look.Dim.Render(body)
		}
		for _, l := range strings.Split(body, "\n") {
			add("      " + l)
		}
	}
	add("")
	if rs.posted {
		add(look.Ready.Render("✓ Posted to GitHub"))
		return strings.Join(lines, "\n")
	}
	var opts []string
	for _, e := range eventNames {
		label := e.key + " " + e.label
		if e.event == rs.event {
			label = look.FocusBar.Render(" " + label + " ")
		} else {
			label = look.Dim.Render(" " + label + " ")
		}
		opts = append(opts, label)
	}
	add("Post as  " + strings.Join(opts, " "))
	if rs.armed {
		add("", look.Sel.Render("⏎ again posts this review to GitHub as you · any other key cancels"))
	}
	return strings.Join(lines, "\n")
}

// reviewHint is the line under the review pane, or the edit box while you edit.
func (s *Section) reviewHint(h section.Host) string {
	rs := s.review(h)
	switch {
	case rs != nil && rs.edit != -1:
		what := "the summary"
		if rs.edit >= 0 {
			what = "the comment"
		}
		return look.Sel.Render("  ↳ Editing "+what) + look.Dim.Render(" · ⏎ saves · alt+⏎ new line · esc cancels") + "\n" + h.Input().View()
	case rs == nil || rs.running || rs.err != "":
		return hint("  v review it with Claude · tab panes · o GitHub")
	case rs.posted:
		return hint("  posted · v reviews again · tab panes · o GitHub")
	}
	return hint("  ↑↓ pick a comment · space keep/drop · e edit it · E edit the summary · a/c/r post as · ⏎ post · v again")
}

// editing reports whether you're editing a comment or the summary.
func (s *Section) editing(h section.Host) bool {
	rs := s.review(h)
	return rs != nil && rs.edit != -1
}

// reviewKey handles keys on the review pane.
func (s *Section) reviewKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	it, rs, vp, in := s.selected(h), s.review(h), h.Viewport(), h.Input()
	k := msg.String()
	if k == "v" && (rs == nil || !rs.running) {
		return s.startReview(h)
	}
	if rs == nil || rs.running || rs.err != "" || rs.posted {
		var cmd tea.Cmd
		*vp, cmd = vp.Update(msg)
		return cmd
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
		in.Reset()
		in.SetValue(text)
		in.Focus()
		h.Layout()
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
		h.Notice("Posting your review…")
		return postReview(*it, rs)
	default:
		var cmd tea.Cmd
		*vp, cmd = vp.Update(msg) // pgup/pgdn
		return cmd
	}
	s.render(h)
	keepInView(vp, rs)
	return nil
}

// keepInView scrolls the review so the selected comment is on screen.
func keepInView(vp *viewport.Model, rs *reviewState) {
	switch {
	case rs.selTop < vp.YOffset:
		vp.SetYOffset(rs.selTop)
	case rs.selTop >= vp.YOffset+vp.Height-3:
		vp.SetYOffset(rs.selTop - vp.Height + 4)
	}
}

// reviewEditKey handles keys while editing a comment or the summary.
func (s *Section) reviewEditKey(h section.Host, msg tea.KeyMsg) tea.Cmd {
	rs, in := s.review(h), h.Input()
	switch msg.String() {
	case "esc":
		rs.edit = -1
	case "enter":
		text := strings.TrimSpace(in.Value())
		if rs.edit >= 0 && rs.edit < len(rs.Comments) {
			rs.Comments[rs.edit].Body = text
		} else {
			rs.Summary = text
		}
		rs.edit = -1
	default:
		in.Focus()
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		h.Layout()
		return cmd
	}
	in.Reset()
	in.Blur()
	h.Layout()
	s.render(h)
	return nil
}

// postReview posts the review as you: the kept comments on their lines, the summary, the risks and
// any kept comment that isn't on a changed line in its text.
func postReview(it gh.Item, rs *reviewState) tea.Cmd {
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

// onReviewPosted marks the review posted and reads the PR and the list again.
func (s *Section) onReviewPosted(h section.Host, msg reviewPostedMsg) tea.Cmd {
	if msg.err != nil {
		h.Notice("GitHub said: " + msg.err.Error())
		return nil
	}
	h.Notice("Review posted")
	if rs := s.reviews[msg.url]; rs != nil {
		rs.posted = true
	}
	s.render(h)
	v, _ := h.PR()
	return tea.Batch(v.Load(prview.ListedRef(msg.url), true), loadList(s.repos))
}
