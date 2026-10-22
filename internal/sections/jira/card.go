package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/proc"
	"crew/internal/widgets/look"
)

// Card is an issue opened from the table, shown in the main pane like a listed PR: the Issue tab
// (its details, description, attachments, linked issues and subtasks) and the Comments tab (the
// thread, and a box to write a comment yourself or have Claude draft one). From either, e edits
// its summary, description and labels and s changes its status. Nothing goes to Jira but on your
// key: ⏎ posts a comment, ⌘S saves an edit, ⏎ in the status list moves it. The Chat tab is crew's
// (a Claude chat about the issue, like a PR's).
type Card struct {
	Issue    Issue
	statuses []string // what s offers: the statuses seen in the issue's space
	detail   detail
	comments []comment
	loading  bool
	err      string
	box      textarea.Model // a comment
	writing  bool           // the comment box is open
	note     string         // one line of feedback: "Posting…", "Saved ✓"
	edit     *editForm
	pick     *statusPick
}

// The card's tabs; the section draws PaneChat.
const (
	PaneIssue = iota
	PaneComments
	PaneChat
)

// PaneNames are the tabs' names, in order.
var PaneNames = []string{"Issue", "Comments", "Chat"}

type detail struct {
	Assignee, Reporter, Parent string
	Labels, Components         []string
	Created, Updated           time.Time
	Items                      []item // attachments, linked issues and subtasks, numbered on the Issue tab
}

// item is something the Issue tab lists under the description, numbered so its digit opens it.
type item struct {
	Kind   string // "Attachments", "Linked issues" or "Subtasks": the heading it goes under
	Name   string // the file's name, or the issue's key
	Rel    string // how an issue is linked: "blocks", "is blocked by"
	Info   string // the file's size and who added it, or the issue's summary
	Status string // a linked issue's status
	URL    string // the file, for an attachment
}

type comment struct {
	Author, Body string
	Created      time.Time
}

// CardMsg is an issue's details and comments coming back.
type CardMsg struct {
	key      string
	issue    Issue // what the view read: its summary, status and description now
	detail   detail
	comments []comment
	err      error
}

// Key is the issue the details are for.
func (m CardMsg) Key() string { return m.key }

// PostedMsg is a comment posted (or not).
type PostedMsg struct {
	key string
	err error
}

// Key is the issue the comment was for.
func (m PostedMsg) Key() string { return m.key }

// SavedMsg is an edit saved, or a status changed (or not).
type SavedMsg struct {
	key, what string
	err       error
}

// Key is the issue that was changed.
func (m SavedMsg) Key() string { return m.key }

// CardEvent is what crew should do after a key on the card.
type CardEvent struct {
	Work     bool   // work on the issue in a crew task
	URL      string // open this in the browser
	Open     string // open this issue (a linked one or a subtask) in crew
	Draft    bool   // have Claude draft a comment (crew runs it; SetDraft puts it in the box)
	Comments bool   // show the Comments tab (the comment box just opened)
}

// OpenCard makes an issue's card and reads its details and comments; statuses are what s offers.
func OpenCard(ctx context.Context, i Issue, statuses []string) (*Card, tea.Cmd) {
	box := textarea.New()
	box.Placeholder = "Write a comment…"
	box.ShowLineNumbers = false
	box.SetHeight(4)
	c := &Card{Issue: i, statuses: statuses, loading: true, box: box}
	return c, c.read(ctx)
}

func (c *Card) read(ctx context.Context) tea.Cmd {
	key := c.Issue.Key
	return func() tea.Msg { return readCard(ctx, key) }
}

