package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/edit"
	"crew/internal/task"
)

// The Files tab's editor, to the right of the tree. It works like a normal editor: type to edit,
// shift+arrows or the mouse select, and the keys follow VS Code where a terminal can send them
// (editorKeys lists them). esc (or ctrl+]) hands the keys back to the tree; ⇧→ from the tree
// comes back.

// editorKeys is the help line while the editor has the keys.
const editorKeys = "ctrl+s save  ctrl+z/y undo/redo  ctrl+c/x/v clipboard  ctrl+f find  ctrl+r replace  ctrl+g go to line  " +
	"ctrl+d select next  ctrl+/ comment  ctrl+k delete line  ctrl+l select line  alt+↑↓ move line  alt+⇧↑↓ copy line  " +
	"alt+⌫ delete word  ctrl+p open file  ctrl+pgup/pgdn switch file  ctrl+w close  ctrl+e VS Code  esc tree"

const maxEditBytes = 2 << 20

// doc is one open file.
type doc struct {
	path        string // relative to the task's folder
	buf         *edit.Buffer
	dirty, crlf bool
	top, left   int // first visible row and column
	mtime       time.Time
	size        int64
	lexer       chroma.Lexer
	hl          struct { // syntax colours for a window of rows around the view
		version, from int
		rows          [][]uint16
	}
}

// editor is one task's open files.
type editor struct {
	docs       []*doc
	cur        int
	w, h       int    // the pane, set by layout
	prompt     string // the status bar asks for "find", "replace" or "goto" (a line number)
	query      string // find text
	repl       string // replacement
	line       string // go to line
	inRepl     bool   // typing goes to the replacement
	note       string // one-line feedback in the status bar, cleared on the next key
	closeArmed bool   // ctrl+w was pressed once on a file with unsaved changes
	dragging   bool
	lastClick  time.Time
	lastPos    edit.Pos
}

func (ed *editor) doc() *doc { return ed.docs[ed.cur] }

func (m Model) editorTab() bool {
	return m.tab == tabFiles && m.current() != nil && m.editors[m.currentID()] != nil
}

func (m Model) editActive() bool { return m.editFocus && m.editorTab() }

func loadDoc(abs, rel string) (*doc, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxEditBytes {
		return nil, fmt.Errorf("%s is too large to edit here (%d KB) · ctrl+e opens it in VS Code", rel, st.Size()>>10)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil, fmt.Errorf("%s is a binary file", rel)
	}
	text := string(data)
	d := &doc{path: rel, crlf: strings.Contains(text, "\r\n"), mtime: st.ModTime(), size: st.Size()}
	d.buf = edit.New(strings.ReplaceAll(text, "\r\n", "\n"))
	d.lexer = lexers.Match(rel)
	if d.lexer == nil {
		d.lexer = lexers.Analyse(text[:min(len(text), 4000)])
	}
	if d.lexer == nil {
		d.lexer = lexers.Fallback
	}
	d.lexer = chroma.Coalesce(d.lexer)
	return d, nil
}

// openInEditor opens p (relative to the task's folder) in a tab, or switches to its tab, and
// gives the editor the keys.
func (m *Model) openInEditor(p string) tea.Cmd {
	t := m.current()
	ed := m.editors[t.ID]
	if ed == nil {
		ed = &editor{}
	}
	i := -1
	for j, d := range ed.docs {
		if d.path == p {
			i = j
		}
	}
	if i < 0 {
		d, err := loadDoc(m.docAbs(*t, p), p)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		ed.docs = append(ed.docs, d)
		i = len(ed.docs) - 1
	}
	m.editors[t.ID], ed.cur = ed, i
	m.tree().file = p
	m.focusOn(areaEditor, -1)
	m.layout()
	ed.ensureVisible()
	return nil
}

func (m *Model) resizeEditor() {
	if m.editorTab() {
		ed := m.editors[m.currentID()]
		ed.w, ed.h = m.vp.Width+2, m.vp.Height // the viewer, a gap and the scroll bar
	}
}

func (ed *editor) textRows() int { return max(ed.h-2, 1) } // minus the tab bar and the status bar

