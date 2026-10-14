package reminders

import (
	"fmt"
	"time"
)

// The REMINDERS chat runs in crew's chat (a task underneath, like REQUESTS'); this is what's
// particular to it: what Claude is told, and how it's locked down.

// ChatBrief is what the chat's Claude is told first; exe is the crew binary it runs as `crew remind`.
func ChatBrief(exe string, now time.Time) string {
	return fmt.Sprintf("You're in crew's REMINDERS chat. You manage my reminders, and nothing else, with %[1]q remind: "+
		"list [--all]; add --at <when> [--every <repeat>] <what>; edit <id> [--at <when>] [--every <repeat>|never] [--text <what>]; "+
		"delete <id>…; restore <id>…; pause <id>…|all [--until <when>]; resume <id>…|all. "+
		"<when> is 30m, 4pm, 16:30, tomorrow 9am, fri 5pm, nov 1 10am or 2026-11-01 10:00. "+
		"<repeat> is day, weekday, week, month, year, 2 weeks, 3 months, or weekdays like mon,wed,fri. "+
		"Run list first: it shows the time now and the ids. Quote multi-word values. After changing anything, say in a short list exactly what changed, "+
		"with each reminder's next time. Ask before deleting more than one, or when what I mean is unclear. "+
		"It's %[2]s now. Nothing else runs here.", exe, now.Format("Monday, Jan 2 2006, 3:04pm"))
}

// ChatArgs lock the chat's Claude down: no files, no MCP servers or user settings (--restricted,
// --strict-mcp-config), and no command but `crew remind`. Anything else is refused, not asked
// (dontAsk, no prompts).
func ChatArgs(exe string) []string {
	return []string{"--restricted", "--strict-mcp-config", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--tools", "Bash", "--allowedTools", "Bash(" + exe + " remind:*)"}
}
