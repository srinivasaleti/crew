package app

import (
	"crew/internal/widgets/term"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse support for the shells: clicks, drags and
// the wheel are sent to the program inside when it asked for them (its mouse mode is on), a click
// also gives that terminal the keys, and the wheel falls back to arrow keys for full-screen programs
// that don't take the mouse.

// termHit is a terminal under the mouse and where its screen starts.
type termHit struct {
	st     *term.Term
	ox, oy int
	shell  int // index in the task's shells
} // first row of a tab's content: frame border, header, tab bar

func (m Model) termAt(x, y int) (termHit, bool) {
	if m.panelWidth() > 0 {
		ox, top := m.sideOuter()+m.chatWidth()+3, 0 // inside the shell frames
		for i, h := range paneHeights(m.bodyHeight(), len(m.taskShells())) {
			st := m.taskShells()[i]
			if x >= ox && x < ox+st.Cols() && y > top && y <= top+h-2 {
				return termHit{st, ox, top + 1, i}, true
			}
			top += h
		}
	}
	return termHit{}, false
}

// mouseTerm sends a mouse event to the terminal under it and reports whether it did.
func (m *Model) mouseTerm(msg tea.MouseMsg) bool {
	h, ok := m.termAt(msg.X, msg.Y)
	if !ok {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.editFocus = false // a click elsewhere takes the keys back from the editor
		}
		return false
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.focusOn(areaShell, h.shell)
	}
	h.st.Mouse(msg, msg.X-h.ox, msg.Y-h.oy)
	return true
}
