package ui

import (
	"path"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/catalog"
	"crew/internal/repo"
	"crew/internal/task"
)

// The message box suggests as you type:
//   @…  files in the task's worktree
//   /…  crew's commands, plus the Claude Code commands, skills and agents from the repo's .claude/
//       and your global ~/.claude
// ↑↓ choose, tab inserts, enter inserts and runs it (commands that take arguments wait for them).

const maxSuggestions = 8

type suggestion struct {
	label  string // "@path" or "/name"
	desc   string
	tag    string // where it comes from: "crew", "skill · repo", "agent · global"…
	insert string // replaces what's being typed
	run    bool   // enter also sends it
	hint   string // arguments it expects, shown under the box after inserting
}

// typing returns what's being completed at the end of the message box: "@" or "/", the query
// after it, and where it starts. Slash commands only at the very start of the message.
// ponytail: only the end of the message is checked, which is where you type; mid-text editing isn't.
func (m Model) typing() (kind, query string, at int, ok bool) {
	v := m.input.Value()
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, " \t\n") {
		return "/", v[1:], 0, true
	}
	at = strings.LastIndex(v, "@")
	if at < 0 || strings.ContainsAny(v[at+1:], " \t\n") || (at > 0 && !strings.ContainsAny(v[at-1:at], " \t\n")) {
		return "", "", 0, false
	}
	return "@", v[at+1:], at, true
}

// suggest refreshes the suggestions for what's being typed.
func (m *Model) suggest() {
	m.sugg = nil
	if m.argHint != "" && !strings.HasPrefix(m.input.Value(), m.argFor) {
		m.argHint = "" // the command it belonged to was edited away
	}
	t := m.current()
	kind, q, _, ok := m.typing()
	if t == nil || m.tab != tabChat || !ok {
		return
	}
	if kind == "/" {
		m.sugg = m.commandSuggestions(*t, strings.ToLower(q))
	} else {
		m.sugg = m.fileSuggestions(*t, strings.ToLower(q))
	}
	m.suggIdx = min(m.suggIdx, max(len(m.sugg)-1, 0))
}

func (m *Model) fileSuggestions(t task.Task, q string) []suggestion {
	type hit struct {
		path string
		rank int
	}
	var hits []hit
	for _, f := range m.repoFiles(t) {
		switch {
		case strings.HasPrefix(strings.ToLower(path.Base(f)), q):
			hits = append(hits, hit{f, 0}) // file name starts with it: best
		case strings.Contains(strings.ToLower(f), q):
			hits = append(hits, hit{f, 1})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return len(hits[i].path) < len(hits[j].path)
	})
	var out []suggestion
	for i := 0; i < len(hits) && i < maxSuggestions; i++ {
		out = append(out, suggestion{label: "@" + hits[i].path, insert: "@" + hits[i].path + " "})
	}
	return out
}

// commandSuggestions matches crew's commands and the task's Claude Code catalog by name, then by
// description; crew first, then the repo's, then your global ones.
func (m *Model) commandSuggestions(t task.Task, q string) []suggestion {
	type hit struct {
		s     suggestion
		rank  int // 0 name starts with q, 1 name contains q, 2 description contains q
		scope int // 0 crew, 1 repo, 2 global
	}
	var hits []hit
	add := func(name, desc string, s suggestion, scope int) {
		n := strings.ToLower(name)
		switch {
		case strings.HasPrefix(n, q):
			hits = append(hits, hit{s, 0, scope})
		case strings.Contains(n, q):
			hits = append(hits, hit{s, 1, scope})
		case q != "" && strings.Contains(strings.ToLower(desc), q):
			hits = append(hits, hit{s, 2, scope})
		}
	}
	for _, c := range slashCommands {
		add(c.name[1:], c.help, suggestion{label: c.name, desc: c.help, tag: "crew", insert: c.name, run: true}, 0)
	}
	for _, it := range m.catalog(t) {
		scope := 1
		if it.Scope == "global" {
			scope = 2
		}
		s := suggestion{label: "/" + it.Name, desc: it.Description, tag: it.Kind + " · " + it.Scope}
		switch {
		case it.Kind == catalog.Agent:
			// An agent needs a request, so it's inserted as the start of one.
			s.label, s.insert = it.Name, "Use the "+it.Name+" agent to "
		case it.NeedsArgs:
			s.insert, s.hint = "/"+it.Name+" ", it.ArgHint
			if s.hint == "" {
				s.hint = "arguments"
			}
		default:
			s.insert, s.run = "/"+it.Name, true
		}
		add(it.Name, it.Description, s, scope)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].scope < hits[j].scope
	})
	var out []suggestion
	for i := 0; i < len(hits) && i < maxSuggestions; i++ {
		out = append(out, hits[i].s)
	}
	return out
}

// catalog is the task's commands, skills and agents, re-read at most every 15 seconds.
func (m *Model) catalog(t task.Task) []catalog.Item {
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
func (m *Model) isClaudeCommand(t task.Task, name string) bool {
	for _, it := range m.catalog(t) {
		if it.Kind != catalog.Agent && "/"+it.Name == name {
			return true
		}
	}
	return false
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

// acceptSuggestion puts the chosen suggestion in the message box; with run (enter) it also sends
// it when it needs nothing more.
func (m *Model) acceptSuggestion(run bool) tea.Cmd {
	_, _, at, ok := m.typing()
	if !ok || len(m.sugg) == 0 {
		return nil
	}
	s := m.sugg[m.suggIdx]
	m.input.SetValue(m.input.Value()[:at] + s.insert)
	m.sugg = nil
	m.argHint, m.argFor = s.hint, s.insert
	m.layout()
	if run && s.run {
		m.argHint = ""
		return m.submit()
	}
	return nil
}

// suggestionsView lists the suggestions (or the arguments a command expects) above the message box.
func (m Model) suggestionsView() string {
	w := max(m.chatWidth()-2, 20)
	if len(m.sugg) == 0 {
		if m.argHint != "" {
			return dim.Render(ansi.Truncate("  "+strings.TrimSpace(m.argFor)+" expects: "+m.argHint, w, "…")) + "\n"
		}
		return ""
	}
	labelW := 0
	for _, s := range m.sugg {
		labelW = max(labelW, ansi.StringWidth(s.label))
	}
	labelW = min(labelW, w/3)
	var b strings.Builder
	for i, s := range m.sugg {
		label := ansi.Truncate(s.label, labelW, "…")
		label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
		tag := ""
		if s.tag != "" {
			tag = "  " + s.tag
		}
		descW := max(w-4-labelW-2-ansi.StringWidth(tag), 0)
		desc := ""
		if s.desc != "" && descW > 3 {
			desc = "  " + ansi.Truncate(s.desc, descW, "…")
		}
		if i == m.suggIdx {
			b.WriteString(sel.Render("  ▶ ") + bold.Render(label) + dim.Render(desc) + notice.Render(tag) + "\n")
		} else {
			b.WriteString("    " + label + dim.Render(desc+tag) + "\n")
		}
	}
	return b.String()
}
