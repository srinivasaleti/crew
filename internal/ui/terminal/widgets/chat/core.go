package chat

import "crew/internal/core"

// The chat's log and what a task's agent is told live in core, which both UIs use; the chat widget
// uses them by these names.
type (
	Message = core.Message
	Log     = core.Log
)

const (
	You    = core.You
	Agent  = core.Agent
	Tool   = core.Tool
	Error  = core.Error
	Result = core.Result
)

var (
	AgentName  = core.AgentName
	Brief      = core.Brief
	Resumable  = core.Resumable
	HasSession = core.HasSession
)
