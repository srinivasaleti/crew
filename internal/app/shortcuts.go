package app

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/chat"
	"crew/internal/jira"
	"crew/internal/setup"
	"crew/internal/tasks"
)

// ⌘ shortcuts. The app (desktop/) sends ⌘ key combinations to crew as escape sequences, and crew
// runs the action they're bound to, for the panel you're in. The bindings are defaultShortcuts,
// changed by ~/.crew/keys (one "keys = action" per line; /keys opens it), which is re-read when it
// changes. A ⌘ letter that isn't bound does what ctrl and the letter do.

type shortcut struct{ keys, action string }

var defaultShortcuts = []shortcut{
	{"cmd+alt+left", "panel-left"}, {"cmd+alt+right", "panel-right"},
	{"cmd+alt+up", "panel-up"}, {"cmd+alt+down", "panel-down"},
	{"cmd+[", "panel-prev"}, {"cmd+]", "panel-next"}, {"cmd+alt+t", "panel-next"},
	{"cmd+d", "shell-new"}, {"cmd+shift+d", "shell-new"},
	{"cmd+w", "close"},
	{"cmd+k", "clear"},
	{"cmd+1", "tab-1"}, {"cmd+2", "tab-2"}, {"cmd+3", "tab-3"}, {"cmd+4", "tab-4"}, {"cmd+5", "tab-5"}, {"cmd+6", "tab-6"},
	{"cmd+shift+[", "tab-prev"}, {"cmd+shift+]", "tab-next"},
	{"cmd+f", "find"},
	{"cmd+c", "copy"},
	{"cmd+shift+z", "redo"},
	{"cmd+shift+p", "palette"}, {"cmd+/", "comment"},
	{"cmd+shift+f", "focus"},
	{"cmd+backspace", "task-delete"},
	{"cmd+shift+h", "home"},
	{"cmd+shift+enter", "maximize"},
	{"cmd+up", "list"}, {"cmd+shift+up", "item-prev"}, {"cmd+shift+down", "item-next"},
	{"cmd+alt+shift+left", "shells-wider"}, {"cmd+alt+shift+right", "shells-narrower"},
}

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
		"task-new": {"create a task", func(m Model) (tea.Model, tea.Cmd) { m.listFocus = false; return m, m.openNew() }},
		"find":     {"find in the editor, or search the tasks or PRs", func(m Model) (tea.Model, tea.Cmd) { return m.Update(tea.KeyMsg{Type: tea.KeyCtrlF}) }},
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
		"focus": {"start a focus session on the task you're in, or stop the one running (FOCUS)", func(m Model) (tea.Model, tea.Cmd) { return m, m.toggleFocus() }},
		"break": {"start a break now, or end the one running (FOCUS)", func(m Model) (tea.Model, tea.Cmd) {
			if m.fc.clock.Kind == "break" {
				return m, m.stopClock()
			}
			return m, m.startBreak()
		}},
		"setup":   {"setup: check Claude Code, the GitHub CLI and git, and install or sign in", func(m Model) (tea.Model, tea.Cmd) { return m, m.openSetup() }},
		"sign-in": {"sign in to Claude again (in a shell)", func(m Model) (tea.Model, tea.Cmd) { return m, m.runFix(setup.ClaudeSignIn, "") }},
		"none":    {"nothing (turns a default off)", func(m Model) (tea.Model, tea.Cmd) { return m, nil }},
	}
}

// keymap is the bindings in use, re-read when the keys file changes.
type keymap struct {
	path  string
	mod   time.Time
	binds map[string]string
}

// bindings are the defaults with the keys file's lines on top.
func (k *keymap) bindings() map[string]string {
	st, err := os.Stat(k.path)
	if k.binds != nil && (err != nil && k.mod.IsZero() || err == nil && st.ModTime().Equal(k.mod)) {
		return k.binds
	}
	k.binds = map[string]string{}
	for _, s := range defaultShortcuts {
		k.binds[s.keys] = s.action
	}
	if err != nil {
		return k.binds
	}
	k.mod = st.ModTime()
	data, _ := os.ReadFile(k.path)
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		keys, action, ok := strings.Cut(line, "=")
		if ok {
			k.binds[normKeys(keys)] = strings.TrimSpace(action)
		}
	}
	return k.binds
}

// normKeys writes key names one way: lower case, no spaces, "option" and "opt" as alt.
func normKeys(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	return strings.NewReplacer("option+", "alt+", "opt+", "alt+", "⌘", "cmd+", "command+", "cmd+").Replace(s)
}

