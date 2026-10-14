// Package files is a task's Files tab: a tree of its files with the selected one shown (read-only,
// highlighted) on the right, a finder (/ or ⌘P), and an editor (editor.go) with tabs for the files
// it has open. crew keeps one Tab per task and hands it keys, the mouse and a Host for the rest.
package files

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/repo"
	"crew/internal/widgets/files/edit"
	"crew/internal/widgets/look"
)

// Host is what a Files tab needs from crew: the main pane's viewport, a notice on the help line,
// laying the window out again, moving the keys between the tree and the editor, and the task's files.
type Host interface {
	Viewport() *viewport.Model
	Notice(string)
	Layout()
	FocusEditor() // the editor gets the keys
	FocusTree()   // the main pane, where the tree is, gets the keys
	LeaveEditor() // the editor gives the keys back to the tree
	RepoFiles() []string
}

// Tab is one task's Files tab: its tree and, once a file is open, its editor. Dir is where the
// task's files are (its worktree, or its repo before it starts); Worktree and Base say what it
// changed, for the tree's colours. crew sets them before each use.
type Tab struct {
	Dir, Worktree, Base string
	tree                *fileTree
	ed                  *editor
}

// New makes a task's Files tab.
func New() *Tab { return &Tab{tree: &fileTree{open: map[string]bool{}}} }

// Editing reports whether the tab has files open in its editor.
func (ft *Tab) Editing() bool { return ft.ed != nil }

// File is the file shown on the right, or "".
func (ft *Tab) File() string { return ft.tree.file }

// Finding reports whether the finder is open.
func (ft *Tab) Finding() bool { return ft.tree.finder }

// OnlyChanged reports whether the tree shows just the changed files.
func (ft *Tab) OnlyChanged() bool { return ft.tree.onlyChanged }

// CloseEditor closes the editor, unsaved changes and all (the task was deleted or moved).
func (ft *Tab) CloseEditor() { ft.ed = nil }

const maxViewBytes = 300 << 10 // bigger files are left to the editor

type fileNode struct {
	name, path string
	dir        bool
	kids       []*fileNode
}

type fileRow struct {
	node  *fileNode
	depth int
}

// fileTree is one task's Files tab state.
type fileTree struct {
	root     *fileNode
	open     map[string]bool // expanded folders, by path
	rows     []fileRow       // what the tree shows now
	sel, top int
	file     string // the file shown on the right
	finder   bool
	query    string
	matches  []string
	idx      int

	changed     map[string]byte // files the task changed: M, A or U (repo.Changed)
	dirs        map[string]bool // folders holding changed files
	onlyChanged bool            // c: show just the changed files
}

func buildTree(files []string) *fileNode {
	root := &fileNode{dir: true}
	dirs := map[string]*fileNode{"": root}
	for _, f := range files {
		parts := strings.Split(f, "/")
		parent := root
		for i := range parts[:len(parts)-1] {
			dp := strings.Join(parts[:i+1], "/")
			d := dirs[dp]
			if d == nil {
				d = &fileNode{name: parts[i], path: dp, dir: true}
				dirs[dp] = d
				parent.kids = append(parent.kids, d)
			}
			parent = d
		}
		parent.kids = append(parent.kids, &fileNode{name: parts[len(parts)-1], path: f})
	}
	var sortKids func(n *fileNode)
	sortKids = func(n *fileNode) {
		slices.SortFunc(n.kids, func(a, b *fileNode) int {
			if a.dir != b.dir { // folders first
				if a.dir {
					return -1
				}
				return 1
			}
			return strings.Compare(a.name, b.name)
		})
		for _, k := range n.kids {
			sortKids(k)
		}
	}
	sortKids(root)
	return root
}

func (ft *fileTree) flatten() {
	ft.rows = ft.rows[:0]
	var walk func(n *fileNode, depth int)
	walk = func(n *fileNode, depth int) {
		for _, k := range n.kids {
			if ft.onlyChanged && !ft.dirs[k.path] && ft.changed[k.path] == 0 {
				continue
			}
			ft.rows = append(ft.rows, fileRow{k, depth})
			if k.dir && (ft.open[k.path] || ft.onlyChanged) { // changed files show unfolded
				walk(k, depth+1)
			}
		}
	}
	if ft.root != nil {
		walk(ft.root, 0)
	}
	ft.sel = min(ft.sel, max(len(ft.rows)-1, 0))
}

