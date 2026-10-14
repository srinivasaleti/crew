package app

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/tasks"
	"crew/internal/widgets/files"
)

// The Files tab lives in its own package (files: the tree, the finder and the editor). This is
// crew's side of it: one tab per task, pointed at where the task's files are, the space it gets,
// and what it needs from crew (files.Host).

// filesOf is a task's Files tab, made the first time, pointed at where its files are now.
func (m Model) filesOf(t tasks.Task) *files.Tab {
	ft := m.fileTabs[t.ID]
	if ft == nil {
		ft = files.New()
		m.fileTabs[t.ID] = ft
	}
	ft.Dir, ft.Worktree, ft.Base = m.taskDir(t), t.Worktree, t.Base
	return ft
}

func (m Model) treeWidth() int { return min(32, max(m.chatWidth()/3, 12)) }

// taskDir is where the task's files are: its worktree, or the repo before the task starts.
func (m *Model) taskDir(t tasks.Task) string {
	if t.Worktree != "" {
		return t.Worktree
	}
	dir, _ := m.cfg.Repos.Resolve(t.Repo)
	return dir
}

// editorTab reports whether the selected task's Files tab is open on its editor.
func (m Model) editorTab() bool {
	if m.tab != tabFiles || m.current() == nil {
		return false
	}
	ft := m.fileTabs[m.currentID()]
	return ft != nil && ft.Editing()
}

// editActive reports whether the editor has the keys.
func (m Model) editActive() bool { return m.editFocus && m.editorTab() }

func (m *Model) resizeEditor() {
	if m.editorTab() {
		m.fileTabs[m.currentID()].Resize(m.vp.Width+2, m.vp.Height) // the viewer, a gap and the scroll bar
	}
}

// stopEditor closes a task's editor (the task was deleted or moved).
func (m *Model) stopEditor(id string) {
	if ft := m.fileTabs[id]; ft != nil {
		ft.CloseEditor()
	}
}

// reloadEditor re-reads a task's open files that changed on disk (the agent edits them).
func (m *Model) reloadEditor(id string) {
	if t := m.find(id); t != nil && m.fileTabs[id] != nil {
		m.filesOf(*t).Reload()
	}
}

// openInEditor opens a file of the selected task (relative to its folder, or absolute) in the editor.
func (m *Model) openInEditor(p string) tea.Cmd { return m.filesOf(*m.current()).Open(m, p) }

// mouseEditor hands the mouse to the editor, when it's open.
func (m *Model) mouseEditor(msg tea.MouseMsg) bool {
	if !m.editorTab() {
		return false
	}
	return m.filesOf(*m.current()).MouseEditor(m, msg, m.sideOuter()+1+m.treeWidth()+1, m.contentY())
}

// mouseTree hands the mouse to the Files tab's tree; what opening a file starts is left in m.pending.
func (m *Model) mouseTree(msg tea.MouseMsg) bool {
	if m.tab != tabFiles || m.current() == nil {
		return false
	}
	cmd, ok := m.filesOf(*m.current()).MouseTree(m, msg, m.sideOuter()+1, m.contentY(), m.treeWidth())
	if cmd != nil {
		m.pending = cmd
	}
	return ok
}

// Viewport is the main pane's viewport (files.Host).
func (m *Model) Viewport() *viewport.Model { return &m.vp }

// Notice shows one line of feedback on the help line until the next key (files.Host).
func (m *Model) Notice(s string) { m.notice = s }

// Layout sizes the window's parts again (files.Host).
func (m *Model) Layout() { m.layout() }

// FocusEditor gives the Files tab's editor the keys (files.Host).
func (m *Model) FocusEditor() { m.focusOn(areaEditor, -1) }

// FocusMain gives the main pane the keys (files.Host, prview.Host).
func (m *Model) FocusMain() { m.focusOn(areaMain, -1) }

// LeaveEditor hands the keys back from the editor to the tree (files.Host).
func (m *Model) LeaveEditor() { m.editFocus = false }

// RepoFiles is the selected task's files (files.Host).
func (m *Model) RepoFiles() []string { return m.repoFiles(*m.current()) }

// openAt opens a file of the task in the editor at a line.
func (m *Model) openAt(path string, line int) tea.Cmd {
	if m.current() == nil {
		m.notice = "The code is in a task's worktree · o opens the comment on GitHub"
		return nil
	}
	cmd := m.setTab(tabFiles)
	m.openInEditor(path)
	m.filesOf(*m.current()).GoTo(path, line)
	return cmd
}
