package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/agent"
	"crew/internal/notify"
	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/term"
)

// Setup: crew checks that git, Claude Code and the GitHub CLI are installed and signed in, when it
// starts and when its window comes back to the front (at most once a minute). What's missing shows
// in a banner on the help line, and ⌘L fixes it: it signs in, or opens the Setup panel (/doctor),
// which lists each tool with ⏎ to install it or sign in. Installing and signing in run in a shell
// panel, so the browser flow and any prompts work as in a terminal; when that shell exits, crew
// checks again. A message that failed because Claude was signed out is kept, and ⏎ in an empty
// message box sends it again once you're signed in.

// setupMsg is a finished check. afterFix is true when it follows an install or sign-in you ran.
type setupMsg struct {
	st       setup.Status
	afterFix bool
}

// checkSetup runs the checks in the background.
func (m *Model) checkSetup(afterFix bool) tea.Cmd {
	m.setupAt = time.Now()
	ctx, dir := m.cfg.Ctx, m.cfg.Notify
	notify.AskApp("check") // the app re-reads macOS's setting; the next check sees it
	return func() tea.Msg { return setupMsg{setup.Check(ctx, dir), afterFix} }
}

// onSetup takes a finished check. An agent's own status can say "signed in" while its server
// rejects the login, so a sign-in prompt raised by a failed turn stays until you sign in or a turn
// works.
func (m *Model) onSetup(msg setupMsg) {
	was := map[string]bool{}
	for _, a := range agent.All {
		was[a.Name] = m.agentReady(a.Name)
	}
	m.setup = msg.st
	if msg.afterFix && m.authFailed != "" && m.setup.Agent(m.authFailed).OK() {
		m.authFailed = ""
	}
	for _, a := range agent.All {
		if !was[a.Name] && m.agentReady(a.Name) {
			m.signedIn(a)
		}
	}
	if m.current() == nil {
		m.refresh() // home shows the checks
	}
}

// agentReady reports whether an agent's CLI is installed and signed in, as far as crew knows.
// Before the first check it's assumed to be, so nothing nags while crew starts.
func (m Model) agentReady(name string) bool {
	return m.authFailed != name && (!m.setup.Checked || m.setup.Agent(name).OK())
}

// signedIn tells the chats whose message to this agent failed on auth that it can go now.
func (m *Model) signedIn(a agent.Agent) {
	n := 0
	for id, c := range m.chat.All() {
		if c.Retry() != nil && chat.AgentName(*m.taskOr(id)) == a.Name {
			m.add(id, chat.Tool, "signed in · ⏎ in an empty message box sends your last message again")
			n++
		}
	}
	m.notice = "Signed in to " + a.Title
	if n > 0 {
		m.notice += fmt.Sprintf(" · ⏎ in an empty message box resends what failed (%d chat(s))", n)
	}
}

// setupBanner is the one problem worth showing on every screen, with the key that fixes it; "" when
// there's none. A missing GitHub or Gemini CLI isn't one: crew works without them.
func (m Model) setupBanner() string {
	s := m.setup
	switch {
	case m.authFailed != "":
		a, _ := agent.Get(m.authFailed)
		return a.Title + " is signed out · ⌘L signs in"
	case s.Checked && s.Claude.Installed && !s.Claude.SignedIn:
		return "Claude is signed out · ⌘L signs in"
	case s.Checked && !s.Claude.Installed:
		return "Claude Code isn't installed · ⌘L shows how"
	case s.Checked && !s.Git.Installed:
		return "git isn't installed · ⌘L shows how"
	case s.Checked && s.GH.Installed && !s.GH.SignedIn:
		return "GitHub is signed out, so PRs can't load · ⌘L signs in"
	case s.Checked && !s.Notify.SignedIn && s.Notify.Off != "off":
		return "Notifications aren't reaching you (macOS refused the last one) · ⌘L opens Setup"
	case s.Checked && !s.Notify.SignedIn:
		return "Notifications are off, so reminders can't reach you · ⌘L allows them"
	}
	return ""
}

// needsFix reports whether the banner has something for ⌘L to fix.
func needsFix(m *Model) bool { return m.setupBanner() != "" }

// fixSetup is ⌘L: sign in when that's all that's missing, else open the Setup panel.
func (m *Model) fixSetup() tea.Cmd {
	s := m.setup
	switch {
	case m.authFailed != "":
		return m.signIn(*s.Agent(m.authFailed))
	case s.Claude.Installed && !s.Claude.SignedIn:
		return m.signIn(s.Claude)
	case s.Claude.Installed && s.Git.Installed && s.GH.Installed && !s.GH.SignedIn:
		return m.signIn(s.GH)
	case s.Claude.OK() && s.Git.Installed && !s.Notify.SignedIn:
		return m.allowNotifications()
	}
	return m.openSetup()
}

