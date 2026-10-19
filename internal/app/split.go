package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/plan"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// Splitting a task into subtasks that agents work on in parallel. /split asks Claude for a
// proposal with the crew:split skill, which writes ~/.crew/splits/<task id>.md (a plan whose steps
// are the subtasks) and stops; crew's hook (plugin/hooks) keeps agents from making tasks
// themselves. The Plan tab shows the proposal, and only you apply it, with ⏎ twice: crew makes a
// task per subtask (parent:, after:), each branching from the parent's branch, and starts the ones
// that wait on nothing. A finished subtask is merged into the parent's branch with m on TASKS or
// /merge, which marks it done and starts the subtasks that waited on it.

func (m Model) splitPath(id string) string { return filepath.Join(m.cfg.Splits, id+".md") }

// subtask is one proposed subtask: a step of the split file ("1. Title") and its indented lines.
type subtask struct {
	n            int
	title, files string
	after        []int
	brief        []string // what its agent is told besides its title
}

// subtasksOf reads the subtasks from a split: "After: 1, 2" and "Files: …" lines are read out, the
// other lines are the brief.
func subtasksOf(p plan.Plan) []subtask {
	var subs []subtask
	for i, s := range p.Steps {
		st := subtask{n: i + 1, title: s.Text}
		if num, title, ok := strings.Cut(s.Text, ". "); ok {
			if n, err := strconv.Atoi(num); err == nil {
				st.n, st.title = n, title
			}
		}
		for _, l := range s.Notes {
			switch k, v, _ := strings.Cut(l, ":"); strings.ToLower(k) {
			case "after":
				for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
					if n, err := strconv.Atoi(f); err == nil {
						st.after = append(st.after, n)
					}
				}
			case "files":
				st.files = strings.TrimSpace(v)
			default:
				st.brief = append(st.brief, l)
			}
		}
		subs = append(subs, st)
	}
	return subs
}

// kept are the subtasks you haven't left out, and how many of them start straight away.
func (pv *planView) kept() (subs []subtask, now int) {
	for _, s := range subtasksOf(pv.split) {
		if pv.skip[s.n] {
			continue
		}
		subs = append(subs, s)
		if len(pv.keptAfter(s)) == 0 {
			now++
		}
	}
	return subs, now
}

// keptAfter are the subtasks s comes after, without the ones left out.
func (pv *planView) keptAfter(s subtask) []int {
	var after []int
	for _, n := range s.after {
		if !pv.skip[n] {
			after = append(after, n)
		}
	}
	return after
}

