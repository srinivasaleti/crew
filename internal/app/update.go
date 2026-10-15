package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/prview"
	"crew/internal/widgets/term"
)

// Messages produced by background work (git, agent turns).
type (
	diffMsg struct {
		id, diff, summary string
		err               error
	}
)

// Update handles a message, then starts loading whatever the screen now waits for (a listed PR).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(Model)
	for _, s := range nm.secs {
		if load := s.AutoLoad(&nm); load != nil {
			cmd = tea.Batch(cmd, load)
		}
	}
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	for _, s := range m.secs { // a section's own messages (FOCUS also notes your keys and clicks, and passes them on)
		if cmd, ok := s.Update(&m, msg); ok {
			return m, cmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.refresh()
		return m, nil

	case chat.StartedMsg:
		cmd := m.chat.Started(&m, msg)
		return m, cmd

	case chat.EventMsg:
		switch msg.Ev.Kind {
		case agent.KindError:
			m.turnError(msg.ID, msg.Ev.Text)
		case agent.KindDone:
			if m.authFailed == chat.AgentName(*m.taskOr(msg.ID)) {
				m.authFailed = "" // a turn worked, so the agent's login does
			}
		default:
			m.chat.Event(&m, msg)
		}
		if a := m.prAgent(false); a != nil && a.ID == msg.ID && m.onPRView() {
			m.prv.Render(&m)
			if m.modal == prModal {
				m.prv.RefreshPopup(&m, false) // answers to questions asked in the popup show as they come
			}
		}
		return m, msg.Next()

	case chat.TurnEndMsg:
		next, notify := m.chat.TurnEnded(&m, msg.ID)
		if notify {
			m.notifyTurn(msg.ID)
		}
		if msg.ID == m.currentID() || m.chatTask() != nil && m.chatTask().ID == msg.ID {
			m.layout()  // the reply may end with questions, shown as buttons above the box
			m.refresh() // drop the "working…" line and fold the turn's actions
		}
		for _, s := range m.secs {
			s.TurnEnded(&m, msg.ID) // a section's chat may have changed its things
		}
		if n := m.prv.TakeReplies(msg.ID, m.lastReply(msg.ID)); n > 0 { // /fix-review's replies to reviewers
			m.add(msg.ID, chat.Tool, fmt.Sprintf("%d drafted repl%s to the PR's reviewers · on the PR tab, D posts them (twice), or r on one edits it first",
				n, map[bool]string{true: "y", false: "ies"}[n == 1]))
		}
		delete(m.files, msg.ID) // the agent may have added files; refresh @ suggestions
		m.reloadEditor(msg.ID)  // and changed files the editor has open
		m.loadPlan(msg.ID)      // and marked plan steps
		if msg.ID != m.currentID() {
			m.unread[msg.ID] = true // "reply ready" dot until you open the task
		}
		if t := m.find(msg.ID); t != nil {
			m.loadUsage(*t)
		}
		if m.tab != tabChat && msg.ID == m.currentID() {
			return m, tea.Batch(next, m.loadTab()) // the agent may have changed files and usage; keep the open tab current
		}
		return m, next

	case prview.LoadedMsg, prview.LogMsg, prview.PostedMsg, prview.CreatedMsg:
		cmd, _ := m.prv.Update(&m, msg)
		return m, cmd

	case prPollMsg:
		return m, m.pollPRs()

	case diffMsg:
		if m.tab != tabChanges || msg.id != m.currentID() {
			return m, nil
		}
		if msg.err != nil {
			m.diffSummary = "error"
			m.vp.SetContent(warn.Render(msg.err.Error()))
			return m, nil
		}
		m.diffSummary = msg.summary
		m.vp.SetContent(clip(msg.diff, m.vp.Width))
		return m, nil

	case term.OutputMsg:
		return m, m.onShellOutput(msg) // the view redraws from the emulator

	case term.ExitMsg:
		m.onShellExit(msg) // closes its pane
		if msg.T.Fix {
			return m, m.checkSetup(true) // an install or sign-in finished
		}
		return m, nil

	case branchMsg:
		m.onBranch(msg)
		return m, nil

	case setupMsg:
		was := m.setup
		m.onSetup(msg)
		var cmds []tea.Cmd
		for _, s := range m.secs {
			cmds = append(cmds, s.SetupChanged(&m, was)) // e.g. just signed in: read now, not at the next poll
		}
		return m, tea.Batch(cmds...)

	case recheckMsg:
		return m, m.checkSetup(false)

	case spinMsg:
		return m, m.onSpin()

	case tea.FocusMsg:
		m.blurred = false
		if time.Since(m.setupAt) > time.Minute {
			return m, m.checkSetup(false) // you may have signed in or installed something elsewhere
		}
		return m, nil

	case tea.BlurMsg:
		m.blurred = true
		return m, nil

	case tea.MouseMsg: // must stay before fmt.Stringer
		if m.modal == noModal && m.mouseResize(msg) {
			return m, nil
		}
		if m.mouseSelect(msg) {
			return m, nil // a drag selected text, or a click opened a link
		}
		if m.modal == noModal && m.mouseEditor(msg) {
			return m, nil
		}
		if m.modal == noModal && m.mouseTerm(msg) {
			return m, nil // a shell or the editor took it
		}
		if m.modal == prModal {
			m.prv.MousePopup(&m, msg, m.sideOuter()+1)
			return m, nil
		}
		if wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown; wheel && m.modal == secModal {
			if s := m.modalSec(); s != nil {
				s.ModalWheel(&m, msg.Button == tea.MouseButtonWheelDown)
			}
			return m, nil
		} else if s, r, ok := m.page(); wheel && m.modal == noModal && ok && s.Wheel(&m, r, msg.Button == tea.MouseButtonWheelDown) {
			return m, nil // a section's page took it
		}
		if m.modal == noModal {
			if cmd, ok := m.prv.Mouse(&m, msg, m.sideOuter()+1, m.contentY()); ok {
				return m, cmd
			}
		}
		if m.modal == noModal && m.mouseTree(msg) {
			next := m.pending
			m.pending = nil
			return m, next
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.click(msg.X, msg.Y)
			next := m.pending
			m.pending = nil
			return m, next
		}
		if m.modal != noModal {
			return m, nil
		}
		if m.current() == nil && m.onGroupRow() && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			m.wheelOverview(msg.Button == tea.MouseButtonWheelDown)
			return m, nil
		}
		// wheel/trackpad scrolls the chat or the diff
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		m.notice = ""
		m.drag.on = false
		if m.modal != noModal {
			return m.updateModal(msg)
		}
		if m.moveArea(msg.String()) { // ⇧+arrows: tasks ⇄ chat ⇄ shells, from anywhere
			if m.current() == nil {
				m.refresh() // the overview's bar follows the keys
			}
			return m, nil
		}
		if msg.String() == "ctrl+t" && m.shellActive() {
			return m, m.addShell() // a new terminal even from inside a shell
		}
		if m.editActive() {
			cmd := m.filesOf(*m.current()).EditorKey(&m, msg) // every key goes to the editor; ctrl+] or ⇧← returns to the tree
			return m, cmd
		}
		if m.shellActive() {
			return m.updateShell(msg) // every key goes to the shell, ctrl+c included; ctrl+] leaves
		}
		if cmd, ok := m.runKeyCommand(msg.String()); ok {
			return m, cmd
		}
		if m.tab != tabChat {
			return m.updateTab(msg)
		}
		if m.listFocus {
			return m.updateList(msg)
		}
		if s, r, ok := m.page(); ok { // a section's own page
			if cmd, ok := s.Key(&m, r, msg); ok {
				return m, cmd
			}
		}
		return m.updateChat(msg)

	case fmt.Stringer: // must stay after tea.KeyMsg, which is also a Stringer
		if keys.ShiftEnter[msg.String()] && m.modal == noModal && m.chatTask() != nil {
			if m.shellActive() {
				return m.updateShell(tea.KeyMsg{Type: tea.KeyEnter}) // shells treat it as enter
			}
			m.input.InsertString("\n")
			m.layout()
			return m, nil
		}
		if r, ok := keys.DecodeCmd(msg.String()); ok {
			return m.cmdKey(r) // ⌘ keys from the app
		}
		if k, ok := keys.DecodeModified(msg.String()); ok {
			return m.Update(k) // e.g. ctrl+] arrives this way once modifyOtherKeys is on
		}
	}
	if m.modal != noModal {
		return m, nil
	}
	return m.typeInto(msg) // e.g. cursor blink
}

