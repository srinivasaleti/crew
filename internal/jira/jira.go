// Package jira is the JIRA section's side that knows Jira: your site and email (~/.crew/jira), the
// API token (in the Keychain, never in a file), the issues assigned to you and not done (Jira Cloud's
// REST API), and the table that lists them. It only reads Jira; nothing in crew changes an issue.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"crew/internal/keychain"
)

const keychainService = "crew-jira"

// Config is where your Jira is and who you are there.
type Config struct {
	Site  string // e.g. https://yourco.atlassian.net
	Email string
}

// LoadConfig reads the config file: "site: …" and "email: …" lines. A missing file is no config.
func LoadConfig(path string) Config {
	var c Config
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		k, v, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(k) {
		case "site":
			c.Site = strings.TrimSpace(v)
		case "email":
			c.Email = strings.TrimSpace(v)
		}
	}
	return c
}

// Ready reports whether Jira is set up: a site, an email, and a token in the Keychain.
func (c Config) Ready() bool {
	return c.Site != "" && c.Email != "" && keychain.Has(keychainService, c.Email)
}

// Setup saves the site and email to path and the token to the Keychain; an empty token keeps the
// one already there.
func Setup(path, site, email, token string) error {
	site = strings.TrimRight(strings.TrimSpace(site), "/")
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	switch {
	case err != nil || u.Host == "":
		return errors.New("Site: your Jira's address, like yourco.atlassian.net")
	case !strings.Contains(email, "@"):
		return errors.New("Email: the one you sign in to Jira with")
	case token == "" && !keychain.Has(keychainService, email):
		return errors.New("API token: make one at id.atlassian.com → Security → API tokens")
	}
	if token != "" {
		if err := keychain.Set(keychainService, email, token); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte("site: "+site+"\nemail: "+email+"\n"), 0o644)
}

// Issue is one Jira issue, as the table and a task's description need it.
type Issue struct {
	Key, Summary, Type, Project, Priority string
	Status, Category                      string // Category is Jira's status category: new, indeterminate or done
	Description                           string // plain text
	Updated                               time.Time
	URL                                   string
}

// Mine reads the issues assigned to you that aren't done, the most recently updated first.
// ponytail: the first 100; page with nextPageToken if someone has more open issues than that.
func Mine(ctx context.Context, c Config) ([]Issue, error) {
	token, err := keychain.Get(keychainService, c.Email)
	if err != nil {
		return nil, errors.New("no Jira API token in the Keychain · S sets up Jira")
	}
	body, _ := json.Marshal(map[string]any{
		"jql":        "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC",
		"fields":     []string{"summary", "status", "priority", "issuetype", "project", "updated", "description"},
		"maxResults": 100,
	})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", c.Site+"/rest/api/3/search/jql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Email, token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, fmt.Errorf("Jira said %s · check the email and API token (S sets up Jira)", resp.Status)
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("Jira said %s", resp.Status)
	}
	var out struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Updated string `json:"updated"`
				Status  struct {
					Name     string `json:"name"`
					Category struct {
						Key string `json:"key"`
					} `json:"statusCategory"`
				} `json:"status"`
				Priority    struct{ Name string } `json:"priority"`
				IssueType   struct{ Name string } `json:"issuetype"`
				Project     struct{ Key string }  `json:"project"`
				Description *adf                  `json:"description"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("reading Jira's answer: %w", err)
	}
	issues := make([]Issue, 0, len(out.Issues))
	for _, it := range out.Issues {
		f := it.Fields
		updated, _ := time.Parse("2006-01-02T15:04:05.000-0700", f.Updated)
		issues = append(issues, Issue{
			Key: it.Key, Summary: f.Summary, Type: f.IssueType.Name, Project: f.Project.Key, Priority: f.Priority.Name,
			Status: f.Status.Name, Category: f.Status.Category.Key, Description: strings.TrimSpace(f.Description.text()),
			Updated: updated, URL: c.Site + "/browse/" + it.Key,
		})
	}
	return issues, nil
}

// adf is Atlassian's document format, which issue descriptions come in; text flattens it.
type adf struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []adf  `json:"content"`
}

// text is the document as plain text: its words, a line per paragraph, heading or list item.
func (a *adf) text() string {
	if a == nil {
		return ""
	}
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
	walk(*a)
	return b.String()
}

// Brief is what a task made from an issue tells its agent: the issue, then its description.
func (i Issue) Brief() string {
	return fmt.Sprintf("Jira issue %s: %s\n%s · %s · %s\n%s\n\n%s", i.Key, i.Summary, i.Type, i.Status, i.Priority, i.URL, i.Description)
}