func (ft *fileTree) selectPath(p string) {
	for i, r := range ft.rows {
		if r.node.path == p {
			ft.sel = i
			return
		}
	}
}

// reveal opens the folders above p and selects it.
func (ft *fileTree) reveal(p string) {
	for i := range p {
		if p[i] == '/' {
			ft.open[p[:i]] = true
		}
	}
	ft.flatten()
	ft.selectPath(p)
}

func (ft *fileTree) selected() *fileNode {
	if ft.sel < len(ft.rows) {
		return ft.rows[ft.sel].node
	}
	return nil
}

// keepVisible scrolls the tree so the selected row shows in h lines.
func (ft *fileTree) keepVisible(h int) {
	ft.top = max(min(ft.top, ft.sel), ft.sel-h+1)
	ft.top = max(ft.top, 0)
}

// refreshChanged re-reads which files the task changed, for the tree's colours.
func (ft *Tab) refreshChanged() {
	tr := ft.tree
	tr.changed, tr.dirs = nil, map[string]bool{}
	if ft.Worktree != "" {
		tr.changed = repo.Changed(ft.Worktree, ft.Base)
	}
	for p := range tr.changed {
		for i := range p {
			if p[i] == '/' {
				tr.dirs[p[:i]] = true
			}
		}
	}
	tr.flatten()
}

