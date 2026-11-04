package mcp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The gate: an agent asks you before an MCP call changes anything. crew's plugin runs `crew mcp-gate`
// before every MCP tool call (plugin/hooks/hooks.json). A call that only reads goes ahead. One that
// writes (send, post, create, update…) is stopped unless you allowed it, and written to the chat's
// pending file; crew shows it in the chat with Allow and Deny. Allow once lets the next such call
// through; Always, every call to that tool in that chat.
//
// The files are under <home>/mcp, by the agent's session id (what the hook knows of the chat):
// allow/<session> ("once <tool>" or "always <tool>" lines) and pending/<session> (a call a line).

// Pending is a call the gate stopped, waiting for you.
type Pending struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	At    time.Time       `json:"at"`
}

var (
	reads  = []string{"get", "list", "search", "read", "fetch", "find", "query", "view", "describe", "lookup", "count", "show", "retrieve", "info", "whoami", "check", "download", "resolve"}
	writes = []string{"create", "update", "delete", "send", "post", "add", "edit", "remove", "set", "merge", "close", "reply", "comment", "transition", "assign", "upload", "write", "move", "archive", "publish", "approve", "invite", "schedule", "cancel", "run", "execute", "trigger", "put", "patch", "react", "mark", "submit", "share", "forward", "draft", "import", "push", "fork", "rename", "lock", "unlock", "pin", "star", "subscribe", "join", "leave"}
	word   = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+`)
)

// IsRead reports whether an MCP tool only reads, from its name (mcp__<server>__<tool>): a word like
// get, list or search and none like send, create or update. When unsure it's a write, so you're asked.
func IsRead(tool string) bool {
	name := tool[strings.LastIndex(tool, "__")+1:]
	var ws []string
	for _, w := range word.FindAllString(name, -1) {
		ws = append(ws, strings.ToLower(w))
	}
	read := false
	for _, w := range ws {
		if slices.Contains(writes, w) {
			return false
		}
		read = read || slices.Contains(reads, w)
	}
	return read
}

func allowPath(home, session string) string   { return filepath.Join(home, "mcp", "allow", session) }
func pendingPath(home, session string) string { return filepath.Join(home, "mcp", "pending", session) }

// hookInput is what Claude Code gives a PreToolUse hook on stdin.
type hookInput struct {
	Session string          `json:"session_id"`
	Tool    string          `json:"tool_name"`
	Input   json.RawMessage `json:"tool_input"`
}

// decision is the hook's answer on stdout.
func decision(allow bool, reason string) []byte {
	d := "deny"
	if allow {
		d = "allow"
	}
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName": "PreToolUse", "permissionDecision": d, "permissionDecisionReason": reason}})
	return b
}

// Gate is `crew mcp-gate`: it reads the hook's input and writes its decision. Anything but an MCP
// call is left to Claude Code (no output).
func Gate(home string, in io.Reader, out io.Writer) {
	var h hookInput
	if json.NewDecoder(in).Decode(&h) != nil || !strings.HasPrefix(h.Tool, "mcp__") {
		return
	}
	if IsRead(h.Tool) {
		out.Write(decision(true, "crew: reading is fine"))
		return
	}
	if h.Session != "" && takeAllow(home, h.Session, h.Tool) {
		out.Write(decision(true, "crew: the user allowed it"))
		return
	}
	if h.Session != "" {
		os.MkdirAll(filepath.Dir(pendingPath(home, h.Session)), 0o755)
		if f, err := os.OpenFile(pendingPath(home, h.Session), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			b, _ := json.Marshal(Pending{h.Tool, h.Input, time.Now()})
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	out.Write(decision(false, "crew: "+h.Tool+" changes something outside, so it needs the user's OK. Don't retry it or work around it: "+
		"say in a sentence what you wanted to do and why, then stop. crew shows the user Allow and Deny; if they allow it, you'll be told to go ahead."))
}

// takeAllow reports whether you allowed this call, using up an Allow once.
func takeAllow(home, session, tool string) bool {
	b, err := os.ReadFile(allowPath(home, session))
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i, l := range lines {
		switch l {
		case "always " + tool:
			return true
		case "once " + tool:
			lines = slices.Delete(lines, i, i+1)
			os.WriteFile(allowPath(home, session), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
			return true
		}
	}
	return false
}

// Allow lets the chat's next call to tool through, or (always) every one.
func Allow(home, session, tool string, always bool) error {
	if err := os.MkdirAll(filepath.Dir(allowPath(home, session)), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(allowPath(home, session), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	kind := "once "
	if always {
		kind = "always "
	}
	_, err = f.WriteString(kind + tool + "\n")
	return err
}

// TakePending are the calls the gate stopped in a session since last asked, and clears them.
func TakePending(home, session string) []Pending {
	b, err := os.ReadFile(pendingPath(home, session))
	if err != nil {
		return nil
	}
	os.Remove(pendingPath(home, session))
	var out []Pending
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var p Pending
		if json.Unmarshal([]byte(l), &p) == nil && p.Tool != "" {
			out = append(out, p)
		}
	}
	return out
}
