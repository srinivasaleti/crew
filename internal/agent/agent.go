// Package agent runs coding-agent CLIs headless, one turn at a time, and streams their output.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crew/internal/proc"
)

type Kind int

const (
	KindText   Kind = iota // assistant prose
	KindTool               // a tool call: "Name: what it's given"
	KindResult             // what a tool call returned (a command's output, a file, an edit's lines)
	KindDelta              // a piece of prose as it's being written (the full text follows as KindText)
	KindDone               // turn finished normally
	KindError              // turn failed
)

type Event struct {
	Kind Kind
	Text string
	Sub  bool // from a subagent (the Agent/Task tool), not the main conversation
}

// Agent describes how to drive one CLI.
type Agent struct {
	Name    string // stored on the task, e.g. "claude"
	Title   string // how it's named on screen, e.g. "Claude"
	Command string
	KeyEnv  string // the API key variable it reads, if you use one instead of signing in
	// Args builds argv for one turn; the prompt is sent on stdin.
	// resume is false on a session's first turn; an empty model means the CLI's default.
	Args  func(session string, resume bool, model string) []string
	Parse func(line []byte) []Event
	// AuthError reports whether a failed turn's error means the CLI isn't signed in (or its login
	// expired), so crew can offer to sign in instead of showing the raw error.
	AuthError func(text string) bool
	// HasSession reports whether a session has had a turn, so the next one resumes it; nil means
	// look in Claude Code's session logs (the usage package).
	HasSession func(session string) bool
}

// ponytail: "auto" lets Claude's classifier approve tool calls since nobody can answer prompts headless.
// Make it per-task if some tasks need "plan" or "acceptEdits".
var Claude = Agent{
	Name:    "claude",
	Title:   "Claude",
	Command: "claude",
	KeyEnv:  "ANTHROPIC_API_KEY",
	Args: func(session string, resume bool, model string) []string {
		a := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "auto"}
		if model != "" {
			a = append(a, "--model", model)
		}
		if resume {
			return append(a, "--resume", session)
		}
		return append(a, "--session-id", session)
	},
	Parse:     parseClaude,
	AuthError: authMatcher(claudeAuthTexts),
}

// claudeAuthTexts are what Claude Code says when it can't use your login: signed out, an expired or
// revoked OAuth token, or a rejected API key.
// ponytail: matched on the CLI's wording; if a new message slips through, it shows as a plain error.
var claudeAuthTexts = []string{
	"/login", "not logged in", "invalid api key", "oauth token", "token has expired", "token expired",
	"token has been revoked", "authentication_error", "invalid bearer token", "invalid x-api-key",
	"please log in", "please sign in",
}

// authMatcher reports whether an error contains one of an agent's sign-in failures.
func authMatcher(texts []string) func(string) bool {
	return func(text string) bool {
		t := strings.ToLower(text)
		for _, s := range texts {
			if strings.Contains(t, s) {
				return true
			}
		}
		return false
	}
}

// Gemini is Google's Gemini CLI. "yolo" approves its tool calls, as "auto" does for Claude: nobody
// can answer prompts headless. --skip-trust trusts the worktree, which crew made for it.
var Gemini = Agent{
	Name:    "gemini",
	Title:   "Gemini",
	Command: "gemini",
	KeyEnv:  "GEMINI_API_KEY",
	Args: func(session string, resume bool, model string) []string {
		a := []string{"--output-format", "stream-json", "--approval-mode", "yolo", "--skip-trust"}
		if model != "" {
			a = append(a, "--model", model)
		}
		if resume {
			return append(a, "--resume", session)
		}
		return append(a, "--session-id", session)
	},
	Parse: parseGemini,
	// ponytail: matched on the CLI's wording, like Claude's.
	AuthError: authMatcher([]string{"auth method", "api key not valid", "api_key_invalid", "unauthenticated",
		"invalid_grant", "login required", "please re-authenticate", "credentials"}),
	HasSession: geminiSession,
}

// All lists the agents a task can use, in the order the new-task form offers them. Another CLI
// (codex, …) goes here with its own Args, Parse and AuthError, and a check in the setup package.
var All = []Agent{Claude, Gemini}

