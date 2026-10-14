// Package jira is the JIRA section: the Jira issues assigned to you that aren't done, read with
// Atlassian's CLI (acli, signed in through the browser; crew never sees a token), and the table
// that lists them. It only reads Jira; nothing in crew changes an issue.
package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Issue is one Jira issue, as the table and a task's description need it.
type Issue struct {
	Key, Summary, Type, Project, Priority string
	Status, Category                      string // Category is Jira's status category: new, indeterminate or done
	Description                           string // plain text
	URL                                   string
}

// JQL is what JIRA lists: your issues that aren't done, the most recently updated first.
const JQL = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"

// Mine reads your issues with acli.
// ponytail: the first 100 (--limit); --paginate if someone has more open issues than that.
func Mine(ctx context.Context) ([]Issue, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "acli", "jira", "workitem", "search", "--jql", JQL,
		// acli allows only some fields (not project or updated): the key gives the project, and the JQL
		// sorts by when an issue was updated.
		"--fields", "key,summary,status,priority,issuetype,description", "--limit", "100", "--json")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, errors.New(firstLine(string(ee.Stderr)))
		}
		return nil, err
	}
	return parse(out)
}

// Spaces names the Jira spaces (projects) you can see, by key: INC is "Incidents". JIRA reads it
// once, for its headings.
func Spaces(ctx context.Context) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "acli", "jira", "project", "list", "--paginate", "--json").Output()
	if err != nil {
		return nil, err
	}
	var ps []struct{ Key, Name string }
	if err := json.Unmarshal(out, &ps); err != nil {
		return nil, fmt.Errorf("reading acli's spaces: %w", err)
	}
	names := map[string]string{}
	for _, p := range ps {
		names[p.Key] = strings.TrimSpace(p.Name)
	}
	return names, nil
}

// workItem is an issue as Jira's REST API (and acli's --json) writes it.
type workItem struct {
	Key    string `json:"key"`
	Self   string `json:"self"` // https://<site>/rest/api/3/issue/<id>, which gives the site
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name     string `json:"name"`
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Priority    struct{ Name string } `json:"priority"`
		IssueType   struct{ Name string } `json:"issuetype"`
		Project     struct{ Key string }  `json:"project"`
		Description json.RawMessage       `json:"description"` // a document (ADF), or text
	} `json:"fields"`
}

// parse reads acli's JSON: a list of work items, or an object holding them under "issues".
func parse(data []byte) ([]Issue, error) {
	var items []workItem
	if err := json.Unmarshal(data, &items); err != nil {
		var wrapped struct {
			Issues []workItem `json:"issues"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return nil, fmt.Errorf("reading acli's answer: %w", err)
		}
		items = wrapped.Issues
	}
	issues := make([]Issue, 0, len(items))
	for _, it := range items {
		f := it.Fields
		site, _, _ := strings.Cut(it.Self, "/rest/")
		project, _, _ := strings.Cut(it.Key, "-") // ACC-123 is in project ACC
		issues = append(issues, Issue{
			Key: it.Key, Summary: f.Summary, Type: f.IssueType.Name, Project: project, Priority: f.Priority.Name,
			Status: f.Status.Name, Category: f.Status.Category.Key, Description: strings.TrimSpace(description(f.Description)),
			URL: site + "/browse/" + it.Key,
		})
	}
	return issues, nil
}

// description is an issue's description as plain text: Jira Cloud sends a document (ADF), older
// APIs a string.
func description(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var doc adf
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return doc.text()
}

// adf is Atlassian's document format; text flattens it.
type adf struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []adf  `json:"content"`
}

// text is the document as plain text: its words, a line per paragraph, heading or list item.
func (a adf) text() string {
	var b strings.Builder
	var walk func(n adf)
	walk = func(n adf) {
		b.WriteString(n.Text)
		if n.Type == "hardBreak" {
			b.WriteString("\n")
		}
		for _, c := range n.Content {
			walk(c)
		}
		switch n.Type {
		case "paragraph", "heading", "codeBlock", "listItem", "blockquote":
			b.WriteString("\n")
		}
	}
	walk(a)
	return b.String()
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// Brief is what a task made from an issue tells its agent: the issue, then its description. acli is
// signed in, so the agent can read more with `acli jira workitem view <key>`.
func (i Issue) Brief() string {
	return fmt.Sprintf("Jira issue %s: %s\n%s · %s · %s\n%s\n(acli jira workitem view %[1]s --json shows it all)\n\n%s",
		i.Key, i.Summary, i.Type, i.Status, i.Priority, i.URL, i.Description)
}
