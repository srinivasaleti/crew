package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crew/internal/gh"
	"crew/internal/setup"
)

// Notifications: crew tells macOS when something needs you and you're not looking (another app has
// focus, or you're on another task): Claude finished a turn, or a task's PR got a failing check,
// a change request, an approval, new comments or was merged; and reminders. CREW_NOTIFY=0 turns
// them off. In crew's app they go to the app (desktop/), which sends them as crew's own; elsewhere
// osascript sends them.

// notifyDir is where crew hands notifications to its app (Config.Notify).
var notifyDir string

// notify shows a macOS notification, with a sound.
func notify(title, body string) {
	if os.Getenv("CREW_NOTIFY") == "0" {
		return
	}
	if setup.InApp() && notifyDir != "" {
		switch setup.NotifyStatus(notifyDir) {
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
	out := filepath.Join(notifyDir, "out")
	os.MkdirAll(out, 0o755)
	data, _ := json.Marshal(map[string]string{"title": title, "body": body})
	name := strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
	tmp := filepath.Join(out, "."+name)
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, filepath.Join(out, name))
	}
}

// askApp asks crew's app to ask macOS for permission ("ask") or to check it again ("check").
func askApp(req string) {
	if setup.InApp() && notifyDir != "" {
		os.MkdirAll(notifyDir, 0o755)
		os.WriteFile(filepath.Join(notifyDir, req), nil, 0o644)
	}
}

// looking reports whether you're looking at a task's tab right now (crew's window has focus, the
// task is selected and that tab is open), so there's no need to notify about it.
func (m Model) looking(id string, t tab) bool {
	return !m.blurred && id == m.currentID() && m.tab == t
}

// notifyTurn tells you a task's turn ended, if you weren't watching it.
func (m Model) notifyTurn(id string) {
	t := m.find(id)
	if t == nil || m.looking(id, tabChat) {
		return
	}
	body := "Claude finished · its reply is ready"
	if msgs := m.chat.Conv(id).Msgs; len(msgs) > 0 && msgs[len(msgs)-1].Role == "error" {
		body = "Claude stopped with an error"
	}
	notify(t.Title, body)
}

// notifyPR tells you what changed on a task's PR since it was last read.
func (m Model) notifyPR(id string, old, cur *gh.PR) {
	t := m.find(id)
	if t == nil || old == nil || cur == nil || old.Number != cur.Number || m.looking(id, tabPR) {
		return
	}
	var news []string
	if cur.State == "MERGED" && old.State != "MERGED" {
		news = append(news, "PR merged")
	}
	failing := func(pr *gh.PR) map[string]bool {
		f := map[string]bool{}
		for _, c := range pr.Checks {
			if c.State == gh.Fail {
				f[c.Name] = true
			}
		}
		return f
	}
	before := failing(old)
	for name := range failing(cur) {
		if !before[name] {
			news = append(news, "check failed: "+name)
		}
	}
	if cur.ReviewDecision != old.ReviewDecision {
		switch cur.ReviewDecision {
		case "CHANGES_REQUESTED":
			news = append(news, "changes requested")
		case "APPROVED":
			news = append(news, "approved")
		}
	}
	comments := func(pr *gh.PR) int {
		n := len(pr.Comments)
		for _, th := range pr.Threads {
			n += len(th.Comments)
		}
		return n
	}
	if old.Threads != nil || cur.Threads == nil { // the poll doesn't read line comments; compare like with like
		if n := comments(cur) - comments(old); n > 0 {
			news = append(news, fmt.Sprintf("%d new comment%s", n, plural(n)))
		}
	}
	if len(news) > 0 {
		notify(t.Title, fmt.Sprintf("#%d %s", cur.Number, strings.Join(news, " · ")))
	}
}