// renderSplit fills the viewport with the proposed split: Claude's questions, then each subtask
// with what it comes after, its files and its brief.
func (m *Model) renderSplit() {
	t, pv := m.current(), m.planOf(m.currentID())
	w := m.vp.Width
	p := pv.split
	qs, after := pv.questions()
	subs := subtasksOf(p)
	pv.sel = max(min(pv.sel, len(qs)+len(subs)-1), 0)
	kept, now := pv.kept()
	title := strings.TrimSpace(strings.TrimPrefix(p.Title, "Split:"))
	lines := []string{bold.Render("Split proposed: "+title) + dim.Render(fmt.Sprintf("   %d subtasks · %d start at once", len(kept), now))}
	if approach, _ := p.Approach(); approach != "" {
		lines = append(lines, dim.Render(wrap(inlineMD(approach, false), w)))
	}
	lines = append(lines, "")
	if len(qs) > 0 {
		lines = append(append(lines, questionLines(qs, pv.sel, w)...), "", bold.Render("Subtasks"))
	}
	for i, s := range subs {
		box := ready.Render("[x]")
		if pv.skip[s.n] {
			box = dim.Render("[ ]")
		}
		head := fmt.Sprintf("%d. %s", s.n, inlineMD(s.title, pv.skip[s.n]))
		if a := pv.keptAfter(s); len(a) > 0 {
			head += dim.Render("   after " + joinInts(a))
		}
		if len(qs)+i == pv.sel {
			lines = append(lines, focusBar.Width(w).Render("▶ "+ansi.Strip(box+" "+head)))
		} else {
			lines = append(lines, "  "+box+" "+head)
		}
		var notes []string
		if s.files != "" {
			notes = append(notes, "Files: "+s.files)
		}
		for _, n := range append(notes, s.brief...) {
			for _, l := range strings.Split(wrap(inlineMD(n, false), w-8), "\n") {
				lines = append(lines, "      "+dim.Render(l))
			}
		}
	}
	if after != "" {
		lines = append(lines, "", m.md.Render(after, w))
	}
	lines = append(lines, "", dim.Render(tildify(m.splitPath(t.ID))+" · e edits it · ask Claude for changes in the chat"))
	m.vp.SetContent(strings.Join(lines, "\n"))
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// splitInputView is the bottom of the Plan tab while a split is proposed: what applying it does,
// and the question that confirms it.
func (m Model) splitInputView() string {
	pv := m.planOf(m.currentID())
	kept, now := pv.kept()
	if pv.confirm {
		return warn.Render(fmt.Sprintf("  Create %d subtasks and start %d agent%s now?", len(kept), now, plural(now))) + dim.Render("  ⏎ yes · esc no")
	}
	return dim.Render("  space leaves a subtask out · ⏎ applies the split · e edits it · x discards it")
}

// updateSplit handles the Plan tab's keys while a split is proposed; ok is false for the keys the
// plan's handler shares (moving, answering a question).
func (m Model) updateSplit(msg tea.KeyMsg) (_ Model, _ tea.Cmd, ok bool) {
	t, pv := m.current(), m.planOf(m.currentID())
	qs, _ := pv.questions()
	subs := subtasksOf(pv.split)
	i := pv.sel - len(qs) // the selected subtask, or < 0 on a question
	k := msg.String()
	if pv.confirm { // the second ⏎ applies; any other key backs out
		pv.confirm = false
		if k == "enter" {
			return m, m.applySplit(t.ID), true
		}
		m.layout()
		return m, nil, true
	}
	switch {
	case k == "e":
		cmd := m.setTab(tabFiles)
		m.openInEditor(m.splitPath(t.ID))
		return m, cmd, true
	case k == "x":
		os.Remove(m.splitPath(t.ID))
		m.loadPlan(t.ID)
		m.notice = "Split discarded"
	case k == "enter" && i >= 0:
		if kept, _ := pv.kept(); len(kept) == 0 {
			m.notice = "Every subtask is left out · space puts one back"
			return m, nil, true
		}
		pv.confirm = true
		m.layout()
	case k == " " && i >= 0 && i < len(subs):
		if pv.skip == nil {
			pv.skip = map[int]bool{}
		}
		pv.skip[subs[i].n] = !pv.skip[subs[i].n]
	default:
		return m, nil, false
	}
	m.renderPlan()
	return m, nil, true
}

// applySplit makes a task for each subtask you kept, under the task id, then starts the ones that
// wait on nothing. Only ⏎ twice on the Plan tab gets here: agents can propose a split, never apply it.
func (m *Model) applySplit(id string) tea.Cmd {
	t, pv := m.find(id), m.planOf(id)
	switch {
	case t == nil:
		return nil
	case t.Worktree == "":
		m.notice = "Send this task a message first: subtasks branch from its branch, made with its first message"
		return nil
	}
	kept, _ := pv.kept()
	title := t.Title // t points into the tasks, which reload replaces
	ids := map[int]string{}
	var made []tasks.Task
	now := time.Now()
	for i, s := range kept {
		body := strings.Join(s.brief, "\n")
		if s.files != "" {
			body += "\n\nFiles: " + s.files
		}
		body += fmt.Sprintf("\n\nThis is subtask %d of %q, split so agents can work on it in parallel: other agents "+
			"work on the other subtasks in their own worktrees, so change only what this one needs. Commit your work "+
			"on this branch when it's done; crew merges it into the parent's branch.", s.n, t.Title)
		st := tasks.Task{ID: m.cfg.Tasks.NewID(s.title), Title: s.title, Repo: t.Repo, Agent: t.Agent, Model: t.Model,
			Status: tasks.Todo, Parent: t.ID, Body: strings.TrimSpace(body),
			Created: now.Add(time.Duration(i) * time.Second)} // a second apart, so they list in the split's order
		if err := m.cfg.Tasks.Save(st); err != nil { // saved now, so the next NewID sees it
			m.notice = err.Error()
			return nil
		}
		ids[s.n] = st.ID
		made = append(made, st)
	}
	for i, s := range kept {
		for _, n := range pv.keptAfter(s) {
			made[i].After = append(made[i].After, ids[n])
		}
		if len(made[i].After) > 0 {
			m.cfg.Tasks.Save(made[i])
		}
	}
	os.Remove(m.splitPath(id))
	m.add(id, chat.Tool, fmt.Sprintf("split into %d subtasks · TASKS lists them under this task; m there merges a finished one into this branch", len(made)))
	m.reload()
	m.loadPlan(id)
	m.renderPlan()
	m.layout()
	cmd := m.startReady(id)
	m.notice = fmt.Sprintf("Made %d subtasks of %s", len(made), title)
	return cmd
}

// startReady starts the agents of the subtasks of task id that haven't started and wait on nothing.
func (m *Model) startReady(id string) tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range tasks.Subtasks(m.tasks, id) {
		if s.Session == "" && s.Status == tasks.Todo && len(tasks.WaitsOn(s, m.tasks)) == 0 && !m.chat.Conv(s.ID).Busy {
			cmds = append(cmds, m.send(s, chat.Outgoing{Text: "Start on this subtask."}))
		}
	}
	return tea.Batch(cmds...)
}

