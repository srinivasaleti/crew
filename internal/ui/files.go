package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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

	"crew/internal/task"
)

// Files tab: a tree of the task's files on the left, the selected file (highlighted, read-only)
// on the right. enter or e opens it in $EDITOR; / or ctrl+p finds a file by typing part of its path.

const maxViewBytes = 300 << 10 // bigger files are left to the editor

type editedMsg struct{ id string }

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
			ft.rows = append(ft.rows, fileRow{k, depth})
			if k.dir && ft.open[k.path] {
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
func (m *Model) taskDir(t task.Task) string {
	if t.Worktree != "" {
		return t.Worktree
	}
	dir, _ := m.cfg.Repos.Resolve(t.Repo)
	return dir
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
	ft.flatten()
	ft.selectPath(prev)
	if ft.file != "" {
		m.showFile()
		return
	}
	msg := "Pick a file in the tree. / or ctrl+p finds one by name."
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
	case "right", " ":
		if n != nil && n.dir {
			if ft.open[n.path] && msg.String() == "right" {
				m.moveTree(1)
			} else {
				ft.open[n.path] = !ft.open[n.path] || msg.String() == "right"
				ft.flatten()
			}
		}
	case "left":
		if n == nil {
			break
		}
		if n.dir && ft.open[n.path] {
			ft.open[n.path] = false
			ft.flatten()
			break
		}
		for i := ft.sel - 1; i >= 0; i-- { // up to the folder that holds it
			if ft.rows[i].depth < ft.rows[ft.sel].depth {
				ft.sel = i
				ft.keepVisible(m.vp.Height)
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
			if editorArgv() != nil {
				return m, m.openInEditor(ft.file)
			}
			m.showFile()
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

// editFile suspends crew and opens the selected file in $EDITOR (nvim, else vi).
func (m *Model) editFile() tea.Cmd {
	t, ft := m.current(), m.tree()
	p := ft.target()
	if p == "" {
		return nil
	}
	argv := strings.Fields(os.Getenv("EDITOR"))
	if len(argv) == 0 {
		argv = []string{"nvim"}
		if _, err := exec.LookPath("nvim"); err != nil {
			argv = []string{"vi"}
		}
	}
	dir, id := m.taskDir(*t), t.ID
	c := exec.Command(argv[0], append(argv[1:], filepath.Join(dir, p))...)
	c.Dir = dir
	// crew asks the terminal for modifyOtherKeys (see main.go); editors get plain keys meanwhile.
	return tea.Sequence(
		func() tea.Msg { fmt.Print("\x1b[>4m"); return nil },
		tea.ExecProcess(c, func(error) tea.Msg { fmt.Print("\x1b[>4;1m"); return editedMsg{id} }),
	)
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
			icon, st := "  ", plain
			if r.node.dir {
				icon, st = "▸ ", folder
				if ft.open[r.node.path] {
					icon = "▾ "
				}
			}
			if i == ft.sel {
				st = cur
			}
			lines = append(lines, cell(strings.Repeat("  ", r.depth)+icon+r.node.name, st))
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
