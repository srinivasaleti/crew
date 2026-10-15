package focus

import (
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// idleMsg is how long the Mac has had no keyboard or mouse input; ok is false when it couldn't be read.
type idleMsg struct {
	idle time.Duration
	ok   bool
}

// hidIdle finds HIDIdleTime (nanoseconds since the last input) in ioreg's output.
var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// readIdle reads the Mac's idle time with ioreg, in the background.
// ponytail: polls ioreg every 10 seconds (one short process); an IOKit binding would avoid the
// process if this ever shows up in Activity Monitor.
func readIdle() tea.Cmd {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return func() tea.Msg {
		out, err := exec.Command("ioreg", "-c", "IOHIDSystem").Output()
		if err != nil {
			return idleMsg{}
		}
		m := hidIdle.FindSubmatch(out)
		if m == nil {
			return idleMsg{}
		}
		ns, err := strconv.ParseInt(string(m[1]), 10, 64)
		return idleMsg{time.Duration(ns), err == nil}
	}
}
