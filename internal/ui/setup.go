package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/setup"
	"crew/internal/tasks"
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
	ctx := m.cfg.Ctx
	return func() tea.Msg { return setupMsg{setup.Check(ctx), afterFix} }
}

// onSetup takes a finished check. Claude's own status can say "signed in" while its server rejects
// the token, so a sign-in prompt raised by a failed turn stays until you sign in or a turn works.
func (m *Model) onSetup(msg setupMsg) {
	was := m.claudeReady()
	m.setup = msg.st
	if msg.afterFix && msg.st.Claude.OK() {
		m.authFailed = false
	}
	if !was && m.claudeReady() {
		m.signedIn()
	}
	if m.current() == nil {
		m.refresh() // home shows the checks
	}
}

// claudeReady reports whether Claude Code is installed and signed in, as far as crew knows. Before
// the first check it's assumed to be, so nothing nags while crew starts.
func (m Model) claudeReady() bool {
	return !m.authFailed && (!m.setup.Checked || m.setup.Claude.OK())
}

// signedIn tells the chats whose message failed on auth that it can go now.
func (m *Model) signedIn() {
	n := 0
	for id, c := range m.convs {
		if c.retry != nil {
			m.add(id, chat.Tool, "signed in · ⏎ in an empty message box sends your last message again")
			n++
		}
	}
	m.notice = "Signed in to Claude"
	if n > 0 {
		m.notice += fmt.Sprintf(" · ⏎ in an empty message box resends what failed (%d chat(s))", n)
	}
}

// setupBanner is the one problem worth showing on every screen, with the key that fixes it; "" when
// there's none. A missing GitHub CLI isn't one: crew works without it and the PR views say so.
func (m Model) setupBanner() string {
	s := m.setup
	switch {
	case m.authFailed || s.Checked && s.Claude.Installed && !s.Claude.SignedIn:
		return "Claude is signed out · ⌘L signs in"
	case s.Checked && !s.Claude.Installed:
		return "Claude Code isn't installed · ⌘L shows how"
	case s.Checked && !s.Git.Installed:
		return "git isn't installed · ⌘L shows how"
	case s.Checked && s.GH.Installed && !s.GH.SignedIn:
		return "GitHub is signed out, so PRs can't load · ⌘L signs in"
	}
	return ""
}

// needsFix reports whether the banner has something for ⌘L to fix.
func needsFix(m *Model) bool { return m.setupBanner() != "" }

// fixSetup is ⌘L: sign in when that's all that's missing, else open the Setup panel.
func (m *Model) fixSetup() tea.Cmd {
	s := m.setup
	switch {
	case m.authFailed || s.Claude.Installed && !s.Claude.SignedIn:
		return m.runFix(setup.ClaudeSignIn)
	case s.Claude.Installed && s.Git.Installed && s.GH.Installed && !s.GH.SignedIn:
		return m.runFix(setup.GHSignIn)
	}
	m.openSetup()
	return nil
}

// turnError shows a failed turn's error. When it means Claude can't be used (signed out, login
// expired, not installed), the chat says how to fix it, and the message is kept to send again.
// ponytail: Claude only; another agent would need its own setup check and sign-in command.
func (m *Model) turnError(id, text string) {
	a, _ := agent.Get(agentName(*m.taskOr(id)))
	c := m.conv(id)
	switch {
	case strings.Contains(text, "executable file not found"):
		m.setup.Claude.Installed, m.setup.Checked = false, true
		c.retry = c.last
		m.add(id, chat.Error, "Claude Code isn't installed, or crew can't find it · ⌘L shows how to install it")
	case a.AuthError != nil && a.AuthError(text):
		m.authFailed = true
		c.retry = c.last
		msg := "Claude's login expired, or you're signed out · ⌘L signs in, then ⏎ in an empty message box sends this again"
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			msg = "Claude rejected your ANTHROPIC_API_KEY · set a new one in your shell profile (e.g. ~/.zshrc) and restart crew, or unset it and ⌘L signs in"
		}
		m.add(id, chat.Error, msg+"\n\nClaude Code said: "+text)
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

// runFix runs an install or sign-in command in a new shell panel. If it fails, the shell waits for
// ⏎ so you can read why; when it closes, crew checks again.
func (m *Model) runFix(command string) tea.Cmd {
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
	st, err := m.spawn(cmd, key)
	if err != nil {
		m.notice = "couldn't start a shell: " + err.Error()
		return nil
	}
	st.dir, st.fix = cmd.Dir, true
	m.shells[key] = append(m.shells[key], st)
	m.focusOn(areaShell, len(m.shells[key])-1)
	m.layout()
	m.refresh()
	m.notice = "Running " + command + " in the shell on the right · crew checks again when it closes"
	return st.wait
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
			return m, m.runFix(t.Install)
		case t.SignIn != "":
			return m, m.runFix(t.SignIn) // signs in again, e.g. as another account
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
		case !t.Installed:
			mark, state, do = warn.Render("✗"), "not installed", "⏎ runs "+t.Install
		case !t.SignedIn || t.Name == "Claude Code" && m.authFailed:
			mark, state, do = warn.Render("✗"), "signed out", "⏎ runs "+t.SignIn
		case t.Account != "":
			state, do = "signed in · "+t.Account, "⏎ signs in again"
		case t.SignIn != "":
			state, do = "signed in", "⏎ signs in again"
		}
		name := fmt.Sprintf("%-12s", t.Name)
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

// getStarted is the top of the home screen until you have a task, or while something's missing:
// the setup checks, then the first task.
func (m Model) getStarted() string {
	if len(m.tasks) > 0 && m.setupBanner() == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(bold.Render("Get started") + "\n\n")
	b.WriteString(m.setupList(-1))
	mark := dim.Render("○")
	if len(m.tasks) > 0 {
		mark = green.Render("✓")
	}
	b.WriteString(fmt.Sprintf("  %s %-12s  %s\n", mark, "First task", "n creates one: a name, a repo folder and what to do"))
	b.WriteString("\n" + dim.Render("  d opens Setup to install or sign in · ⌘L fixes what the help line says") + "\n\n")
	return b.String()
}

// homeKey handles home's own letters: n a new task, d the Setup panel.
func (m *Model) homeKey(k string) (tea.Cmd, bool) {
	if m.selRow().kind != rowHome || m.chatTask() != nil {
		return nil, false
	}
	switch k {
	case "n":
		m.listFocus = false
		return m.openNew(), true
	case "d":
		return m.openSetup(), true
	}
	return nil, false
}
