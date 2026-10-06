// Package edit is the text buffer behind crew's editor: lines of runes, a cursor and selection,
// undo and redo. It knows nothing about the screen.
package edit

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Pos struct{ Row, Col int }

func (p Pos) Before(q Pos) bool { return p.Row < q.Row || p.Row == q.Row && p.Col < q.Col }

// change is one undoable edit: old text at `at` was replaced by new text.
type change struct {
	at, before, after Pos // before and after are the cursor around the edit
	old, new          string
	typing            bool // a single typed character, merged with the next ones into one undo step
}

const maxUndo = 1000

type Buffer struct {
	Lines       [][]rune
	Cur, Anchor Pos // the selection runs between Anchor and Cur when HasSel
	HasSel      bool
	Version     int // grows with every change to the text
	want        int // column that up/down moves aim for
	undo, redo  []change
}

func New(text string) *Buffer {
	b := &Buffer{}
	b.SetText(text)
	return b
}

// SetText replaces everything, e.g. when the file changed on disk; the cursor stays if it still fits.
func (b *Buffer) SetText(text string) {
	parts := strings.Split(text, "\n")
	b.Lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.Lines[i] = []rune(p)
	}
	b.Cur, b.HasSel = b.Clamp(b.Cur), false
	b.undo, b.redo = nil, nil
	b.Version++
}

func (b *Buffer) Text() string { return b.Range(Pos{}, b.End()) }

func (b *Buffer) End() Pos {
	last := len(b.Lines) - 1
	return Pos{last, len(b.Lines[last])}
}

func (b *Buffer) Clamp(p Pos) Pos {
	p.Row = max(min(p.Row, len(b.Lines)-1), 0)
	p.Col = max(min(p.Col, len(b.Lines[p.Row])), 0)
	return p
}

// Range returns the text from a to z (a before z).
func (b *Buffer) Range(a, z Pos) string {
	if a.Row == z.Row {
		return string(b.Lines[a.Row][a.Col:z.Col])
	}
	var s strings.Builder
	s.WriteString(string(b.Lines[a.Row][a.Col:]))
	for r := a.Row + 1; r < z.Row; r++ {
		s.WriteByte('\n')
		s.WriteString(string(b.Lines[r]))
	}
	s.WriteByte('\n')
	s.WriteString(string(b.Lines[z.Row][:z.Col]))
	return s.String()
}

// Selection returns the selected range in order.
func (b *Buffer) Selection() (start, end Pos, ok bool) {
	if !b.HasSel {
		return Pos{}, Pos{}, false
	}
	if b.Cur.Before(b.Anchor) {
		return b.Cur, b.Anchor, true
	}
	return b.Anchor, b.Cur, true
}

func (b *Buffer) SelectedText() string {
	if s, e, ok := b.Selection(); ok {
		return b.Range(s, e)
	}
	return ""
}

// endOf is where the cursor lands after text is inserted at at.
func endOf(at Pos, text string) Pos {
	n := strings.Count(text, "\n")
	if n == 0 {
		return Pos{at.Row, at.Col + utf8.RuneCountInString(text)}
	}
	return Pos{at.Row + n, utf8.RuneCountInString(text[strings.LastIndexByte(text, '\n')+1:])}
}

// apply replaces the text between at and end with text, with no history, and returns where the
// inserted text ends.
func (b *Buffer) apply(at, end Pos, text string) Pos {
	head := slices.Clone(b.Lines[at.Row][:at.Col])
	tail := b.Lines[end.Row][end.Col:]
	parts := strings.Split(text, "\n")
	mid := make([][]rune, len(parts))
	for i, p := range parts {
		mid[i] = []rune(p)
	}
	mid[0] = append(head, mid[0]...)
	last := len(mid) - 1
	mid[last] = append(mid[last], tail...)
	b.Lines = slices.Concat(b.Lines[:at.Row], mid, b.Lines[end.Row+1:])
	return endOf(at, text)
}

