package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Each task can have up to maxShells interactive shells, stacked in a panel on the right of the
// chat. A shell runs your login shell in the task's worktree inside a pseudo-terminal; vt10x keeps
// its screen and crew draws it. ⇧→/⇧← move between the task list, the chat and the shells, ⇧↑/⇧↓
// between shells, and ctrl+g cycles chat → shell 1 → … → chat; while a shell has focus every
// other key goes to it. `exit` closes a shell's pane.

const maxShells = 4

type shellTerm struct {
	dir        string // where it started
	ptmx       *os.File
	in         *ptyWriter // keys and emulator replies, written without blocking the UI
	vt         vt10x.Terminal
	cols, rows int
	wait       tea.Cmd // delivers the next output or exit message

	dirty    atomic.Bool // the screen changed since it was last drawn
	paste    atomic.Bool // the program asked for bracketed paste
	drawn    string      // last drawing, reused while nothing changed
	drawnFor string      // size and focus the drawing was made for

	// Scrollback: vt10x keeps only the screen, so crew keeps the lines that scroll off its top.
	mu        sync.Mutex
	history   []string // drawn lines that scrolled off, oldest first
	prevPlain []string // the last screen, as text and as drawn, to see what scrolled off
	prevDrawn []string
	scroll    int // lines scrolled back from the live screen; 0 shows it live (UI goroutine only)
}

const maxHistory = 5000

// ptyWriter queues writes to the shell on a goroutine. Writing straight to the pty blocks once
// its small input buffer is full (the shell is busy and not reading), which froze crew while you
// typed or pasted into a busy shell.
type ptyWriter struct {
	ch   chan []byte
	done chan struct{}
}

func newPtyWriter(f *os.File) *ptyWriter {
	w := &ptyWriter{ch: make(chan []byte, 1024), done: make(chan struct{})}
	go func() {
		for {
			select {
			case b := <-w.ch:
				f.Write(b)
			case <-w.done:
				return
			}
		}
	}()
	return w
}

// Write never blocks. ponytail: drops input once 1024 writes are queued behind a shell that
// isn't reading; a bigger queue only delays the same outcome.
func (w *ptyWriter) Write(p []byte) (int, error) {
	select {
	case w.ch <- append([]byte(nil), p...):
	case <-w.done:
	default:
	}
	return len(p), nil
}

func (w *ptyWriter) Close() { close(w.done) }

type (
	shellOutputMsg struct{ st *shellTerm }
	shellExitMsg   struct {
		id string
		st *shellTerm
	}
)

func shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// shellKey is whose shells show: the selected task's, or home's (on the home screen and group rows).
func (m Model) shellKey() string {
	if t := m.current(); t != nil {
		return t.ID
	}
	return homeID
}

// taskShells are the shells of what's selected, top to bottom.
func (m Model) taskShells() []*shellTerm { return m.shells[m.shellKey()] }

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
	cmd := exec.CommandContext(m.cfg.Ctx, "/bin/sh", "-c", `stty -ixon 2>/dev/null; exec "$0" -l`, shell())
	cmd.Dir = dir
	st, err := m.spawn(cmd, key)
	if err != nil {
		m.notice = "couldn't start a shell: " + err.Error()
		return nil
	}
	st.dir = dir
	m.shells[key] = append(m.shells[key], st)
	m.focusOn(areaShell, len(m.shells[key])-1)
	m.layout()
	m.refresh()
	return st.wait
}

// spawn runs cmd in a pseudo-terminal whose screen vt10x keeps. Its output and exit arrive as
// shellOutputMsg / shellExitMsg through st.wait.
func (m *Model) spawn(cmd *exec.Cmd, id string) (*shellTerm, error) {
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24}) // layout() fits it next
	if err != nil {
		return nil, err
	}
	st := &shellTerm{ptmx: ptmx, in: newPtyWriter(ptmx), cols: 80, rows: 24}
	// Replies the program asks for (cursor position reports) go back through the same queue, so the
	// emulator never blocks while holding its screen lock.
	st.vt = vt10x.New(vt10x.WithWriter(st.in), vt10x.WithSize(80, 24))

	notify := make(chan struct{}, 1) // coalesces bursts of output into one redraw
	exited := make(chan error, 1)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				st.vt.Write(buf[:n])
				st.track()
				if i := bytes.LastIndex(buf[:n], []byte("\x1b[?2004")); i >= 0 && i+7 < n {
					st.paste.Store(buf[i+7] == 'h')
				}
				st.dirty.Store(true)
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
	st.wait = func() tea.Msg {
		if _, ok := <-notify; ok {
			return shellOutputMsg{st}
		}
		<-exited
		return shellExitMsg{id, st}
	}
	return st, nil
}

// onShellOutput only needs to keep listening: the view reads the emulator when it redraws.
func (m *Model) onShellOutput(msg shellOutputMsg) tea.Cmd { return msg.st.wait }

// onShellExit removes a closed shell's pane; focus returns to the chat if it was on that shell.
func (m *Model) onShellExit(msg shellExitMsg) {
	msg.st.in.Close()
	msg.st.ptmx.Close()
	list := m.shells[msg.id]
	for i, st := range list {
		if st != msg.st {
			continue
		}
		m.shells[msg.id] = append(list[:i:i], list[i+1:]...)
		if msg.id == m.currentID() {
			switch {
			case m.shellFocus == i:
				m.focusChat()
			case m.shellFocus > i:
				m.shellFocus-- // the focused shell moved up one pane
			}
		}
		break
	}
	if len(m.shells[msg.id]) == 0 {
		delete(m.shells, msg.id)
	}
	m.layout()
	m.refresh()
}

