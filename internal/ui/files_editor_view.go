package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/edit"
)

// cellWidth is how many columns r takes at column x; tabs stop every 4 columns.
func cellWidth(r rune, x int) int {
	switch {
	case r == '\t':
		return 4 - x%4
	case r < 0x300:
		return 1
	}
	return ansi.StringWidth(string(r))
}

func visualCol(line []rune, col int) int {
	x := 0
	for _, r := range line[:col] {
		x += cellWidth(r, x)
	}
	return x
}

func colAt(line []rune, vx int) int {
	x := 0
	for i, r := range line {
		w := cellWidth(r, x)
		if vx < x+(w+1)/2 {
			return i
		}
		x += w
	}
	return len(line)
}

// Syntax colours: chroma token types map to SGR parameters, numbered so a row is a []uint16.
var (
	hlIDs  = map[chroma.TokenType]uint16{}
	hlSGR  = []string{""}
	hlKeys = map[string]uint16{"": 0}
)

func styleID(tt chroma.TokenType) uint16 {
	if id, ok := hlIDs[tt]; ok {
		return id
	}
	e := styles.Get("monokai").Get(tt)
	var s strings.Builder
	if e.Colour.IsSet() {
		fmt.Fprintf(&s, ";38;2;%d;%d;%d", e.Colour.Red(), e.Colour.Green(), e.Colour.Blue())
	}
	if e.Bold == chroma.Yes {
		s.WriteString(";1")
	}
	if e.Italic == chroma.Yes {
		s.WriteString(";3")
	}
	id, ok := hlKeys[s.String()]
	if !ok {
		id = uint16(len(hlSGR))
		hlSGR = append(hlSGR, s.String())
		hlKeys[s.String()] = id
	}
	hlIDs[tt] = id
	return id
}

// rowStyles returns the colour of each rune in row, highlighting a window around it when the text
// changed or the view moved away. ponytail: the window starts up to 150 rows above, so a long
// comment or string opened further up can be coloured wrong; highlight from the top if that shows.
func (d *doc) rowStyles(row, rows int) []uint16 {
	h := &d.hl
	if h.version != d.buf.Version || row < h.from || row >= h.from+len(h.rows) {
		lines := d.buf.Lines
		from := max(row-150, 0) / 50 * 50
		to := min(row+rows+150, len(lines))
		h.version, h.from, h.rows = d.buf.Version, from, make([][]uint16, to-from)
		parts := make([]string, to-from)
		for i := range h.rows {
			h.rows[i] = make([]uint16, len(lines[from+i]))
			parts[i] = string(lines[from+i])
		}
		if it, err := d.lexer.Tokenise(nil, strings.Join(parts, "\n")); err == nil {
			r, c := 0, 0
			for tok := it(); tok != chroma.EOF; tok = it() {
				id := styleID(tok.Type)
				for _, ch := range tok.Value {
					if ch == '\n' {
						r, c = r+1, 0
						continue
					}
					if r < len(h.rows) && c < len(h.rows[r]) {
						h.rows[r][c] = id
					}
					c++
				}
			}
		}
	}
	if i := row - h.from; i >= 0 && i < len(h.rows) {
		return h.rows[i]
	}
	return nil
}

const selBG = ";48;2;38;79;120"

// view draws the tab bar, the text and the status bar.
func (ed *editor) view(focused bool) string {
	d := ed.doc()
	b := d.buf
	gw, rows := ed.gutter(), ed.textRows()
	cols := max(ed.w-gw, 1)
	ss, se, hasSel := b.Selection()
	out := []string{ed.tabBar()}
	for i := 0; i < rows; i++ {
		row := d.top + i
		if row >= len(b.Lines) {
			out = append(out, "")
			continue
		}
		num := dim.Render(fmt.Sprintf("%*d  ", gw-2, row+1))
		if row == b.Cur.Row {
			num = fmt.Sprintf("%*d  ", gw-2, row+1)
		}
		out = append(out, num+ed.renderRow(d, row, cols, rows, focused, ss, se, hasSel))
	}
	return strings.Join(append(out, ed.statusBar()), "\n")
}