func Get(name string) (Agent, bool) {
	for _, a := range All {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// NewSessionID returns a random UUID v4.
func NewSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Run executes one turn in dir; extra is appended to the agent's arguments. The channel closes when
// the process exits; the last event is KindDone or KindError, unless ctx is cancelled (Stop), which
// interrupts the agent and sends nothing more.
func (a Agent) Run(ctx context.Context, dir, session string, resume bool, model, prompt string, extra []string) <-chan Event {
	ch := make(chan Event)
	send := func(ev Event) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(ch)
		args := a.Args(session, resume, model)
		if i := slices.Index(args, "--permission-mode"); i >= 0 && slices.Contains(extra, "--permission-mode") {
			args = slices.Delete(args, i, i+2) // the caller picks the mode (a locked-down chat)
		}
		cmd := proc.Command(ctx, a.Command, append(args, extra...)...)
		// On cancel, interrupt like Ctrl+C so the agent can save the session, then kill if it lingers.
		proc.Interrupt(cmd, 3*time.Second)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(prompt)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			send(Event{Kind: KindError, Text: err.Error()})
			return
		}
		ended := false
		// Prose that came only in pieces (Gemini never sends the full text) is sent whole before
		// whatever follows it; Claude sends the full text itself, which clears it.
		var said strings.Builder
		flush := func() {
			if t := strings.TrimSpace(said.String()); t != "" {
				send(Event{Kind: KindText, Text: t})
			}
			said.Reset()
		}
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20) // stream-json lines can be large
		for sc.Scan() {
			for _, ev := range a.Parse(sc.Bytes()) {
				switch {
				case ev.Sub:
				case ev.Kind == KindDelta:
					said.WriteString(ev.Text)
				case ev.Kind == KindText:
					said.Reset()
				default:
					flush()
				}
				ended = ended || ev.Kind == KindDone || ev.Kind == KindError
				send(ev) // after a cancel this drops the event; keep reading so the process can exit
			}
		}
		err = cmd.Wait()
		if ctx.Err() != nil {
			return // stopped; the UI reports it
		}
		flush()
		switch {
		case ended:
		case err != nil:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			send(Event{Kind: KindError, Text: msg})
		default:
			send(Event{Kind: KindDone})
		}
	}()
	return ch
}

func parseClaude(line []byte) []Event {
	var m struct {
		Type            string
		Subtype         string
		ParentToolUseID string `json:"parent_tool_use_id"`
		CompactMetadata struct {
			Pre  int `json:"pre_tokens"`
			Post int `json:"post_tokens"`
		} `json:"compact_metadata"`
		IsError bool `json:"is_error"`
		Result  string
		Message struct {
			Content json.RawMessage // blocks; a plain string for your own prompts
		}
		Event struct { // with --include-partial-messages
			Type  string
			Delta struct{ Type, Text string }
		}
	}
	if json.Unmarshal(line, &m) != nil {
		return nil
	}
	var blocks []struct {
		Type, Text, Name string
		Input            map[string]any
		Content          json.RawMessage // a tool result: a string, or text blocks
		IsError          bool            `json:"is_error"`
	}
	json.Unmarshal(m.Message.Content, &blocks)
	sub := m.ParentToolUseID != ""
	switch m.Type {
	case "system":
		if m.Subtype == "compact_boundary" {
			c := m.CompactMetadata
			return []Event{{Kind: KindTool, Text: fmt.Sprintf("compacted context: %dk → %dk tokens", c.Pre/1000, c.Post/1000)}}
		}
	case "stream_event":
		if m.Event.Type == "content_block_delta" && m.Event.Delta.Type == "text_delta" && m.Event.Delta.Text != "" {
			return []Event{{Kind: KindDelta, Text: m.Event.Delta.Text, Sub: sub}}
		}
	case "assistant":
		var evs []Event
		for _, c := range blocks {
			switch c.Type {
			case "text":
				if t := strings.TrimSpace(c.Text); t != "" {
					evs = append(evs, Event{Kind: KindText, Text: t, Sub: sub})
				}
			case "tool_use":
				evs = append(evs, Event{Kind: KindTool, Text: toolSummary(c.Name, c.Input), Sub: sub})
			}
		}
		return evs
	case "user":
		var evs []Event
		for _, c := range blocks {
			if c.Type == "tool_result" {
				text := resultText(c.Content)
				if c.IsError {
					text = "error: " + text
				}
				evs = append(evs, Event{Kind: KindResult, Text: text, Sub: sub})
			}
		}
		return evs
	case "result":
		if m.IsError || m.Subtype != "success" {
			msg := m.Result
			if msg == "" {
				msg = m.Subtype
			}
			return []Event{{Kind: KindError, Text: msg}}
		}
		return []Event{{Kind: KindDone}}
	}
	return nil
}