// view is an issue as `acli jira workitem view --json` writes it (Jira's REST shape).
type view struct {
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name     string `json:"name"`
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Priority    struct{ Name string }        `json:"priority"`
		IssueType   struct{ Name string }        `json:"issuetype"`
		Description json.RawMessage              `json:"description"`
		Assignee    struct{ DisplayName string } `json:"assignee"`
		Reporter    struct{ DisplayName string } `json:"reporter"`
		Created     string                       `json:"created"`
		Updated     string                       `json:"updated"`
		Labels      []string                     `json:"labels"`
		Components  []struct{ Name string }      `json:"components"`
		Parent      *linked                      `json:"parent"`
		Attachment  []struct {
			Filename string                       `json:"filename"`
			Size     int64                        `json:"size"`
			Created  string                       `json:"created"`
			Content  string                       `json:"content"`
			Author   struct{ DisplayName string } `json:"author"`
		} `json:"attachment"`
		IssueLinks []struct {
			Type struct{ Inward, Outward string } `json:"type"`
			In   *linked                          `json:"inwardIssue"`
			Out  *linked                          `json:"outwardIssue"`
		} `json:"issuelinks"`
		Subtasks []linked `json:"subtasks"`
		Comment  *struct {
			Comments []struct {
				Author  struct{ DisplayName string } `json:"author"`
				Body    json.RawMessage              `json:"body"`
				Created string                       `json:"created"`
			} `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

// linked is another issue as a field names it: a parent, a link or a subtask.
type linked struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string                `json:"summary"`
		Status  struct{ Name string } `json:"status"`
	} `json:"fields"`
}

// readCard reads an issue's details and comments with acli: every field, so attachments, links,
// subtasks and the comments come in one read; if acli refuses that, the fields it's known to allow
// and the comments on their own.
func readCard(ctx context.Context, key string) CardMsg {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := acli(ctx, "jira", "workitem", "view", key, "--json", "--fields", "*all")
	if err != nil {
		out, err = acli(ctx, "jira", "workitem", "view", key, "--json", "--fields",
			"summary,status,priority,issuetype,description,assignee,reporter,created,updated,labels,components,parent")
	}
	if err != nil {
		return CardMsg{key: key, err: err}
	}
	var v view
	if err := json.Unmarshal(out, &v); err != nil {
		return CardMsg{key: key, err: fmt.Errorf("reading acli's answer: %w", err)}
	}
	f := v.Fields
	msg := CardMsg{key: key, issue: Issue{Summary: f.Summary, Status: f.Status.Name, Category: f.Status.Category.Key,
		Priority: f.Priority.Name, Type: f.IssueType.Name, Description: strings.TrimSpace(description(f.Description))}}
	d := detail{Assignee: f.Assignee.DisplayName, Reporter: f.Reporter.DisplayName, Labels: f.Labels,
		Created: jiraTime(f.Created), Updated: jiraTime(f.Updated)}
	for _, c := range f.Components {
		d.Components = append(d.Components, c.Name)
	}
	if f.Parent != nil {
		d.Parent = f.Parent.Key + " " + f.Parent.Fields.Summary
	}
	for _, a := range f.Attachment {
		d.Items = append(d.Items, item{Kind: "Attachments", Name: a.Filename, URL: a.Content,
			Info: size(a.Size) + " · " + a.Author.DisplayName + " · " + look.Ago(jiraTime(a.Created))})
	}
	for _, l := range f.IssueLinks {
		rel, other := l.Type.Outward, l.Out
		if other == nil {
			rel, other = l.Type.Inward, l.In
		}
		if other != nil {
			d.Items = append(d.Items, item{Kind: "Linked issues", Name: other.Key, Rel: rel, Info: other.Fields.Summary, Status: other.Fields.Status.Name})
		}
	}
	for _, s := range f.Subtasks {
		d.Items = append(d.Items, item{Kind: "Subtasks", Name: s.Key, Info: s.Fields.Summary, Status: s.Fields.Status.Name})
	}
	msg.detail = d
	if f.Comment != nil {
		for _, cm := range f.Comment.Comments {
			msg.comments = append(msg.comments, comment{cm.Author.DisplayName, strings.TrimSpace(description(cm.Body)), jiraTime(cm.Created)})
		}
		return msg
	}
	msg.comments, msg.err = listComments(ctx, key)
	return msg
}

// listComments reads an issue's comments on their own, when the view didn't include them.
// ponytail: the first 100 comments; --paginate if an issue ever has more.
func listComments(ctx context.Context, key string) ([]comment, error) {
	out, err := acli(ctx, "jira", "workitem", "comment", "list", "--key", key, "--json", "--limit", "100")
	if err != nil {
		return nil, err
	}
	// acli writes one JSON object per page of comments, one after another.
	var all []comment
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var page struct {
			Comments []struct{ Author, Body string } `json:"comments"`
		}
		if err := dec.Decode(&page); err != nil {
			return all, fmt.Errorf("reading acli's comments: %w", err)
		}
		for _, cm := range page.Comments {
			all = append(all, comment{Author: cm.Author, Body: cm.Body})
		}
	}
	return all, nil
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

// change runs an acli command that changes an issue (an edit, a transition).
func change(ctx context.Context, key, what string, args ...string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, err := acli(ctx, args...)
		return SavedMsg{key, what, err}
	}
}

// acli runs Atlassian's CLI and returns its output, or its first line of stderr as the error.
func acli(ctx context.Context, args ...string) ([]byte, error) {
	out, err := proc.Command(ctx, "acli", args...).Output()
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

// size is a file size people read: 88 KB.
func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// Loading reports whether the card is reading Jira.
func (c *Card) Loading() bool { return c.loading }

// Typing reports whether a box on the card has the keys (a comment, the edit form, the status list),
// so tab and esc are the card's.
func (c *Card) Typing() bool { return c.writing || c.edit != nil || c.pick != nil }

// Done takes the card's details and comments coming back.
func (c *Card) Done(msg CardMsg) {
	c.loading, c.err = false, ""
	if msg.err != nil {
		c.err = msg.err.Error()
	}
	if msg.issue.Summary != "" { // the view read it: what's in Jira now
		i := msg.issue
		i.Key, i.Project, i.URL = c.Issue.Key, c.Issue.Project, c.Issue.URL
		c.Issue, c.detail = i, msg.detail
	}
	if msg.comments != nil || msg.err == nil {
		c.comments = msg.comments
	}
}

// Posted takes a comment posted; on success the box closes and the card reads its comments again.
func (c *Card) Posted(ctx context.Context, msg PostedMsg) tea.Cmd {
	if msg.err != nil {
		c.note = "Couldn't post: " + msg.err.Error() + " · your comment is still in the box"
		return nil
	}
	c.writing, c.note, c.loading = false, "Posted ✓", true
	c.box.Reset()
	c.box.Blur()
	return c.read(ctx)
}

// Saved takes an edit saved or a status changed; on success the card reads the issue again.
func (c *Card) Saved(ctx context.Context, msg SavedMsg) tea.Cmd {
	if msg.err != nil {
		c.note = "Couldn't save the " + msg.what + ": " + msg.err.Error()
		if c.edit != nil {
			c.edit.saving = false
			c.note += " · your edit is still open"
		}
		return nil
	}
	c.edit, c.pick, c.note, c.loading = nil, nil, "Saved the "+msg.what+" ✓", true
	return c.read(ctx)
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

// KeyMsg handles a key on the Issue or Comments tab; ok is false for a key the card doesn't use
// (tab, esc, scrolling), which crew handles. cmd reads or writes Jira in the background.
func (c *Card) KeyMsg(ctx context.Context, pane int, msg tea.KeyMsg) (ev CardEvent, cmd tea.Cmd, ok bool) {
	switch {
	case c.edit != nil:
		return CardEvent{}, c.editKey(ctx, msg), true
	case c.pick != nil:
		return CardEvent{}, c.pickKey(ctx, msg), true
	case c.writing:
		return CardEvent{}, c.commentKey(ctx, msg), true
	}
	k := msg.String()
	c.note = "" // the last "Posted ✓" or error has been read
	switch k {
	case "c":
		c.writing = true
		return CardEvent{Comments: true}, c.box.Focus(), true
	case "a":
		c.note = "Claude is drafting a comment…"
		return CardEvent{Draft: true, Comments: true}, nil, true
	case "e":
		c.edit = newEditForm(c.Issue, c.detail.Labels)
		return CardEvent{}, textinput.Blink, true
	case "s":
		c.pick = newStatusPick(c.statuses, c.Issue.Status)
		return CardEvent{}, textinput.Blink, true
	case "n":
		return CardEvent{Work: true}, nil, true
	case "o":
		return CardEvent{URL: c.Issue.URL}, nil, true
	case "r":
		c.loading = true
		return CardEvent{}, c.read(ctx), true
	}
	if n := int(k[0] - '0'); len(k) == 1 && pane == PaneIssue && n >= 1 && n <= min(len(c.detail.Items), 9) {
		// ponytail: digits open the first 9 items; a picker if issues ever have more.
		it := c.detail.Items[n-1]
		if it.URL != "" {
			return CardEvent{URL: it.URL}, nil, true
		}
		return CardEvent{Open: it.Name}, nil, true
	}
	return CardEvent{}, nil, false
}

// commentKey is a key while writing a comment.
func (c *Card) commentKey(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		c.writing, c.note = false, ""
		c.box.Blur()
		return nil
	case "enter":
		body := strings.TrimSpace(c.box.Value())
		if body == "" {
			return nil
		}
		c.note = "Posting…"
		return PostComment(ctx, c.Issue.Key, body)
	case "alt+enter", "ctrl+j":
		c.box.InsertString("\n")
		return nil
	}
	var cmd tea.Cmd
	c.box, cmd = c.box.Update(msg)
	return cmd
}

// editForm edits an issue's summary, description and labels.
type editForm struct {
	summary textinput.Model
	desc    textarea.Model
	labels  textinput.Model
	field   int // 0 summary, 1 description, 2 labels
	saving  bool
	was     struct{ summary, desc, labels string }
}

func newEditForm(i Issue, labels []string) *editForm {
	f := &editForm{summary: textinput.New(), desc: textarea.New(), labels: textinput.New()}
	f.summary.Prompt, f.labels.Prompt = "", ""
	f.summary.CharLimit, f.labels.CharLimit = 255, 0
	f.desc.ShowLineNumbers = false
	f.desc.CharLimit = 0
	f.labels.Placeholder = "comma-separated"
	f.was.summary, f.was.desc, f.was.labels = i.Summary, i.Description, strings.Join(labels, ", ")
	f.summary.SetValue(f.was.summary)
	f.desc.SetValue(f.was.desc)
	f.labels.SetValue(f.was.labels)
	for f.desc.Line() > 0 { // SetValue leaves the cursor at the end; start at the top
		f.desc.CursorUp()
	}
	f.desc.CursorStart()
	f.summary.Focus()
	return f
}

// focus gives the keys to the form's field.
func (f *editForm) focus(i int) {
	f.field = (i + 3) % 3
	f.summary.Blur()
	f.desc.Blur()
	f.labels.Blur()
	switch f.field {
	case 0:
		f.summary.Focus()
	case 1:
		f.desc.Focus()
	default:
		f.labels.Focus()
	}
}

// editKey is a key in the edit form: tab moves between fields, ⌘S saves, esc drops the edit.
func (c *Card) editKey(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	f := c.edit
	if f.saving {
		return nil
	}
	switch k := msg.String(); {
	case k == "esc":
		c.edit, c.note = nil, "Edit dropped; nothing was sent"
		return nil
	case k == "tab", k == "shift+tab":
		f.focus(f.field + map[string]int{"tab": 1, "shift+tab": -1}[k])
		return nil
	case k == "enter" && f.field != 1:
		f.focus(f.field + 1)
		return nil
	case k == "ctrl+s":
		return c.save(ctx)
	}
	var cmd tea.Cmd
	switch f.field {
	case 0:
		f.summary, cmd = f.summary.Update(msg)
	case 1:
		f.desc, cmd = f.desc.Update(msg)
	default:
		f.labels, cmd = f.labels.Update(msg)
	}
	return cmd
}

// save sends what the form changed, and only that, with acli.
func (c *Card) save(ctx context.Context) tea.Cmd {
	f := c.edit
	args := []string{"jira", "workitem", "edit", "--key", c.Issue.Key, "--yes"}
	summary, desc := strings.TrimSpace(f.summary.Value()), strings.TrimSpace(f.desc.Value())
	if summary == "" {
		c.note = "The summary can't be empty"
		return nil
	}
	if summary != f.was.summary {
		args = append(args, "--summary", summary)
	}
	if desc != f.was.desc {
		// ponytail: the description goes back as plain text, so Jira's formatting (code blocks, lists)
		// becomes plain lines; send ADF (--description-file) to keep it.
		args = append(args, "--description", desc)
	}
	was, now := splitLabels(f.was.labels), splitLabels(f.labels.Value())
	var add, remove []string
	for _, l := range now {
		if !slices.Contains(was, l) {
			add = append(add, l)
		}
	}
	for _, l := range was {
		if !slices.Contains(now, l) {
			remove = append(remove, l)
		}
	}
	if len(add) > 0 {
		args = append(args, "--labels", strings.Join(add, ","))
	}
	if len(remove) > 0 {
		args = append(args, "--remove-labels", strings.Join(remove, ","))
	}
	if len(args) == 6 {
		c.edit, c.note = nil, "Nothing changed"
		return nil
	}
	f.saving, c.note = true, "Saving to Jira…"
	return change(ctx, c.Issue.Key, "edit", args...)
}

// splitLabels reads "a, b c" as labels; Jira's can't have spaces.
func splitLabels(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

// statusPick picks the status to move an issue to: one of those seen in its space, or one you type.
type statusPick struct {
	input   textinput.Model
	options []string
	sel     int
}

func newStatusPick(statuses []string, current string) *statusPick {
	p := &statusPick{input: textinput.New()}
	p.input.Prompt = "› "
	p.input.Placeholder = "type to filter, or a status name"
	for _, s := range statuses {
		if s != current {
			p.options = append(p.options, s)
		}
	}
	p.input.Focus()
	return p
}

// shown are the options matching what's typed.
func (p *statusPick) shown() []string {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	var out []string
	for _, o := range p.options {
		if strings.Contains(strings.ToLower(o), q) {
			out = append(out, o)
		}
	}
	return out
}

// pickKey is a key in the status list: ↑↓ pick, ⏎ moves the issue, esc closes it.
func (c *Card) pickKey(ctx context.Context, msg tea.KeyMsg) tea.Cmd {
	p := c.pick
	shown := p.shown()
	switch msg.String() {
	case "esc":
		c.pick = nil
		return nil
	case "up":
		p.sel = max(p.sel-1, 0)
		return nil
	case "down":
		p.sel = min(p.sel+1, max(len(shown)-1, 0))
		return nil
	case "enter":
		to := strings.TrimSpace(p.input.Value())
		if p.sel < len(shown) {
			to = shown[p.sel]
		}
		if to == "" {
			return nil
		}
		c.note = "Moving it to " + to + "…"
		return change(ctx, c.Issue.Key, "status", "jira", "workitem", "transition", "--key", c.Issue.Key, "--status", to, "--yes")
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.sel = 0
	return cmd
}

// View draws a tab (PaneIssue or PaneComments) w wide, for a pane h tall.
func (c *Card) View(pane, w, h int) string {
	var b []string
	add := func(s ...string) { b = append(b, s...) }
	if c.note != "" {
		add(look.Notice.Render(ansi.Truncate(c.note, w, "…")), "")
	}
	switch {
	case c.edit != nil:
		add(c.editView(w, h-len(b)))
	case c.pick != nil:
		add(c.pickView(w))
	case pane == PaneComments:
		add(c.commentsView(w))
	default:
		add(c.issueView(w))
	}
	return strings.Join(b, "\n")
}

// rule is a section heading with a line after it: "Description ─────".
func rule(name string, w int) string {
	return look.Bold.Render(name) + " " + look.Dim.Render(strings.Repeat("─", max(w-ansi.StringWidth(name)-1, 0)))
}

// issueView is the Issue tab: the details in two columns, the description, then what's attached.
func (c *Card) issueView(w int) string {
	i, d := c.Issue, c.detail
	var b []string
	add := func(s ...string) { b = append(b, s...) }
	add(look.Dim.Render(fmt.Sprintf("%s · %s · %s", i.Type, i.Status, i.Priority)), "")

	var fields []string
	field := func(name, v string) {
		if v != "" {
			fields = append(fields, look.Dim.Render(fmt.Sprintf("%-11s", name))+v)
		}
	}
	field("Assignee", d.Assignee)
	field("Reporter", d.Reporter)
	if !d.Created.IsZero() {
		field("Created", d.Created.Local().Format("2 Jan 2006 3:04pm"))
		field("Updated", look.Ago(d.Updated))
	}
	field("Parent", d.Parent)
	field("Labels", strings.Join(d.Labels, ", "))
	field("Components", strings.Join(d.Components, ", "))
	col := (w - 2) / 2
	for n := 0; n < len(fields); n += 2 {
		if w < 80 || n+1 == len(fields) {
			add(ansi.Truncate(fields[n], w, "…"))
			if w < 80 && n+1 < len(fields) {
				add(ansi.Truncate(fields[n+1], w, "…"))
			}
			continue
		}
		left := ansi.Truncate(fields[n], col, "…")
		add(left + strings.Repeat(" ", col+2-ansi.StringWidth(left)) + ansi.Truncate(fields[n+1], col, "…"))
	}
	if c.loading && d.Created.IsZero() {
		add(look.Dim.Render("Reading it from Jira…"))
	}

	add("", rule("Description", w))
	if i.Description == "" {
		add(look.Dim.Render("none · e adds one"))
	} else {
		add(look.Wrap(i.Description, w))
	}
	heading := ""
	for n, it := range d.Items {
		if it.Kind != heading {
			heading = it.Kind
			count := 0
			for _, o := range d.Items {
				if o.Kind == heading {
					count++
				}
			}
			add("", rule(fmt.Sprintf("%s (%d)", heading, count), w))
		}
		num := look.Dim.Render("   ")
		if n < 9 {
			num = look.Sel.Render(fmt.Sprintf(" %d ", n+1))
		}
		line := num + " " + it.Name + "  " + look.Dim.Render(it.Info)
		if it.URL == "" {
			rel := ""
			if it.Rel != "" {
				rel = look.Dim.Render(fmt.Sprintf("%-15s", it.Rel))
			}
			line = num + " " + rel + look.Sel.Render(it.Name) + "  " + it.Info + look.Dim.Render("  · "+it.Status)
		}
		add(ansi.Truncate(line, w, "…"))
	}
	if c.err != "" {
		add("", look.Warn.Render(look.Wrap("Couldn't read it all: "+c.err, w)))
	}
	return strings.Join(b, "\n")
}

// commentsView is the Comments tab: the thread, oldest first, and the box when you're writing.
func (c *Card) commentsView(w int) string {
	var b []string
	add := func(s ...string) { b = append(b, s...) }
	switch {
	case c.loading && len(c.comments) == 0:
		add(look.Dim.Render("Reading the comments…"))
	case c.err != "":
		add(look.Warn.Render(look.Wrap("Couldn't read them all: "+c.err, w)))
	case len(c.comments) == 0:
		add(look.Dim.Render("No comments yet · c writes one · a has Claude draft one"))
	}
	for _, cm := range c.comments {
		when := ""
		if !cm.Created.IsZero() {
			when = look.Dim.Render(" · " + look.Ago(cm.Created))
		}
		add(look.Sel.Render(cm.Author)+when, look.Wrap(cm.Body, w-2), "")
	}
	if c.writing {
		c.box.SetWidth(w - 2)
		add(rule("Your comment", w), c.box.View(), look.KeyHints("⏎ posts it to Jira · alt+⏎ new line · esc cancel"))
	}
	return strings.Join(b, "\n")
}

// editView is the edit form, its description box as tall as the pane allows.
func (c *Card) editView(w, h int) string {
	f := c.edit
	label := func(i int, name string) string {
		if f.field == i {
			return look.Sel.Render("▸ " + name)
		}
		return look.Dim.Render("  " + name)
	}
	f.summary.Width = w - 4
	f.labels.Width = w - 4
	f.desc.SetWidth(w - 2)
	f.desc.SetHeight(max(h-12, 4))
	return strings.Join([]string{
		look.Bold.Render("Editing " + c.Issue.Key),
		"",
		label(0, "Summary"), "  " + f.summary.View(),
		"",
		label(1, "Description") + look.Dim.Render("  (saved as plain text)"), f.desc.View(),
		"",
		label(2, "Labels"), "  " + f.labels.View(),
		"",
		look.KeyHints("⌘S saves to Jira · tab next field · esc drop the edit"),
	}, "\n")
}

// pickView is the status list.
func (c *Card) pickView(w int) string {
	p := c.pick
	b := []string{look.Bold.Render("Move "+c.Issue.Key) + look.Dim.Render(" · now "+c.Issue.Status), "", p.input.View(), ""}
	shown := p.shown()
	for i, o := range shown {
		if i == p.sel {
			b = append(b, look.Sel.Render("▸ "+o))
		} else {
			b = append(b, "  "+o)
		}
	}
	if len(shown) == 0 && strings.TrimSpace(p.input.Value()) == "" {
		b = append(b, look.Dim.Render("  type the status to move it to"))
	}
	return strings.Join(append(b, "", look.KeyHints("⏎ moves it in Jira · ↑↓ pick · esc cancel"),
		look.Dim.Render(ansi.Truncate("Jira allows only its workflow's next steps; a status it doesn't allow comes back as an error.", w, "…"))), "\n")
}

// Hint is the line under the Issue or Comments tab.
func (c *Card) Hint(pane int) string {
	if c.Typing() {
		return "esc cancel"
	}
	open := ""
	if pane == PaneIssue && len(c.detail.Items) > 0 {
		open = fmt.Sprintf("1–%d opens one · ", min(len(c.detail.Items), 9))
	}
	return open + "c comment · a Claude drafts one · e edit · s status · n work on it in a task · o Jira · r refresh · tab tabs · esc back"
}
