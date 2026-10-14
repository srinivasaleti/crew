package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/look"
)

// Card is an issue opened from the table, like a card on Jira's board: all its details, the
// description and the comments, and a box to write a comment (yourself, or drafted by Claude) that
// goes to Jira on ⏎. crew shows it as a popup.
type Card struct {
	Issue    Issue
	detail   detail
	comments []comment
	loading  bool
	err      string
	scroll   int
	box      textarea.Model
	writing  bool   // the comment box is open
	note     string // one line of feedback: "Posting…", "Claude is drafting…"
	closed   bool
}

type detail struct {
	Assignee, Reporter, Parent string
	Labels, Components         []string
	Created, Updated           time.Time
}

type comment struct{ Author, Body string }

// CardMsg is an issue's details and comments coming back.
type CardMsg struct {
	key      string
	detail   detail
	comments []comment
	err      error
}

// PostedMsg is a comment posted (or not).
type PostedMsg struct {
	key string
	err error
}

// CardEvent is what crew should do after a key on the card.
type CardEvent struct {
	Work  bool // work on the issue in a crew task
	URL   string
	Draft bool // have Claude draft a comment (crew runs it; SetDraft puts it in the box)
}

// OpenCard opens an issue's card and reads its details and comments.
func OpenCard(ctx context.Context, i Issue) (*Card, tea.Cmd) {
	box := textarea.New()
	box.Placeholder = "Write a comment…"
	box.ShowLineNumbers = false
	box.SetHeight(4)
	c := &Card{Issue: i, loading: true, box: box}
	return c, func() tea.Msg { return readCard(ctx, i.Key) }
}

