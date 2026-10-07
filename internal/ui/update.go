package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/requests"
	"crew/internal/task"
)

// Messages produced by background work (git, agent turns).
type (
	startedMsg struct {
		id, repo, worktree, base, prompt string
		err                              error
	}
	diffMsg struct {
		id, diff, summary string
		err               error
	}
	eventMsg struct {
		id string
		ev agent.Event
		ch <-chan agent.Event
	}
	turnEndMsg struct{ id string }
)

// shiftEnter holds the ways terminals report Shift+Enter once main turns on xterm's
// modifyOtherKeys (ESC[>4;1m): xterm style and CSI-u style. bubbletea v1 doesn't decode
// them and delivers them as an unknown-CSI message whose String() has this form.
var shiftEnter = map[string]bool{
	fmt.Sprintf("?CSI%+v?", []byte("27;2;13~")): true,
	fmt.Sprintf("?CSI%+v?", []byte("13;2u")):    true,
}

// Update handles a message, then starts loading whatever the screen now waits for (a listed PR).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(Model)
	if load := nm.autoLoad(); load != nil {
		cmd = tea.Batch(cmd, load)
	}
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.name.Width, m.folder.Width = 44, 44
		m.refresh()
		return m, nil

	case startedMsg:
		t := m.find(msg.id)
		if msg.err != nil || t == nil {
			m.conv(msg.id).busy = false
			m.conv(msg.id).next = nil // setting up failed; send again once it's fixed
			if msg.err != nil {
				m.add(msg.id, chat.Error, msg.err.Error())
			}
			return m, nil
		}
		t.Repo, t.Worktree, t.Base, t.Session = msg.repo, msg.worktree, msg.base, agent.NewSessionID()
		m.cfg.Tasks.Save(*t)
		m.add(t.ID, chat.Tool, "worktree ready: "+msg.worktree)
		return m, m.run(*t, msg.prompt, false)

	case eventMsg:
		ev, c := msg.ev, m.conv(msg.id)
		switch ev.Kind {
		case agent.KindDelta: // the reply as it's written; the full text replaces it
			if !ev.Sub {
				c.live += ev.Text
				if msg.id == m.currentID() {
					m.refresh()
				}
			}
		case agent.KindText:
			if !ev.Sub {
				c.live = ""
			}
			m.addMsg(msg.id, chat.Message{Role: chat.Agent, Text: ev.Text, Sub: ev.Sub})
		case agent.KindTool:
			m.addMsg(msg.id, chat.Message{Role: chat.Tool, Text: ev.Text, Sub: ev.Sub})
		case agent.KindResult:
			m.addMsg(msg.id, chat.Message{Role: chat.Result, Text: ev.Text, Sub: ev.Sub})
		case agent.KindError:
			m.add(msg.id, chat.Error, ev.Text)
		}
		if a := m.prAgent(false); a != nil && a.ID == msg.id && (m.tab == tabPR || m.selPR() != nil && m.prPane == paneConversation) {
			m.renderPR()
			if m.modal == prModal {
				m.refreshPopup(false) // answers to questions asked in the popup show as they come
			}
		}
		return m, wait(msg.id, msg.ch)

	case turnEndMsg:
		c := m.conv(msg.id)
		c.busy, c.live = false, ""
		if c.stop != nil {
			c.stop() // releases the turn's context
			c.stop = nil
		}
		if c.next == nil && !c.stopped {
			m.notifyTurn(msg.id)
		}
		if c.stopped && c.next == nil {
			m.add(msg.id, chat.Tool, "stopped by you · your next message continues the session")
		}
		var next tea.Cmd
		if t := m.find(msg.id); t != nil && c.next != nil { // the message you stopped it for
			q := *c.next
			c.next = nil
			next = m.dispatch(*t, q)
		}
		if msg.id == m.currentID() {
			m.refresh() // drop the "working…" line and fold the turn's actions
		}
		m.reqTurnEnded(msg.id)  // the REQUESTS chat may have written requests
		delete(m.files, msg.id) // the agent may have added files; refresh @ suggestions
		m.reloadEditor(msg.id)  // and changed files nvim has open
		m.loadPlan(msg.id)      // and marked plan steps
		if msg.id != m.currentID() {
			m.unread[msg.id] = true // "reply ready" dot until you open the task
		}
		if t := m.find(msg.id); t != nil {
			m.loadUsage(*t)
		}
		if m.tab != tabChat && msg.id == m.currentID() {
			return m, tea.Batch(next, m.loadTab()) // the agent may have changed files and usage; keep the open tab current
		}
		return m, next

	case prMsg:
		m.onPR(msg)
		return m, nil

	case prPollMsg:
		return m, m.pollPRs()

	case prLogMsg:
		m.notice = ""
		switch {
		case msg.err != nil:
			m.notice = "couldn't read the log: " + msg.err.Error()
		case msg.log == "":
			m.notice = "The check has no failed log lines · o opens it in the browser"
		default:
			st := m.prOf(msg.id)
			st.log, st.logName = msg.log, msg.name
			if m.showingPR(msg.id) {
				m.renderPR()
				m.vp.GotoBottom() // the error is usually at the end
			}
		}
		return m, nil

	case prPostedMsg:
		m.notice = msg.done
		if msg.err != nil {
			m.notice = "GitHub said: " + msg.err.Error()
		}
		return m, m.loadPRRef(m.refFor(msg.id), true) // shows the reply, or undoes a resolve that failed

	case prCreatedMsg:
		st := m.prOf(msg.id)
		st.loading = false
		if msg.err != nil {
			m.notice = "couldn't open the PR: " + msg.err.Error()
			return m, nil
		}
		m.notice = "Opened " + msg.url
		m.setStatus(msg.id, task.InPR)
		if t := m.find(msg.id); t != nil {
			return m, m.loadPR(*t, true)
		}
		return m, nil

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

	case shellOutputMsg:
		return m, m.onShellOutput(msg) // the view redraws from the emulator

	case shellExitMsg:
		m.onShellExit(msg) // closes its pane
		return m, nil

	case prListMsg:
		m.onPRList(msg)
		return m, nil

	case prDiffMsg:
		m.onPRDiff(msg)
		return m, nil

	case reviewMsg:
		m.onReview(msg)
		return m, nil

	case reviewPostedMsg:
		return m, m.onReviewPosted(msg)

	case requests.DoneMsg:
		m.reqv.Done(msg)
		m.refresh()
		return m, nil

	case spinMsg:
		return m, m.onSpin()

	case tea.FocusMsg:
		m.blurred = false
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
			m.mousePopup(msg)
			return m, nil
		}
		if wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown; wheel && m.modal == reqsModal {
			if p := m.reqv.Popup(); p != nil {
				p.Wheel(msg.Button == tea.MouseButtonWheelDown)
			}
			return m, nil
		} else if wheel && m.modal == noModal && m.onReqsPage() {
			m.showReqs().Wheel(msg.Button == tea.MouseButtonWheelDown)
			m.refresh()
			return m, nil
		}
		if m.modal == noModal && m.mousePR(msg) {
			next := m.pending
			m.pending = nil
			return m, next
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
			return m.updateEditor(msg) // every key goes to nvim; ctrl+] or ⇧← returns to the tree
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
		if m.selPR() != nil {
			return m.updatePRRow(msg) // a PR from the PULL REQUESTS list
		}
		if m.onReqsPage() {
			return m.updateReqsPage(msg) // REQUESTS, or an application
		}
		if _, ok := m.reqItem(); ok {
			return m.updateReqRow(msg) // a request, or the REQUESTS chat
		}
		return m.updateChat(msg)

	case fmt.Stringer: // must stay after tea.KeyMsg, which is also a Stringer
		if shiftEnter[msg.String()] && m.modal == noModal && m.chatTask() != nil {
			if m.shellActive() {
				return m.updateShell(tea.KeyMsg{Type: tea.KeyEnter}) // shells treat it as enter
			}
			m.input.InsertString("\n")
			m.layout()
			return m, nil
		}
		if r, ok := decodeCmdKey(msg.String()); ok {
			return m.cmdKey(r) // ⌘ keys from the app
		}
		if k, ok := decodeModifiedKey(msg.String()); ok {
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
	case "left", "right":
		if m.input.Value() == "" && m.current() != nil { // with text typed, the arrows move the cursor
			return m.updateTabKeys(msg)
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
		default:
			// Home has nothing to scroll, so the arrows walk the list, which then keeps the keys.
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
	if m.onReqsPage() && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && strings.ContainsRune("nfxmsEtc", msg.Runes[0]) {
		m.focusOn(areaMain, -1) // the requests screen's letters act on it from the sidebar too
		return m.updateReqsPage(msg)
	}
	switch msg.String() {
	case "up":
		m.moveSel(-1)
	case "down":
		m.moveSel(1)
	case "right", "enter", "tab", " ":
		if m.selRow().kind != rowHome {
			m.focusOn(areaMain, -1) // into the task's chat, the PR or the list's table
			if m.input.Focused() {
				return m, m.input.Focus() // starts the cursor blinking
			}
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

// csiBytes is the sequence inside an escape bubbletea didn't recognise, which it reports as
// "?CSI[49 51 59 50 117]?" (the bytes after ESC [).
func csiBytes(s string) (string, bool) {
	inner, ok := strings.CutPrefix(s, "?CSI[")
	if inner, ok = strings.CutSuffix(inner, "]?"); !ok {
		return "", false
	}
	var raw []byte
	for _, f := range strings.Fields(inner) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return "", false
		}
		raw = append(raw, byte(n))
	}
	return string(raw), true
}

// decodeModifiedKey turns a key reported in xterm's modifyOtherKeys form (ESC[27;<mod>;<code>~)
// or CSI-u form (ESC[<code>;<mod>u) back into a normal key. main turns modifyOtherKeys on for
// Shift+Enter, and the terminal then reports some other combinations (like ctrl+]) this way too;
// bubbletea v1 hands those over undecoded, as a Stringer of the form "?CSI[<bytes>]?".
func decodeModifiedKey(s string) (tea.KeyMsg, bool) {
	seq, ok := csiBytes(s)
	if !ok {
		return tea.KeyMsg{}, false
	}
	var code, mod int
	switch {
	case strings.HasPrefix(seq, "27;") && strings.HasSuffix(seq, "~"):
		if _, err := fmt.Sscanf(seq, "27;%d;%d~", &mod, &code); err != nil {
			return tea.KeyMsg{}, false
		}
	case strings.HasSuffix(seq, "u"):
		if _, err := fmt.Sscanf(seq, "%d;%du", &code, &mod); err != nil {
			return tea.KeyMsg{}, false
		}
	default:
		return tea.KeyMsg{}, false
	}
	bits := mod - 1 // 1 shift, 2 alt, 4 ctrl
	alt, ctrl := bits&2 != 0, bits&4 != 0
	switch {
	case ctrl && (code >= '@' && code <= '_' || code >= 'a' && code <= 'z'):
		return tea.KeyMsg{Type: tea.KeyType(code & 0x1f), Alt: alt}, true // control code, e.g. ctrl+] = 29
	case code == 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}, true
	case code == 9:
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case code == 27:
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}, true
	case code == 127:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	case code >= 32 && code < 0x110000:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(code)}, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}
