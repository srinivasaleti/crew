package jira

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/requests"
	"crew/internal/table"
)

// Screen is the JIRA section: your setup, the issues as last read, and the table and setup form
// that show them. crew's ui hands it messages and draws its table; what a row asks for comes back
// as an Event (working on an issue makes a crew task, which is crew's to do).
type Screen struct {
	ctx    context.Context
	path   string // the config file
	cfg    Config
	ready  bool // set up, token included (checked when the setup changes, not per frame)
	issues []Issue
	err    string // why the last read failed
}

// ListMsg is a read of your issues coming back.
type ListMsg struct {
	issues []Issue
	err    error
}

// Event is what crew should do after an action on the table.
type Event struct {
	Work *Issue // work on this issue in a crew task
	URL  string // open this in the browser
}

// New makes the screen for the config at path.
func New(ctx context.Context, path string) *Screen {
	s := &Screen{ctx: ctx, path: path}
	s.Reload()
	return s
}

// Reload re-reads the setup (after the setup form closes).
func (s *Screen) Reload() {
	s.cfg = LoadConfig(s.path)
	s.ready = s.cfg.Ready()
}

// Ready reports whether Jira is set up.
func (s *Screen) Ready() bool { return s.ready }

// Count is how many open issues you have, for the sidebar row.
func (s *Screen) Count() int { return len(s.issues) }

// Load reads your issues in the background; nil until Jira is set up.
func (s *Screen) Load() tea.Cmd {
	if !s.ready {
		return nil
	}
	ctx, cfg := s.ctx, s.cfg
	return func() tea.Msg {
		issues, err := Mine(ctx, cfg)
		return ListMsg{issues, err}
	}
}

// Done takes a read coming back; a failed one keeps the last list.
func (s *Screen) Done(msg ListMsg) {
	s.err = ""
	if msg.err != nil {
		s.err = msg.err.Error()
		return
	}
	s.issues = msg.issues
}

// Table is the section's table; tasks says which issues have a crew task and how it's doing.
func (s *Screen) Table(tasks map[string]string) table.Table {
	return Table(s.issues, s.ready, tasks, s.err)
}

// Act does what the table asks on an issue (its key; "" for a Global key with no rows). setup is
// the setup form when that's what it asks for (or when Jira isn't set up yet).
func (s *Screen) Act(key, action string) (ev Event, setup *requests.Form) {
	if action == SetUp || !s.ready {
		return Event{}, s.SetupForm()
	}
	for i := range s.issues {
		if s.issues[i].Key != key {
			continue
		}
		if action == Open {
			return Event{URL: s.issues[i].URL}, nil
		}
		return Event{Work: &s.issues[i]}, nil
	}
	return Event{}, nil
}

// SetupForm asks for your site, email and an API token (kept in the Keychain). It's REQUESTS' form;
// crew shows it in that popup, and calls Reload and Load when it closes.
func (s *Screen) SetupForm() *requests.Form {
	path := s.path
	return requests.Fields("Set up Jira", []requests.Field{
		{Label: "Site", Hint: "your Jira's address, like yourco.atlassian.net", Value: s.cfg.Site},
		{Label: "Email", Hint: "the one you sign in to Jira with", Value: s.cfg.Email},
		{Label: "API token", Hint: "make one at id.atlassian.com → Security → API tokens · kept in the Keychain · empty keeps the saved one", Secret: true},
	}, func(v []string) error { return Setup(path, v[0], v[1], v[2]) })
}
