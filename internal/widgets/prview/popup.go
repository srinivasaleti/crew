package prview

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/browser"
	"crew/internal/gh"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
)

// diffHunk colours the code a line comment is on, keeping the last lines (where the comment is).
func diffHunk(h string, w int) string {
	lines := strings.Split(strings.TrimRight(h, "\n"), "\n")
	lines = lines[max(len(lines)-12, 0):]
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "@@"):
			l = look.Dim.Render(l)
		case strings.HasPrefix(l, "+"):
			l = green.Render(l)
		case strings.HasPrefix(l, "-"):
			l = look.Warn.Render(l)
		}
		if i == len(lines)-1 {
			l = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(l) // the commented line
		}
		lines[i] = look.Clip(l, w)
	}
	return strings.Join(lines, "\n")
}

// popupSize is the popup's outer size, inside the main pane.
func (v *View) popupSize(h Host) (w, ht int) {
	pw, bodyH := h.PaneSize()
	return max(pw-4, 40), max(bodyH-4, 12)
}

// PopupInner is the width of the popup's text.
func (v *View) PopupInner(h Host) int { w, _ := v.popupSize(h); return w - 4 }

func (v *View) openPopup(h Host, key string) {
	st := v.Of(h.Ref().Key)
	v.selectKey(h, key)
	st.popKey, st.input = key, ""
	h.SetPopup(true)
	v.RefreshPopup(h, true)
}

func (v *View) closePopup(h Host) {
	st := v.Of(h.Ref().Key)
	st.popKey, st.input = "", ""
	h.SetPopup(false)
	h.Input().Reset()
	h.Input().Blur()
	h.Layout()
	v.Render(h)
}

// RefreshPopup sizes the popup's viewport and redraws its text; top scrolls to the start, otherwise
// it stays put (or follows Claude's answer if you're at the bottom).
func (v *View) RefreshPopup(h Host, top bool) {
	st := v.Of(h.Ref().Key)
	it := v.itemByKey(h, st.popKey)
	if it == nil {
		v.closePopup(h)
		return
	}
	_, ht := v.popupSize(h)
	inputH := 0
	if st.input != "" {
		h.Input().SetWidth(v.PopupInner(h))
		inputH = h.Input().Height() + 1
	}
	follow := v.Pop.AtBottom()
	v.Pop.Width, v.Pop.Height = v.PopupInner(h), max(ht-2-3-inputH, 3) // border, title, rule, hint line
	v.Pop.SetContent(v.popupBody(h, *it, v.PopupInner(h)))
	switch {
	case top:
		v.Pop.GotoTop()
	case follow:
		v.Pop.GotoBottom()
	}
}

// popupBody is the conversation: every comment as Markdown, then what you asked Claude about it.
func (v *View) popupBody(h Host, it prItem, w int) string {
	var b []string
	say := func(c gh.Comment) {
		b = append(b, look.Bold.Render(c.Author)+look.Dim.Render("  "+look.Ago(c.At)), v.md.Render(markdown.Show(c.Body), w), "")
	}
	switch {
	case it.thread != nil:
		if h := it.thread.DiffHunk; h != "" {
			b = append(b, look.Sel.Render(where(it.thread)), diffHunk(h, w), "")
		}
		for _, c := range it.thread.Comments {
			say(c)
		}
	case it.review != nil:
		say(it.review.Comment)
	case it.comment != nil:
		say(*it.comment)
	case it.check != nil:
		b = append(b, checkIcon(it.check.State)+" "+it.check.Name, look.Dim.Render(it.check.URL), "",
			look.Dim.Render("Close this and press ⏎ on the check to read its failed log."), "")
	}
	turns := v.Of(h.Ref().Key).talk[it.key]
	c := &chat.Conv{}
	if a := h.Agent(false); a != nil {
		c = h.Chats().Conv(a.ID)
	}
	if len(turns) > 0 {
		b = append(b, look.Claude.Render("── with Claude ──"), "")
		for i, tt := range turns {
			b = append(b, look.Bold.Render("You")+look.Dim.Render(": ")+tt.asked, "")
			actions := 0
			for _, msg := range c.Msgs[min(tt.at+1, len(c.Msgs)):] {
				if msg.Role == chat.You {
					break
				}
				switch {
				case msg.Sub: // a subagent's steps are in the Chat tab
				case msg.Role == chat.Agent:
					b = append(b, v.md.Render(msg.Text, w))
				case msg.Role == chat.Tool:
					actions++
				case msg.Role == chat.Error:
					b = append(b, look.Warn.Render(msg.Text))
				}
			}
			if actions > 0 {
				b = append(b, look.Dim.Render(fmt.Sprintf("(%d actions · the chat lists them)", actions)))
			}
			if i == len(turns)-1 && c.Busy {
				b = append(b, look.Claude.Render("Claude is working…"))
			}
			b = append(b, "")
		}
	}
	return strings.Join(b, "\n")
}

// popupKeys is what the popup offers for a row.
// popupKeys is what the popup offers for a row; asking Claude and the code need a task (a listed PR
// is reviewed with v instead).
func popupKeys(it prItem, task bool) string {
	var keys []string
	if it.context != "" && task {
		keys = append(keys, "c ask Claude", "s fix it")
	}
	if it.thread != nil {
		keys = append(keys, "r reply in thread")
		if it.thread.Resolved {
			keys = append(keys, "x reopen")
		} else {
			keys = append(keys, "x resolve")
		}
		if task {
			keys = append(keys, "g go to code")
		}
	} else if it.review != nil || it.comment != nil {
		keys = append(keys, "r reply")
	}
	return strings.Join(append(keys, "o GitHub", "n next open", "esc close"), " · ")
}

