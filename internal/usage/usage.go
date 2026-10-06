// Package usage reads token and cost usage for an agent session from Claude Code's own
// session log (~/.claude/projects/<project>/<session>.jsonl), so it covers every turn,
// including ones that ran before crew started tracking anything.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Model is the usage of one model within a session.
type Model struct {
	Name                                           string
	Input, Output, Thinking, CacheRead, CacheWrite int
	Cost                                           float64
}

func (m Model) Tokens() int { return m.Input + m.Output + m.CacheRead + m.CacheWrite }

// Turn is one message you sent and the tokens the agent used answering it.
type Turn struct {
	Prompt         string
	Tokens, Output int
}

type Usage struct {
	Cost           float64
	APIMs, TotalMs int // time spent in API calls, and wall time of the agent runs
	Added, Removed int // lines changed by the agent
	Models         []Model
	Turns          []Turn
	LastModel      string // model of the most recent reply, e.g. claude-sonnet-5-5
	// Context is the conversation size the next message re-sends: the input of the latest reply,
	// or the size right after the latest /compact.
	Context     int
	Compactions int
}

func (u Usage) Tokens() int {
	n := 0
	for _, m := range u.Models {
		n += m.Tokens()
	}
	return n
}

// logPath is the session's log file, or "" when there isn't one (yet).
func logPath(session string) string {
	if session == "" {
		return ""
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	// The project folder is the worktree path in Claude's own encoding; session ids are unique, so glob.
	paths, _ := filepath.Glob(filepath.Join(dir, "projects", "*", session+".jsonl"))
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// Exists reports whether Claude Code has a log for the session, i.e. it has had a turn and can be
// resumed.
func Exists(session string) bool { return logPath(session) != "" }

// Add folds an earlier session of the same task into u (after /new): the totals add up, and u keeps
// its own model and context size, since those are about the session in use.
func (u *Usage) Add(o Usage) {
	u.Cost += o.Cost
	u.APIMs += o.APIMs
	u.TotalMs += o.TotalMs
	u.Added += o.Added
	u.Removed += o.Removed
	u.Compactions += o.Compactions
	u.Turns = append(append([]Turn{}, o.Turns...), u.Turns...)
	for _, om := range o.Models {
		i := slices.IndexFunc(u.Models, func(m Model) bool { return m.Name == om.Name })
		if i < 0 {
			u.Models = append(u.Models, om)
			continue
		}
		m := &u.Models[i]
		m.Input, m.Output, m.Thinking = m.Input+om.Input, m.Output+om.Output, m.Thinking+om.Thinking
		m.CacheRead, m.CacheWrite, m.Cost = m.CacheRead+om.CacheRead, m.CacheWrite+om.CacheWrite, m.Cost+om.Cost
	}
	sort.Slice(u.Models, func(i, j int) bool { return u.Models[i].Cost > u.Models[j].Cost })
	if u.LastModel == "" {
		u.LastModel = o.LastModel
	}
}

// Load reads the session's usage. ok is false when the session file doesn't exist (yet).
func Load(session string) (u Usage, ok bool, err error) {
	path := logPath(session)
	if path == "" {
		return u, false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return u, false, err
	}
	defer f.Close()

	type entry struct {
		Type            string
		Subtype         string
		CompactMetadata struct{ PostTokens int }
		Message         struct {
			ID      string
			Model   string
			Content json.RawMessage
			Usage   struct {
				Input      int `json:"input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
				Output     int `json:"output_tokens"`
			}
		}
		// cost-state lines hold running session totals; the last one wins.
		TotalCostUSD      float64
		TotalAPIDuration  int
		TotalDuration     int
		TotalLinesAdded   int
		TotalLinesRemoved int
		ModelUsage        map[string]struct {
			InputTokens, OutputTokens, ThinkingTokens      int
			CacheReadInputTokens, CacheCreationInputTokens int
			CostUSD                                        float64
		}
	}
	seen := map[string]bool{} // an assistant message is logged once per content block; count it once
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var line entry
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "user":
			// Your messages have plain-string content; tool results come back as arrays.
			var prompt string
			if json.Unmarshal(line.Message.Content, &prompt) == nil {
				u.Turns = append(u.Turns, Turn{Prompt: prompt})
			}
		case "assistant":
			if seen[line.Message.ID] || len(u.Turns) == 0 {
				continue
			}
			seen[line.Message.ID] = true
			if line.Message.Model != "" {
				u.LastModel = line.Message.Model
			}
			u.Context = line.Message.Usage.Input + line.Message.Usage.CacheRead + line.Message.Usage.CacheWrite
			t := &u.Turns[len(u.Turns)-1]
			mu := line.Message.Usage
			t.Tokens += mu.Input + mu.CacheWrite + mu.CacheRead + mu.Output
			t.Output += mu.Output
		case "system":
			if line.Subtype == "compact_boundary" {
				u.Context = line.CompactMetadata.PostTokens
				u.Compactions++
			}
		case "cost-state":
			u.Cost, u.APIMs, u.TotalMs = line.TotalCostUSD, line.TotalAPIDuration, line.TotalDuration
			u.Added, u.Removed = line.TotalLinesAdded, line.TotalLinesRemoved
			u.Models = u.Models[:0]
			for name, m := range line.ModelUsage {
				u.Models = append(u.Models, Model{name, m.InputTokens, m.OutputTokens, m.ThinkingTokens,
					m.CacheReadInputTokens, m.CacheCreationInputTokens, m.CostUSD})
			}
			sort.Slice(u.Models, func(i, j int) bool { return u.Models[i].Cost > u.Models[j].Cost })
		}
	}
	for i := range u.Turns {
		u.Turns[i].Prompt = strings.TrimSpace(u.Turns[i].Prompt)
	}
	return u, true, sc.Err()
}
