package app

// The window's areas that can have the keys. Every move between them goes through focusOn, so the
// flags that record it (listFocus, editFocus, shellFocus) always agree, and the message box has the
// cursor only when the chat has the keys.

type area int

const (
	areaTasks  area = iota // the task list
	areaMain               // the main pane: the chat, a tab, a group's overview
	areaEditor             // the Files tab's editor
	areaShell              // a shell, by index
)

func (m *Model) focusOn(a area, shell int) {
	if a == areaTasks && m.zoom { // the task list comes back
		m.zoom = false
		defer m.layout()
	}
	m.listFocus = a == areaTasks
	m.editFocus = a == areaEditor && m.editorTab()
	m.shellFocus = -1
	if a == areaShell && shell >= 0 && shell < len(m.taskShells()) {
		m.shellFocus, m.lastShell = shell, shell
	}
	if a == areaMain && m.chatOpen() && m.modal == noModal {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	if m.current() == nil {
		m.refresh() // a group's overview draws its bar for whoever has the keys
	}
}

// area is who has the keys now.
func (m Model) area() area {
	switch {
	case m.listFocus:
		return areaTasks
	case m.shellActive():
		return areaShell
	case m.editActive():
		return areaEditor
	}
	return areaMain
}
