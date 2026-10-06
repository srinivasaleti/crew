package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"

	"crew/internal/task"
)

// The Terminal tab is a real interactive shell per task, running in the task's worktree inside a
// pseudo-terminal. vt10x keeps the screen; crew draws it in the main pane. The tab has two modes:
// viewing (tab keys switch tabs as usual) and typing (every key goes to the shell). enter, /shell
// or a click starts typing; ctrl+g (or ctrl+]) goes back to viewing. The shell runs until it exits or crew quits.

type shellTerm struct {
	ptmx       *os.File
	vt         vt10x.Terminal
	cols, rows int
	exited     bool
	exitErr    error
	wait       tea.Cmd // delivers the next output or exit message
}

type (
	shellOutputMsg struct{ id string }
	shellExitMsg   struct {
		id  string
		err error
	}
)

func shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// termSize is the screen area of the Terminal tab: the main pane below the header and tab bar,
// above the one-line hint.
func (m Model) termSize() (cols, rows int) {
	return max(m.w-sidebarWidth-3, 10), max(m.h-2-1-2, 3)
}

// startShell launches your login shell in the task's worktree and copies its output into the
// emulator. Output and exit arrive as shellOutputMsg and shellExitMsg.
func (m *Model) startShell(t task.Task) tea.Cmd {
	cols, rows := m.termSize()
	cmd := exec.CommandContext(m.cfg.Ctx, shell(), "-l") // killed when crew quits
	cmd.Dir = t.Worktree
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		m.notice = "couldn't start a shell: " + err.Error()
		return nil
	}
	st := &shellTerm{ptmx: ptmx, cols: cols, rows: rows}
	// Replies the shell asks for (cursor position reports) are written back to it.
	st.vt = vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(cols, rows))
	m.shells[t.ID] = st

	notify := make(chan struct{}, 1) // coalesces bursts of output into one redraw
	exited := make(chan error, 1)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				st.vt.Write(buf[:n])
				select {
				case notify <- struct{}{}:
				default:
				}
			}
			if err != nil {
				break
			}
		}
		exited <- cmd.Wait()
		close(notify)
	}()
	id := t.ID
	st.wait = func() tea.Msg {
		if _, ok := <-notify; ok {
			return shellOutputMsg{id}
		}
		return shellExitMsg{id, <-exited}
	}
	return st.wait
}

// onShellOutput only needs to keep listening: the view reads the emulator when it redraws.
func (m *Model) onShellOutput(msg shellOutputMsg) tea.Cmd {
	if st := m.shells[msg.id]; st != nil {
		return st.wait
	}
	return nil
}

func (m *Model) onShellExit(msg shellExitMsg) {
	if st := m.shells[msg.id]; st != nil {
		st.exited, st.exitErr = true, msg.err
		st.ptmx.Close()
	}
}

// stopShell ends a task's shell, e.g. when the task is deleted.
func (m *Model) stopShell(id string) {
	if st := m.shells[id]; st != nil && !st.exited {
		st.ptmx.Close() // the shell gets SIGHUP and exits
	}
	delete(m.shells, id)
}

// resizeShells keeps every shell's screen the size of the Terminal tab.
func (m *Model) resizeShells() {
	cols, rows := m.termSize()
	for _, st := range m.shells {
		if st.exited || (st.cols == cols && st.rows == rows) {
			continue
		}
		st.cols, st.rows = cols, rows
		st.vt.Resize(cols, rows)
		pty.Setsize(st.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
}

// openTerminal shows the Terminal tab, starting the task's shell if it isn't running. focus
// starts typing into it right away.
func (m *Model) openTerminal(focus bool) tea.Cmd {
	t := m.current()
	if t == nil {
		return nil
	}
	m.tab = tabTerminal
	m.input.Blur()
	m.layout()
	m.shellFocus = focus && t.Worktree != ""
	if st := m.shells[t.ID]; t.Worktree == "" || (st != nil && !st.exited) {
		return nil
	}
	return m.startShell(*t)
}

// shellRunning reports whether the selected task has a live shell on the Terminal tab.
func (m Model) shellRunning() bool {
	t := m.current()
	if t == nil || m.tab != tabTerminal {
		return false
	}
	st := m.shells[t.ID]
	return st != nil && !st.exited
}

// shellActive reports whether keys go to the shell (typing mode).
func (m Model) shellActive() bool { return m.shellFocus && m.shellRunning() }

// updateShell sends a key to the shell; ctrl+g (or ctrl+]) goes back to viewing mode so tab keys
// work again. ctrl+g is a plain control key, so it arrives in every terminal without setup.
func (m Model) updateShell(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k := msg.String(); k == "ctrl+g" || k == "ctrl+]" {
		m.shellFocus = false
		return m, nil
	}
	st := m.shells[m.current().ID]
	if b := keyBytes(msg, st.vt.Mode()&vt10x.ModeAppCursor != 0); b != "" {
		st.ptmx.WriteString(b)
	}
	return m, nil
}

// updateTerminalIdle handles keys on the Terminal tab in viewing mode: enter starts typing into
// the shell (starting one if needed); the usual keys switch tabs.
func (m Model) updateTerminalIdle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		return m, m.openTerminal(true)
	}
	return m.updateTabKeys(msg)
}

