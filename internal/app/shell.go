package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/keys"
	"crew/internal/widgets/term"
)

// Each task can have up to maxShells interactive shells, stacked in a panel on the right of the
// chat. A shell runs your login shell in the task's worktree inside a pseudo-terminal; vt10x keeps
// its screen and crew draws it. ⇧→/⇧← move between the task list, the chat and the shells, ⇧↑/⇧↓
// between shells, and ctrl+g cycles chat → shell 1 → … → chat; while a shell has focus every
// other key goes to it. `exit` closes a shell's pane.

const maxShells = 4

// shellKey is whose shells show: the selected task's, or home's (on the home screen and group rows).
func (m Model) shellKey() string {
	if t := m.current(); t != nil {
		return t.ID
	}
	return homeID
}

// taskShells are the shells of what's selected, top to bottom.
func (m Model) taskShells() []*term.Term { return m.shells[m.shellKey()] }

// paneHeights splits the body height between n panes; each pane is a title line plus its screen.
func paneHeights(total, n int) []int {
	hs := make([]int, n)
	for i := range hs {
		hs[i] = total / n
	}
	hs[n-1] += total - total/n*n
	return hs
}

// addShell opens another shell for the selected task and gives it focus.
// It starts in the task's worktree (or its repo before the task starts), or in your home folder
// on the home screen.
func (m *Model) addShell() tea.Cmd {
	key, dir := m.shellKey(), ""
	if t := m.current(); t != nil {
		dir = m.taskDir(*t)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if len(m.shells[key]) >= maxShells {
		m.notice = fmt.Sprintf("Up to %d shells here · ⌘W or exit closes one", maxShells)
		return nil
	}
	// stty -ixon turns off Ctrl+S "pause output" (XOFF), which otherwise makes a shell look frozen
	// until Ctrl+Q; then the login shell replaces sh. Killed when crew quits.
	cmd := exec.CommandContext(m.cfg.Ctx, "/bin/sh", "-c", `stty -ixon 2>/dev/null; exec "$0" -l`, term.Shell())
	cmd.Dir = dir
	st, err := term.Start(cmd, key)
	if err != nil {
		m.notice = "couldn't start a shell: " + err.Error()
		return nil
	}
	st.Dir = dir
	m.shells[key] = append(m.shells[key], st)
	m.focusOn(areaShell, len(m.shells[key])-1)
	m.layout()
	m.refresh()
	return st.Wait
}

// onShellOutput only needs to keep listening: the view reads the emulator when it redraws.
func (m *Model) onShellOutput(msg term.OutputMsg) tea.Cmd { return msg.T.Wait }

// onShellExit removes a closed shell's pane. If it had the keys, the next shell gets them (the
// main pane when it was the last one).
func (m *Model) onShellExit(msg term.ExitMsg) {
	msg.T.Close()
	list := m.shells[msg.ID]
	for i, st := range list {
		if st != msg.T {
			continue
		}
		left := append(list[:i:i], list[i+1:]...)
		m.shells[msg.ID] = left
		if msg.ID == m.shellKey() { // its shells are on screen
			switch {
			case m.shellFocus == i && len(left) > 0:
				m.focusOn(areaShell, min(i, len(left)-1)) // the next shell, or the last one
			case m.shellFocus == i:
				m.focusOn(areaMain, -1) // no shells left
			case m.shellFocus > i:
				m.focusOn(areaShell, m.shellFocus-1) // the focused shell moved up one pane
			}
		}
		break
	}
	if len(m.shells[msg.ID]) == 0 {
		delete(m.shells, msg.ID)
	}
	m.layout()
	m.refresh()
}

// stopShells ends all of a task's shells, e.g. when the task is deleted.
func (m *Model) stopShells(id string) {
	for _, st := range m.shells[id] {
		st.HangUp()
	}
	delete(m.shells, id)
	m.stopEditor(id)
}

// resizeShells fits the selected task's shells to their panes.
func (m *Model) resizeShells() {
	list := m.taskShells()
	if len(list) == 0 || m.panelWidth() == 0 { // hidden while the main pane is maximised
		return
	}
	cols := m.panelWidth() - 2
	for i, h := range paneHeights(m.bodyHeight(), len(list)) {
		list[i].Resize(cols, max(h-2, 1))
	}
}

// focusNext moves focus chat → shell 1 → … → last shell → chat (ctrl+g).
func (m *Model) focusNext() {
	n := len(m.taskShells())
	switch {
	case n == 0:
		m.notice = "No terminals yet · /shell opens one in the task's worktree"
	case m.shellFocus+1 < n:
		m.shellFocus++
		m.lastShell = m.shellFocus
		m.input.Blur()
	default:
		m.focusChat()
	}
}

func (m *Model) focusChat() { m.focusOn(areaMain, -1) }

// shellActive reports whether keys go to a shell (ctrl+g and ⇧+arrows move focus).
func (m Model) shellActive() bool {
	return m.shellFocus >= 0 && m.shellFocus < len(m.taskShells())
}

// updateShell sends a key to the focused shell; ctrl+g moves focus on (and back to the chat).
func (m Model) updateShell(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+g":
		m.focusNext()
		return m, nil
	case "ctrl+]":
		m.focusChat()
		return m, nil
	}
	m.taskShells()[m.shellFocus].Send(msg)
	if msg.String() == "ctrl+l" {
		return m, tea.ClearScreen // the shell clears itself; crew repaints everything too
	}
	return m, nil
}

// shellPanel draws the selected task's shells as framed panes stacked top to bottom.
func (m Model) shellPanel() string {
	list := m.taskShells()
	w := m.panelWidth()
	var parts []string
	for i, h := range paneHeights(m.bodyHeight(), len(list)) {
		focused := i == m.shellFocus
		title := fmt.Sprintf("shell %d · %s", i+1, tildify(list[i].Dir))
		parts = append(parts, frame(list[i].Screen(w-2, max(h-2, 1), focused), w, h, title, focused))
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }

// shellAt is the shell pane at row y of the body, or -1.
func (m Model) shellAt(y int) int {
	top := 0
	for i, h := range paneHeights(m.bodyHeight(), len(m.taskShells())) {
		if y >= top && y < top+h {
			return i
		}
		top += h
	}
	return -1
}

// moveArea handles ⇧+arrows (and ⌘⌥+arrows), which move between areas from anywhere, even inside
// a shell: the task list ← main pane (its editor) → shells, ↑/↓ between shells. It reports whether
// the key was used.
func (m *Model) moveArea(k string) bool {
	n := len(m.taskShells())
	if m.editActive() {
		return false // ⇧+arrows select text in the editor; esc leaves it
	}
	switch k {
	case "shift+right":
		switch {
		case m.listFocus:
			m.focusOn(areaMain, -1)
		case m.shellActive():
			// already in the rightmost area
		case m.editorTab():
			m.focusOn(areaEditor, -1) // the Files tab's tree, then its editor
		case n == 0:
			m.notice = keys.Names("No shells yet · ctrl+t opens one")
		default:
			m.focusOn(areaShell, min(m.lastShell, n-1))
		}
	case "shift+left":
		switch {
		case m.shellActive():
			m.focusOn(areaMain, -1)
		case !m.listFocus:
			m.focusOn(areaTasks, -1)
		}
	case "shift+up", "shift+down":
		if !m.shellActive() {
			return false
		}
		if k == "shift+up" {
			m.focusOn(areaShell, max(m.shellFocus-1, 0))
		} else {
			m.focusOn(areaShell, min(m.shellFocus+1, n-1))
		}
	default:
		return false
	}
	return true
}