func (b *Buffer) edit(at, end Pos, text string, typing bool) {
	old, before := b.Range(at, end), b.Cur
	after := b.apply(at, end, text)
	typing = typing && old == ""
	if n := len(b.undo); typing && n > 0 && b.undo[n-1].typing && b.undo[n-1].after == at {
		b.undo[n-1].new += text
		b.undo[n-1].after = after
	} else {
		b.undo = append(b.undo, change{at, before, after, old, text, typing})
		if len(b.undo) > maxUndo {
			b.undo = b.undo[1:]
		}
	}
	b.redo = nil
	b.Cur, b.HasSel, b.want = after, false, after.Col
	b.Version++
}

func (b *Buffer) Undo() bool {
	n := len(b.undo)
	if n == 0 {
		return false
	}
	c := b.undo[n-1]
	b.undo = b.undo[:n-1]
	b.apply(c.at, endOf(c.at, c.new), c.old)
	b.Cur, b.HasSel, b.want = b.Clamp(c.before), false, c.before.Col
	b.redo = append(b.redo, c)
	b.Version++
	return true
}

func (b *Buffer) Redo() bool {
	n := len(b.redo)
	if n == 0 {
		return false
	}
	c := b.redo[n-1]
	b.redo = b.redo[:n-1]
	b.apply(c.at, endOf(c.at, c.old), c.new)
	b.Cur, b.HasSel, b.want = b.Clamp(c.after), false, c.after.Col
	b.undo = append(b.undo, c)
	b.Version++
	return true
}

// Insert types text at the cursor, replacing the selection.
func (b *Buffer) Insert(text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" {
		return
	}
	at, end, sel := b.Selection()
	if !sel {
		at, end = b.Cur, b.Cur
	}
	b.edit(at, end, text, !sel && utf8.RuneCountInString(text) == 1 && text != "\n")
}

// Newline starts a new line with the current line's indentation.
func (b *Buffer) Newline() {
	b.DeleteSelection()
	line := b.Lines[b.Cur.Row][:b.Cur.Col]
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	b.Insert("\n" + string(line[:n]))
}

func (b *Buffer) DeleteSelection() bool {
	s, e, ok := b.Selection()
	if ok {
		b.edit(s, e, "", false)
	}
	return ok
}

func (b *Buffer) Backspace() {
	if b.DeleteSelection() {
		return
	}
	switch p := b.Cur; {
	case p.Col > 0:
		b.edit(Pos{p.Row, p.Col - 1}, p, "", false)
	case p.Row > 0:
		b.edit(Pos{p.Row - 1, len(b.Lines[p.Row-1])}, p, "", false)
	}
}

func (b *Buffer) Delete() {
	if b.DeleteSelection() {
		return
	}
	switch p := b.Cur; {
	case p.Col < len(b.Lines[p.Row]):
		b.edit(p, Pos{p.Row, p.Col + 1}, "", false)
	case p.Row < len(b.Lines)-1:
		b.edit(p, Pos{p.Row + 1, 0}, "", false)
	}
}

// lineSpan is the rows the selection covers, or the cursor's row. A selection ending at the
// start of a line doesn't include that line.
func (b *Buffer) lineSpan() (r0, r1 int) {
	s, e, ok := b.Selection()
	if !ok {
		return b.Cur.Row, b.Cur.Row
	}
	if e.Col == 0 && e.Row > s.Row {
		return s.Row, e.Row - 1
	}
	return s.Row, e.Row
}

// rewriteLines replaces rows r0..r1 with lines in one undo step, then puts the cursor and the
// selection where move says.
func (b *Buffer) rewriteLines(r0, r1 int, lines []string, move func(Pos) Pos) {
	cur, anchor, sel := b.Cur, b.Anchor, b.HasSel
	b.edit(Pos{r0, 0}, Pos{r1, len(b.Lines[r1])}, strings.Join(lines, "\n"), false)
	b.Cur, b.Anchor, b.HasSel = b.Clamp(move(cur)), b.Clamp(move(anchor)), sel
	b.want = b.Cur.Col
}