func (ed *editor) gutter() int { return max(len(fmt.Sprint(len(ed.doc().buf.Lines))), 3) + 2 }

// ensureVisible scrolls so the cursor is on screen.
func (ed *editor) ensureVisible() {
	d := ed.doc()
	rows, cols := ed.textRows(), max(ed.w-ed.gutter()-1, 1)
	c := d.buf.Cur
	d.top = max(min(d.top, c.Row), c.Row-rows+1)
	x := visualCol(d.buf.Lines[c.Row], c.Col)
	d.left = max(min(d.left, x), x-cols+1)
}

// updateEditor handles keys while the editor has them.
func (m Model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ed := m.editors[m.currentID()]
	d, k := ed.doc(), msg.String()
	b := d.buf
	v := b.Version
	defer func() {
		if b.Version != v {
			d.dirty = true
		}
	}()
	if ed.prompt != "" {
		ed.promptKey(msg)
		return m, nil
	}
	ed.note = ""
	if k != "ctrl+w" {
		ed.closeArmed = false
	}
	switch k {
	case "esc", "ctrl+]":
		m.editFocus = false
	case "ctrl+s":
		m.saveDoc(d)
	case "ctrl+z":
		b.Undo()
	case "ctrl+y", "ctrl+shift+z":
		b.Redo()
	case "ctrl+a":
		b.SelectAll()
	case "ctrl+c", "ctrl+x":
		if s := b.SelectedText(); s != "" {
			if err := clipboard.WriteAll(s); err != nil {
				ed.note = "clipboard: " + err.Error()
			} else if k == "ctrl+x" {
				b.DeleteSelection()
			}
		}
	case "ctrl+v":
		if s, err := clipboard.ReadAll(); err == nil {
			b.Insert(s)
		}
	case "ctrl+f", "ctrl+r":
		ed.prompt, ed.inRepl = "find", false
		if k == "ctrl+r" {
			ed.prompt = "replace"
		}
		if s := b.SelectedText(); s != "" && !strings.Contains(s, "\n") {
			ed.query = s
		}
	case "ctrl+g":
		ed.prompt, ed.line = "goto", ""
	case "f3":
		ed.search(true, false)
	case "shift+f3":
		ed.search(false, false)
	case "ctrl+d":
		if !b.SelectNext() {
			ed.note = "no other match"
		}
	case "ctrl+k":
		b.DeleteLines()
	case "ctrl+l":
		b.SelectLine()
	case "alt+up", "alt+down":
		b.MoveLines(map[string]int{"alt+up": -1, "alt+down": 1}[k])
	case "alt+shift+up", "alt+shift+down":
		b.DuplicateLines(k == "alt+shift+down")
	case "ctrl+_", "ctrl+/", "ctrl+7": // terminals send ctrl+/ as ctrl+_
		if p := commentPrefix(d.path); p != "" {
			b.ToggleComment(p)
		} else {
			ed.note = "no line comments in this kind of file"
		}
	case "alt+backspace", "ctrl+backspace":
		b.DeleteWordLeft()
	case "ctrl+p":
		m.editFocus = false
		m.openFinder()
	case "ctrl+w":
		m.closeDoc(ed)
		return m, nil
	case "ctrl+e":
		m.openVSCode(d.path, b.Cur)
	case "ctrl+pgup", "ctrl+pgdown":
		n := len(ed.docs)
		if k == "ctrl+pgup" {
			ed.cur = (ed.cur + n - 1) % n
		} else {
			ed.cur = (ed.cur + 1) % n
		}
		m.tree().file = ed.doc().path
		return m, nil
	case "enter":
		b.Newline()
	case "backspace", "ctrl+h":
		b.Backspace()
	case "delete":
		b.Delete()
	case "tab":
		if s, e, ok := b.Selection(); ok && s.Row != e.Row {
			b.Indent(indentUnit(b), false)
		} else {
			b.Insert(indentUnit(b))
		}
	case "shift+tab":
		b.Indent(indentUnit(b), true)
	default:
		if !moveKey(b, k, ed.textRows()) {
			switch {
			case msg.Paste || msg.Type == tea.KeyRunes && !msg.Alt:
				b.Insert(string(msg.Runes))
			case msg.Type == tea.KeySpace:
				b.Insert(" ")
			}
		}
	}
	ed.ensureVisible()
	return m, nil
}

