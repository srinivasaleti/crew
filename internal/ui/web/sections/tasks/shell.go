package tasks

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"crew/internal/core"
	"crew/internal/ui/web/api"
)

// A task's shells (⌘T in the page, up to maxShells): your login shell in the task's folder, in a pty,
// its output streamed to xterm.js as server-sent events. Each lives until you exit it or crew quits
// (closing the pty hangs it up), so leaving the page or switching tasks keeps it.
func init() { api.Register(api.Module{Key: "shells", Routes: shellRoutes}) }

// maxShells is how many shells a task can have open at once, as in the terminal UI.
const maxShells = 4

// keep is how much of a shell's output it keeps, so a page that (re)connects sees the screen again.
const keep = 256 << 10

// shell is one running shell: its pty, its recent output, and the pages reading it.
type shell struct {
	pty  *os.File
	done chan struct{} // closed once it has ended and left shells
	pid  int           // the shell's process, which leads its own process group (pty.Start makes a session)
	mu   sync.Mutex
	buf  []byte
	subs map[chan []byte]bool
}

// shells are the running shells, by key: the task id, #, and the shell's number (1…maxShells).
var shells = struct {
	sync.Mutex
	m map[string]*shell
}{m: map[string]*shell{}}

// startShell starts a login shell in dir and reads it until it exits; id is its key in shells.
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
	s := &shell{pty: f, pid: cmd.Process.Pid, done: make(chan struct{}), subs: map[chan []byte]bool{}}
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
		close(s.done)
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

// hangUp ends the shell as closing a terminal window does: SIGHUP to its process group, so what runs
// in it (a server, an editor) ends too. It waits until the shell is gone (or 2s), so a page that asks
// for the running shells next doesn't see it again.
func (s *shell) hangUp() {
	syscall.Kill(-s.pid, syscall.SIGHUP)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

// running is the shell with that key, if it's running.
func running(key string) *shell {
	shells.Lock()
	defer shells.Unlock()
	return shells.m[key]
}

// numbers are the task's running shells' numbers, lowest first.
func numbers(id string) []int {
	shells.Lock()
	defer shells.Unlock()
	ns := []int{}
	for n := 1; n <= maxShells; n++ {
		if shells.m[fmt.Sprintf("%s#%d", id, n)] != nil {
			ns = append(ns, n)
		}
	}
	return ns
}

func shellRoutes(c *core.Core, mux *http.ServeMux) {
	key := func(r *http.Request) string { return r.PathValue("id") + "#" + r.PathValue("n") }
	// The task's running shells, by number, so a page that opens (or reloads) shows them all.
	mux.HandleFunc("GET /api/tasks/{id}/shells", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, numbers(r.PathValue("id")))
	})
	// A new shell in the task's folder, under the lowest free number.
	mux.HandleFunc("POST /api/tasks/{id}/shells", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		dir, err := c.Dir(id)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		if dir == "" {
			dir, _ = os.UserHomeDir() // a task that hasn't started has no folder yet
		}
		shells.Lock()
		n := 1
		for n <= maxShells && shells.m[fmt.Sprintf("%s#%d", id, n)] != nil {
			n++
		}
		if n > maxShells {
			shells.Unlock()
			api.Fail(w, fmt.Errorf("up to %d shells per task; exit one first", maxShells), 400)
			return
		}
		k := fmt.Sprintf("%s#%d", id, n)
		s, err := startShell(k, dir)
		if err == nil {
			shells.m[k] = s
		}
		shells.Unlock()
		if err != nil {
			api.Fail(w, fmt.Errorf("couldn't start a shell: %v", err), 500)
			return
		}
		api.Reply(w, map[string]int{"n": n})
	})
	// Its output: "reset", what's kept, then what comes, base64 per event; "exit" when it ends.
	mux.HandleFunc("GET /api/tasks/{id}/shells/{n}/out", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		s := running(key(r))
		if !ok || s == nil {
			api.Fail(w, fmt.Errorf("no shell"), 404)
			return
		}
		ch := make(chan []byte, 256)
		s.mu.Lock()
		if s.subs == nil { // it just ended
			s.mu.Unlock()
			api.Fail(w, fmt.Errorf("no shell"), 404)
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
					if running(key(r)) != s {
						fmt.Fprint(w, "event: exit\ndata: \n\n")
						fl.Flush()
					}
					return // else dropped for being slow: the page reconnects
				}
				fmt.Fprintf(w, "data: %s\n\n", base64.StdEncoding.EncodeToString(out))
				fl.Flush()
			}
		}
	})
	// Keys you type.
	mux.HandleFunc("POST /api/tasks/{id}/shells/{n}/in", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Data string }
		if !api.Body(w, r, &m) {
			return
		}
		s := running(key(r))
		if s == nil {
			api.Fail(w, fmt.Errorf("no shell"), 404)
			return
		}
		s.pty.Write([]byte(m.Data))
		api.Reply(w, struct{}{})
	})
	// The panel's size, so full-screen programs fit.
	mux.HandleFunc("POST /api/tasks/{id}/shells/{n}/size", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Cols, Rows uint16 }
		if !api.Body(w, r, &m) {
			return
		}
		if s := running(key(r)); s != nil && m.Cols > 0 && m.Rows > 0 {
			pty.Setsize(s.pty, &pty.Winsize{Rows: m.Rows, Cols: m.Cols})
		}
		api.Reply(w, struct{}{})
	})
	// Close it (its ×): hang up, as closing a terminal window does.
	mux.HandleFunc("DELETE /api/tasks/{id}/shells/{n}", func(w http.ResponseWriter, r *http.Request) {
		if s := running(key(r)); s != nil {
			s.hangUp()
		}
		api.Reply(w, struct{}{})
	})
}
