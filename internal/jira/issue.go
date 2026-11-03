package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crew/internal/pins"
	"crew/internal/proc"
)

// Detail is what an issue's card shows beyond the list: people, dates, labels, and what's attached.
type Detail struct {
	Assignee, Reporter, Parent string
	Labels, Components         []string
	Created, Updated           time.Time
	Items                      []Item // attachments, linked issues and subtasks, numbered on the Issue tab
}

// Item is something the Issue tab lists under the description, numbered so its digit opens it.
type Item struct {
	Kind   string    // "Attachments", "Linked issues" or "Subtasks": the heading it goes under
	Name   string    // the file's name, or the issue's key
	Rel    string    // how an issue is linked: "blocks", "is blocked by"
	Info   string    // the file's size and who added it, or the issue's summary
	Status string    // a linked issue's status
	URL    string    // the file, for an attachment
	Added  time.Time // when the file was added
}

// Comment is one comment on an issue.
type Comment struct {
	Author, Body string
	Created      time.Time
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

// Read reads an issue's details and comments with acli: every field, so attachments, links,
// subtasks and the comments come in one read; if acli refuses that, the fields it's known to allow
// and the comments on their own.
func Read(ctx context.Context, key string) (Issue, Detail, []Comment, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := acli(ctx, "jira", "workitem", "view", key, "--json", "--fields", "*all")
	if err != nil {
		out, err = acli(ctx, "jira", "workitem", "view", key, "--json", "--fields",
			"summary,status,priority,issuetype,description,assignee,reporter,created,updated,labels,components,parent")
	}
	if err != nil {
		return Issue{}, Detail{}, nil, err
	}
	var v view
	if err := json.Unmarshal(out, &v); err != nil {
		return Issue{}, Detail{}, nil, fmt.Errorf("reading acli's answer: %w", err)
	}
	f := v.Fields
	issue := Issue{Key: key, Summary: f.Summary, Status: f.Status.Name, Category: f.Status.Category.Key,
		Priority: f.Priority.Name, Type: f.IssueType.Name, Description: strings.TrimSpace(description(f.Description))}
	d := Detail{Assignee: f.Assignee.DisplayName, Reporter: f.Reporter.DisplayName, Labels: f.Labels,
		Created: jiraTime(f.Created), Updated: jiraTime(f.Updated)}
	for _, c := range f.Components {
		d.Components = append(d.Components, c.Name)
	}
	if f.Parent != nil {
		d.Parent = f.Parent.Key + " " + f.Parent.Fields.Summary
	}
	for _, a := range f.Attachment {
		d.Items = append(d.Items, Item{Kind: "Attachments", Name: a.Filename, URL: a.Content,
			Info: size(a.Size) + " · " + a.Author.DisplayName, Added: jiraTime(a.Created)})
	}
	for _, l := range f.IssueLinks {
		rel, other := l.Type.Outward, l.Out
		if other == nil {
			rel, other = l.Type.Inward, l.In
		}
		if other != nil {
			d.Items = append(d.Items, Item{Kind: "Linked issues", Name: other.Key, Rel: rel, Info: other.Fields.Summary, Status: other.Fields.Status.Name})
		}
	}
	for _, s := range f.Subtasks {
		d.Items = append(d.Items, Item{Kind: "Subtasks", Name: s.Key, Info: s.Fields.Summary, Status: s.Fields.Status.Name})
	}
	if f.Comment != nil {
		var comments []Comment
		for _, cm := range f.Comment.Comments {
			comments = append(comments, Comment{cm.Author.DisplayName, strings.TrimSpace(description(cm.Body)), jiraTime(cm.Created)})
		}
		return issue, d, comments, nil
	}
	comments, err := listComments(ctx, key)
	return issue, d, comments, err
}

// listComments reads an issue's comments on their own, when the view didn't include them.
// ponytail: the first 100 comments; --paginate if an issue ever has more.
func listComments(ctx context.Context, key string) ([]Comment, error) {
	out, err := acli(ctx, "jira", "workitem", "comment", "list", "--key", key, "--json", "--limit", "100")
	if err != nil {
		return nil, err
	}
	// acli writes one JSON object per page of comments, one after another.
	var all []Comment
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var page struct {
			Comments []struct{ Author, Body string } `json:"comments"`
		}
		if err := dec.Decode(&page); err != nil {
			return all, fmt.Errorf("reading acli's comments: %w", err)
		}
		for _, cm := range page.Comments {
			all = append(all, Comment{Author: cm.Author, Body: cm.Body})
		}
	}
	return all, nil
}

// PostComment posts a comment on an issue.
func PostComment(ctx context.Context, key, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := acli(ctx, "jira", "workitem", "comment", "create", "--key", key, "--body", body)
	return err
}

// Fields are what an edit changes: an issue's summary, description and labels.
type Fields struct {
	Summary, Description string
	Labels               []string
}

