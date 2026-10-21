// Package table is the filtered table a sidebar section shows in the main pane (TASKS, PULL
// REQUESTS): buttons, filter chips, a second filter over one field (a facet, e.g. repo), search and
// pages sized to the pane. It knows nothing about what's in it; a section builds a Table and says
// what opening a row does.
//
// Keys while it has focus: ↑↓ pick, pgup/pgdn page, home/end first/last, ⏎ open, tab/shift+tab
// next or previous chip (⌘1… pick one when crew sets ChipKeys), r next facet value, / search (esc clears), plus the table's own keys. Clicks work on buttons, chips, rows and the table's clickable columns.
package table

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/look"
)

// KeyName shows a key the way the app names it (crew's app sets it: ctrl+o reads ⌘O).
var KeyName = func(k string) string { return k }

// Table is what a section shows.
type Table struct {
	Summary string // shown in the pane's header, after the section's name
	Cols    []Col
	Rows    []Row
	Chips   []Chip            // filters; the first is the default
	Facet   string            // what Row.Facet holds, e.g. "repo"; "" for none
	Keys    map[string]string // extra keys and the action each runs on the selected row
	Global  map[string]bool   // of those, the ones that work with no row selected (the id is "")
	Acts    []Act             // the buttons above the table, as in REQUESTS
	Hint    string            // shown under the pane
	Empty   string            // shown when no rows match
	// Groups names each Row.Group for its heading (JIRA's spaces). Set, the rows show under their
	// group's heading, the group with the most rows first; ←/→ (or ⏎) on a heading fold and unfold it.
	Groups map[string]string
	// ChipKeys numbers the chips "All ⌘1": crew sets it where the chips are the screen's tabs, so
	// ⌘1… pick them (a section with its own tab bar keeps ⌘1… for that).
	ChipKeys bool
}

// Col is a column; Width 0 takes the room the other columns leave.
type Col struct {
	Name   string
	Width  int
	Action string // what a click in this column does ("" opens the row)
}

// Chip is a filter: rows whose Tags hold its ID.
type Chip struct{ ID, Label string }

// Act is a button above the table: what it says and the key it presses. One whose label starts
// with "+" shows with no rows (its key is in Global); the others act on the selected row.
type Act struct{ Label, Key string }

// Row is one line of the table.
type Row struct {
	ID     string
	Cells  []string // one per column, styled
	Tags   []string // the chips it shows under
	Facet  string
	Search string // the text / matches
	Group  string // the heading it shows under, when the table has Groups
}

// State is what you picked in one table; it lasts while crew runs.
type State struct {
	Chip      string // "" is the table's first chip
	Facet     string // "" is every value
	Query     string
	Searching bool
	Sel       int             // index into what's listed: the filtered rows, and headings when grouped
	Folded    map[string]bool // groups folded with ←
}

// Layout is where View drew things, for clicks.
type Layout struct {
	rowAt  map[int]int // content line -> index into what's listed (entries)
	chipAt [][3]int    // start, end (columns) and index of each chip, on line chipLine
	actAt  [][3]int    // the same for the buttons, on line 0
}

const chipLine = 1

// ChipID is the chip in use.
func (st *State) ChipID(t Table) string {
	if st.Chip == "" && len(t.Chips) > 0 {
		return t.Chips[0].ID
	}
	return st.Chip
}

// Filtered is the rows that pass the chip, the facet and the search.
func (st *State) Filtered(t Table) []Row {
	chip, words := st.ChipID(t), strings.Fields(strings.ToLower(st.Query))
	var out []Row
	for _, r := range t.Rows {
		if chip != "" && !slices.Contains(r.Tags, chip) || st.Facet != "" && r.Facet != st.Facet {
			continue
		}
		hay, ok := strings.ToLower(r.Search), true
		for _, w := range words {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			out = append(out, r)
		}
	}
	return out
}

// entry is one line of the list: a row, or a group's heading (row nil) with how many rows it has.
type entry struct {
	row   *Row
	group string
	n     int
}

