// Package catalog lists the Claude Code commands, skills and agents a task can use: the repo's own
// (.claude/ in the worktree) and your global ones (~/.claude, or $CLAUDE_CONFIG_DIR).
package catalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kinds of items.
const (
	Command = "command"
	Skill   = "skill"
	Agent   = "agent"
)

type Item struct {
	Name        string
	Description string
	Kind        string
	Scope       string // "repo" or "global"
	NeedsArgs   bool   // a command or skill that takes arguments (argument-hint or $ARGUMENTS)
	ArgHint     string
}

// Load reads the catalog for a repo directory; repo items win over global ones with the same name.
func Load(repoDir string) []Item {
	global := os.Getenv("CLAUDE_CONFIG_DIR")
	if global == "" {
		home, _ := os.UserHomeDir()
		global = filepath.Join(home, ".claude")
	}
	seen := map[string]bool{}
	var items []Item
	for _, src := range []struct{ dir, scope string }{
		{filepath.Join(repoDir, ".claude"), "repo"},
		{global, "global"},
	} {
		if repoDir == "" && src.scope == "repo" {
			continue
		}
		for _, it := range scan(src.dir, src.scope) {
			if key := it.Kind + "/" + it.Name; !seen[key] {
				seen[key] = true
				items = append(items, it)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func scan(dir, scope string) []Item {
	var items []Item
	walk := func(sub, kind string, match func(path string, d fs.DirEntry) bool) {
		root := filepath.Join(dir, sub)
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !match(path, d) {
				return nil
			}
			fm, body := frontMatter(path)
			name := fm["name"]
			if name == "" {
				name = strings.TrimSuffix(d.Name(), ".md")
				if kind == Skill {
					name = filepath.Base(filepath.Dir(path)) // skills are folders: <name>/SKILL.md
				}
			}
			it := Item{Name: name, Description: fm["description"], Kind: kind, Scope: scope, ArgHint: fm["argument-hint"]}
			if kind != Agent {
				it.NeedsArgs = it.ArgHint != "" || strings.Contains(body, "$ARGUMENTS") || strings.Contains(body, "$1")
			}
			items = append(items, it)
			return nil
		})
	}
	isMD := func(_ string, d fs.DirEntry) bool { return strings.HasSuffix(d.Name(), ".md") }
	walk("commands", Command, isMD)
	walk("agents", Agent, isMD)
	walk("skills", Skill, func(_ string, d fs.DirEntry) bool { return d.Name() == "SKILL.md" })
	return items
}

// frontMatter reads the "key: value" lines between the leading --- markers, and the body after them.
// ponytail: flat keys only, and multi-line YAML values keep just their first line.
func frontMatter(path string) (map[string]string, string) {
	data, err := os.ReadFile(path)
	fm := map[string]string{}
	if err != nil {
		return fm, ""
	}
	s := string(data)
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return fm, s
	}
	head, body, _ := strings.Cut(rest, "\n---")
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v == "|" || v == ">" {
			v = ""
		}
		fm[strings.TrimSpace(k)] = v
	}
	return fm, body
}
