package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/look"
)

// The overview is the main pane for a group row in the sidebar: a table of what's in the group,
// with filter chips, a second filter over one field (a facet, e.g. repo), search and pages sized to
// the pane. It knows nothing about tasks; each kind of group builds an ovTable (groupTable) and
// says what opening a row does.
//
// Keys while it has focus: ↑↓ pick, pgup/pgdn page, home/end first/last, ⏎ open, ←/→ or
// tab/shift+tab next or previous chip, r next facet value, / search (esc clears), plus the table's
// own keys. Clicks work
// on chips, rows and the table's clickable columns; the wheel moves the selection.

type ovTable struct {
	summary string // shown in the pane's header, after the group's name
	cols    []ovCol
	rows    []ovRow
	chips   []ovChip          // filters; the first is the default
	facet   string            // what ovRow.facet holds, e.g. "repo"; "" for none
	keys    map[string]string // extra keys and the action each runs on the selected row
	global  map[string]bool   // of those, the ones that work with no row selected (open gets id "")
	hint    string            // shown in the footer
	empty   string            // shown when no rows match
	// open runs an action on a row: "" (⏎, or a click in a column without an action), or a
	// column's or key's action.
	open func(m *Model, id, action string) tea.Cmd
}

type ovCol struct {
	name   string
	width  int    // 0: takes the room the other columns leave
	action string // what a click in this column does ("" opens the row)
}

type ovChip struct{ id, label string }

type ovRow struct {
	id     string
	cells  []string // one per column, styled
	tags   []string // the chips it shows under
	facet  string
	search string // the text / matches
}

// ovState is what you picked in one group's overview; it lasts while crew runs.
type ovState struct {
	chip      string // "" is the table's first chip
	facet     string // "" is every value
	query     string
	searching bool
	sel       int // index into the filtered rows
}

func (m Model) ovOf(key string) *ovState {
	if m.ovs[key] == nil {
		m.ovs[key] = &ovState{}
	}
	return m.ovs[key]
}

// groupTable is the table of a group row: TASKS or PULL REQUESTS (REQUESTS draws its own screen,
// in the requests package). A new sidebar section with a table adds its case here.
func (m Model) groupTable(r row) ovTable {
	if r.group == "prs" {
		return m.prTable(r)
	}
	return m.taskTable(r)
}

// focusBar marks the selected row of the area that has the keys (the task list, the overview).
var focusBar = look.FocusBar

func (st *ovState) chipID(t ovTable) string {
	if st.chip == "" && len(t.chips) > 0 {
		return t.chips[0].id
	}
	return st.chip
}

// filtered is the rows that pass the chip, the facet and the search.
func (st *ovState) filtered(t ovTable) []ovRow {
	chip, words := st.chipID(t), strings.Fields(strings.ToLower(st.query))
	var out []ovRow
	for _, r := range t.rows {
		if chip != "" && !slices.Contains(r.tags, chip) || st.facet != "" && r.facet != st.facet {
			continue
		}
		hay, ok := strings.ToLower(r.search), true
		for _, w := range words {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			out = append(out, r)
		}
	}
	return out
}

// facets lists the facet values the rows have, in order.
func facets(t ovTable) []string {
	var out []string
	for _, r := range t.rows {
		if r.facet != "" && !slices.Contains(out, r.facet) {
			out = append(out, r.facet)
		}
	}
	slices.Sort(out)
	return out
}

// ovWidths is each column's width in a pane w wide.
func ovWidths(cols []ovCol, w int) []int {
	ws, fixed, flex := make([]int, len(cols)), 2, 0 // 2: the ▶ marker
	for i, c := range cols {
		ws[i] = c.width
		fixed += c.width + 1
		if c.width == 0 {
			flex++
		}
	}
	for i, c := range cols {
		if c.width == 0 {
			ws[i] = max((w-fixed)/max(flex, 1), 12)
		}
	}
	return ws
}

func ovCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// ovPage is how many rows fit: the pane less the chips, filter line, a gap, the column header, a
// gap and the footer.
func (m Model) ovPage() int { return max(m.vp.Height-6, 3) }

// ovLayout is where things were drawn, for clicks.
type ovLayout struct {
	rowAt    map[int]int // content line -> index into the filtered rows
	chipLine int
	chipAt   [][3]int // start, end (columns) and index of each chip
}

// overview draws a group row's table and says where its parts are.
func (m Model) overview(r row) (string, ovLayout) {
	t, st := m.groupTable(r), m.ovOf(m.rowKey(r))
	rows := st.filtered(t)
	lay := ovLayout{rowAt: map[int]int{}, chipLine: 0}
	var lines []string // the title and summary are in the pane's header

	// Chips with their counts (facet applied, search not).
	chips, x, chip := " ", 1, st.chipID(t)
	for i, c := range t.chips {
		n := len((&ovState{chip: c.id, facet: st.facet}).filtered(t))
		label := fmt.Sprintf(" %s %d ", c.label, n)
		style := dim
		switch {
		case c.id == chip:
			style = focusBar.Foreground(lipgloss.Color("15"))
		case n > 0:
			style = lipgloss.NewStyle()
		}
		w := ansi.StringWidth(label)
		lay.chipAt = append(lay.chipAt, [3]int{x, x + w, i})
		chips += style.Render(label) + " "
		x += w + 1
	}
	lines = append(lines, ansi.Truncate(chips, m.vp.Width, "…"))

	// The facet and the search.
	var filters []string
	if vals := facets(t); t.facet != "" && len(vals) > 1 {
		v := "all"
		if st.facet != "" {
			v = sel.Render(st.facet)
		}
		filters = append(filters, dim.Render(t.facet+": ")+v+dim.Render(" (r)"))
	}
	switch {
	case st.searching:
		filters = append(filters, sel.Render("⌕ ")+st.query+"▏"+dim.Render("  ⏎ done · esc clear"))
	case st.query != "":
		filters = append(filters, sel.Render("⌕ ")+st.query+dim.Render("  (/ edit · esc clear)"))
	default:
		filters = append(filters, dim.Render("/ search"))
	}
	lines = append(lines, " "+strings.Join(filters, dim.Render("   ")), "")

	if len(rows) == 0 {
		return strings.Join(append(lines, dim.Render("  "+t.empty)), "\n"), lay
	}
	ws := ovWidths(t.cols, m.vp.Width)
	head := "  "
	for i, c := range t.cols {
		head += ovCell(c.name, ws[i]) + " "
	}
	lines = append(lines, dim.Render(head))

	// The page with the selection on it.
	size := m.ovPage()
	sel := min(max(st.sel, 0), len(rows)-1)
	from := sel / size * size
	to := min(from+size, len(rows))
	focused := m.focus() == "overview"
	for i := from; i < to; i++ {
		body := ""
		for j, c := range rows[i].cells {
			if j == 0 && i == sel {
				c = bold.Render(c)
			}
			body += ovCell(c, ws[j]) + " "
		}
		line := "  " + body
		switch {
		case i == sel && focused: // where the arrows are: the same bar as the focused task list
			line = focusBar.Width(m.vp.Width).Render("▶ " + ansi.Strip(body))
		case i == sel:
			line = sel2(true).Render("▶ ") + body
		}
		lay.rowAt[len(lines)] = i
		lines = append(lines, line)
	}
	footer := fmt.Sprintf("%d–%d of %d", from+1, to, len(rows))
	if len(rows) > size {
		footer += " · pgup/pgdn page"
	}
	if t.hint != "" {
		footer += " · " + t.hint
	}
	lines = append(lines, "", dim.Render(ansi.Truncate(" "+footer, m.vp.Width, "…")))
	return strings.Join(lines, "\n"), lay
}

