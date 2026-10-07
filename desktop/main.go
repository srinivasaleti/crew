// Command crew-desktop is crew in its own Mac app window: a terminal (xterm.js in the system web
// view, through Wails) running the crew binary that sits next to it in the app bundle.
//
// It holds no crew features on purpose. Everything crew does lives in the crew binary, built from
// the same source by `make app`, so the app and crew in a terminal always behave the same. What the
// app adds is only what a terminal can't: ⌘ keys, copy and paste, and a normal window.
package main

import (
	"context"
	"embed"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
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
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// Start runs crew in a pseudo-terminal of cols × rows. Its output reaches the window as "out"
// events (base64, since it's raw bytes), and the window closes when crew quits.
func (a *App) Start(cols, rows int) error {
	if a.ptmx != nil {
		return nil // the page reloaded; crew is already running
	}
	a.cmd = exec.Command(crewPath())
	a.cmd.Env = append(loginEnv(), "TERM=xterm-256color", "COLORTERM=truecolor")
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
	// No Edit menu: its ⌘Z/⌘C/⌘V would take the keys before crew does; the page maps them instead.
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.WindowMenu())
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
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println(err.Error())
	}
}