// updateChat handles keys on a task's Chat tab (and the home screen). Keys it doesn't
// claim go to the message box.
func (m Model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.homeKey(msg.String()); ok {
		return m, cmd
	}
	if m.onGroupRow() {
		if cmd, ok := m.overviewKey(msg); ok {
			return m, cmd
		}
	}
	if len(m.sugg) > 0 { // suggestions (@ files, / commands) take the navigation keys while open
		switch msg.String() {
		case "up":
			m.suggIdx = max(m.suggIdx-1, 0)
			return m, nil
		case "down":
			m.suggIdx = min(m.suggIdx+1, len(m.sugg)-1)
			return m, nil
		case "tab":
			m.acceptSuggestion(false)
			return m, nil
		case "enter":
			return m, m.acceptSuggestion(true) // runs it when it needs nothing more
		case "esc":
			m.sugg = nil
			m.layout()
			return m, nil
		}
	}
	switch k := msg.String(); k {
	case "tab", "shift+tab":
		if m.current() != nil { // a PR's chat switches its panes in updatePRRow
			return m, m.setTab(map[string]tab{"tab": tabChat + 1, "shift+tab": tab(len(tabNames) - 1)}[k])
		}
	case "up", "down":
		switch {
		case m.input.LineCount() > 1:
			break // move between lines of a multi-line message
		case m.chatTask() != nil:
			// In a chat the arrows scroll the conversation; ⇧← moves to the task list.
			if msg.String() == "up" {
				m.vp.ScrollUp(1)
			} else {
				m.vp.ScrollDown(1)
			}
			return m, nil
		case m.selRow().Kind == rowHome: // home's main pane: the arrows scroll it
			if msg.String() == "up" {
				m.vp.ScrollUp(1)
			} else {
				m.vp.ScrollDown(1)
			}
			return m, nil
		default:
			// Nothing here to scroll, so the arrows walk the list, which then keeps the keys.
			m.listFocus = true
			m.input.Blur()
			m.moveSel(map[string]int{"up": -1, "down": 1}[msg.String()])
			return m, nil
		}
	case "esc":
		if m.stopAgent() { // esc stops a running agent first, like in Claude Code
			return m, nil
		}
		m.goUp() // a task goes back to all tasks
		return m, nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case "enter":
		if v := m.input.Value(); strings.HasSuffix(v, "\\") {
			m.input.SetValue(strings.TrimSuffix(v, "\\") + "\n")
			m.layout()
			return m, nil
		}
		return m, m.submit()
	}
	return m.typeInto(msg)
}

// typeInto passes a message to the message box and resizes it to fit.
func (m Model) typeInto(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if _, isKey := msg.(tea.KeyMsg); isKey {
		m.suggest()
	}
	m.layout()
	return m, cmd
}

// updateList handles keys while the task list has focus.
func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.homeKey(msg.String()); ok {
		return m, cmd
	}
	if r := m.selRow(); m.owner(r) != nil {
		if cmd, ok := m.owner(r).ListKey(&m, r, msg); ok {
			return m, cmd
		}
	}
	switch msg.String() {
	case "up":
		m.moveSel(-1)
	case "down":
		m.moveSel(1)
	case "left": // up to the section the open item sits under; the list keeps the keys
		if p, ok := m.parentRow(); ok {
			m.selectRow(p)
		}
	case "enter":
		m.focusOn(areaMain, -1) // into home, the task's chat, the PR or the list's table
		if m.input.Focused() {
			return m, m.input.Focus() // starts the cursor blinking
		}
	case "esc":
		m.goUp()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}
