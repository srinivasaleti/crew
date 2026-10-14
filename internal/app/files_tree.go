package app

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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/edit"
	"crew/internal/repo"
	"crew/internal/tasks"
)

// Files tab: a tree of the task's files on the left, the selected file (highlighted, read-only)
// on the right. enter or e opens it in the editor (editor.go); / or ctrl+p finds a file by typing
// part of its path; ctrl+e opens it in VS Code.

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

func (m Model) tree() *fileTree {
	id := m.currentID()
	if m.trees[id] == nil {
		m.trees[id] = &fileTree{open: map[string]bool{}}
	}
	return m.trees[id]
}

func (m Model) treeWidth() int { return min(32, max(m.chatWidth()/3, 12)) }

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

// taskDir is where the task's files are: its worktree, or the repo before the task starts.
func (m *Model) taskDir(t tasks.Task) string {
	if t.Worktree != "" {
		return t.Worktree
	}
	dir, _ := m.cfg.Repos.Resolve(t.Repo)
	return dir
}

// refreshChanged re-reads which files the task changed, for the tree's colours.
func (m *Model) refreshChanged() {
	t, ft := m.current(), m.tree()
	ft.changed, ft.dirs = nil, map[string]bool{}
	if t.Worktree != "" {
		ft.changed = repo.Changed(t.Worktree, t.Base)
	}
	for p := range ft.changed {
		for i := range p {
			if p[i] == '/' {
				ft.dirs[p[:i]] = true
			}
		}
	}
	ft.flatten()
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

// loadFiles (re)builds the tree from git and re-reads the file on the right.
func (m *Model) loadFiles() {
	t := m.current()
	ft := m.tree()
	prev := ""
	if n := ft.selected(); n != nil {
		prev = n.path
	}
	ft.root = buildTree(m.repoFiles(*t))
	m.refreshChanged()
	ft.selectPath(prev)
	if ft.file != "" {
		m.showFile()
		return
	}
	msg := "Pick a file in the tree. / or ⌘P finds one by name."
	if len(ft.rows) == 0 {
		msg = "No files yet. Send a message in the Chat tab to start the task."
	}
	m.vp.SetContent(dim.Render(msg))
}

func (m *Model) showFile() {
	t, ft := m.current(), m.tree()
	m.vp.SetContent(clip(renderFile(filepath.Join(m.taskDir(*t), ft.file)), m.vp.Width))
	m.vp.GotoTop()
}

// renderFile returns the file with line numbers and syntax colours.
func renderFile(p string) string {
	data, err := os.ReadFile(p)
	switch {
	case err != nil:
		return warn.Render(err.Error())
	case len(data) > maxViewBytes:
		return dim.Render(fmt.Sprintf("%d KB is too large to show here. Press enter to open it in your editor.", len(data)>>10))
	case bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0:
		return dim.Render("Binary file.")
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
		lines[i] = dim.Render(fmt.Sprintf("%4d ", i+1)) + l
	}
	return strings.Join(lines, "\n")
}

// moveTree moves the selection by d rows; landing on a file shows it.
func (m *Model) moveTree(d int) {
	ft := m.tree()
	ft.sel = max(min(ft.sel+d, len(ft.rows)-1), 0)
	ft.keepVisible(m.vp.Height)
	if n := ft.selected(); n != nil && !n.dir && n.path != ft.file && m.editors[m.currentID()] == nil { // nvim covers the preview
		ft.file = n.path
		m.showFile()
	}
}

func (m *Model) openFinder() {
	ft := m.tree()
	ft.finder, ft.query, ft.idx = true, "", 0
	ft.matches = fuzzy(m.repoFiles(*m.current()), "")
}

// updateFiles handles keys on the Files tab; ok is false for keys it leaves to the generic tab keys.
func (m Model) updateFiles(msg tea.KeyMsg) (_ Model, _ tea.Cmd, ok bool) {
	ft := m.tree()
	if ft.finder {
		m, cmd := m.updateFinder(msg)
		return m, cmd, true
	}
	n := ft.selected()
	switch msg.String() {
	case "up":
		m.moveTree(-1)
	case "down":
		m.moveTree(1)
	case "home":
		m.moveTree(-len(ft.rows))
	case "end":
		m.moveTree(len(ft.rows))
	case " ":
		if n != nil && n.dir {
			ft.open[n.path] = !ft.open[n.path]
			ft.flatten()
		}
	case "right": // unfold a folder
		if n != nil && n.dir && !ft.open[n.path] {
			ft.open[n.path] = true
			ft.flatten()
		}
	case "left": // fold a folder, or go up to the folder it's in
		if n != nil && n.dir && ft.open[n.path] {
			ft.open[n.path] = false
			ft.flatten()
			break
		}
		for i := ft.sel - 1; i >= 0; i-- {
			if ft.rows[i].depth < ft.rows[ft.sel].depth {
				m.moveTree(i - ft.sel)
				break
			}
		}
	case "enter", "e":
		if n != nil && n.dir && msg.String() == "enter" {
			ft.open[n.path] = !ft.open[n.path]
			ft.flatten()
			break
		}
		if p := ft.target(); p != "" {
			return m, m.openInEditor(p), true
		}
	case "/":
		m.openFinder()
	case "c":
		ft.onlyChanged = !ft.onlyChanged
		ft.flatten()
		ft.keepVisible(m.vp.Height)
	case "ctrl+e":
		if p := ft.target(); p != "" {
			m.openVSCode(p, edit.Pos{})
		}
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

func (m Model) updateFinder(msg tea.KeyMsg) (Model, tea.Cmd) {
	ft := m.tree()
	switch msg.Type {
	case tea.KeyEsc:
		ft.finder = false
	case tea.KeyUp:
		ft.idx = max(ft.idx-1, 0)
	case tea.KeyDown:
		ft.idx = min(ft.idx+1, len(ft.matches)-1)
	case tea.KeyEnter:
		if ft.idx < len(ft.matches) {
			ft.finder = false
			ft.file = ft.matches[ft.idx]
			ft.reveal(ft.file)
			ft.keepVisible(m.vp.Height)
			return m, m.openInEditor(ft.file)
		}
	case tea.KeyBackspace:
		if r := []rune(ft.query); len(r) > 0 {
			ft.query = string(r[:len(r)-1])
		}
		fallthrough
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type != tea.KeyBackspace && msg.Type != tea.KeySpace {
			ft.query += string(msg.Runes)
		}
		ft.matches, ft.idx = fuzzy(m.repoFiles(*m.current()), ft.query), 0
	}
	return m, nil
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

// treeView is the left column of the Files tab: the tree, or the finder, plus a divider.
func (m Model) treeView() string {
	ft, w, h := m.tree(), m.treeWidth(), m.vp.Height
	cell := func(s string, st lipgloss.Style) string {
		s = ansi.Truncate(s, w, "…")
		return st.Render(s + strings.Repeat(" ", w-ansi.StringWidth(s)))
	}
	plain, folder, cur := lipgloss.NewStyle(), lipgloss.NewStyle().Foreground(lipgloss.Color("12")), lipgloss.NewStyle().Reverse(true)
	var lines []string
	if ft.finder {
		lines = append(lines, cell("/ "+ft.query+"▏", sel))
		if len(ft.matches) == 0 {
			lines = append(lines, cell("no match", dim))
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
		sep[i] = dim.Render("│")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(lines, "\n"), strings.Join(sep, "\n"))
}

// mouseTree handles the mouse over the tree: the wheel scrolls it, a click selects a row, opens or
// closes a folder, and a second click on the selected file opens it in the editor. The result of
// opening is left in m.pending.
func (m *Model) mouseTree(msg tea.MouseMsg) bool {
	if m.tab != tabFiles || m.current() == nil {
		return false
	}
	ft, x0 := m.tree(), m.sideOuter()+1
	if msg.X < x0 || msg.X >= x0+m.treeWidth() || msg.Y < m.contentY() || ft.finder {
		return false
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		d := 3
		if msg.Button == tea.MouseButtonWheelUp {
			d = -3
		}
		ft.top = max(min(ft.top+d, len(ft.rows)-m.vp.Height), 0)
		return true
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft:
		return true
	}
	m.focusOn(areaMain, -1) // the tree is part of the main pane
	i := ft.top + msg.Y - m.contentY()
	if i >= len(ft.rows) {
		return true
	}
	n := ft.rows[i].node
	switch {
	case n.dir:
		ft.sel = i
		ft.open[n.path] = !ft.open[n.path]
		ft.flatten()
	case i == ft.sel || m.editors[m.currentID()] != nil: // click again, or any click while the editor is open
		m.pending = m.openInEditor(n.path)
	default:
		ft.sel = i
		m.moveTree(0) // shows the file
	}
	return true
}
