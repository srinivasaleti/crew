package app

import (
	"os"
	"path/filepath"
	"strings"

	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// Commands that run crew's developer skills (plugin/skills) in a task: /debug finds a bug's root
// cause and fixes it, /test writes tests for the task's change, /pr writes its PR's title and
// description to a file that P opens the PR with. Like /plan, each sends /crew:<skill> raw and
// needs Claude.

// prDescPath is where /pr writes a task's PR description, next to its plan (agents can write there).
func (m Model) prDescPath(id string) string { return filepath.Join(m.cfg.Plans, id+".pr.md") }

// PRDescription is the description file P opens a task's PR with, "" when /pr hasn't written one
// (prview.Host).
func (m *Model) PRDescription(id string) string {
	if _, err := os.Stat(m.prDescPath(id)); err != nil {
		return ""
	}
	return m.prDescPath(id)
}

// runDebug is /debug <symptom>: Claude finds the root cause with crew:debug, fixes it and adds a test.
func runDebug(m *Model, t *tasks.Task, symptom string) {
	if !m.canUseSkill(t, "debug") {
		return
	}
	if symptom = strings.TrimSpace(symptom); symptom == "" {
		m.add(t.ID, chat.Error, "say what's wrong: /debug <the error, a log line, or what it does wrong>")
		return
	}
	m.pending = m.send(*t, chat.Outgoing{Text: "/crew:debug Symptom " + symptom, Raw: true})
}

// runTest is /test [area]: Claude writes tests for the task's change with crew:test.
func runTest(m *Model, t *tasks.Task, area string) {
	if !m.canUseSkill(t, "test") {
		return
	}
	if t.Base == "" || t.Worktree == "" {
		m.add(t.ID, chat.Error, "nothing to test yet · the task's worktree is made with your first message")
		return
	}
	text := "/crew:test Base " + t.Base
	if area = strings.TrimSpace(area); area != "" {
		text += ". Area " + area
	}
	m.pending = m.send(*t, chat.Outgoing{Text: text, Raw: true})
}

// runPR is /pr: Claude writes the task's PR title and description with crew:pr, to the file P
// opens the PR with.
func runPR(m *Model, t *tasks.Task, _ string) {
	if !m.canUseSkill(t, "pr") {
		return
	}
	if t.Base == "" || t.Worktree == "" {
		m.add(t.ID, chat.Error, "no change to describe yet · the task's worktree is made with your first message")
		return
	}
	os.MkdirAll(m.cfg.Plans, 0o755)
	m.pending = m.send(*t, chat.Outgoing{Text: "/crew:pr Description file " + m.prDescPath(t.ID) + ". Base " + t.Base, Raw: true})
}