// entries is what the table lists: the filtered rows, under their group's heading when the table
// has Groups (the group with the most rows first), without the rows of a folded group.
func (st *State) entries(t Table) []entry {
	rows := st.Filtered(t)
	var out []entry
	if t.Groups == nil {
		for i := range rows {
			out = append(out, entry{row: &rows[i]})
		}
		return out
	}
	count := map[string]int{}
	var order []string
	for _, r := range rows {
		if count[r.Group] == 0 {
			order = append(order, r.Group)
		}
		count[r.Group]++
	}
	slices.SortStableFunc(order, func(a, b string) int {
		if count[a] != count[b] {
			return count[b] - count[a]
		}
		return strings.Compare(t.Groups[a], t.Groups[b])
	})
	for _, g := range order {
		out = append(out, entry{group: g, n: count[g]})
		for i := range rows {
			if rows[i].Group == g && !st.Folded[g] {
				out = append(out, entry{row: &rows[i]})
			}
		}
	}
	return out
}

// fold folds or unfolds a group.
func (st *State) fold(g string, folded bool) {
	if st.Folded == nil {
		st.Folded = map[string]bool{}
	}
	st.Folded[g] = folded
}

// SelectID puts the selection on the row with this id, if it's shown.
func (st *State) SelectID(t Table, id string) {
	if i := slices.IndexFunc(st.entries(t), func(e entry) bool { return e.row != nil && e.row.ID == id }); i >= 0 {
		st.Sel = i
	}
}

// selected is the selected row's id, "" with no rows or on a heading.
func (st *State) selected(t Table) string {
	es := st.entries(t)
	if len(es) == 0 || es[min(max(st.Sel, 0), len(es)-1)].row == nil {
		return ""
	}
	return es[min(max(st.Sel, 0), len(es)-1)].row.ID
}

// facets lists the facet values the rows have, in order.
func facets(t Table) []string {
	var out []string
	for _, r := range t.Rows {
		if r.Facet != "" && !slices.Contains(out, r.Facet) {
			out = append(out, r.Facet)
		}
	}
	slices.Sort(out)
	return out
}

// widths is each column's width in a pane w wide.
func widths(cols []Col, w int) []int {
	ws, fixed, flex := make([]int, len(cols)), 2, 0 // 2: the ▶ marker
	for i, c := range cols {
		ws[i] = c.Width
		fixed += c.Width + 1
		if c.Width == 0 {
			flex++
		}
	}
	for i, c := range cols {
		if c.Width == 0 {
			ws[i] = max((w-fixed)/max(flex, 1), 12)
		}
	}
	return ws
}

func cell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// Page is how many rows fit in a pane h tall: less the buttons, chips, filter line, a gap, the
// column header, a gap and the footer.
func Page(h int) int { return max(h-7, 3) }

