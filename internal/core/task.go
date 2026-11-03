package core

import (
	"crew/internal/agent"
	"crew/internal/tasks"
	"crew/internal/usage"
)

// AgentName is the agent a task's chat runs: Claude unless it picked another.
func AgentName(t tasks.Task) string {
	if t.Agent == "" {
		return agent.Claude.Name
	}
	return t.Agent
}

// Brief is what an agent is told about its task before your first message. A section's chat keeps
// its whole brief in the body.
func Brief(t tasks.Task) string {
	if t.Section() {
		return t.Body
	}
	s := "Task: " + t.Title
	if t.Body != "" {
		s += "\n\n" + t.Body
	}
	return s
}

// Resumable reports whether the task's session has had a turn and can be resumed.
func Resumable(t tasks.Task) bool { return t.Session != "" && HasSession(t) }

// HasSession reports whether the task's session has had a turn, in its agent's own records.
func HasSession(t tasks.Task) bool {
	if a, ok := agent.Get(AgentName(t)); ok && a.HasSession != nil {
		return a.HasSession(t.Session)
	}
	return usage.Exists(t.Session)
}
