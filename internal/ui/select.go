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

// textSel is a mouse selection: from a to b, kept inside one panel. In the main pane's scrolling
// content (the chat, a tab) a and b are content positions (line, column), so the selection keeps
// growing as dragging past the top or bottom scrolls; elsewhere they're screen cells.
type textSel struct {
	armed    bool // the left button went down in a panel that can be selected in
	dragging bool
	on       bool // there's a selection to show (until the next key or click)
	content  bool // a and b are in the main pane's content, not on the screen
	pane     rect // screen cells, or (content) the columns 0..width-1
	a, b     point
}

// contentArea is where the main pane's scrolling content is on screen, when a selection there
// can follow it (no popup, and not the Files tab, whose editor has its own selection).
func (m Model) contentArea() (rect, bool) {
	r := rect{m.sideOuter() + 1, contentY, m.sideOuter() + m.vp.Width, contentY + m.vp.Height - 1}
	return r, m.modal == noModal && m.tab != tabFiles
}

// contentLines is all of the main pane's content as drawn, not just what's on screen.
func (m Model) contentLines() []string {
	all := m.vp
	all.YOffset, all.Height = 0, max(m.vp.TotalLineCount(), 1)
	return strings.Split(all.View(), "\n")
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
	case x >= 1 && x <= m.sideOuter()-2:
		return rect{1, 1, m.sideOuter() - 2, bodyH - 2}, -2, true
	case x >= m.sideOuter()+1 && x <= m.sideOuter()+cw:
		return rect{m.sideOuter() + 1, 1, m.sideOuter() + cw, bodyH - 2}, -1, true
	}
	pw := m.panelWidth()
	if ox := m.sideOuter() + cw + 2; pw > 0 && x > ox && x < ox+pw-1 {
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
		return r, x < m.sideOuter()+1+m.treeWidth() // the tree is fine; the editor selects for itself
	}
	return r, true
}

// mouseSelect handles drags (select) and clicks on links. A press still goes on to the other
// handlers, so clicks keep working; it reports whether it used the event.
func (m *Model) mouseSelect(msg tea.MouseMsg) bool {
	at := point{msg.X, msg.Y}
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		if !m.drag.content {
			m.drag.on = false // the text under it moves; a content selection moves with it
		}
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.drag = textSel{}
		if c, ok := m.contentArea(); ok && at.x >= c.x0 && at.x <= c.x1 && at.y >= c.y0 && at.y <= c.y1 {
			m.drag = textSel{armed: true, content: true, pane: rect{0, 0, m.vp.Width - 1, 0},
				a: point{at.x - c.x0, m.vp.YOffset + at.y - c.y0}}
		} else if r, ok := m.selectable(msg.X, msg.Y); ok {
			m.drag.armed, m.drag.pane, m.drag.a = true, r, at
		}
	case msg.Action == tea.MouseActionMotion && m.drag.armed && m.drag.content:
		c, _ := m.contentArea()
		switch { // dragging past the edge scrolls
		case at.y < c.y0:
			m.vp.ScrollUp(1)
		case at.y > c.y1:
			m.vp.ScrollDown(1)
		}
		y := min(max(at.y, c.y0), c.y1)
		p := point{min(max(at.x-c.x0, 0), m.vp.Width-1), m.vp.YOffset + y - c.y0}
		if m.drag.dragging || moved(p, m.drag.a) {
			m.drag.dragging, m.drag.on, m.drag.b = true, true, p
		}
		return true
	case msg.Action == tea.MouseActionMotion && m.drag.armed:
		r := m.drag.pane
		p := point{min(max(at.x, r.x0), r.x1), min(max(at.y, r.y0), r.y1)}
		if m.drag.dragging || moved(p, m.drag.a) {
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

// selRange is the columns the selection covers on row y (a screen row, or a content line for a
// content selection), or ok false.
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
	c, _ := m.contentArea()
	for y, line := range lines {
		row, dx := y, 0
		if m.drag.content { // a content line, where it is on screen now
			if y < c.y0 || y > c.y1 {
				continue
			}
			row, dx = m.vp.YOffset+y-c.y0, c.x0
		}
		if from, to, ok := m.selRange(row); ok {
			from, to = from+dx, to+dx
			lines[y] = ansi.Cut(line, 0, from) + selStyle.Render(ansi.Strip(ansi.Cut(line, from, to))) + ansi.TruncateLeft(line, to, "")
		}
	}
	return strings.Join(lines, "\n")
}

// selText is the selected text as plain lines, without crew's chat bars and scroll bar.
func (m Model) selText() string {
	lines := strings.Split(m.screen(), "\n")
	if m.drag.content {
		lines = m.contentLines()
	}
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

// moved says whether the mouse went far enough from where it was pressed to be a drag, not a
// click that wobbled: two columns, or another line.
func moved(p, from point) bool { return p.y != from.y || p.x-from.x >= 2 || from.x-p.x >= 2 }