// maxResult is how much of a tool's output is kept.
// ponytail: the start of a long output; the full text is in Claude Code's session log.
const maxResult = 8000

// resultText is a tool result's text: a string, or the text of its blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var parts []struct{ Type, Text string }
		json.Unmarshal(raw, &parts)
		var texts []string
		for _, p := range parts {
			if p.Type == "text" {
				texts = append(texts, p.Text)
			}
		}
		s = strings.Join(texts, "\n")
	}
	s = strings.TrimRight(s, "\n ")
	if len(s) > maxResult {
		s = s[:maxResult] + "\n…"
	}
	return s
}

// parseGemini turns a line of Gemini CLI's stream-json into events. Its reply comes only in pieces
// (Run joins them); a turn ends with a result, "success" or "error".
func parseGemini(line []byte) []Event {
	var m struct {
		Type, Role, Content, Status, Output, Severity, Message string
		ToolName                                               string `json:"tool_name"`
		Parameters                                             map[string]any
		Error                                                  struct{ Message string }
	}
	if json.Unmarshal(line, &m) != nil {
		return nil
	}
	switch m.Type {
	case "message":
		if m.Role == "assistant" && m.Content != "" {
			return []Event{{Kind: KindDelta, Text: m.Content}}
		}
	case "tool_use":
		return []Event{{Kind: KindTool, Text: toolSummary(m.ToolName, m.Parameters)}}
	case "tool_result":
		text := m.Output
		if m.Status == "error" {
			text = "error: " + m.Error.Message
		}
		if len(text) > maxResult {
			text = text[:maxResult] + "\n…"
		}
		return []Event{{Kind: KindResult, Text: strings.TrimRight(text, "\n ")}}
	case "error": // a warning, or the reason a result "error" follows
		return []Event{{Kind: KindTool, Text: m.Severity + ": " + m.Message}}
	case "result":
		if m.Status == "success" {
			return []Event{{Kind: KindDone}}
		}
		msg := m.Error.Message
		if msg == "" {
			msg = "Gemini stopped with an error (see the line above)"
		}
		return []Event{{Kind: KindError, Text: msg}}
	}
	return nil
}

// geminiSession reports whether Gemini CLI has saved this session: it keeps each one in
// ~/.gemini/tmp/<project>/chats/session-<time>-<first 8 of the id>.jsonl.
// ponytail: assumes ~/.gemini; read GEMINI_CLI_HOME too if someone moves it.
func geminiSession(id string) bool {
	if len(id) < 8 {
		return false
	}
	home, _ := os.UserHomeDir()
	found, _ := filepath.Glob(filepath.Join(home, ".gemini", "tmp", "*", "chats", "session-*-"+id[:8]+".json*"))
	return len(found) > 0
}

// toolSummary writes a tool call as "Bash: go test ./...": the tool and what it was given, in full
// (a long command or prompt is cut at 2000 characters).
func toolSummary(name string, input map[string]any) string {
	for _, k := range []string{"command", "file_path", "absolute_path", "pattern", "path", "dir_path", "url", "description", "prompt"} {
		if v, ok := input[k].(string); ok && v != "" {
			if len(v) > 2000 {
				v = v[:2000] + "…"
			}
			return name + ": " + strings.TrimSpace(v)
		}
	}
	return name
}