func moveKey(b *edit.Buffer, k string, page int) bool {
	ext := strings.Contains(k, "shift+")
	switch strings.Replace(k, "shift+", "", 1) {
	case "left":
		b.Left(ext)
	case "right":
		b.Right(ext)
	case "up":
		b.Vertical(-1, ext)
	case "down":
		b.Vertical(1, ext)
	case "home":
		b.LineStart(ext)
	case "end":
		b.LineEnd(ext)
	case "ctrl+home":
		b.MoveTo(edit.Pos{}, ext)
	case "ctrl+end":
		b.MoveTo(b.End(), ext)
	case "ctrl+left", "alt+left", "alt+b":
		b.WordLeft(ext)
	case "ctrl+right", "alt+right", "alt+f":
		b.WordRight(ext)
	case "pgup":
		b.Vertical(-page, ext)
	case "pgdown":
		b.Vertical(page, ext)
	default:
		return false
	}
	return true
}

// indentUnit is a tab if the file indents with tabs, else four spaces.
func indentUnit(b *edit.Buffer) string {
	for _, l := range b.Lines[:min(len(b.Lines), 500)] {
		if len(l) > 0 && l[0] == '\t' {
			return "\t"
		}
	}
	return "    "
}

// promptKey handles keys while the status bar asks for find text, a replacement or a line number.
func (ed *editor) promptKey(msg tea.KeyMsg) {
	b := ed.doc().buf
	ed.note = ""
	field := &ed.query
	switch {
	case ed.prompt == "goto":
		field = &ed.line
	case ed.inRepl:
		field = &ed.repl
	}
	switch k := msg.String(); {
	case k == "esc" || k == "ctrl+]":
		ed.prompt = ""
		return
	case ed.prompt == "goto" && k == "enter":
		if n, err := strconv.Atoi(ed.line); err == nil && n > 0 {
			b.MoveTo(edit.Pos{Row: n - 1}, false)
			ed.center()
		}
		ed.prompt = ""
		return
	case ed.prompt == "goto":
	case k == "tab" || k == "shift+tab":
		ed.inRepl = ed.prompt == "replace" && !ed.inRepl
		return
	case ed.inRepl && k == "enter":
		if strings.EqualFold(b.SelectedText(), ed.query) {
			b.Insert(ed.repl)
		}
		ed.search(true, false)
		return
	case ed.inRepl && k == "ctrl+a":
		ed.note = fmt.Sprintf("replaced %d", b.ReplaceAll(ed.query, ed.repl))
		ed.ensureVisible()
		return
	case k == "enter" || k == "down" || k == "f3":
		ed.search(true, false)
		return
	case k == "up" || k == "shift+f3":
		ed.search(false, false)
		return
	}
	switch {
	case msg.String() == "backspace":
		if r := []rune(*field); len(r) > 0 {
			*field = string(r[:len(r)-1])
		}
	case msg.Type == tea.KeyRunes && !msg.Alt:
		*field += string(msg.Runes)
	case msg.Type == tea.KeySpace:
		*field += " "
	default:
		return
	}
	if field == &ed.query {
		ed.search(true, true) // as you type, from where the current match starts
	}
}

// center scrolls so the cursor's line is in the middle of the pane.
func (ed *editor) center() {
	d := ed.doc()
	d.top = max(d.buf.Cur.Row-ed.textRows()/2, 0)
	ed.ensureVisible()
}

// commentPrefix is the line comment for a file, or "" when it has none.
func commentPrefix(p string) string {
	switch base := filepath.Base(p); strings.ToLower(filepath.Ext(p)) {
	case ".py", ".sh", ".bash", ".zsh", ".rb", ".yaml", ".yml", ".toml", ".r", ".pl", ".conf", ".nix", ".mk", ".tf", ".env", ".ini", ".cfg":
		return "#"
	case ".sql", ".lua", ".hs":
		return "--"
	case ".html", ".xml", ".css", ".md", ".json", ".svg":
		return ""
	case "":
		if base == "Makefile" || base == "Dockerfile" || strings.HasPrefix(base, ".") {
			return "#"
		}
	}
	return "//"
}