func (b *Buffer) rows(r0, r1 int) []string {
	out := make([]string, 0, r1-r0+1)
	for r := r0; r <= r1; r++ {
		out = append(out, string(b.Lines[r]))
	}
	return out
}

func leading(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// Indent adds unit to the start of the selected lines (or the cursor's line), or removes one
// level of indentation when dedent is set.
func (b *Buffer) Indent(unit string, dedent bool) {
	r0, r1 := b.lineSpan()
	lines, delta := b.rows(r0, r1), make([]int, r1-r0+1)
	for i, l := range lines {
		switch {
		case !dedent && l != "":
			lines[i], delta[i] = unit+l, utf8.RuneCountInString(unit)
		case dedent:
			n := 0
			if strings.HasPrefix(l, "\t") {
				n = 1
			} else {
				for n < len(l) && n < 4 && l[n] == ' ' {
					n++
				}
			}
			lines[i], delta[i] = l[n:], -n
		}
	}
	b.rewriteLines(r0, r1, lines, func(p Pos) Pos {
		if p.Row >= r0 && p.Row <= r1 {
			p.Col = max(p.Col+delta[p.Row-r0], 0)
		}
		return p
	})
}

// ToggleComment comments the selected lines (or the cursor's line) with prefix, or uncomments
// them when every non-blank one is already commented.
func (b *Buffer) ToggleComment(prefix string) {
	r0, r1 := b.lineSpan()
	lines := b.rows(r0, r1)
	all, indent := true, -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimLeft(l, " \t"), prefix) {
			all = false
		}
		if n := len(leading(l)); indent < 0 || n < indent {
			indent = n
		}
	}
	if indent < 0 {
		return // only blank lines
	}
	delta := make([]int, len(lines))
	for i, l := range lines {
		switch {
		case strings.TrimSpace(l) == "":
		case all:
			ws := leading(l)
			rest := strings.TrimPrefix(l[len(ws):], prefix)
			trimmed := strings.TrimPrefix(rest, " ")
			lines[i] = ws + trimmed
			delta[i] = -(len(l) - len(lines[i]))
		default:
			lines[i] = l[:indent] + prefix + " " + l[indent:]
			delta[i] = len(prefix) + 1
		}
	}
	b.rewriteLines(r0, r1, lines, func(p Pos) Pos {
		if p.Row >= r0 && p.Row <= r1 {
			p.Col = max(p.Col+delta[p.Row-r0], 0)
		}
		return p
	})
}

// MoveLines moves the selected lines (or the cursor's line) up (d < 0) or down one row.
func (b *Buffer) MoveLines(d int) {
	r0, r1 := b.lineSpan()
	if d < 0 && r0 == 0 || d > 0 && r1 == len(b.Lines)-1 {
		return
	}
	shift := func(p Pos) Pos { p.Row += d; return p }
	if d < 0 {
		b.rewriteLines(r0-1, r1, append(b.rows(r0, r1), string(b.Lines[r0-1])), shift)
	} else {
		b.rewriteLines(r0, r1+1, append([]string{string(b.Lines[r1+1])}, b.rows(r0, r1)...), shift)
	}
}

// DuplicateLines copies the selected lines (or the cursor's line) below themselves; with down the
// cursor follows the copy.
func (b *Buffer) DuplicateLines(down bool) {
	r0, r1 := b.lineSpan()
	n := r1 - r0 + 1
	lines := b.rows(r0, r1)
	b.rewriteLines(r0, r1, append(lines, lines...), func(p Pos) Pos {
		if down {
			p.Row += n
		}
		return p
	})
}

// DeleteLines removes the selected lines (or the cursor's line).
func (b *Buffer) DeleteLines() {
	r0, r1 := b.lineSpan()
	col := b.Cur.Col
	switch {
	case r1 < len(b.Lines)-1:
		b.edit(Pos{r0, 0}, Pos{r1 + 1, 0}, "", false)
	case r0 > 0:
		b.edit(Pos{r0 - 1, len(b.Lines[r0-1])}, Pos{r1, len(b.Lines[r1])}, "", false)
		r0--
	default:
		b.edit(Pos{0, 0}, Pos{r1, len(b.Lines[r1])}, "", false)
	}
	b.MoveTo(Pos{r0, col}, false)
}

