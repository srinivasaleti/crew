// Command crew-desktop is crew in its own Mac app window: a terminal (xterm.js in the system web
// view, through Wails) running the crew binary that sits next to it in the app bundle.
//
// It holds no crew features on purpose. Everything crew does lives in the crew binary, built from
// the same source by `make app`, so the app and crew in a terminal always behave the same. What the
// app adds is only what a terminal can't: ⌘ keys, copy and paste, a normal window, and macOS
// notifications in crew's own name (relayed from crew, see relayNotifications).
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	url  string      // web mode: the page crew serve gave us
	env  []string    // your login shell's environment, read once
	once sync.Once   // the notification and window relays start once, however often the mode changes

	menus   map[bool]*menu.Menu // the app's menu for each UI (true: the web one), see appMenu
	webMenu bool                // the menu showing is the web UI's
}

// appMenu is the app's menu bar. The web UI gets an Edit menu: a web view's text fields only copy,
// paste, cut, select all and undo through one. The terminal UI doesn't: its ⌘Z/⌘C/⌘V would take the
// keys before crew does, so the page sends them on instead. Neither has Close in Window (⌘W closes
// a shell in crew, not the window).
func appMenu(app *App, web bool) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	if web {
		m.Append(menu.EditMenu())
	}
	win := m.AddSubmenu("Window")
	win.AddText("Minimise", keys.CmdOrCtrl("m"), func(*menu.CallbackData) { runtime.WindowMinimise(app.ctx) })
	win.AddText("Zoom", nil, func(*menu.CallbackData) { runtime.WindowToggleMaximise(app.ctx) })
	return m
}

// startup runs as the window opens. Every launch starts in the web UI: the choice in ~/.crew/ui is
// only for switching while the app is open.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	os.Remove(filepath.Join(crewHome(os.Environ()), "ui"))
}

// Start runs crew the way ~/.crew/ui says. In the terminal UI that's crew in a pseudo-terminal of
// cols × rows: its output reaches the window as "out" events (base64, since it's raw bytes). In the
// web UI (the default) it's `crew serve`, and the page to load comes back. Either way the window
// closes when crew quits, unless crew quit to switch UIs (exited).
func (a *App) Start(cols, rows int) (string, error) {
	if a.cmd != nil {
		return a.url, nil // the page reloaded; crew is already running
	}
	if a.env == nil {
		a.env = loginEnv()
	}
	notify := filepath.Join(crewHome(a.env), "notify")
	a.once.Do(func() {
		go a.relayNotifications(notify)
		go a.relayWindow(notify)
	})
	web := webMode(a.env)
	if web != a.webMenu { // the window opens with the web UI's menu; change it only when switching
		a.webMenu = web
		go runtime.MenuSetApplicationMenu(a.ctx, a.menus[web]) // never holds up starting crew
	}
	if web {
		return a.startWeb(a.env)
	}
	a.cmd = exec.Command(crewPath())
	// CREW_APP tells crew it runs in this window, so it hands its notifications to us.
	a.cmd.Env = append(a.env, "TERM=xterm-256color", "COLORTERM=truecolor", "CREW_APP=1")
	a.cmd.Dir, _ = os.UserHomeDir()
	ptmx, err := pty.StartWithSize(a.cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		a.cmd = nil
		return "", err
	}
	a.ptmx = ptmx
	cmd := a.cmd
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
		cmd.Wait()
		a.exited(false)
	}()
	return "", nil
}

// exited is what happens when crew's process ends: the window closes, unless the UI it ran wrote the
// other one into ~/.crew/ui to switch (the terminal's Switch to the web UI, the page's button). Then
// the window starts over from its own page, whose Start runs the other UI. The old crew has quit by
// now, so its lock is free.
func (a *App) exited(web bool) {
	if webMode(a.env) != web {
		a.cmd, a.ptmx, a.url = nil, nil, ""
		runtime.WindowExecJS(a.ctx, "location.href = 'wails://wails/'")
		return
	}
	a.done.Store(true)
	runtime.Quit(a.ctx)
}

// webMode says whether to show the web UI (the default) instead of the terminal one.
func webMode(env []string) bool {
	b, _ := os.ReadFile(filepath.Join(crewHome(env), "ui"))
	return strings.TrimSpace(string(b)) != "terminal"
}

// startWeb runs `crew serve` and returns the page it prints on its first line. When crew can't
// start (another one holds the folder), that line is the reason, and it comes back as the error.
func (a *App) startWeb(env []string) (string, error) {
	a.cmd = exec.Command(crewPath(), "serve")
	a.cmd.Env = append(env, "CREW_APP=1")
	a.cmd.Dir, _ = os.UserHomeDir()
	out, err := a.cmd.StdoutPipe()
	if err != nil {
		a.cmd = nil
		return "", err
	}
	if err := a.cmd.Start(); err != nil {
		a.cmd = nil
		return "", err
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "http://") {
		a.cmd.Wait()
		a.cmd = nil
		return "", fmt.Errorf("crew serve: %s", line)
	}
	a.url = line
	cmd := a.cmd
	go func() {
		cmd.Wait()
		a.exited(true)
	}()
	return a.url, nil
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
	if a.ptmx != nil {
		a.ptmx.Close()
	}
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

// relayWindow does what crew asks of its window for a break, in dir/window: "front" brings it in
// front of every app, filling the screen and staying on top; "normal" puts it back. crew decides
// when (FOCUS's break screen); the app only moves its window.
// ponytail: polled twice a second, like notifications.
func (a *App) relayWindow(dir string) {
	p := filepath.Join(dir, "window")
	for range time.Tick(500 * time.Millisecond) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		os.Remove(p)
		switch strings.TrimSpace(string(data)) {
		case "front":
			runtime.WindowUnminimise(a.ctx)
			runtime.Show(a.ctx) // brings the app in front of the one you're in
			runtime.WindowMaximise(a.ctx)
			runtime.WindowSetAlwaysOnTop(a.ctx, true)
		case "normal":
			runtime.WindowSetAlwaysOnTop(a.ctx, false)
			runtime.WindowUnmaximise(a.ctx)
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
	app := &App{menus: map[bool]*menu.Menu{}, webMenu: true}
	app.menus[false] = appMenu(app, false)
	app.menus[true] = appMenu(app, true)
	m := app.menus[true] // every launch opens the web UI
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