// View draws the table w wide and h tall and says where its parts are. focused draws the
// selection as the bar of the area that has the keys.
func (st *State) View(t Table, w, h int, focused bool) (string, Layout) {
	rows := st.Filtered(t)
	lay := Layout{rowAt: map[int]int{}}
	var lines []string

	// The buttons, as REQUESTS draws them.
	bar, x := " ", 1
	for i, a := range t.Acts {
		if len(rows) == 0 && !strings.HasPrefix(a.Label, "+") {
			continue
		}
		b := look.Button(a.Label, KeyName(a.Key))
		if x+ansi.StringWidth(b) > w {
			break
		}
		lay.actAt = append(lay.actAt, [3]int{x, x + ansi.StringWidth(b), i})
		bar += b + " "
		x += ansi.StringWidth(b) + 1
	}
	lines = append(lines, bar)

	// Chips with their counts (facet applied, search not).
	chips, chip := " ", st.ChipID(t)
	x = 1
	for i, c := range t.Chips {
		n := len((&State{Chip: c.ID, Facet: st.Facet}).Filtered(t))
		label := fmt.Sprintf(" %s %d ", c.Label, n)
		if t.ChipKeys && i < 6 { // crew binds ⌘1…⌘6
			label = fmt.Sprintf(" %s %d  ⌘%d ", c.Label, n, i+1)
		}
		style := look.Dim
		switch {
		case c.ID == chip:
			style = look.FocusBar.Foreground(lipgloss.Color("15"))
		case n > 0:
			style = lipgloss.NewStyle()
		}
		cw := ansi.StringWidth(label)
		lay.chipAt = append(lay.chipAt, [3]int{x, x + cw, i})
		chips += style.Render(label) + " "
		x += cw + 1
	}
	lines = append(lines, ansi.Truncate(chips, w, "…"))

	// The facet and the search.
	var filters []string
	if vals := facets(t); t.Facet != "" && len(vals) > 1 {
		v := "all"
		if st.Facet != "" {
			v = look.Sel.Render(st.Facet)
		}
		filters = append(filters, look.Dim.Render(t.Facet+": ")+v+look.Dim.Render(" (r)"))
	}
	switch {
	case st.Searching:
		filters = append(filters, look.Sel.Render("⌕ ")+st.Query+"▏"+look.Dim.Render("  ⏎ done · esc clear"))
	case st.Query != "":
		filters = append(filters, look.Sel.Render("⌕ ")+st.Query+look.Dim.Render("  (/ edit · esc clear)"))
	default:
		filters = append(filters, look.Dim.Render("/ search"))
	}
	lines = append(lines, " "+strings.Join(filters, look.Dim.Render("   ")), "")

	if len(rows) == 0 {
		return strings.Join(append(lines, look.Dim.Render("  "+t.Empty)), "\n"), lay
	}
	ws := widths(t.Cols, w)
	head := "  "
	for i, c := range t.Cols {
		head += cell(c.Name, ws[i]) + " "
	}
	lines = append(lines, look.Dim.Render(head))

	// The page with the selection on it.
	es := st.entries(t)
	size := Page(h)
	cur := min(max(st.Sel, 0), len(es)-1)
	from := cur / size * size
	to := min(from+size, len(es))
	for i := from; i < to; i++ {
		body := ""
		if e := es[i]; e.row == nil { // a group's heading
			arrow := "▾ "
			if st.Folded[e.group] {
				arrow = "▸ "
			}
			body = look.Bold.Render(arrow+t.Groups[e.group]) + look.Dim.Render(fmt.Sprintf("  %s · %d", e.group, e.n))
		}
		for j, c := range rowCells(es[i]) {
			if j == 0 && i == cur {
				c = look.Bold.Render(c)
			}
			body += cell(c, ws[j]) + " "
		}
		line := "  " + body
		switch {
		case i == cur && focused: // where the arrows are: the same bar as the focused task list
			line = look.FocusBar.Width(w).Render("▶ " + ansi.Strip(body))
		case i == cur:
			line = look.Sel.Render("▶ ") + body
		}
		lay.rowAt[len(lines)] = i
		lines = append(lines, line)
	}
	footer := fmt.Sprintf("%d–%d of %d", from+1, to, len(es))
	if len(es) > size {
		footer += " · pgup/pgdn page"
	}
	if t.Groups != nil {
		footer += " · ←/→ fold or unfold a group"
	}
	lines = append(lines, "", look.Dim.Render(ansi.Truncate(" "+footer, w, "…")))
	return strings.Join(lines, "\n"), lay
}

// rowCells is an entry's cells: a row's, or none for a heading.
func rowCells(e entry) []string {
	if e.row == nil {
		return nil
	}
	return e.row.Cells
}