// EditArgs is the acli command that changes what's different from was to now, and only that; nil
// when nothing is. An empty summary is refused.
func EditArgs(key string, was, now Fields) ([]string, error) {
	args := []string{"jira", "workitem", "edit", "--key", key, "--yes"}
	summary, desc := strings.TrimSpace(now.Summary), strings.TrimSpace(now.Description)
	if summary == "" {
		return nil, errors.New("the summary can't be empty")
	}
	if summary != strings.TrimSpace(was.Summary) {
		args = append(args, "--summary", summary)
	}
	if desc != strings.TrimSpace(was.Description) {
		// ponytail: the description goes back as plain text, so Jira's formatting (code blocks, lists)
		// becomes plain lines; send ADF (--description-file) to keep it.
		args = append(args, "--description", desc)
	}
	var add, remove []string
	for _, l := range now.Labels {
		if !slices.Contains(was.Labels, l) {
			add = append(add, l)
		}
	}
	for _, l := range was.Labels {
		if !slices.Contains(now.Labels, l) {
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
		return nil, nil
	}
	return args, nil
}

// Transition is the acli command that moves an issue to a status.
func Transition(key, to string) []string {
	return []string{"jira", "workitem", "transition", "--key", key, "--status", to, "--yes"}
}

// Run runs an acli command that changes an issue (EditArgs, Transition).
func Run(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := acli(ctx, args...)
	return err
}

// SplitLabels reads "a, b c" as labels; Jira's can't have spaces.
func SplitLabels(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

// Statuses are the statuses an issue in this space can be moved to, as far as crew knows: those of
// your issues in it, and Done (which the list never shows).
// ponytail: from your own issues; ask Jira for the workflow's transitions if these aren't enough.
func Statuses(issues []Issue, project string) []string {
	var out []string
	for _, i := range issues {
		if i.Project == project && !slices.Contains(out, i.Status) {
			out = append(out, i.Status)
		}
	}
	return append(out, "Done")
}

// Every is how often JIRA reads your issues: CREW_JIRA_EVERY ("2m", "90s"), else 3 minutes, and
// never more often than every 30 seconds.
func Every() time.Duration {
	d, err := time.ParseDuration(os.Getenv("CREW_JIRA_EVERY"))
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return max(d, 30*time.Second)
}

// BrowseURL is an issue's address in the browser, on the site acli is signed in to.
func BrowseURL(site, key string) string { return "https://" + site + "/browse/" + key }

// CardText is an issue as plain text, for Claude to draft a comment from.
func CardText(i Issue, d Detail, comments []Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n%s · %s · %s\nAssignee: %s · Reporter: %s\n\n%s\n\nComments:\n",
		i.Key, i.Summary, i.Type, i.Status, i.Priority, d.Assignee, d.Reporter, i.Description)
	for _, cm := range comments {
		fmt.Fprintf(&b, "- %s: %s\n", cm.Author, cm.Body)
	}
	return b.String()
}

// DraftComment has Claude write a comment for an issue from its card text, in one headless run with
// no tools; it's only ever a draft you edit and post yourself.
func DraftComment(ctx context.Context, card string) (string, error) {
	prompt := "Draft one comment for this Jira issue, as the person it's assigned to: short, specific and " +
		"useful (a status update, a question or the next step), in plain text. Reply with the comment only.\n\n" + card
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := proc.Command(ctx, "claude", "-p", prompt, "--output-format", "text", "--tools", "")
	cmd.Dir = os.TempDir() // no repo: the card is all it needs
	out, err := cmd.Output()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ChatKind marks an issue's chat in its task file (kind: jira, issue: <key>).
const ChatKind = "jira"

// ChatBrief is what an issue's chat tells Claude before your first message.
func ChatBrief(i Issue) string {
	return fmt.Sprintf("You're helping me with Jira issue %s. Atlassian's CLI (acli) is signed in: read the issue with "+
		"`acli jira workitem view %[1]s --fields '*all' --json` and its comments with `acli jira workitem comment list --key %[1]s`. "+
		"Don't edit, comment on or move it in Jira unless I ask; when I do, use acli and say what you changed.\n\n%s",
		i.Key, i.Brief())
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

// Prefs are your stars (projects pinned on top) and hidden projects, in <home>/jira.json.
func Prefs(home string) pins.Pins { return pins.Load(PrefsPath(home)) }

// PrefsPath is where they're kept.
func PrefsPath(home string) string { return filepath.Join(home, "jira.json") }

// Visible is the list without the projects you hid, and how many issues that left out.
func Visible(p pins.Pins, issues []Issue) (shown []Issue, hidden int) {
	for _, i := range issues {
		if p.IsHidden(i.Project) {
			hidden++
		} else {
			shown = append(shown, i)
		}
	}
	return shown, hidden
}
