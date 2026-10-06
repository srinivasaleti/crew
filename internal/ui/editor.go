package ui

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// The Files tab's editor: one terminal editor per task, in a pseudo-terminal like the shells,
// drawn to the right of the tree. It is CREW_EDITOR, else micro (a normal, non-modal editor:
// ctrl+s saves, ctrl+q quits), else nano, else nvim. ⇧← or ctrl+] returns to the tree, ⇧→ comes back.
// nvim listens on a socket, so picking another file opens it in the same nvim; the others can't,
// so you save and quit first. With none installed, enter suspends crew and runs $EDITOR.

func nvimBin() string {
	p, _ := exec.LookPath("nvim")
	return p
}

// editorArgv is the editor to embed, or nil when none is installed.
func editorArgv() []string {
	if v := strings.Fields(os.Getenv("CREW_EDITOR")); len(v) > 0 {
		return v
	}
	for _, name := range []string{"micro", "nano", "nvim"} {
		if p, _ := exec.LookPath(name); p != "" {
			return []string{p}
		}
	}
	return nil
}

func isNvim(argv []string) bool { return filepath.Base(argv[0]) == "nvim" }

func editorSock(id string) string {
	h := fnv.New32a()
	h.Write([]byte(id))
	return filepath.Join(os.TempDir(), fmt.Sprintf("crew-nvim-%08x.sock", h.Sum32()))
}

// nvimRemote sends args to the task's running nvim without blocking the UI.
func nvimRemote(id string, args ...string) {
	go exec.Command(nvimBin(), append([]string{"--server", editorSock(id)}, args...)...).Run()
}

// editorTab reports whether the selected task's Files tab has an nvim running.
func (m Model) editorTab() bool {
	return m.tab == tabFiles && m.current() != nil && m.editors[m.currentID()] != nil
}

func (m Model) editActive() bool { return m.editFocus && m.editorTab() }

// openInEditor opens p (relative to the worktree) in the task's nvim, starting it if needed, and
// gives it the keys.
func (m *Model) openInEditor(p string) tea.Cmd {
	t, ft := m.current(), m.tree()
	argv := editorArgv()
	if argv == nil {
		return m.editFile()
	}
	dir := m.taskDir(*t)
	abs := filepath.Join(dir, p)
	if m.editors[t.ID] != nil {
		m.editFocus = true
		if !isNvim(argv) {
			m.notice = "Save and quit the editor first (ctrl+s, ctrl+q), then open the other file"
			return nil
		}
		ft.file = p
		nvimRemote(t.ID, "--remote", abs)
		return nil
	}
	ft.file = p
	args := append(argv[1:], abs)
	if isNvim(argv) {
		sock := editorSock(t.ID)
		os.Remove(sock)
		args = []string{"--listen", sock, abs}
	}
	cmd := exec.CommandContext(m.cfg.Ctx, argv[0], args...)
	cmd.Dir = dir
	st, err := m.spawn(cmd, t.ID, true)
	if err != nil {
		m.notice = "couldn't start " + filepath.Base(argv[0]) + ": " + err.Error()
		return nil
	}
	m.editors[t.ID], m.editFocus = st, true
	m.layout()
	return st.wait
}

// updateEditor sends a key to nvim.
func (m Model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+]" {
		m.editFocus = false
		return m, nil
	}
	st := m.editors[m.currentID()]
	if b := keyBytes(msg, st.vt.Mode()&vt10x.ModeAppCursor != 0); b != "" {
		st.in.Write([]byte(b))
	}
	if msg.String() == "ctrl+l" {
		return m, tea.ClearScreen
	}
	return m, nil
}

// resizeEditor fits nvim to the right-hand side of the Files tab.
func (m *Model) resizeEditor() {
	if !m.editorTab() {
		return
	}
	st, cols, rows := m.editors[m.currentID()], m.vp.Width+2, m.vp.Height // the viewer, a gap and the scroll bar
	if st.cols == cols && st.rows == rows {
		return
	}
	st.cols, st.rows = cols, rows
	st.vt.Resize(cols, rows)
	st.dirty.Store(true)
	pty.Setsize(st.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// reloadEditor makes nvim pick up files the agent changed on disk (micro and nano ask on their own).
func (m *Model) reloadEditor(id string) {
	if argv := editorArgv(); m.editors[id] != nil && argv != nil && isNvim(argv) {
		nvimRemote(id, "--remote-send", "<Cmd>checktime<CR>")
	}
}

func (m *Model) onEditorExit(msg shellExitMsg) {
	msg.st.in.Close()
	msg.st.ptmx.Close()
	if m.editors[msg.id] != msg.st {
		return
	}
	delete(m.editors, msg.id)
	os.Remove(editorSock(msg.id))
	if msg.id != m.currentID() {
		return
	}
	m.editFocus = false
	if m.tab == tabFiles {
		m.layout()
		m.loadFiles() // the viewer takes the right-hand side back
	}
}

// stopEditor ends a task's nvim, e.g. when the task is deleted.
func (m *Model) stopEditor(id string) {
	if st := m.editors[id]; st != nil {
		st.in.Close()
		st.ptmx.Close()
		delete(m.editors, id)
		os.Remove(editorSock(id))
	}
}
