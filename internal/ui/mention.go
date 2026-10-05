package ui

import (
	"path"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"crew/internal/repo"
	"crew/internal/task"
)

const maxSuggestions = 8

// mention returns the "@query" being typed at the end of the message box and where its @ is.
// ponytail: only the end of the message is checked, which is where you type; mid-text editing isn't.
func (m Model) mention() (query string, at int, ok bool) {
	v := m.input.Value()
	at = strings.LastIndex(v, "@")
	if at < 0 || strings.ContainsAny(v[at+1:], " \t\n") || (at > 0 && !strings.ContainsAny(v[at-1:at], " \t\n")) {
		return "", 0, false
	}
	return v[at+1:], at, true
}

// suggest refreshes the @ file suggestions for what's being typed.
func (m *Model) suggest() {
	m.sugg = nil
	t := m.current()
	q, _, ok := m.mention()
	if t == nil || m.tab != tabChat || !ok {
		return
	}
	q = strings.ToLower(q)
	type hit struct {
		path string
		rank int
	}
	var hits []hit
	for _, f := range m.repoFiles(*t) {
		lf := strings.ToLower(f)
		switch {
		case strings.HasPrefix(strings.ToLower(path.Base(f)), q):
			hits = append(hits, hit{f, 0}) // file name starts with it: best
		case strings.Contains(lf, q):
			hits = append(hits, hit{f, 1})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return len(hits[i].path) < len(hits[j].path)
	})
	for i := 0; i < len(hits) && i < maxSuggestions; i++ {
		m.sugg = append(m.sugg, hits[i].path)
	}
	m.suggIdx = min(m.suggIdx, max(len(m.sugg)-1, 0))
}

// repoFiles lists the task's files from its worktree (or the repo before it starts), cached until
// the agent's next turn ends.
func (m *Model) repoFiles(t task.Task) []string {
	if files, ok := m.files[t.ID]; ok {
		return files
	}
	dir := t.Worktree
	if dir == "" {
		dir, _ = m.cfg.Repos.Resolve(t.Repo)
	}
	files, _ := repo.Files(dir)
	m.files[t.ID] = files
	return files
}

// acceptSuggestion replaces the "@query" with the chosen file path.
func (m *Model) acceptSuggestion() {
	_, at, ok := m.mention()
	if !ok || len(m.sugg) == 0 {
		return
	}
	m.input.SetValue(m.input.Value()[:at] + "@" + m.sugg[m.suggIdx] + " ")
	m.sugg = nil
	m.layout()
}

// suggestionsView lists the matching files above the message box.
func (m Model) suggestionsView() string {
	var b strings.Builder
	for i, f := range m.sugg {
		line := ansi.Truncate("@"+f, max(m.w-6, 10), "…")
		if i == m.suggIdx {
			b.WriteString(sel.Render("  ▶ ") + bold.Render(line) + "\n")
		} else {
			b.WriteString("    " + dim.Render(line) + "\n")
		}
	}
	return b.String()
}
