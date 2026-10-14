package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/catalog"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// The message box's suggestions (chat.Suggestion): what's offered for what's being typed, the one
// picked, and where they come from: the task's files and Claude Code catalog, and crew's commands.

// suggest refreshes the suggestions for what's being typed.
func (m *Model) suggest() {
	m.sugg = nil
	if m.argHint != "" && !strings.HasPrefix(m.input.Value(), m.argFor) {
		m.argHint = "" // the command it belonged to was edited away
	}
	t := m.chatTask()
	kind, q, _, ok := chat.Typing(m.input.Value())
	if t == nil || !m.chatOpen() || !ok {
		return
	}
	if kind == "/" {
		var cmds []chat.Command
		for _, c := range m.commands() {
			cmds = append(cmds, chat.Command{Name: c.name, Help: c.help})
		}
		m.sugg = chat.CommandSuggestions(cmds, m.catalog(*t), strings.ToLower(q))
	} else {
		m.sugg = chat.FileSuggestions(m.repoFiles(*t), strings.ToLower(q))
	}
	m.suggIdx = min(m.suggIdx, max(len(m.sugg)-1, 0))
}

// catalog is the task's commands, skills and agents, re-read at most every 15 seconds.
func (m *Model) catalog(t tasks.Task) []catalog.Item {
	if c, ok := m.catalogs[t.ID]; ok && time.Since(c.at) < 15*time.Second {
		return c.items
	}
	dir := t.Worktree
	if dir == "" {
		dir, _ = m.cfg.Repos.Resolve(t.Repo)
	}
	items := catalog.Load(dir)
	m.catalogs[t.ID] = catalogCache{items, time.Now()}
	return items
}

type catalogCache struct {
	items []catalog.Item
	at    time.Time
}

// isClaudeCommand reports whether /name is a command or skill from the task's catalog.
func (m *Model) isClaudeCommand(t tasks.Task, name string) bool {
	for _, it := range m.catalog(t) {
		if it.Kind != catalog.Agent && "/"+it.Name == name {
			return true
		}
	}
	return false
}

// repoFiles lists the task's files from its worktree (or the repo before it starts), cached until
// the agent's next turn ends.
func (m *Model) repoFiles(t tasks.Task) []string {
	if files, ok := m.files[t.ID]; ok {
		return files
	}
	if m.taskDir(t) == "" {
		return nil // a PR's chat before its first message, or without a checkout
	}
	files, _ := repo.Files(m.taskDir(t))
	m.files[t.ID] = files
	return files
}

// acceptSuggestion puts the chosen suggestion in the message box; with run (enter) it also sends
// it when it needs nothing more.
func (m *Model) acceptSuggestion(run bool) tea.Cmd {
	_, _, at, ok := chat.Typing(m.input.Value())
	if !ok || len(m.sugg) == 0 {
		return nil
	}
	s := m.sugg[m.suggIdx]
	m.input.SetValue(m.input.Value()[:at] + s.Insert)
	m.sugg = nil
	m.argHint, m.argFor = s.Hint, s.Insert
	m.layout()
	if run && s.Run {
		m.argHint = ""
		return m.submit()
	}
	return nil
}

// suggestionsView lists the suggestions (or the arguments a command expects) above the message box.
func (m Model) suggestionsView() string {
	return chat.SuggestionsView(m.sugg, m.suggIdx, m.argHint, m.argFor, max(m.chatWidth()-2, 20))
}
