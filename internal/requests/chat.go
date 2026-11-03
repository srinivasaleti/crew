package requests

import "fmt"

// The REQUESTS chat runs in crew's chat (a task underneath, like a PR's chat); this is what's
// particular to it: what Claude is told, and how it's locked down.

// ChatBrief is what the chat's Claude is told first; exe is the crew binary it runs as `crew req`.
func ChatBrief(exe string) string {
	return fmt.Sprintf("You're in crew's REQUESTS chat, in the folder of my HTTP requests: one folder per application, "+
		"each with env/<env>.env and its .http files, grouped in folders (<app>/<folder>/*.http; <app>/*.http is the default folder). "+
		"Follow the crew:requests skill. Your commands: %[1]q req apps (applications and environments; key names only), "+
		"%[1]q req list, and %[1]q req run <app>/[<folder>/]<file>[#n] [--env <name>] (sends a request with the environment's "+
		"auth; crew fills in the keys, you never see them). Nothing else runs here.", exe)
}

// ChatArgs lock the chat's Claude down: file tools only in the requests folder, no MCP servers or
// user settings (--restricted, --strict-mcp-config), and no command but `crew req` and Claude
// Code's own read-only ones (whoami, wc…), which --restricted keeps in the folder. Anything else is
// refused, not asked (dontAsk, no prompts): security, curl, cat outside, even chained. plugin is
// crew's plugin folder, "" for none.
func ChatArgs(exe, plugin string) []string {
	var a []string
	if plugin != "" {
		a = append(a, "--plugin-dir", plugin)
	}
	return append(a, "--restricted", "--strict-mcp-config", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--tools", "Read,Write,Edit,Glob,Grep,Bash",
		"--allowedTools", "Read", "Write", "Edit", "Glob", "Grep", "Bash("+exe+" req:*)")
}
