// Command crew-desktop is crew in its own Mac app window: a terminal (xterm.js in the system web
// view, through Wails) running the crew binary that sits next to it in the app bundle.
//
// It holds no crew features on purpose. Everything crew does lives in the crew binary, built from
// the same source by `make app`, so the app and crew in a terminal always behave the same. What the
// app adds is only what a terminal can't: ⌘ keys, copy and paste, a normal window, and macOS
// notifications in crew's own name (relayed from crew, see relayNotifications).
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS

// App connects the window's terminal to crew running in a pseudo-terminal.
type App struct {
	ctx  context.Context
	cmd  *exec.Cmd
	ptmx *os.File
	done atomic.Bool // crew has quit, so the window may close
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// Start runs crew in a pseudo-terminal of cols × rows. Its output reaches the window as "out"
// events (base64, since it's raw bytes), and the window closes when crew quits.
func (a *App) Start(cols, rows int) error {
	if a.ptmx != nil {
		return nil // the page reloaded; crew is already running
	}
	env := loginEnv()
	a.cmd = exec.Command(crewPath())
	// CREW_APP tells crew it runs in this window, so it hands its notifications to us.
	a.cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "CREW_APP=1")
	go a.relayNotifications(filepath.Join(crewHome(env), "notify"))
	a.cmd.Dir, _ = os.UserHomeDir()
	ptmx, err := pty.StartWithSize(a.cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return err
	}
	a.ptmx = ptmx
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				runtime.EventsEmit(a.ctx, "out", base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		a.cmd.Wait()
		a.done.Store(true)
		runtime.Quit(a.ctx)
	}()
	return nil
}

// Write sends keys (and pastes) to crew.
func (a *App) Write(data string) {
	if a.ptmx != nil {
		a.ptmx.Write([]byte(data))
	}
}

// Resize fits crew to the window.
func (a *App) Resize(cols, rows int) {
	if a.ptmx != nil {
		pty.Setsize(a.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
}

// beforeClose runs on ⌘Q, the Dock's Quit and the window's close button. While crew runs, it hands
// crew ⌘Q instead, which asks "Quit crew?" (y or a second ⌘Q quits); crew quitting then closes the
// window. ponytail: a crew that hangs can't be quit this way; Force Quit (⌥⌘⎋) still can.
func (a *App) beforeClose(context.Context) (prevent bool) {
	if a.ptmx == nil || a.done.Load() {
		return false
	}
	a.Write("\x1b[113;9u") // ⌘Q, as the page sends ⌘ keys
	return true
}

// shutdown asks crew to quit the way a terminal closing would (it stops its agents cleanly), and
// gives up after a few seconds.
func (a *App) shutdown(context.Context) {
	if a.cmd == nil || a.cmd.Process == nil {
		return
	}
	a.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { a.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		a.cmd.Process.Kill()
	}
	a.ptmx.Close()
}

// relayNotifications sends crew's notifications as crew's own (they show under crew in System
// Settings → Notifications), and tells crew whether they're allowed. crew writes each one as a file
// in dir/out ({"title", "subtitle", "body"}), and "ask" or "check" in dir to have permission asked
// for or checked; the answer goes in dir/status: allowed, off, or unavailable: <why>.
// ponytail: polled twice a second; a file watcher would be instant.
func (a *App) relayNotifications(dir string) {
	out := filepath.Join(dir, "out")
	os.MkdirAll(out, 0o755)
	status := func(s string) { os.WriteFile(filepath.Join(dir, "status"), []byte(s+"\n"), 0o644) }
	check := func() {
		if ok, err := runtime.CheckNotificationAuthorization(a.ctx); err != nil {
			status("unavailable: " + err.Error())
		} else if ok {
			status("allowed")
		} else {
			status("off")
		}
	}
	if err := runtime.InitializeNotifications(a.ctx); err != nil {
		status("unavailable: " + err.Error()) // crew then sends them with osascript, as in a terminal
		return
	}
	check()
	for range time.Tick(500 * time.Millisecond) {
		for _, req := range []string{"ask", "check"} {
			p := filepath.Join(dir, req)
			if !fileExists(p) {
				continue
			}
			os.Remove(p)
			if req == "ask" { // macOS asks you once; after that it's System Settings. It waits for you, so not here
				go func() { runtime.RequestNotificationAuthorization(a.ctx); check() }()
				continue
			}
			check()
		}
		files, _ := os.ReadDir(out)
		for _, f := range files {
			if strings.HasPrefix(f.Name(), ".") {
				continue // crew is still writing it
			}
			p := filepath.Join(out, f.Name())
			data, err := os.ReadFile(p)
			os.Remove(p)
			var n struct{ Title, Subtitle, Body string }
			if err != nil || json.Unmarshal(data, &n) != nil {
				continue
			}
			err = runtime.SendNotification(a.ctx, runtime.NotificationOptions{ID: f.Name(), Title: n.Title, Subtitle: n.Subtitle, Body: n.Body})
			logSend(dir, n.Title, err)
			if err != nil { // macOS refused it: say so in a way that still reaches you
				q := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
				exec.Command("osascript", "-e", `display notification "`+q(n.Body)+`" with title "crew" subtitle "`+q(n.Title)+`" sound name "Glass"`).Run()
			}
		}
	}
}

// logSend notes how sending a notification went, in dir/log (the last 50), and keeps the last
// failure in dir/error for crew's Setup to show; a send that works removes it.
func logSend(dir, title string, err error) {
	line := time.Now().Format("2006-01-02 15:04:05") + " sent " + title
	if err != nil {
		line = time.Now().Format("2006-01-02 15:04:05") + " failed " + title + ": " + err.Error()
		os.WriteFile(filepath.Join(dir, "error"), []byte(err.Error()+"\n"), 0o644)
	} else {
		os.Remove(filepath.Join(dir, "error"))
	}
	p := filepath.Join(dir, "log")
	old, _ := os.ReadFile(p)
	lines := append(strings.Split(strings.TrimSpace(string(old)), "\n"), line)
	os.WriteFile(p, []byte(strings.TrimSpace(strings.Join(lines[max(len(lines)-50, 0):], "\n"))+"\n"), 0o644)
}

// crewHome is crew's folder: CREW_HOME from your login shell, else ~/.crew.
func crewHome(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "CREW_HOME="); ok && v != "" {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".crew")
}

// crewPath is the crew binary bundled next to this one, else the one on PATH (when run from source).
func crewPath() string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "crew"); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("crew"); err == nil {
		return p
	}
	return "crew"
}

