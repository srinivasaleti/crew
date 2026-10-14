package app

import (
	"fmt"
	"maps"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// ⌘ shortcuts. The app (desktop/) sends ⌘ key combinations to crew as escape sequences, and crew
// runs the action they're bound to, for the panel you're in. The bindings are defaultShortcuts,
// changed by ~/.crew/keys (one "keys = action" per line; /keys opens it), which is re-read when it
// changes. A ⌘ letter that isn't bound does what ctrl and the letter do.

type shortcutAction struct {
	help string
	run  func(m Model) (tea.Model, tea.Cmd)
}

// shortcutActions are what a ⌘ key can do, with what each one does for the keys file. Filled in
// init: the actions reach code that reads the slash commands, which list /keys, which reads this.
var shortcutActions map[string]shortcutAction

func init() {
	shortcutActions = map[string]shortcutAction{
		"panel-left":  {"move to the panel on the left (the editor goes back to its file tree)", func(m Model) (tea.Model, tea.Cmd) { return m.panelMove("shift+left") }},
		"panel-right": {"move to the panel on the right", func(m Model) (tea.Model, tea.Cmd) { return m.panelMove("shift+right") }},
		"panel-up":    {"move to the shell above", func(m Model) (tea.Model, tea.Cmd) { return m.panelMove("shift+up") }},
		"panel-down":  {"move to the shell below", func(m Model) (tea.Model, tea.Cmd) { return m.panelMove("shift+down") }},
		"panel-prev":  {"the previous panel: task list, main pane, shells", func(m Model) (tea.Model, tea.Cmd) { m.panelCycle(-1); return m, nil }},
		"panel-next":  {"the next panel: task list, main pane, then each shell", func(m Model) (tea.Model, tea.Cmd) { m.panelCycle(1); return m, nil }},
		"shell-new": {"open a new shell in the task's worktree (in the editor: select the next match)", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
			}
			return m, m.addShell()
		}},
		"quit": {"ask before quitting crew (pressed again, it quits); ⌘Q in the app comes here", func(m Model) (tea.Model, tea.Cmd) {
			if m.modal == quitModal {
				return m, tea.Quit
			}
			m.openQuit()
			return m, nil
		}},
		"home": {"home: setup, and turning sections on or off", func(m Model) (tea.Model, tea.Cmd) { m.goHome(); return m, nil }},
		"task-delete": {"delete the task (asks first; its worktree is kept)", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m, nil // ⌫ with ⌘ is an editing key there, never a delete
			}
			m.openDelete(m.currentID())
			return m, nil
		}},
		"close": {"close the shell you're in, or the file open in the editor (never quits crew)", func(m Model) (tea.Model, tea.Cmd) { m.closeHere(); return m, nil }},
		"clear": {"clear the shell you're in (elsewhere: repaint the screen)", func(m Model) (tea.Model, tea.Cmd) { return m.clearHere() }},
		"tab-1": {"tab 1 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(0) }},
		"tab-2": {"tab 2 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(1) }},
		"tab-3": {"tab 3 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(2) }},
		"tab-4": {"tab 4 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(3) }},
		"tab-5": {"tab 5 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(4) }},
		"tab-6": {"tab 6 on the tab bar on screen (a task's, a PR's, an application's)", func(m Model) (tea.Model, tea.Cmd) { return m, m.jumpTab(5) }},
		"tab-prev": {"the previous tab (in the editor: the previous open file)", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgUp})
			}
			cur, _ := m.tabsHere()
			return m, m.gotoTab(cur - 1)
		}},
		"tab-next": {"the next tab (in the editor: the next open file)", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
			}
			cur, _ := m.tabsHere()
			return m, m.gotoTab(cur + 1)
		}},
		"task-new": {"create a task", func(m Model) (tea.Model, tea.Cmd) {
			m.listFocus = false
			cmd := m.taskSec().New(&m)
			return m, cmd
		}},
		"find": {"find in the editor, or search the tasks or PRs", func(m Model) (tea.Model, tea.Cmd) { return m.Update(tea.KeyMsg{Type: tea.KeyCtrlF}) }},
		"copy": {"copy the text you selected (in the editor, its selection)", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			}
			m.copyHere()
			return m, nil
		}},
		"redo": {"redo in the editor", func(m Model) (tea.Model, tea.Cmd) { return m.Update(tea.KeyMsg{Type: tea.KeyCtrlY}) }},
		"maximize": {"maximise the panel you're in (hides the task list); again to restore", func(m Model) (tea.Model, tea.Cmd) {
			m.zoom = !m.zoom
			if m.zoom && m.listFocus {
				m.focusOn(areaMain, -1)
			}
			m.layout()
			m.refresh()
			return m, nil
		}},
		"shells-wider":    {"make the shells wider", func(m Model) (tea.Model, tea.Cmd) { m.resizeShellPanel(5); return m, nil }},
		"shells-narrower": {"make the shells narrower", func(m Model) (tea.Model, tea.Cmd) { m.resizeShellPanel(-5); return m, nil }},
		"list":            {"back to the list: a task to all tasks, a PR to all PRs (esc does it too)", func(m Model) (tea.Model, tea.Cmd) { m.goUp(); return m, nil }},
		"item-prev":       {"open the previous task or PR in its list", func(m Model) (tea.Model, tea.Cmd) { return m, m.stepItem(-1) }},
		"item-next":       {"open the next task or PR in its list", func(m Model) (tea.Model, tea.Cmd) { return m, m.stepItem(1) }},
		"palette":         {"list every command and shortcut, and run one", func(m Model) (tea.Model, tea.Cmd) { m.openPalette(); return m, nil }},
		"comment": {"in the editor, comment the line; elsewhere, the command list", func(m Model) (tea.Model, tea.Cmd) {
			if m.editActive() {
				return m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
			}
			m.openPalette()
			return m, nil
		}},
		"setup":   {"setup: check Claude Code, the GitHub CLI and git, and install or sign in", func(m Model) (tea.Model, tea.Cmd) { return m, m.openSetup() }},
		"sign-in": {"sign in to Claude again (in a shell)", func(m Model) (tea.Model, tea.Cmd) { return m, m.runFix(setup.ClaudeSignIn, "") }},
		"none":    {"nothing (turns a default off)", func(m Model) (tea.Model, tea.Cmd) { return m, nil }},
	}
}

