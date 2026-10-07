package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
	"crew/internal/plan"
	"crew/internal/task"
	"crew/internal/usage"
)

// Plan tab: the task's plan, a Markdown checklist in ~/.crew/plans/<task id>.md that Claude writes
// and keeps current with the crew:plan skill (plugin/skills/plan). /plan <goal> asks for one. The
// tab shows the steps and their progress; s has Claude work on a step, n on the next one, x
// changes a step's mark by hand, and e opens the file in the editor.

// planView is one task's plan as last read, and the selected step.
type planView struct {
	plan      plan.Plan
	ok        bool // the file exists
	sel       int  // selected row: Claude's questions first, then the steps
	answering int  // the question (from 1) the message box answers, or 0
}

// questions is what Claude asks in the plan, and the rest of the text after the steps.
func (pv *planView) questions() ([]plan.Question, string) { return pv.plan.Questions() }

func (m Model) planPath(id string) string { return filepath.Join(m.cfg.Plans, id+".md") }

func (m Model) planOf(id string) *planView {
	if m.plans[id] == nil {
		m.plans[id] = &planView{}
	}
	return m.plans[id]
}

// loadPlan re-reads a task's plan file (small; read on the spot).
func (m *Model) loadPlan(id string) {
	pv := m.planOf(id)
	data, err := os.ReadFile(m.planPath(id))
	pv.ok = err == nil
	pv.plan = plan.Parse(string(data))
}

// agentExtra is what every agent run gets on top of its own arguments: crew's plugin (its skills)
// and access to the plans folder, so Claude can write and update the task's plan.
func (m Model) agentExtra() []string {
	var a []string
	if m.pluginOK() {
		a = append(a, "--plugin-dir", m.cfg.Plugin)
	}
	return append(a, "--add-dir", m.cfg.Plans)
}

func (m Model) pluginOK() bool {
	_, err := os.Stat(filepath.Join(m.cfg.Plugin, ".claude-plugin", "plugin.json"))
	return m.cfg.Plugin != "" && err == nil
}

// planNote tells a new session about the task's plan, if it has one.
func (m Model) planNote(t task.Task) string {
	if _, err := os.Stat(m.planPath(t.ID)); err != nil {
		return ""
	}
	return "\n\nThis task has a plan at " + m.planPath(t.ID) + ". Read it and work through it with the crew:plan skill."
}

// planProgress is "4/9" for a task with a plan, else "".
func (m Model) planProgress(id string) string {
	pv := m.plans[id]
	if pv == nil || !pv.ok || len(pv.plan.Steps) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", pv.plan.Count(plan.Done), len(pv.plan.Steps))
}

var stepMarks = map[plan.Status]string{plan.Todo: dim.Render("○"), plan.Doing: running.Render("●"), plan.Done: ready.Render("✓"), plan.Blocked: warn.Render("⊘")}

