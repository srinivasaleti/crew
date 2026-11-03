package app

import (
	"strings"

	"crew/internal/tasks"
	"crew/internal/ui/terminal/widgets/chat"
)

// runUI is /ui [web]: switch to the web UI (docs/web-ui.md). It asks first, since the app closes this
// crew, with what's running in it, and opens the web one; the page has a button back.
func runUI(m *Model, t *tasks.Task, arg string) {
	arg = strings.TrimSpace(arg)
	m.add(t.ID, chat.You, strings.TrimSpace("/ui "+arg))
	switch arg {
	case "web":
		m.openSwitch()
	case "", "terminal":
		m.add(t.ID, chat.Tool, "this is the terminal UI · /ui web switches to the web UI (also in the ⌘⇧P palette)")
	default:
		m.add(t.ID, chat.Error, "say /ui web")
	}
}