// Key handles a key in a pane h tall. open is set when it opens a row: its id ("" for a Global key
// with no rows) and the action. handled is false for keys it doesn't use.
func (st *State) Key(t Table, msg tea.KeyMsg, h int) (id, action string, open, handled bool) {
	k := msg.String()
	if st.Searching {
		switch {
		case k == "esc":
			st.Query, st.Searching = "", false
		case k == "enter":
			st.Searching = false
		case k == "backspace":
			if q := []rune(st.Query); len(q) > 0 {
				st.Query = string(q[:len(q)-1])
			}
		case msg.Type == tea.KeyRunes:
			st.Query += string(msg.Runes)
		case msg.Type == tea.KeySpace:
			st.Query += " "
		default:
			return "", "", false, true
		}
		st.Sel = 0
		return "", "", false, true
	}
	es := st.entries(t)
	last := max(len(es)-1, 0)
	st.Sel = min(max(st.Sel, 0), last)
	var on entry // what the selection is on
	if len(es) > 0 {
		on = es[st.Sel]
	}
	heading := len(es) > 0 && on.row == nil
	switch k {
	case "up":
		st.Sel = max(st.Sel-1, 0)
	case "down":
		st.Sel = min(st.Sel+1, last)
	case "pgup":
		st.Sel = max(st.Sel-Page(h), 0)
	case "pgdown":
		st.Sel = min(st.Sel+Page(h), last)
	case "home":
		st.Sel = 0
	case "end":
		st.Sel = last
	case "tab", "shift+tab":
		if len(t.Chips) == 0 {
			return "", "", false, false
		}
		i := slices.IndexFunc(t.Chips, func(c Chip) bool { return c.ID == st.ChipID(t) })
		d := map[string]int{"tab": 1, "shift+tab": -1}[k]
		st.Chip, st.Sel = t.Chips[(i+d+len(t.Chips))%len(t.Chips)].ID, 0
	case "r":
		vals := facets(t)
		if t.Facet == "" || len(vals) < 2 {
			return st.keyAction(t, k, es, on, heading) // a table with no facet may use r itself (refresh)
		}
		i := slices.Index(vals, st.Facet) // -1 (all) steps to the first value
		st.Facet, st.Sel = "", 0
		if i+1 < len(vals) {
			st.Facet = vals[i+1]
		}
	case "/":
		st.Searching = true
	case "esc":
		if st.Query == "" {
			return "", "", false, false
		}
		st.Query, st.Sel = "", 0
	case "left", "right":
		switch {
		case t.Groups == nil || len(es) == 0:
			return "", "", false, false
		case heading:
			st.fold(on.group, k == "left")
		case k == "left": // up to its group's heading
			for st.Sel > 0 && es[st.Sel].row != nil {
				st.Sel--
			}
		}
	case "enter":
		switch {
		case len(es) == 0:
		case heading:
			st.fold(on.group, !st.Folded[on.group])
		default:
			return on.row.ID, "", true, true
		}
	default:
		return st.keyAction(t, k, es, on, heading)
	}
	return "", "", false, true
}

// keyAction is one of the table's own keys (Table.Keys) on the selected row, or with none when
// the key is Global.
func (st *State) keyAction(t Table, k string, es []entry, on entry, heading bool) (id, action string, open, handled bool) {
	switch {
	case t.Keys[k] == "":
		return "", "", false, false
	case len(es) > 0 && !heading:
		return on.row.ID, t.Keys[k], true, true
	case t.Global[k]:
		return "", t.Keys[k], true, true
	}
	return "", "", false, false
}

// Click handles a click at column x of content line y, as View drew it w wide: a button presses
// its key, a chip filters, a row opens with the action of the column clicked.
func (st *State) Click(t Table, lay Layout, x, y, w int) (id, action string, open bool) {
	if y == 0 {
		for _, a := range lay.actAt {
			if x >= a[0] && x < a[1] {
				k := t.Acts[a[2]].Key
				return st.selected(t), t.Keys[k], true // "enter" isn't in Keys: it opens
			}
		}
		return "", "", false
	}
	if y == chipLine {
		for _, c := range lay.chipAt {
			if x >= c[0] && x < c[1] {
				st.Chip, st.Sel = t.Chips[c[2]].ID, 0
			}
		}
		return "", "", false
	}
	i, ok := lay.rowAt[y]
	if !ok {
		return "", "", false
	}
	st.Sel = i
	e := st.entries(t)[i]
	if e.row == nil { // a heading folds or unfolds
		st.fold(e.group, !st.Folded[e.group])
		return "", "", false
	}
	pos := 2
	for j, cw := range widths(t.Cols, w) {
		if x >= pos && x < pos+cw {
			action = t.Cols[j].Action
		}
		pos += cw + 1
	}
	return e.row.ID, action, true
}

// Wheel moves the selection with the mouse wheel.
func (st *State) Wheel(t Table, down bool) {
	if down {
		st.Sel = min(st.Sel+3, max(len(st.entries(t))-1, 0))
	} else {
		st.Sel = max(st.Sel-3, 0)
	}
}