// readCard reads an issue's details and comments with acli.
func readCard(ctx context.Context, key string) CardMsg {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := acli(ctx, "jira", "workitem", "view", key, "--json",
		"--fields", "summary,assignee,reporter,created,updated,labels,components,parent")
	if err != nil {
		return CardMsg{key: key, err: err}
	}
	var v struct {
		Fields struct {
			Assignee   struct{ DisplayName string } `json:"assignee"`
			Reporter   struct{ DisplayName string } `json:"reporter"`
			Created    string                       `json:"created"`
			Updated    string                       `json:"updated"`
			Labels     []string                     `json:"labels"`
			Components []struct{ Name string }      `json:"components"`
			Parent     *struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
				} `json:"fields"`
			} `json:"parent"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return CardMsg{key: key, err: fmt.Errorf("reading acli's answer: %w", err)}
	}
	f := v.Fields
	d := detail{Assignee: f.Assignee.DisplayName, Reporter: f.Reporter.DisplayName, Labels: f.Labels,
		Created: jiraTime(f.Created), Updated: jiraTime(f.Updated)}
	for _, c := range f.Components {
		d.Components = append(d.Components, c.Name)
	}
	if f.Parent != nil {
		d.Parent = f.Parent.Key + " " + f.Parent.Fields.Summary
	}
	// ponytail: the first 100 comments; --paginate if an issue ever has more.
	out, err = acli(ctx, "jira", "workitem", "comment", "list", "--key", key, "--json", "--limit", "100")
	if err != nil {
		return CardMsg{key: key, detail: d, err: err}
	}
	// acli writes one JSON object per page of comments, one after another.
	var all []comment
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var page struct {
			Comments []comment `json:"comments"`
		}
		if err := dec.Decode(&page); err != nil {
			return CardMsg{key: key, detail: d, err: fmt.Errorf("reading acli's comments: %w", err)}
		}
		all = append(all, page.Comments...)
	}
	return CardMsg{key: key, detail: d, comments: all}
}

// PostComment posts a comment on an issue with acli.
func PostComment(ctx context.Context, key, body string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, err := acli(ctx, "jira", "workitem", "comment", "create", "--key", key, "--body", body)
		return PostedMsg{key, err}
	}
}

// acli runs Atlassian's CLI and returns its output, or its first line of stderr as the error.
func acli(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "acli", args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return nil, errors.New(firstLine(string(ee.Stderr)))
	}
	return out, err
}

func jiraTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000-0700", s)
	return t
}

// IssueKey is the card's issue, e.g. ACC-123.
func (c *Card) IssueKey() string { return c.Issue.Key }

// Closed reports whether the card was closed.
func (c *Card) Closed() bool { return c.closed }

// Done takes the card's details and comments coming back.
func (c *Card) Done(msg CardMsg) {
	if msg.key != c.Issue.Key {
		return
	}
	c.loading, c.err, c.detail, c.comments = false, "", msg.detail, msg.comments
	if msg.err != nil {
		c.err = msg.err.Error()
	}
}

// Posted takes a comment posted; on success the box closes and the card reads its comments again.
func (c *Card) Posted(ctx context.Context, msg PostedMsg) tea.Cmd {
	if msg.key != c.Issue.Key {
		return nil
	}
	if msg.err != nil {
		c.note = "Couldn't post: " + msg.err.Error() + " · your comment is still in the box"
		return nil
	}
	c.writing, c.note, c.loading = false, "Posted ✓", true
	c.box.Reset()
	key := c.Issue.Key
	return func() tea.Msg { return readCard(ctx, key) }
}

// SetDraft puts Claude's draft in the comment box, for you to edit and post.
func (c *Card) SetDraft(text string, err error) {
	if err != nil {
		c.note = "Claude couldn't draft one: " + err.Error()
		return
	}
	c.writing, c.note = true, "Claude's draft · edit it, ⏎ posts it, esc keeps it unposted"
	c.box.SetValue(strings.TrimSpace(text))
	c.box.Focus()
}

// Text is the card as plain text, for Claude to draft a comment from.
func (c *Card) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n%s · %s · %s\nAssignee: %s · Reporter: %s\n\n%s\n\nComments:\n",
		c.Issue.Key, c.Issue.Summary, c.Issue.Type, c.Issue.Status, c.Issue.Priority, c.detail.Assignee, c.detail.Reporter, c.Issue.Description)
	for _, cm := range c.comments {
		fmt.Fprintf(&b, "- %s: %s\n", cm.Author, cm.Body)
	}
	return b.String()
}

// KeyMsg handles a key; cmd reads or posts in the background.
func (c *Card) KeyMsg(ctx context.Context, msg tea.KeyMsg) (CardEvent, tea.Cmd) {
	k := msg.String()
	if c.writing {
		switch k {
		case "esc":
			c.writing, c.note = false, ""
			c.box.Blur()
			return CardEvent{}, nil
		case "enter":
			body := strings.TrimSpace(c.box.Value())
			if body == "" {
				return CardEvent{}, nil
			}
			c.note = "Posting…"
			return CardEvent{}, PostComment(ctx, c.Issue.Key, body)
		case "alt+enter", "ctrl+j":
			c.box.InsertString("\n")
			return CardEvent{}, nil
		}
		var cmd tea.Cmd
		c.box, cmd = c.box.Update(msg)
		return CardEvent{}, cmd
	}
	switch k {
	case "esc", "q":
		c.closed = true
	case "up", "k":
		c.scroll = max(c.scroll-1, 0)
	case "down", "j":
		c.scroll++
	case "pgup":
		c.scroll = max(c.scroll-10, 0)
	case "pgdown", " ":
		c.scroll += 10
	case "c":
		c.writing, c.note = true, ""
		return CardEvent{}, c.box.Focus()
	case "a":
		c.note = "Claude is drafting a comment…"
		return CardEvent{Draft: true}, nil
	case "n":
		c.closed = true
		return CardEvent{Work: true}, nil
	case "o":
		return CardEvent{URL: c.Issue.URL}, nil
	case "r":
		c.loading = true
		key := c.Issue.Key
		return CardEvent{}, func() tea.Msg { return readCard(ctx, key) }
	}
	return CardEvent{}, nil
}

// Wheel scrolls the card.
func (c *Card) Wheel(down bool) {
	if down {
		c.scroll += 3
	} else {
		c.scroll = max(c.scroll-3, 0)
	}
}

// View draws the card in a box w wide and at most h tall.
func (c *Card) View(w, h int) string {
	inner := w - 6 // the box's border and padding
	wrap := func(s string) string { return lipgloss.NewStyle().Width(inner).Render(s) }
	i, d := c.Issue, c.detail
	var body []string
	add := func(s string) { body = append(body, strings.Split(s, "\n")...) }

	add(look.Sel.Render(i.Key) + "  " + look.Bold.Render(wrap(i.Summary)))
	add(look.Dim.Render(fmt.Sprintf("%s · %s · %s", i.Type, i.Status, i.Priority)))
	field := func(name, v string) {
		if v != "" {
			add(look.Dim.Render(fmt.Sprintf("%-11s", name)) + ansi.Truncate(v, inner-11, "…"))
		}
	}
	add("")
	field("Assignee", d.Assignee)
	field("Reporter", d.Reporter)
	field("Parent", d.Parent)
	field("Labels", strings.Join(d.Labels, ", "))
	field("Components", strings.Join(d.Components, ", "))
	if !d.Created.IsZero() {
		field("Created", d.Created.Local().Format("2 Jan 2006 3:04pm")+look.Dim.Render(" · updated "+look.Ago(d.Updated)))
	}
	add("")
	add(look.Bold.Render("Description"))
	if i.Description == "" {
		add(look.Dim.Render("none"))
	} else {
		add(wrap(i.Description))
	}
	add("")
	switch {
	case c.loading:
		add(look.Dim.Render("Reading the comments…"))
	case c.err != "":
		add(look.Warn.Render("Couldn't read it all: " + c.err))
	case len(c.comments) == 0:
		add(look.Bold.Render("Comments") + look.Dim.Render("  none yet"))
	default:
		add(look.Bold.Render(fmt.Sprintf("Comments (%d)", len(c.comments))))
		for _, cm := range c.comments {
			add("")
			add(look.Sel.Render(cm.Author))
			add(wrap(strings.TrimSpace(cm.Body)))
		}
	}

	// The bottom: the comment box while writing, else the keys; a note above either.
	var foot []string
	if c.note != "" {
		foot = append(foot, look.Notice.Render(ansi.Truncate(c.note, inner, "…")))
	}
	if c.writing {
		c.box.SetWidth(inner)
		foot = append(foot, strings.Split(c.box.View(), "\n")...)
		foot = append(foot, look.KeyHints("⏎ posts it to Jira · alt+⏎ new line · esc cancel"))
	} else {
		foot = append(foot, look.KeyHints("c comment · a Claude drafts one · n work on it in a task · o open in Jira · r refresh · ↑↓ scroll · esc close"))
	}

	room := max(h-4-len(foot)-1, 3) // border and padding, the gap above the foot
	c.scroll = min(c.scroll, max(len(body)-room, 0))
	shown := body[c.scroll:min(c.scroll+room, len(body))]
	if len(body) > room {
		shown = append(shown, look.Dim.Render(fmt.Sprintf("%d–%d of %d lines", c.scroll+1, c.scroll+len(shown), len(body))))
	}
	return look.MenuBox.Width(w).Render(strings.Join(shown, "\n") + "\n\n" + strings.Join(foot, "\n"))
}