func changeStyle(c byte) lipgloss.Style {
	switch c {
	case 'A', 'U':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	case 'D':
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
}

// Load (re)builds the tree from git and re-reads the file on the right.
func (ft *Tab) Load(h Host) {
	tr := ft.tree
	prev := ""
	if n := tr.selected(); n != nil {
		prev = n.path
	}
	tr.root = buildTree(h.RepoFiles())
	ft.refreshChanged()
	tr.selectPath(prev)
	if tr.file != "" {
		ft.showFile(h)
		return
	}
	msg := "Pick a file in the tree. / or ⌘P finds one by name."
	if len(tr.rows) == 0 {
		msg = "No files yet. Send a message in the Chat tab to start the task."
	}
	h.Viewport().SetContent(look.Dim.Render(msg))
}

func (ft *Tab) showFile(h Host) {
	vp := h.Viewport()
	vp.SetContent(look.Clip(renderFile(filepath.Join(ft.Dir, ft.tree.file)), vp.Width))
	vp.GotoTop()
}

// renderFile returns the file with line numbers and syntax colours.
func renderFile(p string) string {
	data, err := os.ReadFile(p)
	switch {
	case err != nil:
		return look.Warn.Render(err.Error())
	case len(data) > maxViewBytes:
		return look.Dim.Render(fmt.Sprintf("%d KB is too large to show here. Press enter to open it in your editor.", len(data)>>10))
	case bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0:
		return look.Dim.Render("Binary file.")
	}
	src := strings.ReplaceAll(string(data), "\t", "    ")
	lexer := lexers.Match(p)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	var b bytes.Buffer
	it, err := chroma.Coalesce(lexer).Tokenise(nil, src)
	if err == nil {
		err = formatters.Get("terminal256").Format(&b, styles.Get("monokai"), it)
	}
	if err != nil {
		b.Reset()
		b.WriteString(src)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i, l := range lines {
		lines[i] = look.Dim.Render(fmt.Sprintf("%4d ", i+1)) + l
	}
	return strings.Join(lines, "\n")
}

// moveTree moves the selection by d rows; landing on a file shows it.
func (ft *Tab) moveTree(h Host, d int) {
	tr := ft.tree
	tr.sel = max(min(tr.sel+d, len(tr.rows)-1), 0)
	tr.keepVisible(h.Viewport().Height)
	if n := tr.selected(); n != nil && !n.dir && n.path != tr.file && ft.ed == nil { // nvim covers the preview
		tr.file = n.path
		ft.showFile(h)
	}
}

// OpenFinder opens the finder, which finds a file by typing part of its path.
func (ft *Tab) OpenFinder(h Host) {
	tr := ft.tree
	tr.finder, tr.query, tr.idx = true, "", 0
	tr.matches = fuzzy(h.RepoFiles(), "")
}

// Key handles a key on the Files tab; ok is false for keys it leaves to the generic tab keys.
func (ft *Tab) Key(h Host, msg tea.KeyMsg) (_ tea.Cmd, ok bool) {
	tr := ft.tree
	if tr.finder {
		return ft.finderKey(h, msg), true
	}
	n := tr.selected()
	switch msg.String() {
	case "up":
		ft.moveTree(h, -1)
	case "down":
		ft.moveTree(h, 1)
	case "home":
		ft.moveTree(h, -len(tr.rows))
	case "end":
		ft.moveTree(h, len(tr.rows))
	case " ":
		if n != nil && n.dir {
			tr.open[n.path] = !tr.open[n.path]
			tr.flatten()
		}
	case "right": // unfold a folder
		if n != nil && n.dir && !tr.open[n.path] {
			tr.open[n.path] = true
			tr.flatten()
		}
	case "left": // fold a folder, or go up to the folder it's in
		if n != nil && n.dir && tr.open[n.path] {
			tr.open[n.path] = false
			tr.flatten()
			break
		}
		for i := tr.sel - 1; i >= 0; i-- {
			if tr.rows[i].depth < tr.rows[tr.sel].depth {
				ft.moveTree(h, i-tr.sel)
				break
			}
		}
	case "enter", "e":
		if n != nil && n.dir && msg.String() == "enter" {
			tr.open[n.path] = !tr.open[n.path]
			tr.flatten()
			break
		}
		if p := tr.target(); p != "" {
			return ft.Open(h, p), true
		}
	case "/":
		ft.OpenFinder(h)
	case "c":
		tr.onlyChanged = !tr.onlyChanged
		tr.flatten()
		tr.keepVisible(h.Viewport().Height)
	case "ctrl+e":
		if p := tr.target(); p != "" {
			ft.openVSCode(h, p, edit.Pos{})
		}
	case "pgup", "pgdown":
		var cmd tea.Cmd
		vp := h.Viewport()
		*vp, cmd = vp.Update(msg)
		return cmd, true
	default:
		return nil, false
	}
	return nil, true
}

func (ft *Tab) finderKey(h Host, msg tea.KeyMsg) tea.Cmd {
	tr := ft.tree
	switch msg.Type {
	case tea.KeyEsc:
		tr.finder = false
	case tea.KeyUp:
		tr.idx = max(tr.idx-1, 0)
	case tea.KeyDown:
		tr.idx = min(tr.idx+1, len(tr.matches)-1)
	case tea.KeyEnter:
		if tr.idx < len(tr.matches) {
			tr.finder = false
			tr.file = tr.matches[tr.idx]
			tr.reveal(tr.file)
			tr.keepVisible(h.Viewport().Height)
			return ft.Open(h, tr.file)
		}
	case tea.KeyBackspace:
		if r := []rune(tr.query); len(r) > 0 {
			tr.query = string(r[:len(r)-1])
		}
		fallthrough
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type != tea.KeyBackspace && msg.Type != tea.KeySpace {
			tr.query += string(msg.Runes)
		}
		tr.matches, tr.idx = fuzzy(h.RepoFiles(), tr.query), 0
	}
	return nil
}

// fuzzy returns up to 200 files that contain the letters of q in order, best first: matches in
// the file name, then in the path, shorter paths first.
func fuzzy(files []string, q string) []string {
	q = strings.ToLower(q)
	type hit struct {
		f     string
		score int
	}
	var hits []hit
	for _, f := range files {
		l, i := strings.ToLower(f), 0
		for _, r := range l {
			if i < len(q) && rune(q[i]) == r {
				i++
			}
		}
		if i < len(q) {
			continue
		}
		s := len(f)
		switch {
		case strings.Contains(path.Base(l), q):
			s -= 1000
		case strings.Contains(l, q):
			s -= 500
		}
		hits = append(hits, hit{f, s})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	out := make([]string, 0, min(len(hits), 200))
	for _, h := range hits[:min(len(hits), 200)] {
		out = append(out, h.f)
	}
	return out
}

// target is the selected file, else the one on show.
func (ft *fileTree) target() string {
	if n := ft.selected(); n != nil && !n.dir {
		return n.path
	}
	return ft.file
}

// TreeView is the left column of the Files tab: the tree, or the finder, plus a divider.
func (t *Tab) TreeView(w, h int) string {
	ft := t.tree
	cell := func(s string, st lipgloss.Style) string {
		s = ansi.Truncate(s, w, "…")
		return st.Render(s + strings.Repeat(" ", w-ansi.StringWidth(s)))
	}
	plain, folder, cur := lipgloss.NewStyle(), lipgloss.NewStyle().Foreground(lipgloss.Color("12")), lipgloss.NewStyle().Reverse(true)
	var lines []string
	if ft.finder {
		lines = append(lines, cell("/ "+ft.query+"▏", look.Sel))
		if len(ft.matches) == 0 {
			lines = append(lines, cell("no match", look.Dim))
		}
		top := max(ft.idx-(h-2), 0)
		for i := top; i < len(ft.matches) && len(lines) < h; i++ {
			st := plain
			if i == ft.idx {
				st = cur
			}
			lines = append(lines, cell(ft.matches[i], st))
		}
	} else {
		ft.keepVisible(h)
		for i := ft.top; i < len(ft.rows) && len(lines) < h; i++ {
			r := ft.rows[i]
			icon, st, mark, mst := "  ", plain, "", plain
			switch c := ft.changed[r.node.path]; {
			case r.node.dir:
				icon, st = "▸ ", folder
				if ft.open[r.node.path] || ft.onlyChanged {
					icon = "▾ "
				}
				if ft.dirs[r.node.path] {
					mark, mst = "•", changeStyle('M')
				}
			case c != 0:
				mark, st, mst = string(c), changeStyle(c), changeStyle(c)
			}
			if !r.node.dir && r.node.path == ft.file {
				st = st.Bold(true).Underline(true) // the file open on the right
			}
			if i == ft.sel {
				st, mst = cur, cur
			}
			label := ansi.Truncate(strings.Repeat("  ", r.depth)+icon+r.node.name, w-2, "…")
			label += strings.Repeat(" ", w-2-ansi.StringWidth(label))
			lines = append(lines, st.Render(label)+mst.Render(fmt.Sprintf(" %-1s", mark)))
		}
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	sep := make([]string, h)
	for i := range sep {
		sep[i] = look.Dim.Render("│")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(lines, "\n"), strings.Join(sep, "\n"))
}

// MouseTree handles the mouse over the tree, w wide at (x0, y0) on the screen: the wheel scrolls
// it, a click selects a row, opens or closes a folder, and a second click on the selected file
// opens it in the editor. It reports whether it used the event, and what opening started.
func (ft *Tab) MouseTree(h Host, msg tea.MouseMsg, x0, y0, w int) (tea.Cmd, bool) {
	tr := ft.tree
	if msg.X < x0 || msg.X >= x0+w || msg.Y < y0 || tr.finder {
		return nil, false
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		d := 3
		if msg.Button == tea.MouseButtonWheelUp {
			d = -3
		}
		tr.top = max(min(tr.top+d, len(tr.rows)-h.Viewport().Height), 0)
		return nil, true
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft:
		return nil, true
	}
	h.FocusTree() // the tree is part of the main pane
	i := tr.top + msg.Y - y0
	if i >= len(tr.rows) {
		return nil, true
	}
	n := tr.rows[i].node
	switch {
	case n.dir:
		tr.sel = i
		tr.open[n.path] = !tr.open[n.path]
		tr.flatten()
	case i == tr.sel || ft.ed != nil: // click again, or any click while the editor is open
		return ft.Open(h, n.path), true
	default:
		tr.sel = i
		ft.moveTree(h, 0) // shows the file
	}
	return nil, true
}
