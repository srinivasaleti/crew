package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/chat"
	"crew/internal/task"
)

// keyCommand is a shortcut that does the same thing on every screen (home, chat, task list,
// Changes/Usage tabs). Screen-specific keys (typing, scrolling, switching tabs) stay in that
// screen's handler. Update tries these first; the help line is built from them.
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
	{[]string{"ctrl+n"}, "new task", nil, func(m *Model) tea.Cmd {
		m.listFocus = false
		return m.openNew()
	}},
	{[]string{"ctrl+s"}, "status", hasTask, func(m *Model) tea.Cmd { m.openStatus(); return nil }},
	{[]string{"ctrl+d"}, "delete task", hasTask, func(m *Model) tea.Cmd { m.openDelete(); return nil }},
	{[]string{"ctrl+g"}, "terminals", hasShells, func(m *Model) tea.Cmd { m.focusNext(); return nil }},
	{[]string{"ctrl+f"}, "filter", nil, func(m *Model) tea.Cmd { return m.openFilter() }},
	{[]string{"ctrl+t"}, "new terminal", hasTask, func(m *Model) tea.Cmd { return m.addShell() }},
	{[]string{"ctrl+r"}, "actions", hasTask, func(m *Model) tea.Cmd {
		m.expandActions = !m.expandActions
		m.refresh()
		return nil
	}},
	{[]string{"ctrl+o"}, "archived", nil, func(m *Model) tea.Cmd { m.toggleArchived(); return nil }},
	{[]string{"ctrl+x"}, "stop agent", agentRunning, func(m *Model) tea.Cmd { m.stopAgent(); return nil }},
	// iTerm's ⌘K wipes the screen without telling crew, which only redraws lines that change;
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

// keyHelp lists the shortcuts that apply right now, e.g. "ctrl+n new task  ctrl+c quit".
func (m Model) keyHelp() string {
	var parts []string
	for _, c := range keyCommands {
		if c.help != "" && (c.when == nil || c.when(&m)) {
			parts = append(parts, c.keys[0]+" "+c.help)
		}
	}
	return strings.Join(parts, "  ")
}

// slashCommand is a command typed in a task's message box. crew handles it; it's never sent as
// a message (a command may still ask the agent to do something, like /compact).
type slashCommand struct {
	name string
	help string
	run  func(m *Model, t *task.Task, arg string)
}

var slashCommands = []slashCommand{
	{"/model", "pick the model, or /model <name|id|default>", runModel},
	{"/compact", "summarise the conversation to save tokens", runCompact},
	{"/shell", "open a shell in the worktree, in the panel on the right (up to 4)", func(m *Model, t *task.Task, _ string) {
		m.pending = m.addShell()
	}},
}

// command runs the slash command typed as text.
func (m *Model) command(t *task.Task, text string) {
	name, arg, _ := strings.Cut(text, " ")
	for _, c := range slashCommands {
		if c.name == name {
			c.run(m, t, strings.TrimSpace(arg))
			return
		}
	}
	m.add(t.ID, chat.You, text)
	if !m.isClaudeCommand(*t, name) {
		m.add(t.ID, chat.Error, "unknown command "+name+" · type / to see what's available")
		return
	}
	// A Claude Code command or skill from the repo's .claude/ or ~/.claude: send it as typed.
	switch {
	case m.conv(t.ID).busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then run "+name)
	case t.Session == "":
		m.pending = m.start(*t, text) // the command must lead the prompt, so no task brief this time
	default:
		m.pending = m.run(*t, text, true)
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

func runModel(m *Model, t *task.Task, arg string) {
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
func runCompact(m *Model, t *task.Task, _ string) {
	m.add(t.ID, chat.You, "/compact")
	switch {
	case t.Session == "":
		m.add(t.ID, chat.Error, "nothing to compact yet, the task hasn't started")
	case m.conv(t.ID).busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then /compact")
	default:
		m.pending = m.run(*t, "/compact", true)
	}
}