// PopupView draws the popup: one row's whole conversation, and its keys.
func (v *View) PopupView(h Host) string {
	st := v.Of(h.Ref().Key)
	it := v.itemByKey(h, st.popKey)
	if it == nil {
		return ""
	}
	w, ht := v.popupSize(h)
	inner := v.PopupInner(h)
	title := it.about
	if title == "" {
		title = it.check.Name
	}
	status := ""
	switch {
	case it.thread != nil && it.thread.Resolved:
		status = "  " + doneMark + look.Dim.Render(" resolved")
	case it.thread != nil && it.thread.Outdated:
		status = "  " + oldMark + look.Dim.Render(" outdated (the code changed since)")
	case needsYou(*it):
		status = "  " + todoMark + orange.Render(" to do")
	}
	parts := []string{look.Clip(look.Bold.Render(title)+status, inner), look.Dim.Render(strings.Repeat("─", inner)), v.Pop.View()}
	switch st.input {
	case "ask":
		parts = append(parts, look.Claude.Render("↳ Ask Claude")+look.Dim.Render(" · ⏎ send · esc cancel"), h.Input().View())
	case "reply":
		label := yellow.Render("↳ Reply in this thread on GitHub") + look.Dim.Render(" · ⏎ posts it · esc cancel")
		if it.thread == nil {
			label = yellow.Render("↳ New PR comment quoting @"+strings.TrimPrefix(strings.Split(it.about, "'")[0], "@")) +
				look.Dim.Render(" (GitHub can't reply in place here) · ⏎ posts it · esc cancel")
		}
		parts = append(parts, look.Clip(label, inner), h.Input().View())
	}
	parts = append(parts, look.Clip(look.Dim.Render(popupKeys(*it, h.Ref().Task != nil)), inner))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(look.Accent).Padding(0, 1).Width(w - 2).Height(ht - 2)
	return box.Render(strings.Join(parts, "\n"))
}

// PopupKey handles keys while the popup is open.
func (v *View) PopupKey(h Host, msg tea.KeyMsg) (tea.Cmd, bool) {
	ref, st := h.Ref(), v.Of(h.Ref().Key)
	it := v.itemByKey(h, st.popKey)
	if it == nil {
		v.closePopup(h)
		return nil, true
	}
	k := msg.String()
	if st.input != "" {
		switch k {
		case "esc":
			st.input = ""
			h.Input().Reset()
			h.Input().Blur()
			v.RefreshPopup(h, false)
			return nil, true
		case "enter":
			val := h.Input().Value()
			if strings.HasSuffix(val, "\\") { // \⏎ adds a line, as in the chat
				h.Input().SetValue(strings.TrimSuffix(val, "\\") + "\n")
				v.RefreshPopup(h, false)
				return nil, true
			}
			text := strings.TrimSpace(val)
			if text == "" {
				return nil, true
			}
			mode := st.input
			st.input = ""
			h.Input().Reset()
			h.Input().Blur()
			var cmd tea.Cmd
			if mode == "ask" {
				cmd = v.askAbout(h, *it, text)
			} else {
				cmd = v.reply(h, ref, *it, text)
			}
			v.RefreshPopup(h, false)
			v.Pop.GotoBottom()
			return cmd, true
		}
		in := h.Input()
		in.Focus()
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		v.RefreshPopup(h, false)
		return cmd, true
	}
	switch k {
	case "esc", "q":
		v.closePopup(h)
	case "up", "down", "pgup", "pgdown", "k", "j":
		var cmd tea.Cmd
		v.Pop, cmd = v.Pop.Update(msg)
		return cmd, true
	case "home":
		v.Pop.GotoTop()
	case "end":
		v.Pop.GotoBottom()
	case "c", "r":
		if k == "c" && it.context == "" || k == "r" && it.thread == nil && it.review == nil && it.comment == nil {
			break
		}
		st.input = map[string]string{"c": "ask", "r": "reply"}[k]
		h.Input().Reset()
		h.Input().Placeholder = "e.g. “is this right?” or “fix it but keep the API”…"
		if k == "r" {
			h.Input().Placeholder = "Your reply…"
		}
		h.Input().SetHeight(3)
		h.Input().Focus()
		v.RefreshPopup(h, false)
	case "s":
		if it.context != "" {
			cmd := v.askAbout(h, *it, "Please fix this. Keep the change small, then tell me what you changed.")
			v.RefreshPopup(h, false)
			v.Pop.GotoBottom()
			return cmd, true
		}
	case "x":
		if it.thread != nil {
			cmd := v.toggleResolved(h, ref, it.thread)
			v.RefreshPopup(h, false)
			return cmd, true
		}
	case "g":
		if it.thread != nil {
			v.closePopup(h)
			return h.OpenAt(it.thread.Path, it.thread.Line), true
		}
	case "o":
		browser.Open(it.url)
	case "n":
		if v.nextOpen(h) {
			v.openPopup(h, v.selected(h).key)
		}
	}
	return nil, true
}

// MousePopup handles the mouse while the popup is open: the wheel scrolls it, a click outside it
// closes it. x0 is where the main pane's inside starts on the screen.
func (v *View) MousePopup(h Host, msg tea.MouseMsg, x0 int) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		v.Pop, _ = v.Pop.Update(msg)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		w, ht := v.popupSize(h)
		pw, bodyH := h.PaneSize()
		left, top := x0+(pw-w)/2, 1+(bodyH-2-ht)/2
		if msg.X < left || msg.X >= left+w || msg.Y < top || msg.Y >= top+ht {
			v.closePopup(h)
		}
	}
}
