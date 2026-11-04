package core

import (
	"encoding/json"
	"errors"
	"strings"

	"crew/internal/mcp"
)

// MCP calls that wait for your OK. The gate (mcp.Gate, run by crew's plugin before each MCP call)
// stops a call that would change something and keeps it as pending for the chat's session; after
// the turn, crew puts each in the chat as an Ask, and AnswerAsk lets it through or not.

// asks puts the MCP calls the gate stopped in the task's last turn into its chat.
func (c *Core) asks(id string) {
	t, err := c.task(id)
	if err != nil || t.Session == "" {
		return
	}
	for _, p := range mcp.TakePending(c.cfg.Home, t.Session) {
		b, _ := json.Marshal(p)
		c.say(id, Ask, string(b), false)
	}
}

// AnswerAsk answers an MCP call waiting in the task's chat: "once" or "always" (every call to that
// tool in this chat) allows it and tells the agent to go ahead; "deny" tells it not to.
func (c *Core) AnswerAsk(id, tool, answer string) error {
	t, err := c.task(id)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(tool, "mcp__") || t.Session == "" {
		return errors.New("nothing to allow")
	}
	switch answer {
	case "once", "always":
		if err := mcp.Allow(c.cfg.Home, t.Session, tool, answer == "always"); err != nil {
			return err
		}
		return c.Send(id, "I allowed "+tool+". Go ahead with it as you planned.")
	case "deny":
		return c.Send(id, "Don't use "+tool+" for that. Leave it, and tell me what you'd do instead.")
	}
	return errors.New("once, always or deny")
}
