package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		switch msg.ev.Kind {
		case agent.KindText:
			m.add(msg.id, chat.Agent, msg.ev.Text)
		case agent.KindTool:
			m.add(msg.id, chat.Tool, msg.ev.Text)
		case agent.KindError:
			m.add(msg.id, chat.Error, msg.ev.Text)
		}
		return m, wait(msg.id, msg.ch)

	case turnEndMsg:
		m.conv(msg.id).busy = false
		m.setStatus(msg.id, task.Waiting)
		if m.changes && msg.id == m.currentID() {
			return m, m.loadDiff() // the agent may have changed files; keep the open view current
		}
		return m, nil

	case diffMsg:
		if !m.changes || msg.id != m.currentID() {
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

	case tea.MouseMsg: // wheel/trackpad scrolls the chat or the diff; must stay before fmt.Stringer
		if m.modal != noModal {
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		m.notice = ""
		if m.modal != noModal {
			return m.updateModal(msg)
		}
		if m.changes {
			return m.updateChanges(msg)
		}
		if m.listFocus {
			return m.updateList(msg)
		}
		return m.updateChat(msg)

	case fmt.Stringer: // must stay after tea.KeyMsg, which is also a Stringer
		if shiftEnter[msg.String()] && m.modal == noModal && m.current() != nil {
			m.input.InsertString("\n")
			m.layout()
			return m, nil
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
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+n":
		return m, m.openNew()
	case "ctrl+d":
		m.openDelete()
		return m, nil
	case "tab", "shift+tab", "ctrl+g":
		return m, m.openChanges()
	case "right":
		if m.input.Value() == "" { // with text typed, arrows move the cursor instead
			return m, m.openChanges()
		}
	case "left":
		if m.input.Value() == "" && m.current() != nil {
			m.listFocus = true
			m.input.Blur()
			return m, nil
		}
	case "up":
		if m.input.LineCount() > 1 {
			break // move between lines of a multi-line message
		}
		m.moveSel(-1)
		return m, nil
	case "down":
		if m.input.LineCount() > 1 {
			break
		}
		m.moveSel(1)
		return m, nil
	case "esc":
		m.sel = 0
		m.onSelect()
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
	m.layout()
	return m, cmd
}

// updateList handles keys while the task list has focus.
func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+n":
		m.listFocus = false
		return m, m.openNew()
	case "ctrl+d":
		m.openDelete()
	case "up":
		m.moveSel(-1)
	case "down":
		m.moveSel(1)
	case "right", "enter", "tab":
		if m.current() != nil {
			m.listFocus = false
			return m, m.input.Focus()
		}
	case "esc":
		m.sel = 0
		m.onSelect()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}