// cmdKey runs the action bound to a ⌘ key; an unbound ⌘ letter does what ctrl and the letter do.
func (m Model) cmdKey(keys string) (tea.Model, tea.Cmd) {
	if action, ok := m.keys.Bindings()[keys]; ok {
		if a, ok := m.actions()[action]; ok {
			return a.run(m)
		}
		m.notice = fmt.Sprintf("%s is bound to %q, which isn't an action · /keys lists them", keys, action)
		return m, nil
	}
	if l := strings.TrimPrefix(keys, "cmd+"); len(l) == 1 && l[0] >= 'a' && l[0] <= 'z' {
		ctrl := tea.KeyMsg{Type: tea.KeyType(l[0] - 'a' + 1)} // ctrl+a is 1 … ctrl+z is 26
		if !m.editActive() && m.modal == noModal {
			// crew's command first, even in a shell: ⌘ keys are crew's, ctrl keys the shell's.
			if cmd, ok := m.runKeyCommand(ctrl.String()); ok {
				return m, cmd
			}
		}
		return m.Update(ctrl)
	}
	return m, nil
}

// panelMove moves between panels the way ⇧+arrows do, from anywhere (the editor too).
func (m Model) panelMove(k string) (tea.Model, tea.Cmd) {
	if m.editActive() { // the editor sits between its file tree and the shells
		switch n := len(m.taskShells()); {
		case k == "shift+left":
			m.focusOn(areaMain, -1)
		case k == "shift+right" && n > 0:
			m.focusOn(areaShell, min(m.lastShell, n-1))
		}
		return m, nil
	}
	m.moveArea(k)
	return m, nil
}

