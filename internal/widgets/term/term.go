// Package term is a terminal crew embeds: a program (your login shell, an install or a sign-in) in
// a pseudo-terminal whose screen vt10x keeps, drawn by crew, with scrollback, keys and the mouse.
package term

import (
	"bytes"
	"crew/internal/widgets/look"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Term is one terminal.
type Term struct {
	Dir        string      // where it started
	Fix        bool        // it runs an install or sign-in; crew checks again when it exits
	proc       *os.Process // the shell; its process group is everything running in it
	ptmx       *os.File
	in         *ptyWriter // keys and emulator replies, written without blocking the UI
	vt         vt10x.Terminal
	cols, rows int
	Wait       tea.Cmd // delivers the next output or exit message

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
	ch    chan []byte
	done  chan struct{}
	close sync.Once
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

// Close stops the writer. A shell's writer is closed when you close the shell (⌘W) and again when
// it exits, so closing twice is fine (closing a channel twice would crash crew).
func (w *ptyWriter) Close() { w.close.Do(func() { close(w.done) }) }

type (
	// OutputMsg is new output from a terminal: crew keeps listening (T.Wait) and redraws.
	OutputMsg struct{ T *Term }
	// ExitMsg is a terminal whose program exited; ID is whose terminals it was in.
	ExitMsg struct {
		ID string
		T  *Term
	}
)

// Shell is your login shell.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// Start runs cmd in a pseudo-terminal whose screen vt10x keeps. Its output and exit arrive as
// OutputMsg / ExitMsg through st.Wait; id is whose terminals it's in.
func Start(cmd *exec.Cmd, id string) (*Term, error) {
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24}) // layout() fits it next
	if err != nil {
		return nil, err
	}
	st := &Term{ptmx: ptmx, in: newPtyWriter(ptmx), cols: 80, rows: 24, proc: cmd.Process}
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
	st.Wait = func() tea.Msg {
		if _, ok := <-notify; ok {
			return OutputMsg{st}
		}
		<-exited
		return ExitMsg{id, st}
	}
	return st, nil
}

// Send writes a key (or a paste) to the program in the terminal. A paste is wrapped in the
// bracketed-paste markers when the program asked for them, so editors don't auto-indent it.
func (st *Term) Send(k tea.KeyMsg) {
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

// Screen returns the shell's drawing, redrawing only when its output, size or focus changed.
func (st *Term) Screen(cols, rows int, focused bool) string {
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
func (st *Term) scrolledBack(live string, cols, rows int) string {
	st.mu.Lock()
	lines := append(append([]string{}, st.history...), strings.Split(live, "\n")...)
	st.mu.Unlock()
	end := len(lines) - st.scroll
	view := lines[max(end-rows, 0):end]
	for i := range view {
		view[i] = ansi.Truncate(view[i], cols, "")
	}
	view[0] = look.Dim.Render(ansi.Truncate(fmt.Sprintf("↑ %d lines back · scroll down or type to return", st.scroll), cols, "…"))
	return strings.Join(view, "\n")
}

// track keeps the lines that just scrolled off the top of the screen.
// ponytail: compares each screen with the one before; output that scrolls a whole screen or more
// between two reads loses those lines. Full-screen programs (vim, less) keep no history.
func (st *Term) track() {
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

// HangUp ends a shell and whatever runs in it, as closing a terminal tab does: a hang-up to its
// process group, then a kill if it's still there a moment later. Its pane goes when it exits.
// (Closing the pseudo-terminal alone doesn't end the shell on macOS.)
func (st *Term) HangUp() {
	pid := st.proc.Pid
	syscall.Kill(-pid, syscall.SIGHUP) // the shell leads its own session and process group
	go func() {
		time.Sleep(2 * time.Second)
		syscall.Kill(-pid, syscall.SIGKILL) // no-op when it's gone
	}()
}

// Mouse gives the terminal a mouse event at zero-based cell (col, row): the program inside gets it
// when it asked for the mouse; in a plain shell the wheel scrolls back through its history.
func (st *Term) Mouse(msg tea.MouseMsg, col, row int) {
	if wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown; wheel &&
		st.vt.Mode()&(vt10x.ModeMouseMask|vt10x.ModeAltScreen) == 0 { // a plain shell: scroll its history
		st.mu.Lock()
		n := len(st.history)
		st.mu.Unlock()
		if msg.Button == tea.MouseButtonWheelUp {
			st.scroll = min(st.scroll+3, n)
		} else {
			st.scroll = max(st.scroll-3, 0)
		}
		st.dirty.Store(true)
		return
	}
	if b := mouseBytes(msg, st, col, row); b != "" {
		st.in.Write([]byte(b))
	}
}

// mouseBytes is what a terminal sends for a mouse event at zero-based cell (col, row).
func mouseBytes(msg tea.MouseMsg, st *Term, col, row int) string {
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

// Cols is how wide the terminal's screen is.
func (st *Term) Cols() int { return st.cols }

// Resize fits the terminal to cols × rows.
func (st *Term) Resize(cols, rows int) {
	if st.cols == cols && st.rows == rows {
		return
	}
	st.cols, st.rows = cols, rows
	st.vt.Resize(cols, rows)
	st.dirty.Store(true)
	pty.Setsize(st.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Write sends bytes to the program, as if typed.
func (st *Term) Write(b []byte) { st.in.Write(b) }

// Close releases a terminal whose program exited.
func (st *Term) Close() {
	st.in.Close()
	st.ptmx.Close()
}

// TakesMouse reports whether the program inside asked for the mouse (vim, less…), so a drag there
// is the program's, not a text selection.
func (st *Term) TakesMouse() bool { return st.vt.Mode()&vt10x.ModeMouseMask != 0 }
