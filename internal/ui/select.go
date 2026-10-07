package ui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
)

// Selecting and copying text with the mouse, panel by panel. crew turns on mouse reporting (for
// clicks, the wheel and the editor), so the terminal can't select text; crew does it instead,
// inside one panel at a time (the task list, the main pane, a shell), as if each were a window:
// drag to select, let go to copy. A plain click on a link opens it.

type point struct{ x, y int }

// rect is a panel's inside, in screen cells, edges included.
type rect struct{ x0, y0, x1, y1 int }

// textSel is a mouse selection: from a to b, kept inside one panel.
type textSel struct {
	armed    bool // the left button went down in a panel that can be selected in
	dragging bool
	on       bool // there's a selection to show (until the next key or click)
	pane     rect
	a, b     point
}

var selStyle = lipgloss.NewStyle().Reverse(true)

// paneAt is the panel at a screen position: its inside, and which one (-2 the task list, -1 the
// main pane, else a shell's index).
func (m Model) paneAt(x, y int) (rect, int, bool) {
	bodyH, cw := m.bodyHeight(), m.chatWidth()
	if y < 1 || y > bodyH-2 {
		return rect{}, 0, false
	}
	switch {
	case x >= 1 && x <= sideOuter-2:
		return rect{1, 1, sideOuter - 2, bodyH - 2}, -2, true
	case x >= sideOuter+1 && x <= sideOuter+cw:
		return rect{sideOuter + 1, 1, sideOuter + cw, bodyH - 2}, -1, true
	}
	pw := m.panelWidth()
	if ox := sideOuter + cw + 2; pw > 0 && x > ox && x < ox+pw-1 {
		top := 0
		for i, h := range paneHeights(bodyH, len(m.taskShells())) {
			if y > top && y < top+h-1 {
				return rect{ox + 1, top + 1, ox + pw - 2, top + h - 2}, i, true
			}
			top += h
		}
	}
	return rect{}, 0, false
}

// selectable says whether a drag at x, y selects text: not in the editor (it has its own
// selection), and not in a shell whose program takes the mouse (vim, less…).
func (m Model) selectable(x, y int) (rect, bool) {
	r, which, ok := m.paneAt(x, y)
	switch {
	case !ok:
		return r, false
	case which >= 0:
		return r, m.taskShells()[which].vt.Mode()&vt10x.ModeMouseMask == 0
	case which == -1 && m.editorTab() && m.modal == noModal:
		return r, x < sideOuter+1+m.treeWidth() // the tree is fine; the editor selects for itself
	}
	return r, true
}

// mouseSelect handles drags (select) and clicks on links. A press still goes on to the other
// handlers, so clicks keep working; it reports whether it used the event.
func (m *Model) mouseSelect(msg tea.MouseMsg) bool {
	at := point{msg.X, msg.Y}
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		m.drag.on = false // the text under it moves
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.drag = textSel{}
		if r, ok := m.selectable(msg.X, msg.Y); ok {
			m.drag.armed, m.drag.pane, m.drag.a = true, r, at
		}
	case msg.Action == tea.MouseActionMotion && m.drag.armed:
		r := m.drag.pane
		p := point{min(max(at.x, r.x0), r.x1), min(max(at.y, r.y0), r.y1)}
		if m.drag.dragging || p != m.drag.a {
			m.drag.dragging, m.drag.on, m.drag.b = true, true, p
		}
		return true
	case msg.Action == tea.MouseActionRelease && m.drag.armed:
		m.drag.armed = false
		if m.drag.dragging {
			m.drag.dragging = false
			m.copySel()
			return true
		}
		if u := m.urlAt(msg.X, msg.Y); u != "" {
			openURL(u)
			m.notice = "Opened " + u
			return true
		}
	}
	return false
}

// selRange is the columns the selection covers on screen row y, or ok false.
func (m Model) selRange(y int) (from, to int, ok bool) {
	s := m.drag
	a, b := s.a, s.b
	if b.y < a.y || b.y == a.y && b.x < a.x {
		a, b = b, a
	}
	if !s.on || y < a.y || y > b.y {
		return 0, 0, false
	}
	from, to = s.pane.x0, s.pane.x1+1
	if y == a.y {
		from = a.x
	}
	if y == b.y {
		to = b.x + 1
	}
	return from, to, to > from
}

// highlightSel shows the selection on the drawn screen.
func (m Model) highlightSel(screen string) string {
	if !m.drag.on {
		return screen
	}
	lines := strings.Split(screen, "\n")
	for y, line := range lines {
		if from, to, ok := m.selRange(y); ok {
			lines[y] = ansi.Cut(line, 0, from) + selStyle.Render(ansi.Strip(ansi.Cut(line, from, to))) + ansi.TruncateLeft(line, to, "")
		}
	}
	return strings.Join(lines, "\n")
}

// selText is the selected text as plain lines, without crew's chat bars and scroll bar.
func (m Model) selText() string {
	lines := strings.Split(m.screen(), "\n")
	var out []string
	for y := range lines {
		if from, to, ok := m.selRange(y); ok {
			l := strings.TrimRight(ansi.Strip(ansi.Cut(lines[y], from, to)), " │┃")
			out = append(out, strings.TrimPrefix(strings.TrimPrefix(l, "▌ "), "▌"))
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n ")
}

func (m *Model) copySel() {
	text := m.selText()
	if text == "" {
		m.drag.on = false
		return
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.notice = "couldn't copy: " + err.Error()
		return
	}
	n := strings.Count(text, "\n") + 1
	m.notice = fmt.Sprintf("Copied %d line%s", n, plural(n))
}

// urlAt is the http(s) link under a screen position, if there is one.
func (m Model) urlAt(x, y int) string {
	lines := strings.Split(m.screen(), "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	line := []rune(ansi.Strip(lines[y]))
	if x < 0 || x >= len(line) || line[x] == ' ' {
		return ""
	}
	from, to := x, x
	for from > 0 && line[from-1] != ' ' {
		from--
	}
	for to < len(line) && line[to] != ' ' {
		to++
	}
	word := strings.Trim(string(line[from:to]), "()<>[]{}'\"`,;")
	if i := strings.Index(word, "http"); i >= 0 {
		word = word[i:] // e.g. ](https://…) from a Markdown link
	}
	if strings.HasPrefix(word, "https://") || strings.HasPrefix(word, "http://") {
		return strings.TrimRight(word, ".…")
	}
	return ""
}
