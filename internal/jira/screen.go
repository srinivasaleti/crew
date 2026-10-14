package jira

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/table"
)

// Screen is the JIRA section: the issues as last read, and the table that shows them. crew's ui
// hands it messages and draws its table; what a row asks for comes back as an Event (working on an
// issue makes a crew task, and signing in runs acli in a shell; both are crew's to do).
type Screen struct {
	ctx    context.Context
	issues []Issue
	spaces map[string]string // the spaces' names, by key, read once
	err    string            // why the last read failed
}

// ListMsg is a read of your issues coming back.
type ListMsg struct {
	issues []Issue
	spaces map[string]string // nil unless this read asked for them
	err    error
}

// Event is what crew should do after an action on the table.
type Event struct {
	Work   *Issue // work on this issue in a crew task
	URL    string // open this in the browser
	SignIn bool   // install acli or sign it in (crew's Setup does both)
}

// New makes the screen.
func New(ctx context.Context) *Screen { return &Screen{ctx: ctx} }

// Count is how many open issues you have, for the sidebar row.
func (s *Screen) Count() int { return len(s.issues) }

// Load reads your issues in the background (crew calls it once acli is signed in), and the spaces'
// names the first time.
func (s *Screen) Load() tea.Cmd {
	ctx, needSpaces := s.ctx, s.spaces == nil
	return func() tea.Msg {
		issues, err := Mine(ctx)
		var spaces map[string]string
		if needSpaces && err == nil {
			spaces, _ = Spaces(ctx) // without them, headings show the key
		}
		return ListMsg{issues, spaces, err}
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
	if msg.spaces != nil {
		s.spaces = msg.spaces
	}
}

// Table is the section's table. ready is whether acli is installed and signed in; tasks says which
// issues have a crew task and how it's doing.
func (s *Screen) Table(ready bool, tasks map[string]string) table.Table {
	return Table(s.issues, s.spaces, ready, tasks, s.err)
}

// Act does what the table asks on an issue (its key; "" for a Global key with no rows).
func (s *Screen) Act(ready bool, key, action string) Event {
	if action == SignIn || !ready {
		return Event{SignIn: true}
	}
	for i := range s.issues {
		switch {
		case s.issues[i].Key != key:
		case action == Open:
			return Event{URL: s.issues[i].URL}
		default:
			return Event{Work: &s.issues[i]}
		}
	}
	return Event{}
}
