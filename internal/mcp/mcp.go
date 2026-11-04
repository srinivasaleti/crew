// Package mcp is MCP: the MCP servers Claude Code has (claude.ai connectors such as Jira, Outlook,
// GitHub and Slack, plugins' servers, and your own), read and changed with `claude mcp`, which
// costs no tokens; and the gate that makes an agent ask you before an MCP call changes anything
// (gate.go). Claude Code holds the servers and their sign-ins; crew keeps nothing but the gate's files.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"crew/internal/proc"
)

// Server is one MCP server as `claude mcp list` reports it.
type Server struct {
	Name   string `json:"name"`   // "claude.ai Atlassian", "plugin:productivity:slack", "my-db"
	Target string `json:"target"` // its URL, or the command it runs
	Status string `json:"status"` // connected, signin, failed, off
	Detail string `json:"detail"` // what claude said about it ("Needs authentication", the error)
	Source string `json:"source"` // claude.ai (a connector), plugin, or yours
	Title  string `json:"title"`  // the name to show: "Atlassian", "slack"
}

var listLine = regexp.MustCompile(`^(.+?): (.*) - (✔|!|✘|-) (.*)$`)

var statuses = map[string]string{"✔": "connected", "!": "signin", "✘": "failed", "-": "off"}

// Parse reads `claude mcp list`'s output.
func Parse(out string) []Server {
	var list []Server
	for _, l := range strings.Split(out, "\n") {
		m := listLine.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		s := Server{Name: m[1], Target: strings.TrimSpace(m[2]), Status: statuses[m[3]], Detail: strings.TrimSpace(m[4]), Source: "yours", Title: m[1]}
		switch {
		case strings.HasPrefix(s.Name, "claude.ai "):
			s.Source, s.Title = "claude.ai", strings.TrimPrefix(s.Name, "claude.ai ")
		case strings.HasPrefix(s.Name, "plugin:"):
			parts := strings.Split(s.Name, ":")
			s.Source, s.Title = "plugin "+parts[1], parts[len(parts)-1]
		}
		list = append(list, s)
	}
	return list
}

// claude runs `claude mcp …` and returns what it printed.
func claude(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := proc.Command(ctx, "claude", append([]string{"mcp"}, args...)...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("claude mcp %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// List is every MCP server Claude Code has, with whether it's connected (it checks each one, so it
// takes a few seconds).
func List() ([]Server, error) {
	out, err := claude(90*time.Second, "list")
	if err != nil {
		return nil, err
	}
	return Parse(out), nil
}

var serverName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Add adds one of your own servers for every project (user scope): a URL (http) or a command
// (stdio, its words split on spaces).
func Add(name, target string) error {
	name, target = strings.TrimSpace(name), strings.TrimSpace(target)
	switch {
	case !serverName.MatchString(name):
		return errors.New("a name is letters, digits, - and _")
	case target == "":
		return errors.New("give its URL, or the command that runs it")
	case strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://"):
		_, err := claude(30*time.Second, "add", "--transport", "http", "--scope", "user", name, target)
		return err
	}
	_, err := claude(30*time.Second, append([]string{"add", "--scope", "user", name, "--"}, strings.Fields(target)...)...)
	return err
}

// Remove removes one of your own servers (a connector or a plugin's isn't yours to remove here).
func Remove(name string) error {
	if strings.HasPrefix(name, "claude.ai ") || strings.HasPrefix(name, "plugin:") || !serverName.MatchString(name) {
		return errors.New("only your own servers can be removed here")
	}
	_, err := claude(30*time.Second, "remove", "--scope", "user", name)
	return err
}

// Connect is how to sign in to a server: a link to open (a claude.ai connector), or what to do in
// Claude Code (a plugin's server or yours; its sign-in opens the browser).
func Connect(s Server) (url, how string) {
	if s.Source == "claude.ai" {
		return "https://claude.ai/settings/connectors", "Connect " + s.Title + " in claude.ai's Connectors, then Check again."
	}
	return "", "In a terminal run claude, type /mcp, pick " + s.Name + " and sign in. Then Check again."
}

// ChatBrief is what the MCP chat's Claude is told first.
func ChatBrief() string {
	return "You're in crew's MCP chat. Use my connected MCP servers (Jira, Outlook, GitHub, Slack and the rest) to find and read what I ask about, and say where it came from. " +
		"Reading is fine. Anything that changes something (posting, sending, commenting, creating, updating) needs my OK: crew stops the call and shows me Allow or Deny, " +
		"so say in a sentence what you want to do and why, then stop. You can't run commands or change files."
}

// ChatArgs are the MCP chat's: its MCP servers and crew's plugin (whose hook is the gate), and no
// commands or file edits.
func ChatArgs(plugin string) []string {
	var a []string
	if plugin != "" {
		a = append(a, "--plugin-dir", plugin)
	}
	return append(a, "--disallowedTools", "Bash", "Write", "Edit", "NotebookEdit")
}