func (ed *editor) search(forward, again bool) {
	b := ed.doc().buf
	from := b.Cur
	if s, _, ok := b.Selection(); ok && (again || !forward) {
		from = s
	}
	s, e, ok := b.Find(ed.query, from, forward)
	if !ok {
		if ed.query != "" {
			ed.note = "no match"
		}
		return
	}
	b.Anchor, b.Cur, b.HasSel = s, e, true
	ed.ensureVisible()
}

func (m *Model) saveDoc(d *doc) bool {
	ed := m.editors[m.currentID()]
	abs := m.docAbs(*m.current(), d.path)
	text := d.buf.Text()
	if d.crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(abs, []byte(text), mode); err != nil {
		ed.note = "save failed: " + err.Error()
		return false
	}
	if st, err := os.Stat(abs); err == nil {
		d.mtime, d.size = st.ModTime(), st.Size()
	}
	d.dirty, ed.note = false, "saved"
	m.refreshChanged() // the tree's change marks
	return true
}

// closeDoc closes the current tab; unsaved changes need a second ctrl+w.
func (m *Model) closeDoc(ed *editor) {
	if ed.doc().dirty && !ed.closeArmed {
		ed.closeArmed, ed.note = true, "unsaved changes · ctrl+s saves, ctrl+w again discards them"
		return
	}
	ed.closeArmed = false
	ed.docs = append(ed.docs[:ed.cur], ed.docs[ed.cur+1:]...)
	if len(ed.docs) > 0 {
		ed.cur = min(ed.cur, len(ed.docs)-1)
		m.tree().file = ed.doc().path
		return
	}
	delete(m.editors, m.currentID())
	m.editFocus = false
	m.tree().file = ""
	m.layout()
	m.loadFiles() // the viewer takes the pane back
}

// reloadEditor re-reads open files that changed on disk (the agent edits them); files with unsaved
// changes are kept and flagged.
func (m *Model) reloadEditor(id string) {
	ed, t := m.editors[id], m.find(id)
	if ed == nil || t == nil {
		return
	}
	for _, d := range ed.docs {
		abs := m.docAbs(*t, d.path)
		st, err := os.Stat(abs)
		if err != nil || st.ModTime().Equal(d.mtime) && st.Size() == d.size {
			continue
		}
		if d.dirty {
			ed.note = d.path + " changed on disk · saving overwrites those changes"
			continue
		}
		if fresh, err := loadDoc(abs, d.path); err == nil {
			cur := d.buf.Cur
			d.buf, d.mtime, d.size, d.crlf = fresh.buf, fresh.mtime, fresh.size, fresh.crlf
			d.buf.MoveTo(cur, false)
		}
	}
}

func (m *Model) stopEditor(id string) { delete(m.editors, id) }

// docAbs is where an open file is: paths are relative to the task's folder, except files outside it
// (the task's plan), which are kept absolute.
func (m *Model) docAbs(t task.Task, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(m.taskDir(t), p)
}

func codeBin() string {
	if p, err := exec.LookPath("code"); err == nil {
		return p
	}
	p := "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// openVSCode opens the task's folder in VS Code at p:pos, saving p first if it has unsaved changes.
func (m *Model) openVSCode(p string, pos edit.Pos) {
	bin := codeBin()
	if bin == "" {
		m.notice = "VS Code's code command isn't installed · in VS Code run “Shell Command: Install 'code' command in PATH”"
		return
	}
	if ed := m.editors[m.currentID()]; ed != nil {
		for _, d := range ed.docs {
			if d.path == p && d.dirty && !m.saveDoc(d) {
				return
			}
		}
	}
	dir := m.taskDir(*m.current())
	c := exec.Command(bin, dir, "--goto", fmt.Sprintf("%s:%d:%d", m.docAbs(*m.current(), p), pos.Row+1, pos.Col+1))
	if err := c.Start(); err != nil {
		m.notice = "couldn't start VS Code: " + err.Error()
		return
	}
	go c.Wait()
	m.notice = "Opened " + p + " in VS Code"
}
