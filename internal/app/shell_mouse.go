package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"
)

// Mouse support for the shells: clicks, drags and
// the wheel are sent to the program inside when it asked for them (its mouse mode is on), a click
// also gives that terminal the keys, and the wheel falls back to arrow keys for full-screen programs
// that don't take the mouse.

// termHit is a terminal under the mouse and where its screen starts.
type termHit struct {
	st     *shellTerm
	ox, oy int
	shell  int // index in the task's shells
} // first row of a tab's content: frame border, header, tab bar

func (m Model) termAt(x, y int) (termHit, bool) {
	if m.panelWidth() > 0 {
		ox, top := m.sideOuter()+m.chatWidth()+3, 0 // inside the shell frames
		for i, h := range paneHeights(m.bodyHeight(), len(m.taskShells())) {
			st := m.taskShells()[i]
			if x >= ox && x < ox+st.cols && y > top && y <= top+h-2 {
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
	if wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown; wheel &&
		h.st.vt.Mode()&(vt10x.ModeMouseMask|vt10x.ModeAltScreen) == 0 { // a plain shell: scroll its history
		h.st.mu.Lock()
		n := len(h.st.history)
		h.st.mu.Unlock()
		if msg.Button == tea.MouseButtonWheelUp {
			h.st.scroll = min(h.st.scroll+3, n)
		} else {
			h.st.scroll = max(h.st.scroll-3, 0)
		}
		h.st.dirty.Store(true)
		return true
	}
	if b := mouseBytes(msg, h.st, msg.X-h.ox, msg.Y-h.oy); b != "" {
		h.st.in.Write([]byte(b))
	}
	return true
}

// mouseBytes is what a terminal sends for a mouse event at zero-based cell (col, row).
func mouseBytes(msg tea.MouseMsg, st *shellTerm, col, row int) string {
	mode := st.vt.Mode()
	if mode&vt10x.ModeMouseMask == 0 {
		if mode&vt10x.ModeAltScreen != 0 { // a full-screen program without mouse support
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				return "\x1b[A\x1b[A\x1b[A"
			case tea.MouseButtonWheelDown:
				return "\x1b[B\x1b[B\x1b[B"
			}
		}
		return ""
	}
	var b int
	switch msg.Button {
	case tea.MouseButtonLeft:
	case tea.MouseButtonMiddle:
		b = 1
	case tea.MouseButtonRight:
		b = 2
	case tea.MouseButtonWheelUp:
		b = 64
	case tea.MouseButtonWheelDown:
		b = 65
	case tea.MouseButtonNone:
		if msg.Action != tea.MouseActionRelease {
			return "" // hovering with no button held
		}
	default:
		return ""
	}
	release := msg.Action == tea.MouseActionRelease
	if msg.Action == tea.MouseActionMotion {
		if mode&(vt10x.ModeMouseMotion|vt10x.ModeMouseMany) == 0 {
			return ""
		}
		b += 32
	}
	if msg.Shift {
		b += 4
	}
	if msg.Alt {
		b += 8
	}
	if msg.Ctrl {
		b += 16
	}
	if mode&vt10x.ModeMouseSgr != 0 {
		end := "M"
		if release {
			end = "m"
		}
		return fmt.Sprintf("\x1b[<%d;%d;%d%s", b, col+1, row+1, end)
	}
	if release {
		b = b&^3 | 3 // the old encoding doesn't say which button was let go
	}
	if col+33 > 255 || row+33 > 255 {
		return ""
	}
	return "\x1b[M" + string([]byte{byte(32 + b), byte(33 + col), byte(33 + row)})
}