// instanceID names this crew folder's window for the single-instance lock. ponytail: it reads
// CREW_HOME from the app's own environment, not the login shell's (read later, so the window opens
// at once); crew's own lock still keeps one crew per folder.
func instanceID() string {
	sum := sha256.Sum256([]byte(crewHome(os.Environ())))
	return "dev.crew.app." + hex.EncodeToString(sum[:8])
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// loginEnv is your login shell's environment. An app opened from Finder doesn't get your PATH, so
// crew wouldn't find claude, gh or git without it.
func loginEnv() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", "env -0").Output()
	if err != nil {
		return os.Environ()
	}
	var env []string
	for _, kv := range strings.Split(string(out), "\x00") {
		if strings.Contains(kv, "=") {
			env = append(env, strings.TrimSpace(kv))
		}
	}
	return env
}

func main() {
	app := &App{}
	// No Edit menu, and a Window menu without Close: their ⌘Z/⌘C/⌘V/⌘W would take the keys
	// before crew does (⌘W closes a shell in crew, not the window). The page sends them on.
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	win := m.AddSubmenu("Window")
	win.AddText("Minimise", keys.CmdOrCtrl("m"), func(*menu.CallbackData) { runtime.WindowMinimise(app.ctx) })
	win.AddText("Zoom", nil, func(*menu.CallbackData) { runtime.WindowToggleMaximise(app.ctx) })
	err := wails.Run(&options.App{
		Title:            "crew",
		Width:            1440,
		Height:           900,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: &options.RGBA{R: 22, G: 24, B: 30, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		Menu:             m,
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		// One window per crew folder: opening the app again brings this one to the front (crew also
		// refuses to run twice on one folder).
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID(),
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				runtime.WindowUnminimise(app.ctx)
				runtime.Show(app.ctx)
			},
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		println(err.Error())
	}
}