// renderPlan fills the viewport with the plan: the steps first, then its notes, open questions
// and background as Markdown.
func (m *Model) renderPlan() {
	t, pv := m.current(), m.planOf(m.currentID())
	w := m.vp.Width
	if !pv.ok {
		msg := "No plan yet.\n\n/plan <goal> asks Claude to read the code and write one with the crew:plan skill " +
			"(without the goal, it plans the task's description). Big tasks go better with one. e writes one yourself."
		if !m.pluginOK() {
			msg += "\n\n" + warn.Render("crew's skills weren't found, so Claude won't have crew:plan. Run make install from the crew repo.")
		}
		m.vp.SetContent(dim.Render(wrap(msg, w)))
		return
	}
	p := pv.plan
	qs, after := pv.questions()
	pv.sel = max(min(pv.sel, len(qs)+len(p.Steps)-1), 0)
	approach, background := p.Approach()
	var lines []string
	if p.Title != "" {
		lines = append(lines, bold.Render(p.Title))
	}
	if approach != "" {
		lines = append(lines, dim.Render(wrap(inlineMD(approach, false), w)))
	}
	lines = append(lines, "")
	if len(qs) > 0 { // what Claude needs from you comes first
		lines = append(lines, claude.Render(fmt.Sprintf("Claude asks (%d)", len(qs)))+dim.Render("  ⏎ answers the selected one"))
		for i, q := range qs {
			text := fmt.Sprintf("%d. %s", i+1, inlineMD(q.Text, false))
			for j, l := range strings.Split(wrap(text, w-4), "\n") {
				switch {
				case i == pv.sel && j == 0:
					lines = append(lines, focusBar.Width(w).Render("▶ "+ansi.Strip(l)))
				case i == pv.sel:
					lines = append(lines, focusBar.Width(w).Render("  "+ansi.Strip(l)))
				default:
					lines = append(lines, "  "+l)
				}
			}
			for j, o := range q.Options {
				style := dim
				if j == q.Recommended {
					style = ready
				}
				for k, l := range strings.Split(wrap(ansi.Strip(inlineMD(o, false)), w-8), "\n") {
					lead := fmt.Sprintf("     %c) ", 'a'+j)
					if k > 0 {
						lead = "        "
					}
					lines = append(lines, style.Render(lead+l))
				}
			}
		}
		lines = append(lines, "", bold.Render("Steps"))
	}
	for i, s := range p.Steps {
		mark := stepMarks[s.Status]
		for j, l := range strings.Split(wrap(inlineMD(s.Text, s.Status == plan.Done), w-4), "\n") {
			lead := "  " + mark + " "
			if j > 0 {
				lead = "    " // the title goes on under itself
			}
			if len(qs)+i == pv.sel {
				if j == 0 {
					lead = "▶ " + ansi.Strip(mark) + " "
				}
				lines = append(lines, focusBar.Width(w).Render(lead+ansi.Strip(l)))
			} else {
				lines = append(lines, lead+l)
			}
		}
		for _, n := range s.Notes { // every step shows its notes
			for _, l := range strings.Split(wrap(inlineMD(n, false), w-6), "\n") {
				lines = append(lines, "      "+dim.Render(l))
			}
		}
	}
	if after != "" {
		lines = append(lines, "", m.markdown(after, w))
	}
	if background != "" {
		lines = append(lines, "", bold.Render("Background"), m.markdown(background, w))
	}
	lines = append(lines, "", dim.Render(tildify(m.planPath(t.ID))+" · e edits it"))
	m.vp.SetContent(strings.Join(lines, "\n"))
}

// inlineMD styles a line's `code` and **bold** for the terminal; done steps stay dim.
func inlineMD(s string, done bool) string {
	if done {
		return dim.Render(strings.NewReplacer("`", "", "**", "").Replace(s))
	}
	s = mdCode.ReplaceAllStringFunc(s, func(c string) string { return sel.Render(strings.Trim(c, "`")) })
	return mdBold.ReplaceAllStringFunc(s, func(b string) string { return bold.Render(strings.Trim(b, "*")) })
}

var (
	mdCode = regexp.MustCompile("`[^`]+`")
	mdBold = regexp.MustCompile(`\*\*[^*]+\*\*`)
)

// planSummary is the tab bar's note: progress and a bar.
func (m Model) planSummary() string {
	pv := m.planOf(m.currentID())
	n := len(pv.plan.Steps)
	if !pv.ok || n == 0 {
		return "/plan <goal> writes one"
	}
	done := pv.plan.Count(plan.Done)
	s := fmt.Sprintf("%d of %d done", done, n)
	if d := pv.plan.Count(plan.Doing); d > 0 {
		s += fmt.Sprintf(" · %d doing", d)
	}
	if b := pv.plan.Count(plan.Blocked); b > 0 {
		s += fmt.Sprintf(" · %d blocked", b)
	}
	const w = 16
	return s + "  " + ready.Render(strings.Repeat("█", done*w/n)) + dim.Render(strings.Repeat("░", w-done*w/n))
}