// SelectLine selects the cursor's line, or extends the selection by a line.
func (b *Buffer) SelectLine() {
	if !b.HasSel {
		b.Anchor, b.HasSel = Pos{b.Cur.Row, 0}, true
	}
	if b.Cur.Row < len(b.Lines)-1 {
		b.Cur = Pos{b.Cur.Row + 1, 0}
	} else {
		b.Cur = b.End()
	}
}

// DeleteWordLeft deletes back to the start of the word.
func (b *Buffer) DeleteWordLeft() {
	if b.DeleteSelection() {
		return
	}
	end := b.Cur
	b.WordLeft(false)
	start := b.Cur
	b.Cur = end
	b.edit(start, end, "", false)
}

// SelectNext selects the word at the cursor, or the next occurrence of the selection.
func (b *Buffer) SelectNext() bool {
	q := b.SelectedText()
	if q == "" {
		p, line := b.Cur, b.Lines[b.Cur.Row]
		if p.Col > 0 && (p.Col == len(line) || !isWord(line[p.Col])) && isWord(line[p.Col-1]) {
			p.Col--
		}
		b.SelectWord(p)
		return true
	}
	_, e, _ := b.Selection()
	s, e, ok := b.Find(q, e, true)
	if ok {
		b.Anchor, b.Cur, b.HasSel = s, e, true
	}
	return ok
}

// ReplaceAll replaces every match of q (ignoring case, like Find) with repl in one undo step and
// returns how many it replaced.
func (b *Buffer) ReplaceAll(q, repl string) int {
	text := b.Text()
	lower, lq := strings.ToLower(text), strings.ToLower(q)
	if q == "" || len(lower) != len(text) { // ponytail: case folding changed byte lengths; match exactly
		lower, lq = text, q
	}
	var out strings.Builder
	n, i := 0, 0
	for {
		j := strings.Index(lower[i:], lq)
		if q == "" || j < 0 {
			break
		}
		out.WriteString(text[i : i+j])
		out.WriteString(repl)
		i += j + len(lq)
		n++
	}
	if n > 0 {
		out.WriteString(text[i:])
		cur := b.Cur
		b.edit(Pos{}, b.End(), out.String(), false)
		b.MoveTo(cur, false)
	}
	return n
}

// MoveTo puts the cursor at p; with extend it grows the selection instead of clearing it.
func (b *Buffer) MoveTo(p Pos, extend bool) {
	b.move(b.Clamp(p), extend)
	b.want = b.Cur.Col
}

func (b *Buffer) move(p Pos, extend bool) {
	if extend && !b.HasSel {
		b.Anchor, b.HasSel = b.Cur, true
	}
	if !extend {
		b.HasSel = false
	}
	b.Cur = p
	if b.Cur == b.Anchor {
		b.HasSel = false
	}
}

func (b *Buffer) Left(extend bool) {
	if s, _, ok := b.Selection(); ok && !extend {
		b.MoveTo(s, false)
		return
	}
	p := b.Cur
	if p.Col > 0 {
		p.Col--
	} else if p.Row > 0 {
		p.Row--
		p.Col = len(b.Lines[p.Row])
	}
	b.MoveTo(p, extend)
}

func (b *Buffer) Right(extend bool) {
	if _, e, ok := b.Selection(); ok && !extend {
		b.MoveTo(e, false)
		return
	}
	p := b.Cur
	if p.Col < len(b.Lines[p.Row]) {
		p.Col++
	} else if p.Row < len(b.Lines)-1 {
		p.Row++
		p.Col = 0
	}
	b.MoveTo(p, extend)
}

// Vertical moves d lines (negative is up), keeping the column; past either end it goes to that end.
func (b *Buffer) Vertical(d int, extend bool) {
	row := b.Cur.Row + d
	switch {
	case row < 0:
		b.MoveTo(Pos{}, extend)
	case row >= len(b.Lines):
		b.MoveTo(b.End(), extend)
	default:
		b.move(Pos{row, min(b.want, len(b.Lines[row]))}, extend)
	}
}