// keyBytes turns a key into what a terminal sends for it.
func keyBytes(k tea.KeyMsg, appCursor bool) string {
	prefix := ""
	if k.Alt {
		prefix = "\x1b"
	}
	arrow := func(c string) string {
		if appCursor { // full-screen apps (vim, less) ask for this form
			return "\x1bO" + c
		}
		return "\x1b[" + c
	}
	switch k.Type {
	case tea.KeyRunes:
		return prefix + string(k.Runes)
	case tea.KeySpace:
		return prefix + " "
	case tea.KeyUp:
		return arrow("A")
	case tea.KeyDown:
		return arrow("B")
	case tea.KeyRight:
		return arrow("C")
	case tea.KeyLeft:
		return arrow("D")
	case tea.KeyCtrlUp:
		return "\x1b[1;5A"
	case tea.KeyCtrlDown:
		return "\x1b[1;5B"
	case tea.KeyCtrlRight:
		return "\x1b[1;5C"
	case tea.KeyCtrlLeft:
		return "\x1b[1;5D"
	case tea.KeyHome:
		return "\x1b[H"
	case tea.KeyEnd:
		return "\x1b[F"
	case tea.KeyPgUp:
		return "\x1b[5~"
	case tea.KeyPgDown:
		return "\x1b[6~"
	case tea.KeyDelete:
		return "\x1b[3~"
	case tea.KeyInsert:
		return "\x1b[2~"
	case tea.KeyShiftTab:
		return "\x1b[Z"
	}
	if k.Type >= 0 && k.Type < 128 { // enter, tab, backspace, esc and ctrl+letters are their control codes
		return prefix + string(rune(k.Type))
	}
	return ""
}

// shellView draws the selected task's terminal screen, or how to start one.
func (m Model) shellView() string {
	t := m.current()
	st := m.shells[t.ID]
	cols, rows := m.termSize()
	switch {
	case t.Worktree == "":
		return dim.Render("No worktree yet. Send a message in the Chat tab to start the task; the terminal opens in its worktree.")
	case st == nil:
		return dim.Render("Press enter to start a shell in " + tildify(t.Worktree) + ".")
	case st.exited:
		reason := "exited"
		if st.exitErr != nil {
			reason = "exited: " + st.exitErr.Error()
		}
		return dim.Render(fmt.Sprintf("The shell %s. Press enter to start a new one in %s.", reason, tildify(t.Worktree)))
	}
	return renderScreen(st.vt, cols, rows)
}

// renderScreen turns the emulator's cells into ANSI text, one SGR sequence per run of same-styled cells.
func renderScreen(vt vt10x.Terminal, cols, rows int) string {
	vt.Lock()
	defer vt.Unlock()
	vc, vr := vt.Size()
	cur := vt.Cursor()
	showCursor := vt.CursorVisible()
	var b strings.Builder
	for y := 0; y < min(rows, vr); y++ {
		last := "-"
		for x := 0; x < min(cols, vc); x++ {
			g := vt.Cell(x, y)
			if sgr := sgrFor(g, showCursor && x == cur.X && y == cur.Y); sgr != last {
				b.WriteString("\x1b[0" + sgr + "m")
				last = sgr
			}
			if g.Char == 0 {
				g.Char = ' '
			}
			b.WriteRune(g.Char)
		}
		b.WriteString("\x1b[0m")
		if y < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// vt10x's cell attribute bits (unexported there).
const (
	cellReverse   = 1 << 0
	cellUnderline = 1 << 1
	cellBold      = 1 << 2
	cellItalic    = 1 << 4
)

func sgrFor(g vt10x.Glyph, cursor bool) string {
	var s strings.Builder
	if g.Mode&cellBold != 0 {
		s.WriteString(";1")
	}
	if g.Mode&cellItalic != 0 {
		s.WriteString(";3")
	}
	if g.Mode&cellUnderline != 0 {
		s.WriteString(";4")
	}
	// vt10x applies reverse video by swapping colours; with default colours that needs SGR 7.
	reversedDefaults := g.FG == vt10x.DefaultBG && g.BG == vt10x.DefaultFG
	if reversedDefaults != cursor {
		s.WriteString(";7")
	}
	if !reversedDefaults {
		s.WriteString(colorSGR(g.FG, 30))
		s.WriteString(colorSGR(g.BG, 40))
	}
	return s.String()
}

// colorSGR is the SGR parameter for a vt10x colour; base is 30 for foreground, 40 for background.
func colorSGR(c vt10x.Color, base int) string {
	switch {
	case c >= vt10x.DefaultFG:
		return ""
	case c < 8:
		return fmt.Sprintf(";%d", base+int(c))
	case c < 16:
		return fmt.Sprintf(";%d", base+60+int(c)-8)
	case c < 256:
		return fmt.Sprintf(";%d;5;%d", base+8, c)
	default:
		return fmt.Sprintf(";%d;2;%d;%d;%d", base+8, c>>16&0xff, c>>8&0xff, c&0xff)
	}
}
