// Package home is the crew assistant: it turns what you type in the home chat into task actions.
package home

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"crew/internal/chat"
	"crew/internal/task"
)

// Action is one change crew should make. Which fields matter depends on Op.
type Action struct {
	Op          string `json:"op"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Repo        string `json:"repo"`
	Agent       string `json:"agent"`
	Description string `json:"description"`
}

type Reply struct {
	Reply   string   `json:"reply"`
	Actions []Action `json:"actions"`
}

const schema = `{"type":"object","required":["reply","actions"],"properties":{
"reply":{"type":"string","description":"One or two sentences shown to the user"},
"actions":{"type":"array","items":{"type":"object","required":["op"],"properties":{
"op":{"type":"string","enum":["attach","create","update","start","open","delete"]},
"id":{"type":"string","description":"Existing task id"},
"title":{"type":"string","description":"Short imperative task title"},
"repo":{"type":"string","description":"Repo path, or the folder name of an attached repo"},
"agent":{"type":"string"},
"description":{"type":"string","description":"Full instructions for the coding agent"}}}}}}`

const system = `You are crew, a terminal assistant that manages coding tasks. Each task gets its own git worktree and a coding agent that works on it.
Turn the user's message into actions plus a short reply.

Actions:
- attach: repo = path of a git repo to add (e.g. ~/code/app).
- create: title, repo (attached repo path or folder name, or a new path), agent (default claude), description = detailed instructions for the coding agent written from what the user said.
- update: id plus only the fields to change. description replaces the whole description, so keep any old text that still applies.
- start: id. To start a task created in this same reply, leave id empty.
- open: id. Switches the user to that task's chat.
- delete: id.

Rules: only use ids from the task list. If something essential is unclear (for example which repo), ask in reply and return no actions. Never say you did something you did not include as an action. To talk to a task's agent the user opens that task's chat.`

// Ask sends the message, with the current tasks and recent home chat as context, to a fast Claude model.
func Ask(ctx context.Context, dir string, tasks []task.Task, repos, agents []string, history []chat.Message, msg string) (Reply, error) {
	var p strings.Builder
	p.WriteString("Attached repos:\n")
	for _, r := range repos {
		fmt.Fprintf(&p, "- %s (name: %s)\n", r, filepath.Base(r))
	}
	fmt.Fprintf(&p, "\nAgents: %s\n\nTasks:\n", strings.Join(agents, ", "))
	for _, t := range tasks {
		fmt.Fprintf(&p, "- id: %s | title: %s | repo: %s | agent: %s | status: %s\n", t.ID, t.Title, t.Repo, t.Agent, t.Status)
		if t.Body != "" {
			fmt.Fprintf(&p, "  description: %s\n", strings.ReplaceAll(t.Body, "\n", "\n  "))
		}
	}
	p.WriteString("\nConversation so far:\n")
	for _, m := range history {
		role := m.Role
		if role == chat.Agent {
			role = "crew"
		}
		fmt.Fprintf(&p, "%s: %s\n", role, m.Text)
	}
	fmt.Fprintf(&p, "\nNew message from the user:\n%s\n", msg)

	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", "--tools", "",
		"--strict-mcp-config", "--setting-sources", "", "--no-session-persistence",
		"--system-prompt", system, "--output-format", "json", "--json-schema", schema)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(p.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var res struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Output  *Reply `json:"structured_output"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		if err == nil {
			err = jerr
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = errors.New(s)
		}
		return Reply{}, err
	}
	if res.IsError || res.Output == nil {
		return Reply{}, fmt.Errorf("crew assistant failed: %s", res.Result)
	}
	return *res.Output, nil
}
