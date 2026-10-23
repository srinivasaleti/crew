package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/agent"
	"crew/internal/repo"
	taskssec "crew/internal/sections/tasks"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// TASKS is a section (sections/tasks: its rows, the table, the new-task form). A task it opens is
// crew's: its workspace's buttons, the status menu and delete prompt (which work from inside the
// task too), deleting a task (which reaches its chat, plan and shells), and /branch.

// taskActs are the buttons over an open task, as REQUESTS has over its lists; each presses a key.
func (m Model) taskActs() []table.Act {
	acts := []table.Act{{Label: "Status", Key: "ctrl+s"}, {Label: "Terminal", Key: "ctrl+t"}, {Label: "Find file", Key: "ctrl+p"}}
	if agentRunning(&m) {
		acts = append(acts, table.Act{Label: "Stop", Key: "ctrl+x"})
	}
	return append(acts, table.Act{Label: "Delete", Key: "ctrl+d"}, table.Act{Label: "All tasks", Key: "esc"})
}

// taskBar draws the task's buttons w wide and says where each one is.
func (m Model) taskBar(w int) (string, [][3]int) {
	var at [][3]int
	bar, x := " ", 1
	for i, a := range m.taskActs() {
		b := look.Button(a.Label, keys.CmdName(a.Key))
		if x+ansi.StringWidth(b) > w {
			break
		}
		at = append(at, [3]int{x, x + ansi.StringWidth(b), i})
		bar += b + " "
		x += ansi.StringWidth(b) + 1
	}
	return bar, at
}

// clickTaskBar presses the button at column x of the task's buttons.
func (m *Model) clickTaskBar(x int) {
	_, at := m.taskBar(m.chatWidth())
	for _, a := range at {
		if x < a[0] || x >= a[1] {
			continue
		}
		if k := m.taskActs()[a[2]].Key; k == "esc" {
			m.goUp()
		} else {
			m.pending, _ = m.runKeyCommand(k)
		}
	}
}

// openStatus shows a task's status menu: over its chat (ctrl+s, the header's status), or over
// TASKS (s on a row).
func (m *Model) openStatus(id string) {
	if t := m.find(id); t != nil && !t.Section() {
		m.statusMenu, m.modal = taskssec.NewMenu(*t), statusModal
		m.input.Blur()
	}
}

// updateStatus hands a key to the status menu.
func (m Model) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if pick, closed := m.statusMenu.Key(msg.String()); closed {
		m.applyStatus(pick)
	}
	return m, nil
}

// applyStatus closes the status menu and saves what was picked ("" picks nothing). On TASKS you
// stay on the list, with the bar on the task; on a task you follow it.
func (m *Model) applyStatus(pick string) {
	id := m.statusMenu.Task.ID
	m.closeModal()
	t := m.find(id)
	if t == nil || pick == "" {
		return
	}
	if pick == "archive" && m.chat.Conv(id).Busy {
		m.notice = t.Title + " is busy, wait for its agent to finish"
		return
	}
	m.notice = tasks.SetStatus(t, pick)
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.notice = err.Error()
		return
	}
	onList := m.current() == nil
	m.reload() // re-sorts the list
	switch {
	case onList:
		r := m.selRow()
		m.ovOf(m.rowKey(r)).SelectID(m.groupTable(r), id)
		m.refresh()
	case pick == "archive":
		m.selKey = "g:tasks" // the task left the list; its overview's Archived chip has it
		m.onSelect()
	default:
		m.selectID(id) // follow the task to its new place
	}
}

// openDelete asks before deleting a task (ctrl+d on it, x on its row in TASKS).
func (m *Model) openDelete(id string) {
	t := m.find(id)
	switch {
	case t == nil || t.Section():
		// delete only applies to tasks
	case m.chat.Conv(id).Busy:
		m.notice = t.Title + " is busy, wait for its agent to finish"
	default:
		m.modal, m.delID, m.delLoss = deleteModal, id, ""
		m.input.Blur()
	}
}

// ownWorktree is the worktree crew made for a task (<repo>.worktrees/<id>, on its own branch <id>),
// which deleting the task removes; "" for none, or for a branch that was there before the task
// (the New task form's Branch, /branch), whose worktree may be yours and is kept.
func ownWorktree(t tasks.Task) string {
	if t.Worktree == "" || t.Branch != "" || filepath.Base(t.Worktree) != t.ID {
		return ""
	}
	return t.Worktree
}

// delCheckMsg is what removing a task's worktree would lose, read while the delete prompt asks.
type delCheckMsg struct{ id, loss string }

// checkDelete reads, in the background, what deleting the task the prompt asks about would lose
// with its worktree: its uncommitted changes and unpushed commits. Update calls it while the prompt
// is open (delLoss "" is not read yet).
func (m *Model) checkDelete() tea.Cmd {
	t := m.find(m.delID)
	if m.modal != deleteModal || m.delLoss != "" || t == nil || ownWorktree(*t) == "" {
		return nil
	}
	m.delLoss = "…" // reading
	id, wt := t.ID, t.Worktree
	return func() tea.Msg {
		loss := repo.Unsaved(wt)
		if loss == "" {
			loss = "-" // nothing to lose
		}
		return delCheckMsg{id, loss}
	}
}

// worktreeGoneMsg is a deleted task's worktree removed, or why it couldn't be.
type worktreeGoneMsg struct {
	title, wt string
	err       error
}