// updatePlan handles keys on the Plan tab; ok is false for keys it leaves to the generic tab keys.
func (m Model) updatePlan(msg tea.KeyMsg) (_ Model, _ tea.Cmd, ok bool) {
	t, pv := m.current(), m.planOf(m.currentID())
	path := m.planPath(t.ID)
	k := msg.String()
	if pv.answering > 0 { // typing an answer: every key goes to the box
		return m.updateAnswer(msg)
	}
	if k == "e" { // write or edit it yourself, plan or not
		cmd := m.setTab(tabFiles)
		if _, err := os.Stat(path); err != nil {
			os.MkdirAll(m.cfg.Plans, 0o755)
			os.WriteFile(path, []byte("# Plan: "+t.Title+"\n\n- [ ] 1. \n"), 0o644)
		}
		m.openInEditor(path)
		return m, cmd, true
	}
	qs, _ := pv.questions()
	if !pv.ok || len(qs)+len(pv.plan.Steps) == 0 {
		return m, nil, false
	}
	step := pv.sel - len(qs) // the selected step, or < 0 on a question
	switch {
	case k == "up":
		pv.sel = max(pv.sel-1, 0)
	case k == "down":
		pv.sel = min(pv.sel+1, len(qs)+len(pv.plan.Steps)-1)
	case k == "enter" && step < 0:
		pv.answering = pv.sel + 1
		m.input.Reset()
		m.input.Placeholder = "a, b, … or your own answer"
		if len(qs[pv.sel].Options) == 0 {
			m.input.Placeholder = "Your answer…"
		}
		m.input.Focus()
		m.layout()
	case k == "n":
		m.notice = "Sent to Claude · the Chat tab shows it working"
		return m, m.send(*t, outgoing{text: "Continue with the next open step of the plan (" + path + ")."}), true
	case step < 0:
		return m, nil, false // the step keys below need a step
	case k == "x":
		data, err := os.ReadFile(path)
		if err != nil {
			m.notice = err.Error()
			break
		}
		cur := pv.plan.Steps[step].Status
		next := plan.Cycle[0]
		for i, s := range plan.Cycle {
			if s == cur {
				next = plan.Cycle[(i+1)%len(plan.Cycle)]
			}
		}
		if err := os.WriteFile(path, []byte(plan.SetStatus(string(data), step, next)), 0o644); err != nil {
			m.notice = err.Error()
		}
		m.loadPlan(t.ID)
	case k == "s":
		s := pv.plan.Steps[step]
		m.notice = "Sent to Claude · the Chat tab shows it working"
		return m, m.send(*t, outgoing{text: fmt.Sprintf("Work on step %d of the plan (%s): %s", step+1, path, s.Text)}), true
	default:
		return m, nil, false
	}
	m.renderPlan()
	return m, nil, true
}

// updateAnswer handles keys while the message box answers one of Claude's questions: ⏎ sends the
// answer (a letter picks that option), esc cancels.
func (m Model) updateAnswer(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	t, pv := m.current(), m.planOf(m.currentID())
	switch msg.String() {
	case "esc":
		pv.answering = 0
		m.input.Reset()
		m.input.Blur()
		m.layout()
		return m, nil, true
	case "enter":
		qs, _ := pv.questions()
		n := pv.answering
		answer := strings.TrimSpace(m.input.Value())
		if answer == "" || n > len(qs) {
			return m, nil, true
		}
		q := qs[n-1]
		if len(answer) == 1 && answer[0] >= 'a' && int(answer[0]-'a') < len(q.Options) {
			answer = q.Options[answer[0]-'a'] // a letter picks that option
		}
		pv.answering = 0
		m.input.Reset()
		m.input.Blur()
		m.layout()
		m.notice = "Answer sent · Claude updates the plan"
		text := fmt.Sprintf("Answer to question %d of the plan (%s): %s\n→ %s\n\nUpdate the plan to match (crew:plan), then stop.", n, m.planPath(t.ID), q.Text, answer)
		return m, m.send(*t, outgoing{text: text}), true
	}
	m.input.Focus()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd, true
}

// planInputView is the bottom of the Plan tab: the answer box while you answer a question.
func (m Model) planInputView() string {
	pv := m.planOf(m.currentID())
	if pv.answering == 0 {
		return dim.Render("  The plan is a checklist Claude keeps current · /plan <goal> in the chat writes one")
	}
	return claude.Render(fmt.Sprintf("  ↳ Answer question %d", pv.answering)) + dim.Render(" · ⏎ send · esc cancel") + "\n" + m.input.View()
}

// runPlan is /plan [goal]: Claude reads the code and writes the task's plan with crew:plan.
func runPlan(m *Model, t *task.Task, goal string) {
	if !m.pluginOK() {
		m.add(t.ID, chat.Error, "crew's skills weren't found, so Claude can't use crew:plan · run make install from the crew repo")
		return
	}
	if goal == "" {
		goal = brief(*t) // plan the task itself
	}
	os.MkdirAll(m.cfg.Plans, 0o755)
	m.setTab(tabPlan)
	m.pending = m.send(*t, outgoing{text: "/crew:plan Plan file " + m.planPath(t.ID) + ". Goal " + goal, raw: true})
}

// resumable reports whether the task's session has had a turn and can be resumed.
func resumable(t task.Task) bool { return t.Session != "" && usage.Exists(t.Session) }
