// Package jira is Jira through Atlassian's CLI (acli, signed in through the browser; crew never sees
// a token): your open issues (acli.go), an issue's details and comments, and the changes you make to
// one (issue.go). Both UIs' JIRA sections use it; nothing here changes an issue unless it's asked to.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"crew/internal/proc"
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
	cmd := proc.Command(ctx, "acli", "jira", "workitem", "search", "--jql", JQL,
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
	out, err := proc.Command(ctx, "acli", "jira", "project", "list", "--paginate", "--json").Output()
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

// Site is the Jira site acli is signed in to (oolio.atlassian.net), for issue links: the search
// gives Jira's internal address, which the browser can't use.
func Site(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, _ := acli(ctx, "jira", "auth", "status")
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Site:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
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

// parse reads acli's JSON: a list of work items (or an object holding them under "issues"), one
// after another when acli writes a page at a time.
func parse(data []byte) ([]Issue, error) {
	var items []workItem
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("reading acli's answer: %w", err)
		}
		var page []workItem
		if json.Unmarshal(raw, &page) != nil {
			var wrapped struct {
				Issues []workItem `json:"issues"`
			}
			if err := json.Unmarshal(raw, &wrapped); err != nil {
				return nil, fmt.Errorf("reading acli's answer: %w", err)
			}
			page = wrapped.Issues
		}
		items = append(items, page...)
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
	return doc.markdown()
}

// adf is Atlassian's document format (descriptions and comments); markdown turns it into Markdown.
type adf struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Content []adf          `json:"content"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	} `json:"marks"`
}

// attr is one of a node's attributes as text ("" when it has none).
func (a adf) attr(k string) string {
	switch v := a.Attrs[k].(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	}
	return ""
}

// markdown is the document as Markdown, so the page draws it as Jira does: headings, lists (nested),
// code with its language, quotes, panels, rules, tables, and bold, italic, code, struck and linked
// text; a mention is @Name, an emoji itself, a status [STATUS], an attachment [attachment: name].
// ponytail: the nodes Jira writes in comments and descriptions; anything else keeps just its words.
func (a adf) markdown() string {
	var block func(n adf, indent string) string
	var inline func(ns []adf) string
	inline = func(ns []adf) string {
		var b strings.Builder
		for _, n := range ns {
			switch n.Type {
			case "text":
				t := n.Text
				for _, m := range n.Marks {
					switch m.Type {
					case "strong":
						t = "**" + t + "**"
					case "em":
						t = "*" + t + "*"
					case "code":
						t = "`" + t + "`"
					case "strike":
						t = "~~" + t + "~~"
					case "link":
						if href, _ := m.Attrs["href"].(string); href != "" {
							t = "[" + t + "](" + href + ")"
						}
					}
				}
				b.WriteString(t)
			case "hardBreak":
				b.WriteString("\n")
			case "mention":
				name := strings.TrimPrefix(n.attr("text"), "@")
				b.WriteString("@" + name)
			case "emoji":
				if t := n.attr("text"); t != "" {
					b.WriteString(t)
				} else {
					b.WriteString(n.attr("shortName"))
				}
			case "inlineCard":
				b.WriteString(n.attr("url"))
			case "status":
				b.WriteString("[" + strings.ToUpper(n.attr("text")) + "]")
			case "date":
				if ms, err := strconv.ParseInt(n.attr("timestamp"), 10, 64); err == nil {
					b.WriteString(time.UnixMilli(ms).UTC().Format("2 Jan 2006"))
				}
			default:
				b.WriteString(inline(n.Content))
			}
		}
		return b.String()
	}
	block = func(n adf, indent string) string {
		switch n.Type {
		case "doc":
			var parts []string
			for _, c := range n.Content {
				if s := block(c, indent); strings.TrimSpace(s) != "" {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, "\n\n")
		case "paragraph":
			return indent + strings.ReplaceAll(inline(n.Content), "\n", "\n"+indent)
		case "heading":
			level, _ := strconv.Atoi(n.attr("level"))
			return strings.Repeat("#", max(1, min(level, 6))) + " " + inline(n.Content)
		case "bulletList", "orderedList":
			var lines []string
			for i, item := range n.Content {
				mark := "- "
				if n.Type == "orderedList" {
					mark = strconv.Itoa(i+1) + ". "
				}
				var parts []string
				for j, c := range item.Content {
					s := block(c, indent+"  ")
					if j == 0 {
						s = indent + mark + strings.TrimPrefix(s, indent+"  ")
					}
					parts = append(parts, s)
				}
				lines = append(lines, strings.Join(parts, "\n"))
			}
			return strings.Join(lines, "\n")
		case "codeBlock":
			return indent + "```" + n.attr("language") + "\n" + inline(n.Content) + "\n" + indent + "```"
		case "blockquote", "panel":
			var parts []string
			for _, c := range n.Content {
				parts = append(parts, block(c, ""))
			}
			return "> " + strings.ReplaceAll(strings.Join(parts, "\n\n"), "\n", "\n> ")
		case "rule":
			return "---"
		case "table":
			var rows []string
			for i, r := range n.Content {
				var cells []string
				for _, c := range r.Content {
					var parts []string
					for _, p := range c.Content {
						parts = append(parts, strings.ReplaceAll(block(p, ""), "\n", " "))
					}
					cells = append(cells, strings.ReplaceAll(strings.Join(parts, " "), "|", "\\|"))
				}
				rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
				if i == 0 {
					rows = append(rows, "|"+strings.Repeat("---|", len(cells)))
				}
			}
			return strings.Join(rows, "\n")
		case "mediaSingle", "mediaGroup":
			var names []string
			for _, m := range n.Content {
				if name := m.attr("alt"); name != "" {
					names = append(names, name)
				}
			}
			if len(names) == 0 {
				return "[attachment]"
			}
			return "[attachment: " + strings.Join(names, ", ") + "]"
		case "blockCard", "embedCard":
			return n.attr("url")
		}
		if len(n.Content) > 0 && n.Content[0].Type != "text" {
			var parts []string
			for _, c := range n.Content {
				parts = append(parts, block(c, indent))
			}
			return strings.Join(parts, "\n\n")
		}
		return indent + inline(n.Content)
	}
	return strings.TrimSpace(block(a, ""))
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