// LineStart goes to the first non-blank character, then to column 0 on a second press.
func (b *Buffer) LineStart(extend bool) {
	line, first := b.Lines[b.Cur.Row], 0
	for first < len(line) && (line[first] == ' ' || line[first] == '\t') {
		first++
	}
	if b.Cur.Col == first {
		first = 0
	}
	b.MoveTo(Pos{b.Cur.Row, first}, extend)
}

func (b *Buffer) LineEnd(extend bool) {
	b.MoveTo(Pos{b.Cur.Row, len(b.Lines[b.Cur.Row])}, extend)
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (b *Buffer) WordLeft(extend bool) {
	p, line := b.Cur, b.Lines[b.Cur.Row]
	if p.Col == 0 {
		b.Left(extend)
		return
	}
	i := p.Col
	for i > 0 && unicode.IsSpace(line[i-1]) {
		i--
	}
	if i > 0 {
		w := isWord(line[i-1])
		for i > 0 && !unicode.IsSpace(line[i-1]) && isWord(line[i-1]) == w {
			i--
		}
	}
	b.MoveTo(Pos{p.Row, i}, extend)
}

func (b *Buffer) WordRight(extend bool) {
	p, line := b.Cur, b.Lines[b.Cur.Row]
	if p.Col >= len(line) {
		b.Right(extend)
		return
	}
	i := p.Col
	if !unicode.IsSpace(line[i]) {
		w := isWord(line[i])
		for i < len(line) && !unicode.IsSpace(line[i]) && isWord(line[i]) == w {
			i++
		}
	}
	for i < len(line) && unicode.IsSpace(line[i]) {
		i++
	}
	b.MoveTo(Pos{p.Row, i}, extend)
}

func (b *Buffer) SelectAll() {
	b.Anchor, b.Cur, b.HasSel = Pos{}, b.End(), true
}

// SelectWord selects the word (or run of punctuation or blanks) at p.
func (b *Buffer) SelectWord(p Pos) {
	p = b.Clamp(p)
	line := b.Lines[p.Row]
	if len(line) == 0 {
		b.MoveTo(p, false)
		return
	}
	i := min(p.Col, len(line)-1)
	class := func(r rune) int {
		switch {
		case isWord(r):
			return 0
		case unicode.IsSpace(r):
			return 1
		}
		return 2
	}
	c, s, e := class(line[i]), i, i+1
	for s > 0 && class(line[s-1]) == c {
		s--
	}
	for e < len(line) && class(line[e]) == c {
		e++
	}
	b.Anchor, b.Cur, b.HasSel = Pos{p.Row, s}, Pos{p.Row, e}, true
}

// Find looks for q (ignoring case) from `from`, wrapping around, and returns the match.
func (b *Buffer) Find(q string, from Pos, forward bool) (start, end Pos, ok bool) {
	q = strings.ToLower(q)
	if q == "" {
		return
	}
	n, qn := len(b.Lines), utf8.RuneCountInString(q)
	for i := 0; i <= n; i++ {
		row := from.Row + i
		if !forward {
			row = from.Row - i
		}
		row = (row%n + n) % n
		line := strings.ToLower(string(b.Lines[row]))
		var idx int
		if forward {
			off := 0
			if i == 0 {
				off = len(strings.ToLower(string(b.Lines[row][:min(from.Col, len(b.Lines[row]))])))
			}
			if idx = strings.Index(line[off:], q); idx >= 0 {
				idx += off
			}
		} else {
			cut := len(line)
			if i == 0 {
				cut = len(strings.ToLower(string(b.Lines[row][:min(from.Col, len(b.Lines[row]))])))
			}
			idx = strings.LastIndex(line[:cut], q)
		}
		if idx >= 0 {
			col := utf8.RuneCountInString(line[:idx])
			return Pos{row, col}, Pos{row, col + qn}, true
		}
	}
	return
}