// overviewKey handles keys on a group row's overview; ok is false for keys it doesn't use.
func (m *Model) overviewKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	r := m.selRow()
	t, st := m.groupTable(r), m.ovOf(m.rowKey(r))
	k := msg.String()
	if st.searching {
		switch {
		case k == "esc":
			st.query, st.searching = "", false
		case k == "enter":
			st.searching = false
		case k == "backspace":
			if q := []rune(st.query); len(q) > 0 {
				st.query = string(q[:len(q)-1])
			}
		case msg.Type == tea.KeyRunes:
			st.query += string(msg.Runes)
		case msg.Type == tea.KeySpace:
			st.query += " "
		default:
			return nil, true
		}
		st.sel = 0
		m.refresh()
		return nil, true
	}
	rows := st.filtered(t)
	last := max(len(rows)-1, 0)
	st.sel = min(max(st.sel, 0), last)
	switch k {
	case "up":
		st.sel = max(st.sel-1, 0)
	case "down":
		st.sel = min(st.sel+1, last)
	case "pgup":
		st.sel = max(st.sel-m.ovPage(), 0)
	case "pgdown":
		st.sel = min(st.sel+m.ovPage(), last)
	case "home":
		st.sel = 0
	case "end":
		st.sel = last
	case "right", "left", "tab", "shift+tab":
		if len(t.chips) == 0 {
			return nil, false
		}
		i := slices.IndexFunc(t.chips, func(c ovChip) bool { return c.id == st.chipID(t) })
		d := map[string]int{"right": 1, "tab": 1, "left": -1, "shift+tab": -1}[k]
		st.chip, st.sel = t.chips[(i+d+len(t.chips))%len(t.chips)].id, 0
	case "r":
		vals := facets(t)
		if t.facet == "" || len(vals) < 2 {
			return nil, false
		}
		i := slices.Index(vals, st.facet) // -1 (all) steps to the first value
		st.facet, st.sel = "", 0
		if i+1 < len(vals) {
			st.facet = vals[i+1]
		}
	case "/":
		st.searching = true
	case "esc":
		if st.query == "" {
			return nil, false
		}
		st.query, st.sel = "", 0
	case "enter":
		if len(rows) == 0 {
			return nil, true
		}
		return t.open(m, rows[st.sel].id, ""), true
	default:
		switch {
		case t.keys[k] == "":
			return nil, false
		case len(rows) > 0:
			return t.open(m, rows[st.sel].id, t.keys[k]), true
		case t.global[k]:
			return t.open(m, "", t.keys[k]), true
		}
		return nil, false
	}
	m.refresh()
	return nil, true
}

// clickOverview handles a click on the overview (x, y inside the main pane's content): a chip
// filters, a row opens with the action of the column clicked.
func (m *Model) clickOverview(x, y int) tea.Cmd {
	r := m.selRow()
	t, st := m.groupTable(r), m.ovOf(m.rowKey(r))
	_, lay := m.overview(r)
	line := m.vp.YOffset + y
	if line == lay.chipLine {
		for _, c := range lay.chipAt {
			if x >= c[0] && x < c[1] {
				st.chip, st.sel = t.chips[c[2]].id, 0
				m.refresh()
			}
		}
		return nil
	}
	i, ok := lay.rowAt[line]
	if !ok {
		return nil
	}
	st.sel = i
	action, pos := "", 2
	for j, w := range ovWidths(t.cols, m.vp.Width) {
		if x >= pos && x < pos+w {
			action = t.cols[j].action
		}
		pos += w + 1
	}
	return t.open(m, st.filtered(t)[i].id, action)
}

// wheelOverview moves the overview's selection with the mouse wheel.
func (m *Model) wheelOverview(down bool) {
	r := m.selRow()
	st := m.ovOf(m.rowKey(r))
	if down {
		st.sel = min(st.sel+3, max(len(st.filtered(m.groupTable(r)))-1, 0))
	} else {
		st.sel = max(st.sel-3, 0)
	}
	m.refresh()
}
