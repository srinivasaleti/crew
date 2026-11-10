// Package browser opens links in your browser, and shows the Mac's folder picker.
package browser

import (
	"os/exec"
	"strings"
)

// Open opens a URL in the browser; "" does nothing.
func Open(u string) {
	if u != "" {
		if c := exec.Command("open", u); c.Start() == nil {
			go c.Wait()
		}
	}
}

// ChooseFolder shows macOS's folder picker and is the folder picked, or "" when you cancel. It waits
// for you, so call it off the UI's thread (the web UI's handler runs on its own goroutine).
func ChooseFolder(prompt string) string {
	out, err := exec.Command("osascript", "-e", "activate",
		"-e", `POSIX path of (choose folder with prompt "`+strings.ReplaceAll(prompt, `"`, `'`)+`")`).Output()
	if err != nil { // -128: cancelled
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), "/")
}