// keysFile is the keys file with every binding in use, and the actions there are.
func (k *keymap) keysFile() string {
	var b strings.Builder
	b.WriteString("# crew's ⌘ shortcuts (in the app). One per line: keys = action. Save to apply.\n")
	b.WriteString("# Keys: cmd plus any of ctrl, alt (option), shift, then a letter, digit, [ ], enter, backspace or an arrow\n")
	b.WriteString("# (left, right, up, down). A ⌘ letter that isn't bound does what ctrl and the letter do.\n#\n# Actions:\n")
	var names []string
	for n := range shortcutActions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "#   %-12s %s\n", n, shortcutActions[n].help)
	}
	b.WriteString("\n")
	binds := k.bindings()
	var keys []string
	for key := range binds {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "%s = %s\n", key, binds[key])
	}
	return b.String()
}

// decodeCmdKey names a ⌘ key the app sends: ESC [ code ; mods u, or ESC [ 1 ; mods A–D for the
// arrows, with the super bit (8) in mods. Terminals keep ⌘ keys to themselves; only the app sends them.
func decodeCmdKey(s string) (string, bool) {
	seq, ok := csiBytes(s)
	if !ok || len(seq) < 3 {
		return "", false
	}
	end := seq[len(seq)-1]
	code, mod, ok := strings.Cut(seq[:len(seq)-1], ";")
	c, err1 := strconv.Atoi(code)
	md, err2 := strconv.Atoi(mod)
	if !ok || err1 != nil || err2 != nil || (md-1)&8 == 0 {
		return "", false
	}
	var key string
	switch {
	case end >= 'A' && end <= 'D' && c == 1:
		key = map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left"}[end]
	case end == 'u' && c == 13:
		key = "enter"
	case end == 'u' && c > 32 && c < 127:
		key = string(rune(c))
	case end == 'u' && c == 127:
		key = "backspace"
	default:
		return "", false
	}
	bits, name := md-1, "cmd+"
	for _, mm := range []struct {
		bit  int
		name string
	}{{4, "ctrl+"}, {2, "alt+"}, {1, "shift+"}} {
		if bits&mm.bit != 0 {
			name += mm.name
		}
	}
	return name + key, true
}

// cmdKey runs the action bound to a ⌘ key; an unbound ⌘ letter does what ctrl and the letter do.
func (m Model) cmdKey(keys string) (tea.Model, tea.Cmd) {
	if action, ok := m.keys.bindings()[keys]; ok {
		if a, ok := shortcutActions[action]; ok {
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
		m.taskShells()[m.shellFocus].hangUp() // the shell exits and its pane goes
	case m.editActive():
		m.closeDoc(m.editors[m.currentID()])
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
		m.taskShells()[m.shellFocus].in.Write([]byte{0x0c}) // ctrl+l: the shell clears its screen
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
	case m.selPR() != nil:
		return m.prPane, len(paneNames)
	case m.selIssue() != nil:
		return m.issuePane, len(jira.PaneNames)
	case m.onReqsPage():
		return m.showReqs().Pane()
	case m.onRemPage():
		return m.remPane, len(remPaneNames)
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
	case m.selPR() != nil:
		m.setPRPane(i)
	case m.selIssue() != nil:
		m.setIssuePane(i)
	case m.onRemPage():
		cmd := m.setRemPane(i)
		m.refresh()
		return cmd
	default:
		m.reqv.SetPane(i)
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

// runKeys is /keys: open the keys file in the editor, written out with the defaults the first time.
func runKeys(m *Model, t *tasks.Task, _ string) {
	if _, err := os.Stat(m.keys.path); err != nil {
		if err := os.WriteFile(m.keys.path, []byte(m.keys.keysFile()), 0o644); err != nil {
			m.add(t.ID, chat.Error, err.Error())
			return
		}
	}
	m.pending = m.setTab(tabFiles)
	m.openInEditor(m.keys.path)
}

// activeKeys is the keymap in use, for keyNames (the editor draws its own hints without the Model).
var activeKeys *keymap

var ctrlKey = regexp.MustCompile(`\bctrl\+([a-z])\b`)

// keyNames writes crew's ctrl keys in hints as the ⌘ keys you press: ctrl+f as ⌘F. Hints only ever
// show ⌘ keys; a ⌘ letter bound to an action that isn't its ctrl key (a /keys rebinding) keeps ctrl.
func keyNames(s string) string {
	if activeKeys == nil {
		return s
	}
	binds := activeKeys.bindings()
	return ctrlKey.ReplaceAllStringFunc(s, func(k string) string {
		letter := k[len("ctrl+"):]
		if action, bound := binds["cmd+"+letter]; bound && ctrlFor[action] != k {
			return k
		}
		return "⌘" + strings.ToUpper(letter)
	})
}

// ctrlFor is the ctrl key an action does the same as, so its ⌘ key can stand for it in hints.
var ctrlFor = map[string]string{"find": "ctrl+f", "redo": "ctrl+y", "close": "ctrl+w", "copy": "ctrl+c", "clear": "ctrl+k", "shell-new": "ctrl+d"}
