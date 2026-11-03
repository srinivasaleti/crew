package web

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"

	"crew/internal/core"
)

// A task's shell (⌘T in the page): your login shell in the task's folder, in a pty, its output
// streamed to xterm.js as server-sent events. It lives until you exit it or crew quits (closing the
// pty hangs it up), so leaving the page or switching tasks keeps it.
// ponytail: one shell per task; a list per task like the terminal UI's if one isn't enough.
func init() { modules = append(modules, module{key: "shells", routes: shellRoutes}) }

// keep is how much of a shell's output it keeps, so a page that (re)connects sees the screen again.
const keep = 256 << 10

// shell is one running shell: its pty, its recent output, and the pages reading it.
type shell struct {
	pty  *os.File
	mu   sync.Mutex
	buf  []byte
	subs map[chan []byte]bool
}

// shells are the running shells, by task id.
var shells = struct {
	sync.Mutex
	m map[string]*shell
}{m: map[string]*shell{}}

// startShell starts a login shell in dir and reads it until it exits.
func startShell(id, dir string) (*shell, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	// stty -ixon turns off Ctrl+S "pause output", which otherwise makes a shell look frozen; then the
	// login shell replaces sh. Not proc.Command: a shell has its own pty, which is the point.
	cmd := exec.Command("/bin/sh", "-c", `stty -ixon 2>/dev/null; exec "$0" -l`, sh)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return nil, err
	}
	s := &shell{pty: f, subs: map[chan []byte]bool{}}
	go func() {
		b := make([]byte, 32<<10)
		for {
			n, err := f.Read(b)
			if n > 0 {
				s.write(append([]byte(nil), b[:n]...))
			}
			if err != nil {
				break
			}
		}
		cmd.Wait()
		f.Close()
		shells.Lock()
		if shells.m[id] == s {
			delete(shells.m, id)
		}
		shells.Unlock()
		s.mu.Lock()
		for ch := range s.subs {
			close(ch) // the page hears the shell ended
		}
		s.subs = nil
		s.mu.Unlock()
	}()
	return s, nil
}

// write keeps out and hands it to every page reading. A page too slow to keep up is dropped; it
// reconnects and redraws from what's kept.
func (s *shell) write(out []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, out...)
	if len(s.buf) > keep {
		s.buf = append([]byte(nil), s.buf[len(s.buf)-keep:]...)
	}
	for ch := range s.subs {
		select {
		case ch <- out:
		default:
			close(ch)
			delete(s.subs, ch)
		}
	}
}

// running is the task's shell, if it has one.
func running(id string) *shell {
	shells.Lock()
	defer shells.Unlock()
	return shells.m[id]
}

func shellRoutes(c *core.Core, mux *http.ServeMux) {
	// Open the task's shell: the running one, or a new one in its folder.
	mux.HandleFunc("POST /api/tasks/{id}/shell", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if running(id) != nil {
			reply(w, struct{}{})
			return
		}
		dir, err := c.Dir(id)
		if dir == "" {
			dir, _ = os.UserHomeDir() // a task that hasn't started has no folder yet
		}
		if err != nil {
			fail(w, err, 400)
			return
		}
		s, err := startShell(id, dir)
		if err != nil {
			fail(w, fmt.Errorf("couldn't start a shell: %v", err), 500)
			return
		}
		shells.Lock()
		shells.m[id] = s
		shells.Unlock()
		reply(w, struct{}{})
	})
	// Its output: "reset", what's kept, then what comes, base64 per event; "exit" when it ends.
	mux.HandleFunc("GET /api/tasks/{id}/shell/out", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		s := running(r.PathValue("id"))
		if !ok || s == nil {
			fail(w, fmt.Errorf("no shell"), 404)
			return
		}
		ch := make(chan []byte, 256)
		s.mu.Lock()
		if s.subs == nil { // it just ended
			s.mu.Unlock()
			fail(w, fmt.Errorf("no shell"), 404)
			return
		}
		kept := append([]byte(nil), s.buf...)
		s.subs[ch] = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			if s.subs[ch] {
				delete(s.subs, ch)
			}
			s.mu.Unlock()
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprintf(w, "event: reset\ndata: %s\n\n", base64.StdEncoding.EncodeToString(kept))
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case out, ok := <-ch:
				if !ok {
					if running(r.PathValue("id")) == nil {
						fmt.Fprint(w, "event: exit\ndata: \n\n")
						fl.Flush()
					}
					return // dropped for being slow: the page reconnects
				}
				fmt.Fprintf(w, "data: %s\n\n", base64.StdEncoding.EncodeToString(out))
				fl.Flush()
			}
		}
	})
	// Keys you type.
	mux.HandleFunc("POST /api/tasks/{id}/shell/in", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Data string }
		if !body(w, r, &m) {
			return
		}
		s := running(r.PathValue("id"))
		if s == nil {
			fail(w, fmt.Errorf("no shell"), 404)
			return
		}
		s.pty.Write([]byte(m.Data))
		reply(w, struct{}{})
	})
	// The panel's size, so full-screen programs fit.
	mux.HandleFunc("POST /api/tasks/{id}/shell/size", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Cols, Rows uint16 }
		if !body(w, r, &m) {
			return
		}
		if s := running(r.PathValue("id")); s != nil && m.Cols > 0 && m.Rows > 0 {
			pty.Setsize(s.pty, &pty.Winsize{Rows: m.Rows, Cols: m.Cols})
		}
		reply(w, struct{}{})
	})
	// Close it (the panel's ×): hang up, as closing a terminal window does.
	mux.HandleFunc("DELETE /api/tasks/{id}/shell", func(w http.ResponseWriter, r *http.Request) {
		if s := running(r.PathValue("id")); s != nil {
			s.pty.Close()
		}
		reply(w, struct{}{})
	})
}
