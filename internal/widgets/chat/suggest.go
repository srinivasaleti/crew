package chat

import (
	"path"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"crew/internal/catalog"
	"crew/internal/widgets/look"
)

// The message box suggests as you type:
//   @…  files in the task's worktree
//   /…  crew's commands, plus the Claude Code commands, skills and agents from the repo's .claude/
//       and your global ~/.claude
// ↑↓ choose, tab inserts, enter inserts and runs it (commands that take arguments wait for them).
// This is the matching and drawing; crew keeps what's suggested and picked.

const maxSuggestions = 8

// Suggestion is one thing the message box offers.
type Suggestion struct {
	Label  string // "@path" or "/name"
	Desc   string
	Tag    string // where it comes from: "crew", "skill · repo", "agent · global"…
	Insert string // replaces what's being typed
	Run    bool   // enter also sends it
	Hint   string // arguments it expects, shown under the box after inserting
}

// Command is one of crew's own commands, for suggestions.
type Command struct{ Name, Help string }

// Typing returns what's being completed at the end of the message box (v): "@" or "/", the query
// after it, and where it starts. Slash commands only at the very start of the message.
// ponytail: only the end of the message is checked, which is where you type; mid-text editing isn't.
func Typing(v string) (kind, query string, at int, ok bool) {
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, " \t\n") {
		return "/", v[1:], 0, true
	}
	at = strings.LastIndex(v, "@")
	if at < 0 || strings.ContainsAny(v[at+1:], " \t\n") || (at > 0 && !strings.ContainsAny(v[at-1:at], " \t\n")) {
		return "", "", 0, false
	}
	return "@", v[at+1:], at, true
}

// FileSuggestions are the files (paths in the task's folder) matching q: names that start with
// it first, then paths that have it, shorter first.
func FileSuggestions(files []string, q string) []Suggestion {
	type hit struct {
		path string
		rank int
	}
	var hits []hit
	for _, f := range files {
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
	var out []Suggestion
	for i := 0; i < len(hits) && i < maxSuggestions; i++ {
		out = append(out, Suggestion{Label: "@" + hits[i].path, Insert: "@" + hits[i].path + " "})
	}
	return out
}

// CommandSuggestions matches crew's commands and the task's Claude Code catalog by name, then by
// description; crew first, then the repo's, then your global ones.
func CommandSuggestions(cmds []Command, items []catalog.Item, q string) []Suggestion {
	type hit struct {
		s     Suggestion
		rank  int // 0 name starts with q, 1 name contains q, 2 description contains q
		scope int // 0 crew, 1 repo, 2 global
	}
	var hits []hit
	add := func(name, desc string, s Suggestion, scope int) {
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
	for _, c := range cmds {
		add(c.Name[1:], c.Help, Suggestion{Label: c.Name, Desc: c.Help, Tag: "crew", Insert: c.Name, Run: true}, 0)
	}
	for _, it := range items {
		scope := 1
		if it.Scope == "global" {
			scope = 2
		}
		s := Suggestion{Label: "/" + it.Name, Desc: it.Description, Tag: it.Kind + " · " + it.Scope}
		switch {
		case it.Kind == catalog.Agent:
			// An agent needs a request, so it's inserted as the start of one.
			s.Label, s.Insert = it.Name, "Use the "+it.Name+" agent to "
		case it.NeedsArgs:
			s.Insert, s.Hint = "/"+it.Name+" ", it.ArgHint
			if s.Hint == "" {
				s.Hint = "arguments"
			}
		default:
			s.Insert, s.Run = "/"+it.Name, true
		}
		add(it.Name, it.Description, s, scope)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].scope < hits[j].scope
	})
	var out []Suggestion
	for i := 0; i < len(hits) && i < maxSuggestions; i++ {
		out = append(out, hits[i].s)
	}
	return out
}

// SuggestionsView lists the suggestions, idx picked (or, with none, the arguments the command
// argFor expects, argHint) above the message box, w wide.
func SuggestionsView(list []Suggestion, idx int, argHint, argFor string, w int) string {
	if len(list) == 0 {
		if argHint != "" {
			return look.Dim.Render(ansi.Truncate("  "+strings.TrimSpace(argFor)+" expects: "+argHint, w, "…")) + "\n"
		}
		return ""
	}
	labelW := 0
	for _, s := range list {
		labelW = max(labelW, ansi.StringWidth(s.Label))
	}
	labelW = min(labelW, w/3)
	var b strings.Builder
	for i, s := range list {
		label := ansi.Truncate(s.Label, labelW, "…")
		label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
		tag := ""
		if s.Tag != "" {
			tag = "  " + s.Tag
		}
		descW := max(w-4-labelW-2-ansi.StringWidth(tag), 0)
		desc := ""
		if s.Desc != "" && descW > 3 {
			desc = "  " + ansi.Truncate(s.Desc, descW, "…")
		}
		if i == idx {
			b.WriteString(look.Sel.Render("  ▶ ") + look.Bold.Render(label) + look.Dim.Render(desc) + look.Notice.Render(tag) + "\n")
		} else {
			b.WriteString("    " + label + look.Dim.Render(desc+tag) + "\n")
		}
	}
	return b.String()
}
