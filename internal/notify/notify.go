// Package notify sends macOS notifications for crew: through crew's app (desktop/), which sends
// them as crew's own, or with osascript elsewhere. CREW_NOTIFY=0 turns them off.
package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crew/internal/setup"
)

// Dir is where crew hands notifications to its app (~/.crew/notify); main sets it.
var Dir string

// Send shows a macOS notification, with a sound.
func Send(title, body string) {
	if os.Getenv("CREW_NOTIFY") == "0" {
		return
	}
	if setup.InApp() && Dir != "" {
		switch setup.NotifyStatus(Dir) {
		case "allowed":
			handToApp(title, body)
			return
		case "off":
			return // you turned crew's notifications off in System Settings
		}
	}
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace // AppleScript string quoting
	go exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "crew" subtitle "%s" sound name "Glass"`, q(body), q(title))).Run()
}

// handToApp leaves a notification for the app to send: written aside, then renamed in, so the app
// never reads half a file.
func handToApp(title, body string) {
	out := filepath.Join(Dir, "out")
	os.MkdirAll(out, 0o755)
	data, _ := json.Marshal(map[string]string{"title": title, "body": body})
	name := strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
	tmp := filepath.Join(out, "."+name)
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, filepath.Join(out, name))
	}
}

// Window asks crew's app to bring its window to the front, filling the screen above every app
// ("front", for a break), or to put it back as it was ("normal"). Outside the app it does nothing.
func Window(state string) {
	if setup.InApp() && Dir != "" {
		os.MkdirAll(Dir, 0o755)
		os.WriteFile(filepath.Join(Dir, "window"), []byte(state), 0o644)
	}
}

// AskApp asks crew's app to ask macOS for permission ("ask") or to check it again ("check").
func AskApp(req string) {
	if setup.InApp() && Dir != "" {
		os.MkdirAll(Dir, 0o755)
		os.WriteFile(filepath.Join(Dir, req), nil, 0o644)
	}
}
