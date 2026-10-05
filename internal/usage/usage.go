// Package usage reads token and cost usage for an agent session from Claude Code's own
// session log (~/.claude/projects/<project>/<session>.jsonl), so it covers every turn,
// including ones that ran before crew started tracking anything.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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

// Load reads the session's usage. ok is false when the session file doesn't exist (yet).
func Load(session string) (u Usage, ok bool, err error) {
	if session == "" {
		return u, false, nil
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	// The project folder is the worktree path in Claude's own encoding; session ids are unique, so glob.
	paths, _ := filepath.Glob(filepath.Join(dir, "projects", "*", session+".jsonl"))
	if len(paths) == 0 {
		return u, false, nil
	}
	f, err := os.Open(paths[0])
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