// stopShells ends all of a task's shells, e.g. when the task is deleted.
func (m *Model) stopShells(id string) {
	for _, st := range m.shells[id] {
		st.in.Close()
		st.ptmx.Close() // the shell gets SIGHUP and exits
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
		st, rows := list[i], max(h-2, 1)
		if st.cols == cols && st.rows == rows {
			continue
		}
		st.cols, st.rows = cols, rows
		st.vt.Resize(cols, rows)
		st.dirty.Store(true)
		pty.Setsize(st.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
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
	m.taskShells()[m.shellFocus].send(msg)
	if msg.String() == "ctrl+l" {
		return m, tea.ClearScreen // the shell clears itself; crew repaints everything too
	}
	return m, nil
}

// send writes a key (or a paste) to the program in the terminal. A paste is wrapped in the
// bracketed-paste markers when the program asked for them, so editors don't auto-indent it.
func (st *shellTerm) send(k tea.KeyMsg) {
	if st.scroll > 0 { // typing goes back to the live screen
		st.scroll = 0
		st.dirty.Store(true)
	}
	b := keyBytes(k, st.vt.Mode()&vt10x.ModeAppCursor != 0)
	if b == "" {
		return
	}
	if k.Paste && st.paste.Load() {
		b = "\x1b[200~" + b + "\x1b[201~"
	}
	st.in.Write([]byte(b))
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

// shellPanel draws the selected task's shells as framed panes stacked top to bottom.
func (m Model) shellPanel() string {
	list := m.taskShells()
	w := m.panelWidth()
	var parts []string
	for i, h := range paneHeights(m.bodyHeight(), len(list)) {
		focused := i == m.shellFocus
		title := fmt.Sprintf("shell %d · %s", i+1, tildify(list[i].dir))
		parts = append(parts, frame(list[i].screen(w-2, max(h-2, 1), focused), w, h, title, focused))
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

// screen returns the shell's drawing, redrawing only when its output, size or focus changed.
func (st *shellTerm) screen(cols, rows int, focused bool) string {
	key := fmt.Sprint(cols, rows, focused, st.scroll)
	if st.dirty.Swap(false) || key != st.drawnFor {
		st.drawn, st.drawnFor = renderScreen(st.vt, cols, rows, focused && st.scroll == 0), key
		if st.scroll > 0 {
			st.drawn = st.scrolledBack(st.drawn, cols, rows)
		}
	}
	return st.drawn
}

// scrolledBack is the screen moved st.scroll lines back into the history, with a note on top.
func (st *shellTerm) scrolledBack(live string, cols, rows int) string {
	st.mu.Lock()
	lines := append(append([]string{}, st.history...), strings.Split(live, "\n")...)
	st.mu.Unlock()
	end := len(lines) - st.scroll
	view := lines[max(end-rows, 0):end]
	for i := range view {
		view[i] = ansi.Truncate(view[i], cols, "")
	}
	view[0] = dim.Render(ansi.Truncate(fmt.Sprintf("↑ %d lines back · scroll down or type to return", st.scroll), cols, "…"))
	return strings.Join(view, "\n")
}

// track keeps the lines that just scrolled off the top of the screen.
// ponytail: compares each screen with the one before; output that scrolls a whole screen or more
// between two reads loses those lines. Full-screen programs (vim, less) keep no history.
func (st *shellTerm) track() {
	st.vt.Lock()
	cols, rows := st.vt.Size()
	alt := st.vt.Mode()&vt10x.ModeAltScreen != 0
	st.vt.Unlock()
	if alt {
		st.prevPlain, st.prevDrawn = nil, nil
		return
	}
	drawn := strings.Split(renderScreen(st.vt, cols, rows, false), "\n")
	plain := make([]string, len(drawn))
	for i, l := range drawn {
		plain[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	if k := scrolledBy(st.prevPlain, plain); k > 0 {
		st.mu.Lock()
		st.history = append(st.history, st.prevDrawn[:k]...)
		st.history = st.history[max(len(st.history)-maxHistory, 0):]
		st.mu.Unlock()
	}
	st.prevPlain, st.prevDrawn = plain, drawn
}

// scrolledBy is how many lines the screen moved up from prev to cur, or 0.
func scrolledBy(prev, cur []string) int {
	n := len(cur)
	if len(prev) != n {
		return 0
	}
	for k := 1; k < n; k++ {
		same, text := true, false
		for i := 0; i < n-k && same; i++ {
			same = prev[i+k] == cur[i]
			text = text || cur[i] != ""
		}
		if same && text { // blank screens "match" any shift
			return k
		}
	}
	return 0
}

// renderScreen turns the emulator's cells into ANSI text, one SGR sequence per run of same-styled
// cells. The cursor shows only in the focused shell.
func renderScreen(vt vt10x.Terminal, cols, rows int, focused bool) string {
	vt.Lock()
	defer vt.Unlock()
	vc, vr := vt.Size()
	cur := vt.Cursor()
	showCursor := focused && vt.CursorVisible()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		last := "-"
		for x := 0; x < cols; x++ {
			g := vt10x.Glyph{Char: ' ', FG: vt10x.DefaultFG, BG: vt10x.DefaultBG}
			if x < vc && y < vr {
				g = vt.Cell(x, y)
			}
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
			m.notice = keyNames("No shells yet · ctrl+t opens one")
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