// panelCycle steps through the panels: the task list, the main pane, then each shell.
func (m *Model) panelCycle(d int) {
	n := 2 + len(m.taskShells())
	cur := 1
	switch m.area() {
	case areaTasks:
		cur = 0
	case areaShell:
		cur = 2 + m.shellFocus
	}
	switch next := (cur + d + n) % n; next {
	case 0:
		m.focusOn(areaTasks, -1)
	case 1:
		m.focusOn(areaMain, -1)
	default:
		m.focusOn(areaShell, next-2)
	}
}

// closeHere closes the shell you're in, or the file open in the editor.
func (m *Model) closeHere() {
	switch {
	case m.shellActive():
		m.taskShells()[m.shellFocus].HangUp() // the shell exits and its pane goes
	case m.editActive():
		m.filesOf(*m.current()).CloseDoc(m)
	default:
		m.notice = "Nothing to close here · ⌘W closes a shell or an open file"
	}
}

// clearHere clears the shell you're in, like a terminal's ⌘K; in the editor it deletes the line;
// elsewhere it repaints.
func (m Model) clearHere() (tea.Model, tea.Cmd) {
	if m.editActive() {
		return m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	}
	if m.shellActive() {
		m.taskShells()[m.shellFocus].Write([]byte{0x0c}) // ctrl+l: the shell clears its screen
		return m, nil
	}
	return m, tea.ClearScreen
}

func (m *Model) copyHere() {
	if m.drag.on {
		m.copySel()
	} else {
		m.notice = "Drag over text to select it; it's copied when you let go"
	}
}

// tabsHere is the tab bar on screen: the open tab and how many there are (n is 0 for none): a
// task's tabs, a listed PR's panes, or an application's tabs in REQUESTS.
func (m Model) tabsHere() (cur, n int) {
	switch {
	case m.current() != nil:
		return int(m.tab), len(tabNames)
	case m.ownerTabs():
		r := m.selRow()
		return m.owner(r).TabsHere(&m, r)
	}
	return 0, 0
}

// gotoTab switches to tab i of the tab bar on screen, wrapping around.
func (m *Model) gotoTab(i int) tea.Cmd {
	_, n := m.tabsHere()
	if n == 0 {
		return nil
	}
	i = (i%n + n) % n
	m.listFocus = false
	switch {
	case m.current() != nil:
		return m.setTab(tab(i))
	case m.ownerTabs():
		r := m.selRow()
		return m.owner(r).GotoTab(m, r, i)
	}
	m.refresh()
	return nil
}

// jumpTab is ⌘1…: tab i of the tab bar on screen, when it has one.
func (m *Model) jumpTab(i int) tea.Cmd {
	if _, n := m.tabsHere(); i < n {
		return m.gotoTab(i)
	}
	return nil
}

// actions are what a ⌘ key can do: crew's own, then the sections' (section.Action).
func (m Model) actions() map[string]shortcutAction {
	all := maps.Clone(shortcutActions)
	for _, s := range m.secs {
		for _, a := range s.Actions() {
			run := a.Run
			all[a.Name] = shortcutAction{a.Help, func(m Model) (tea.Model, tea.Cmd) {
				cmd := run(&m)
				return m, cmd
			}}
		}
	}
	return all
}

// actionHelps is what each ⌘ shortcut action does, by name, for the keys file.
func (m Model) actionHelps() map[string]string {
	helps := map[string]string{}
	for n, a := range m.actions() {
		helps[n] = a.help
	}
	return helps
}

// runKeys is /keys: open the keys file in the editor, written out with the defaults the first time.
func runKeys(m *Model, t *tasks.Task, _ string) {
	if _, err := os.Stat(m.keys.Path); err != nil {
		if err := os.WriteFile(m.keys.Path, []byte(m.keys.File(m.actionHelps())), 0o644); err != nil {
			m.add(t.ID, chat.Error, err.Error())
			return
		}
	}
	m.pending = m.setTab(tabFiles)
	m.openInEditor(m.keys.Path)
}
