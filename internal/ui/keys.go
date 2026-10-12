package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/tasks"
)

// keyCommand is a shortcut that does the same thing on every screen (home, chat, task list,
// Changes/Usage tabs). Screen-specific keys (typing, scrolling, switching tabs) stay in that
// screen's handler. Update tries these first; the help line is built from them.
//
// keys are the ctrl keys the command runs on: an unbound ⌘ letter arrives as its ctrl key
// (cmdKey). They're never shown: hints name the ⌘ key (cmdName).
type keyCommand struct {
	keys []string
	help string                 // label for the help line; "" keeps it out
	when func(m *Model) bool    // whether it applies right now; nil means always
	run  func(m *Model) tea.Cmd // the action
}

func hasTask(m *Model) bool { return m.current() != nil }

func hasShells(m *Model) bool { return len(m.taskShells()) > 0 }

func agentRunning(m *Model) bool {
	c := m.conv(m.currentID())
	return c.stop != nil && !c.stopped
}

var keyCommands = []keyCommand{
	{[]string{"ctrl+s"}, "status", hasTask, func(m *Model) tea.Cmd { m.openStatus(m.currentID()); return nil }},
	{[]string{"ctrl+d"}, "delete task", hasTask, func(m *Model) tea.Cmd { m.openDelete(m.currentID()); return nil }},
	{[]string{"ctrl+g"}, "terminals", hasShells, func(m *Model) tea.Cmd { m.focusNext(); return nil }},
	{[]string{"ctrl+p"}, "find file", hasTask, func(m *Model) tea.Cmd {
		m.listFocus = false
		cmd := m.setTab(tabFiles)
		m.openFinder()
		return cmd
	}},
	{[]string{"ctrl+f"}, "search", nil, func(m *Model) tea.Cmd { m.openSearch(); return nil }},
	{[]string{"ctrl+t"}, "new terminal", nil, func(m *Model) tea.Cmd { return m.addShell() }},
	{[]string{"ctrl+r"}, "actions", func(m *Model) bool { return m.chatTask() != nil }, func(m *Model) tea.Cmd {
		m.expandActions = !m.expandActions
		m.refresh()
		return nil
	}},
	{[]string{"ctrl+o"}, "archived", nil, func(m *Model) tea.Cmd { m.toggleArchived(); return nil }},
	{[]string{"ctrl+x"}, "stop agent", agentRunning, func(m *Model) tea.Cmd { m.stopAgent(); return nil }},
	// ⌘L fixes what the help line's setup banner says (setup.go); it's named there, not in the help.
	{[]string{"ctrl+l"}, "", needsFix, func(m *Model) tea.Cmd { return m.fixSetup() }},
	// ctrl+l repaints everything, as in vim or htop.
	{[]string{"ctrl+l"}, "redraw", nil, func(*Model) tea.Cmd { return tea.ClearScreen }},
	{[]string{"ctrl+c"}, "quit", nil, func(m *Model) tea.Cmd { m.openQuit(); return nil }},
}

// runKeyCommand runs the shortcut bound to key if one applies, reporting whether it did.
func (m *Model) runKeyCommand(key string) (tea.Cmd, bool) {
	for _, c := range keyCommands {
		if slices.Contains(c.keys, key) && (c.when == nil || c.when(m)) {
			return c.run(m), true
		}
	}
	return nil, false
}

// keyHelp lists the shortcuts that apply right now, e.g. "⌘S status  ⌘Q quit".
func (m Model) keyHelp() string {
	var parts []string
	for _, c := range keyCommands {
		if c.help != "" && !inListHint[c.keys[0]] && (c.when == nil || c.when(&m)) {
			parts = append(parts, cmdName(c.keys[0])+" "+c.help)
		}
	}
	return strings.Join(parts, "  ")
}

// inListHint are commands only a list uses (search, archived tasks): the list's own hint names
// them, so the help line, which is for keys that work everywhere, leaves them out.
var inListHint = map[string]bool{"ctrl+f": true, "ctrl+o": true}

// cmdOf is the ⌘ key for a command whose ctrl key's ⌘ letter does something else: ⌘D opens a
// shell, so deleting a task is ⌘⌫; the app quits on ⌘Q.
var cmdOf = map[string]string{"ctrl+d": "cmd+backspace", "ctrl+c": "cmd+q"}

// cmdName is how a command's key is shown: its ⌘ key, e.g. ctrl+s as ⌘S.
func cmdName(k string) string {
	if c, ok := cmdOf[k]; ok {
		return prettyKeys(c)
	}
	return keyNames(k)
}

