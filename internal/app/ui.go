package app

import (
	"strings"

	"crew/internal/core"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// runUI is /ui [web|terminal]: choose the UI the app opens next time (docs/web-ui.md), or say which.
func runUI(m *Model, t *tasks.Task, arg string) {
	arg = strings.TrimSpace(arg)
	m.add(t.ID, chat.You, strings.TrimSpace("/ui "+arg))
	if arg == "" {
		m.add(t.ID, chat.Tool, "the app opens the "+core.UIMode(m.cfg.Home)+" UI · /ui web or /ui terminal changes it")
		return
	}
	if err := core.SetUIMode(m.cfg.Home, arg); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	m.add(t.ID, chat.Tool, "the app will open the "+arg+" UI · quit crew and open it again")
}