// mergedMsg is a subtask's branch merged into its parent's, or why it wasn't.
type mergedMsg struct {
	id        string
	conflicts []string
	err       error
}

// mergeSub merges a finished subtask's branch into its parent's worktree (m on TASKS, /merge).
// It's local: nothing is pushed.
func (m *Model) mergeSub(id string) tea.Cmd {
	t := m.find(id)
	if t == nil || t.Parent == "" {
		m.notice = "Only a subtask merges into its parent · /split makes subtasks"
		return nil
	}
	p := m.find(t.Parent)
	switch {
	case p == nil || p.Worktree == "":
		m.notice = "Its parent task's worktree isn't there"
		return nil
	case t.Worktree == "":
		m.notice = t.Title + " hasn't started, so there's nothing to merge"
		return nil
	case m.chat.Conv(id).Busy || m.chat.Conv(p.ID).Busy:
		m.notice = "Wait for the agents of " + t.Title + " and its parent to finish, then merge"
		return nil
	}
	m.notice = "Merging " + t.Title + " into " + p.Title + "…"
	wt, into := t.Worktree, p.Worktree
	return func() tea.Msg {
		if repo.Dirty(wt) {
			return mergedMsg{id: id, err: fmt.Errorf("it has changes that aren't committed · ask its agent to commit them")}
		}
		branch := repo.Branch(wt)
		if branch == "" {
			return mergedMsg{id: id, err: fmt.Errorf("can't tell its branch")}
		}
		conflicts, err := repo.Merge(into, branch)
		return mergedMsg{id: id, conflicts: conflicts, err: err}
	}
}

// onMerged marks a merged subtask done and starts the ones that waited on it; a merge that clashed
// was undone, and the subtask is set to blocked.
func (m *Model) onMerged(msg mergedMsg) tea.Cmd {
	t := m.find(msg.id)
	if t == nil {
		return nil
	}
	p := m.find(t.Parent)
	switch {
	case len(msg.conflicts) > 0:
		m.setStatus(t.ID, tasks.Blocked)
		m.notice = t.Title + " clashes with its parent in " + strings.Join(msg.conflicts, ", ") + " · merge undone, set to blocked"
	case msg.err != nil:
		m.notice = "Not merged: " + msg.err.Error()
	case p != nil:
		m.setStatus(t.ID, tasks.Done)
		m.add(p.ID, chat.Tool, "merged subtask "+t.Title+" into this branch")
		m.notice = "Merged " + t.Title + " into " + p.Title
		m.refresh()
		return m.startReady(p.ID)
	}
	m.refresh()
	return nil
}

// runSplit is /split [goal]: Claude proposes subtasks with crew:split, on the Plan tab, for you to
// apply.
func runSplit(m *Model, t *tasks.Task, goal string) {
	if t.Parent != "" {
		m.add(t.ID, chat.Error, "this is already a subtask · split its parent instead, or /plan it")
		return
	}
	if !m.canUseSkill(t, "split") {
		return
	}
	if goal == "" {
		goal = chat.Brief(*t)
	}
	m.setTab(tabPlan)
	m.pending = m.send(*t, chat.Outgoing{Text: "/crew:split Split file " + m.splitPath(t.ID) + ". Goal " + goal, Raw: true})
}

// runMerge is /merge: a subtask's branch merged into its parent's.
func runMerge(m *Model, t *tasks.Task, _ string) {
	m.add(t.ID, chat.You, "/merge")
	m.pending = m.mergeSub(t.ID)
}