// signIn runs a tool's sign-in command in a shell panel, or opens Setup before the first check
// has said what that command is. For Notifications it asks macOS.
func (m *Model) signIn(t setup.Tool) tea.Cmd {
	if t.Name == setup.Notifications {
		return m.allowNotifications()
	}
	if t.SignIn == "" {
		return m.openSetup()
	}
	return m.runFix(t.SignIn, t.Hint)
}

// allowNotifications gets crew's notifications allowed. macOS lets an app ask once, so the first ⏎
// has the app ask (macOS shows "crew would like to send notifications"); after that, or once they've
// been turned off, it opens System Settings → Notifications, where you turn crew on. Either way crew
// checks again shortly, and when its window comes back to the front.
func (m *Model) allowNotifications() tea.Cmd {
	if setup.NotifyStatus(m.cfg.Notify) == "allowed" { // allowed, but sending failed: Setup shows why
		return m.openSetup()
	}
	if m.modal == setupModal {
		m.closeModal()
	}
	if !m.askedNotify {
		m.askedNotify = true
		notify.AskApp("ask")
		m.notice = "macOS asks whether crew may send notifications · Allow · if nothing showed, ⌘L opens System Settings"
	} else {
		exec.Command("open", "x-apple.systempreferences:com.apple.Notifications-Settings.extension").Start()
		m.notice = "In System Settings → Notifications, pick crew and turn on Allow notifications, then come back"
	}
	m.setupAt = time.Time{} // coming back to crew checks again
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return recheckMsg{} })
}

// recheckMsg runs the setup checks again (after asking macOS about notifications).
type recheckMsg struct{}

// turnError shows a failed turn's error. When it means the agent can't be used (signed out, login
// expired, not installed), the chat says how to fix it, and the message is kept to send again.
func (m *Model) turnError(id, text string) {
	a, _ := agent.Get(chat.AgentName(*m.taskOr(id)))
	c := m.chat.Conv(id)
	switch {
	case strings.Contains(text, "executable file not found"):
		m.setup.Agent(a.Name).Installed, m.setup.Checked = false, true
		c.KeepForRetry()
		m.add(id, chat.Error, a.Title+"'s CLI ("+a.Command+") isn't installed, or crew can't find it · /doctor shows how to install it")
	case a.AuthError != nil && a.AuthError(text):
		m.authFailed = a.Name
		c.KeepForRetry()
		msg := a.Title + "'s login expired, or you're signed out · ⌘L signs in, then ⏎ in an empty message box sends this again"
		if a.KeyEnv != "" && os.Getenv(a.KeyEnv) != "" {
			msg = a.Title + " rejected your " + a.KeyEnv + " · set a new one in your shell profile (e.g. ~/.zshrc) and restart crew, or unset it and ⌘L signs in"
		}
		m.add(id, chat.Error, msg+"\n\n"+a.Title+" said: "+text)
	default:
		m.add(id, chat.Error, text)
	}
}

// taskOr is the task with this id, or an empty one (agentName then gives the default agent).
func (m *Model) taskOr(id string) *tasks.Task {
	if t := m.find(id); t != nil {
		return t
	}
	return &tasks.Task{}
}

// runFix runs an install or sign-in command in a new shell panel; hint says what to do in it. If it
// fails, the shell waits for ⏎ so you can read why; when it closes, crew checks again.
func (m *Model) runFix(command, hint string) tea.Cmd {
	key := m.shellKey()
	if len(m.shells[key]) >= maxShells {
		m.notice = fmt.Sprintf("Up to %d shells here · ⌘W or exit closes one, then try again", maxShells)
		return nil
	}
	if m.modal == setupModal {
		m.closeModal()
	}
	script := command + ` || { echo; printf 'That didn'"'"'t work · ⏎ closes this shell '; read _; }`
	cmd := exec.CommandContext(m.cfg.Ctx, "/bin/sh", "-c", script)
	cmd.Dir, _ = os.UserHomeDir()
	st, err := term.Start(cmd, key)
	if err != nil {
		m.notice = "couldn't start a shell: " + err.Error()
		return nil
	}
	st.Dir, st.Fix = cmd.Dir, true
	m.shells[key] = append(m.shells[key], st)
	m.focusOn(areaShell, len(m.shells[key])-1)
	m.layout()
	m.refresh()
	m.notice = "Running " + command + " in the shell on the right"
	if hint != "" {
		m.notice += ": " + hint
	}
	m.notice += " · crew checks again when it closes"
	return st.Wait
}

// openSetup opens the Setup panel (/doctor), with a fresh check.
func (m *Model) openSetup() tea.Cmd {
	m.modal = setupModal
	m.setupSel = 0
	m.input.Blur()
	return m.checkSetup(false)
}