// deleteTask deletes a task, its chat, plan and proposed split, and the worktree and branch crew
// made for it (in the background, after the task: git may take a moment on a big checkout).
func (m *Model) deleteTask(id string) tea.Cmd {
	t := m.find(id)
	if t == nil {
		return nil
	}
	title, wt, own, repoRef := t.Title, t.Worktree, ownWorktree(*t), t.Repo
	if err := m.cfg.Tasks.Delete(id); err != nil {
		m.notice = err.Error()
		return nil
	}
	os.Remove(m.planPath(id))
	os.Remove(m.splitPath(id))
	os.Remove(m.prDescPath(id))
	m.chat.Forget(id)
	delete(m.plans, id)
	delete(m.changes, id)
	m.stopShells(id)
	m.tab = tabChat
	m.reload()
	m.notice = "Deleted " + title
	switch {
	case own != "":
		m.notice += " · removing its worktree…"
		repos := m.cfg.Repos
		return func() tea.Msg {
			rp, err := repos.Resolve(repoRef)
			if err == nil {
				err = repo.RemoveWorktree(rp, own, id)
			}
			return worktreeGoneMsg{title, own, err}
		}
	case wt != "":
		m.notice += " (its branch was there before it, so its worktree is kept at " + tildify(wt) + ")"
	}
	return nil
}

// branchMsg is a task moved to an existing branch's worktree (/branch), or why it couldn't be.
type branchMsg struct {
	id, path, branch, done string
	err                    error
}

// runBranch is /branch: with a branch or PR, the task works in that branch's worktree from now on
// (the one that has it checked out, or a new one; git won't check a branch out twice). Claude Code
// keeps a conversation per folder, so a started task begins a fresh one there. "/branch cleanup"
// removes the worktree crew made for the task if it's no longer used and nothing happened in it.
// With nothing, it says where the task works.
func runBranch(m *Model, t *tasks.Task, arg string) {
	m.add(t.ID, chat.You, strings.TrimSpace("/branch "+arg))
	switch {
	case m.chat.Conv(t.ID).Busy:
		m.add(t.ID, chat.Error, "wait for the agent to finish, then /branch")
		return
	case t.Section():
		m.add(t.ID, chat.Error, "/branch is for tasks")
		return
	case arg == "":
		where := "not started yet · its first message makes a new branch"
		switch {
		case t.Worktree != "":
			where = "on " + repo.Branch(t.Worktree) + " in " + tildify(t.Worktree)
		case t.Branch != "":
			where = "not started yet · its first message starts it on " + t.Branch
		}
		m.add(t.ID, chat.Tool, "this task works "+where+" · /branch <branch or #PR> moves it")
		return
	case t.Worktree == "" && arg != "cleanup": // not started: the first message uses it
		t.Branch = arg
		if err := m.cfg.Tasks.Save(*t); err != nil {
			m.add(t.ID, chat.Error, err.Error())
			return
		}
		m.add(t.ID, chat.Tool, "the first message starts this task on "+arg+" (its worktree, or a new one)")
		return
	}
	id, repos, ref, wt := t.ID, m.cfg.Repos, t.Repo, t.Worktree
	if arg == "cleanup" {
		m.add(t.ID, chat.Tool, "checking the worktree crew made for this task…")
		m.pending = func() tea.Msg {
			rp, err := repos.Resolve(ref)
			if err != nil {
				return branchMsg{id: id, err: err}
			}
			made := filepath.Join(rp+".worktrees", id)
			if made == wt {
				return branchMsg{id: id, err: fmt.Errorf("%s is the one this task works in, so it stays", tildify(made))}
			}
			done, err := repo.RemoveIfUnused(rp, made)
			return branchMsg{id: id, done: done, err: err}
		}
		return
	}
	m.add(t.ID, chat.Tool, "finding "+arg+"…")
	m.pending = func() tea.Msg {
		rp, err := repos.Resolve(ref)
		if err != nil {
			return branchMsg{id: id, err: err}
		}
		path, branch, err := repo.UseBranch(rp, arg)
		return branchMsg{id: id, path: path, branch: branch, err: err}
	}
}

// onBranch moves the task to the worktree /branch found, or says what happened.
func (m *Model) onBranch(msg branchMsg) {
	t := m.find(msg.id)
	switch {
	case t == nil:
		return
	case msg.err != nil:
		m.add(t.ID, chat.Error, msg.err.Error())
		return
	case msg.done != "":
		m.add(t.ID, chat.Tool, msg.done)
		return
	}
	old := t.Worktree
	t.Branch, t.Worktree, t.Base = msg.branch, msg.path, "" // the changes view diffs against origin/HEAD
	fresh := chat.HasSession(*t)
	if fresh { // Claude Code can't carry a conversation to another folder
		t.Previous = append(t.Previous, t.Session)
		t.Session = agent.NewSessionID()
	}
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	delete(m.files, t.ID) // @ suggestions, the Files tab and the PR tab read the new worktree
	delete(m.fileTabs, t.ID)
	m.prv.Drop(t.ID)
	m.stopEditor(t.ID)
	line := "now on " + msg.branch + " in " + tildify(msg.path)
	if fresh {
		line += " · Claude starts a fresh conversation there (it can't follow one across folders); it gets the task again with your next message"
	}
	if len(m.shells[t.ID]) > 0 {
		line += " · open shells are still in " + tildify(old)
	}
	if old != "" && filepath.Base(old) == t.ID {
		line += " · /branch cleanup removes " + tildify(old) + " if nothing happened in it"
	}
	m.add(t.ID, chat.Tool, line)
}