// slashCommand is a command typed in a task's message box. crew handles it; it's never sent as
// a message (a command may still ask the agent to do something, like /compact).
type slashCommand struct {
	name string
	help string
	run  func(m *Model, t *tasks.Task, arg string)
}

var slashCommands = []slashCommand{
	{"/model", "pick the model, or /model <name|id|default>", runModel},
	{"/compact", "summarise the conversation to save tokens", runCompact},
	{"/plan", "have Claude read the code and write the task's plan (Plan tab); /plan <goal>", runPlan},
	{"/keys", "change the app's ⌘ shortcuts (opens the keys file; save to apply)", runKeys},
	{"/new", "start a fresh Claude session in the same worktree (no earlier context); /new <message> sends it", runNew},
	{"/doctor", "check that Claude Code, the GitHub CLI and git are installed and signed in; ⏎ fixes one", func(m *Model, t *tasks.Task, _ string) {
		m.pending = m.openSetup()
	}},
	{"/login", "sign in to the task's agent again, in a shell (Claude: claude auth login)", func(m *Model, t *tasks.Task, _ string) {
		m.pending = m.signIn(*m.setup.Agent(agentName(*t)))
	}},
	{"/shell", "open a shell in the worktree, in the panel on the right (up to 4)", func(m *Model, t *tasks.Task, _ string) {
		m.pending = m.addShell()
	}},
}

// command runs the slash command typed as text.
func (m *Model) command(t *tasks.Task, text string) {
	name, arg, _ := strings.Cut(text, " ")
	for _, c := range slashCommands {
		if c.name == name {
			c.run(m, t, strings.TrimSpace(arg))
			return
		}
	}
	m.add(t.ID, chat.You, text)
	if agentName(*t) != agent.Claude.Name {
		m.add(t.ID, chat.Error, "unknown command "+name+" · crew's commands are "+slashNames()+"; Claude Code's own work only with Claude")
		return
	}
	if !m.isClaudeCommand(*t, name) {
		m.add(t.ID, chat.Error, "unknown command "+name+" · type / to see what's available")
		return
	}
	// A Claude Code command or skill from the repo's .claude/ or ~/.claude: send it as typed.
	m.conv(t.ID).last = nil // not a message to send again if it fails on auth
	switch {
	case m.conv(t.ID).busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then run "+name)
	case t.Session == "":
		m.pending = m.start(*t, text) // the command must lead the prompt, so no task brief this time
	default:
		m.pending = m.run(*t, text, hasSession(*t))
	}
}

// slashNames is "/model, /compact" for hints and errors.
func slashNames() string {
	var names []string
	for _, c := range slashCommands {
		names = append(names, c.name)
	}
	return strings.Join(names, ", ")
}

func runModel(m *Model, t *tasks.Task, arg string) {
	if arg == "" {
		m.openModelMenu()
		return
	}
	m.add(t.ID, chat.You, "/model "+arg)
	if arg == "default" {
		arg = ""
	}
	t.Model = arg
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	m.add(t.ID, chat.Tool, "model set to "+m.modelName(*t)+" from the next message on")
}

// runCompact asks Claude Code to summarise the conversation so later messages re-send far fewer tokens.
func runCompact(m *Model, t *tasks.Task, _ string) {
	m.add(t.ID, chat.You, "/compact")
	switch {
	case agentName(*t) == agent.Gemini.Name:
		m.add(t.ID, chat.Error, "Gemini compresses its context on its own when it gets long · /new starts a fresh session")
	case t.Session == "" || !hasSession(*t):
		m.add(t.ID, chat.Error, "nothing to compact yet, this session hasn't had a turn")
	case m.conv(t.ID).busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then /compact")
	default:
		m.conv(t.ID).last = nil // not a message to send again if it fails on auth
		m.pending = m.run(*t, "/compact", true)
	}
}

// runNew starts a new Claude session for the task, in the same worktree and branch, so the next
// message doesn't re-send the earlier conversation. The old session's usage keeps counting.
func runNew(m *Model, t *tasks.Task, arg string) {
	switch {
	case m.conv(t.ID).busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then /new")
		return
	case t.Session == "" || !hasSession(*t):
		m.add(t.ID, chat.Error, "this is already a fresh session")
		return
	}
	m.add(t.ID, chat.You, "/new")
	t.Previous = append(t.Previous, t.Session)
	t.Session = agent.NewSessionID()
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	m.add(t.ID, chat.Tool, "new session · Claude won't see the conversation above; it gets the task description with your next message")
	if arg != "" {
		m.pending = m.send(*t, outgoing{text: arg})
	}
}