// updateSetup handles keys in the Setup panel: ↑↓ pick a tool, ⏎ installs it or signs in, r checks
// again, esc closes.
func (m Model) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tools := m.setup.Tools()
	switch msg.String() {
	case "up":
		m.setupSel = max(m.setupSel-1, 0)
	case "down":
		m.setupSel = min(m.setupSel+1, len(tools)-1)
	case "r":
		return m, m.checkSetup(false)
	case "enter":
		t := tools[m.setupSel]
		switch {
		case !m.setup.Checked:
		case !t.Installed:
			return m, m.runFix(t.Install, "")
		case t.Name == setup.Notifications && t.SignIn != "" && (t.SignedIn || setup.NotifyStatus(m.cfg.Notify) == "allowed"):
			notify.Send("crew", "Notifications work · this is how reminders reach you")
			m.notice = "Sent a test notification · if none appeared, ~/.crew/notify/log says why · crew checks again in 5s"
			return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return recheckMsg{} })
		case t.SignIn != "":
			return m, m.signIn(t) // signs in again, e.g. as another account
		}
	case "esc", "q":
		m.closeModal()
	}
	return m, nil
}

// setupView is the Setup panel: each tool, whether it's ready, and what ⏎ does on it.
func (m Model) setupView() string {
	var b strings.Builder
	b.WriteString(bold.Render("Setup") + dim.Render("  what crew needs on this Mac") + "\n\n")
	b.WriteString(m.setupList(m.setupSel) + "\n")
	b.WriteString(dim.Render("↑↓ pick · ⏎ install or sign in (in a shell) · r check again · esc close"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Render(b.String())
}

// setupList lists the tools with a ✓ or ✗ and what's wrong; pick highlights a row (-1: none).
func (m Model) setupList(pick int) string {
	if !m.setup.Checked {
		return dim.Render("  checking…") + "\n"
	}
	var b strings.Builder
	for i, t := range m.setup.Tools() {
		mark, state, do := green.Render("✓"), "ready", ""
		switch {
		case !t.Installed && t.Optional:
			mark, state, do = dim.Render("○"), "not installed (optional)", "⏎ runs "+t.Install
		case !t.Installed:
			mark, state, do = warn.Render("✗"), "not installed", "⏎ runs "+t.Install
		case !t.SignedIn && t.Name == setup.Notifications && setup.NotifyStatus(m.cfg.Notify) == "allowed":
			mark, state, do = warn.Render("✗"), t.Off, "⏎ sends a test · ~/.crew/notify/log has the details"
		case !t.SignedIn && t.Name == setup.Notifications:
			mark, state, do = warn.Render("✗"), t.Off, "⏎ asks macOS to allow them (or opens System Settings)"
		case !t.SignedIn || t.Agent != "" && t.Agent == m.authFailed:
			mark, state, do = warn.Render("✗"), "signed out", "⏎ runs "+t.SignIn
		case t.Name == setup.Notifications:
			state = t.Account
			if t.SignIn != "" {
				do = "⏎ sends a test notification"
			}
		case t.Account != "":
			state, do = "signed in · "+t.Account, "⏎ signs in again"
		case t.SignIn != "":
			state, do = "signed in", "⏎ signs in again"
		}
		name := fmt.Sprintf("%-13s", t.Name)
		if i == pick {
			name = sel.Render(name)
		}
		fmt.Fprintf(&b, "  %s %s  %s\n", mark, name, state)
		line := t.For
		if i == pick && do != "" {
			line += " · " + do
		}
		b.WriteString(dim.Render("     "+line) + "\n")
	}
	return b.String()
}

// getStarted is the top of the home screen: the setup checks, always, so you can see what's
// installed and signed in. Until you have a task it's "Get started", with the first task as its
// last step.
func (m Model) getStarted() string {
	var b strings.Builder
	title := "Setup"
	if len(m.tasks) == 0 {
		title = "Get started"
	}
	b.WriteString(bold.Render(title) + "\n\n")
	b.WriteString(m.setupList(-1))
	if len(m.tasks) == 0 {
		b.WriteString(fmt.Sprintf("  %s %-13s  %s\n", dim.Render("○"), "First task", "n creates one: a name, a repo folder and what to do"))
	}
	b.WriteString("\n" + dim.Render("  d opens Setup to install or sign in · ⌘L fixes what the help line says") + "\n\n")
	return b.String()
}

// homeKey handles home's own letters: n a new task, d the Setup panel, and each section's letter,
// which turns it on or off.
func (m *Model) homeKey(k string) (tea.Cmd, bool) {
	if m.selRow().Kind != rowHome || m.chatTask() != nil {
		return nil, false
	}
	switch k {
	case "n":
		if !m.sectionOn("tasks") {
			m.notice = "TASKS is off · t turns it on"
			return nil, true
		}
		m.listFocus = false
		return m.taskSec().New(m), true
	case "d":
		return m.openSetup(), true
	}
	return m.sectionKey(k)
}
