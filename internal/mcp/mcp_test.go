package mcp

import (
	"bytes"
	"strings"
	"testing"
)

// TestParse checks `claude mcp list` lines read into servers, with their source and status.
func TestParse(t *testing.T) {
	out := `Checking MCP server health…

claude.ai Atlassian: https://mcp.atlassian.com/v1/mcp - ✔ Connected
plugin:productivity:slack: https://mcp.slack.com/mcp (HTTP) - ! Needs authentication
plugin:productivity:gmail:  (HTTP) - - Not configured
my-db: npx db-mcp --ro - ✘ Failed to connect`
	l := Parse(out)
	if len(l) != 4 || l[0].Title != "Atlassian" || l[0].Source != "claude.ai" || l[0].Status != "connected" ||
		l[1].Title != "slack" || l[1].Status != "signin" || l[1].Source != "plugin productivity" ||
		l[2].Status != "off" || l[3].Source != "yours" || l[3].Target != "npx db-mcp --ro" || l[3].Status != "failed" {
		t.Errorf("%+v", l)
	}
}

// TestIsRead checks reads go ahead and anything that may write asks, from the tool's name.
func TestIsRead(t *testing.T) {
	for tool, want := range map[string]bool{
		"mcp__claude_ai_Atlassian__getJiraIssue":             true,
		"mcp__claude_ai_Atlassian__searchJiraIssuesUsingJql": true,
		"mcp__claude_ai_Microsoft_365__outlook_email_search": true,
		"mcp__claude_ai_Atlassian__addCommentToJiraIssue":    false,
		"mcp__plugin_productivity_slack__slack_send_message": false,
		"mcp__github__create_pull_request":                   false,
		"mcp__claude_ai_Atlassian__transitionJiraIssue":      false,
		"mcp__x__do_thing": false, // unsure: ask
		"mcp__claude_ai_Microsoft_365__teams_list_chats":       true,
		"mcp__claude_ai_Atlassian__getTransitionsForJiraIssue": true,
	} {
		if IsRead(tool) != want {
			t.Errorf("%s: %v", tool, !want)
		}
	}
}

// TestGate checks a write is stopped and kept as pending, Allow once lets one through, Always every
// one, and a read or a non-MCP tool isn't stopped.
func TestGate(t *testing.T) {
	home := t.TempDir()
	gate := func(tool string) string {
		var out bytes.Buffer
		Gate(home, strings.NewReader(`{"session_id":"s1","tool_name":"`+tool+`","tool_input":{"text":"hi"}}`), &out)
		return out.String()
	}
	send := "mcp__slack__send_message"
	if !strings.Contains(gate(send), `"deny"`) {
		t.Fatal("a write went through")
	}
	if p := TakePending(home, "s1"); len(p) != 1 || p[0].Tool != send || string(p[0].Input) != `{"text":"hi"}` || TakePending(home, "s1") != nil {
		t.Errorf("pending %+v", p)
	}
	Allow(home, "s1", send, false)
	if !strings.Contains(gate(send), `"allow"`) || !strings.Contains(gate(send), `"deny"`) {
		t.Error("Allow once")
	}
	Allow(home, "s1", send, true)
	if !strings.Contains(gate(send), `"allow"`) || !strings.Contains(gate(send), `"allow"`) {
		t.Error("Always")
	}
	if !strings.Contains(gate("mcp__jira__getIssue"), `"allow"`) || gate("Bash") != "" {
		t.Error("a read, or Bash")
	}
}