func (ed *editor) renderRow(d *doc, row, cols, rows int, focused bool, ss, se edit.Pos, hasSel bool) string {
	line, sty := d.buf.Lines[row], d.rowStyles(row, rows)
	var b strings.Builder
	last := "-"
	emit := func(sgr, s string) {
		if sgr != last {
			b.WriteString("\x1b[0" + sgr + "m")
			last = sgr
		}
		b.WriteString(s)
	}
	cur := d.buf.Cur
	x := 0
	for i, r := range line {
		w := cellWidth(r, x)
		if x >= d.left+cols {
			break
		}
		if x >= d.left && x+w <= d.left+cols {
			sgr := ""
			if i < len(sty) {
				sgr = hlSGR[sty[i]]
			}
			p := edit.Pos{Row: row, Col: i}
			if hasSel && !p.Before(ss) && p.Before(se) {
				sgr += selBG
			}
			if focused && cur == p {
				sgr += ";7"
			}
			s := string(r)
			switch {
			case r == '\t':
				s = strings.Repeat(" ", w)
			case r < 32:
				s = "·"
			}
			emit(sgr, s)
		}
		x += w
	}
	if x >= d.left && x < d.left+cols {
		switch {
		case focused && cur == (edit.Pos{Row: row, Col: len(line)}):
			emit(";7", " ")
		case hasSel && row >= ss.Row && row < se.Row:
			emit(selBG, " ") // the selected line break
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// tabLabels are the tab bar's names, in order; tabBar and clicks share them.
func (ed *editor) tabLabels() []string {
	var ls []string
	for _, d := range ed.docs {
		l := " " + filepath.Base(d.path) + " "
		if d.dirty {
			l += "● "
		}
		ls = append(ls, l)
	}
	return ls
}

func (ed *editor) tabBar() string {
	active := lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("15")).Bold(true)
	var parts []string
	for i, l := range ed.tabLabels() {
		if i == ed.cur {
			parts = append(parts, active.Render(l))
		} else {
			parts = append(parts, dim.Render(l))
		}
	}
	return ansi.Truncate(strings.Join(parts, dim.Render("│")), ed.w, "…")
}

// tabAt is the tab under column x of the tab bar, or -1.
func (ed *editor) tabAt(x int) int {
	pos := 0
	for i, l := range ed.tabLabels() {
		w := ansi.StringWidth(l)
		if x >= pos && x < pos+w {
			return i
		}
		pos += w + 1
	}
	return -1
}

func (ed *editor) statusBar() string {
	d := ed.doc()
	b := d.buf
	bar := lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("250"))
	left := fmt.Sprintf(" %s  Ln %d, Col %d", d.path, b.Cur.Row+1, visualCol(b.Lines[b.Cur.Row], b.Cur.Col)+1)
	if s := b.SelectedText(); s != "" {
		left += fmt.Sprintf(" (%d selected)", len([]rune(s)))
	}
	right := keyNames("ctrl+s save · ctrl+f find · ctrl+w close · esc tree ")
	caret := func(on bool) string {
		if on {
			return "▏"
		}
		return ""
	}
	switch ed.prompt {
	case "goto":
		left, right = " Go to line: "+ed.line+"▏", "⏎ go · esc cancel "
	case "find":
		left, right = " Find: "+ed.query+"▏", "⏎/↓ next · ↑ previous · esc done "
	case "replace":
		left = " Find: " + ed.query + caret(!ed.inRepl) + "   Replace: " + ed.repl + caret(ed.inRepl)
		right = "⏎ next · tab replace field · esc done "
		if ed.inRepl {
			right = keyNames("⏎ replace · ctrl+a replace all · tab find field · esc done ")
		}
	}
	if ed.note != "" {
		right = ed.note + " "
	}
	gap := ed.w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return bar.Render(ansi.Truncate(left+" "+right, ed.w, "…"))
	}
	return bar.Render(left + strings.Repeat(" ", gap) + right)
}

// mouseEditor handles the mouse over the editor: click places the cursor (shift extends, a
// double click selects a word), drag selects, the wheel scrolls, and the tab bar switches files.
func (m *Model) mouseEditor(msg tea.MouseMsg) bool {
	if !m.editorTab() {
		return false
	}
	ed := m.editors[m.currentID()]
	d := ed.doc()
	ox, oy := m.sideOuter()+1+m.treeWidth()+1, m.contentY()
	x, y := msg.X-ox, msg.Y-oy
	pos := func() edit.Pos {
		row := min(max(d.top+y-1, 0), len(d.buf.Lines)-1)
		return edit.Pos{Row: row, Col: colAt(d.buf.Lines[row], max(d.left+x-ed.gutter(), 0))}
	}
	if ed.dragging { // keeps selecting outside the pane until the button is let go
		switch msg.Action {
		case tea.MouseActionMotion:
			d.buf.MoveTo(pos(), true)
			ed.ensureVisible()
		case tea.MouseActionRelease:
			ed.dragging = false
		}
		return true
	}
	if x < 0 || x >= ed.w || y < 0 || y >= ed.h {
		return false
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		d.top = max(d.top-3, 0)
		return true
	case tea.MouseButtonWheelDown:
		d.top = max(min(d.top+3, len(d.buf.Lines)-ed.textRows()), 0)
		return true
	case tea.MouseButtonLeft:
	default:
		return true
	}
	if msg.Action != tea.MouseActionPress {
		return true
	}
	m.focusOn(areaEditor, -1)
	switch {
	case y == 0:
		if i := ed.tabAt(x); i >= 0 {
			ed.cur = i
			m.tree().file = ed.doc().path
		}
	case y <= ed.textRows():
		p := pos()
		if !msg.Shift && p == ed.lastPos && time.Since(ed.lastClick) < 400*time.Millisecond {
			d.buf.SelectWord(p)
		} else {
			d.buf.MoveTo(p, msg.Shift)
			ed.dragging = true
		}
		ed.lastPos, ed.lastClick = p, time.Now()
	}
	return true
}
