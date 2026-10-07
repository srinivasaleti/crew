package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"crew/internal/gh"
)

// Notifications: crew tells macOS when something needs you and you're not looking (another app has
// focus, or you're on another task): Claude finished a turn, or a task's PR got a failing check,
// a change request, an approval, new comments or was merged. CREW_NOTIFY=0 turns them off.

// notify shows a macOS notification.
func notify(title, body string) {
	if os.Getenv("CREW_NOTIFY") == "0" {
		return
	}
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace // AppleScript string quoting
	go exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "crew" subtitle "%s"`, q(body), q(title))).Run()
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
	if msgs := m.conv(id).msgs; len(msgs) > 0 && msgs[len(msgs)-1].Role == "error" {
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
