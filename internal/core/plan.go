package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crew/internal/plan"
)

// PlanView is a task's plan read into its parts, for a UI to lay out: what it does, the steps with
// their marks and notes, Claude's questions with their choices, and the rest (notes).
type PlanView struct {
	Title      string         `json:"title"`
	Approach   string         `json:"approach"`
	Background string         `json:"background"`
	Steps      []PlanStep     `json:"steps"`
	Questions  []PlanQuestion `json:"questions"`
	Rest       string         `json:"rest"` // Markdown after the steps, without the questions
	Done       int            `json:"done"`
}

// PlanStep is one step: its line, its status (todo, doing, done, blocked) and the lines under it.
type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Notes  string `json:"notes"`
}

// PlanQuestion is one of the plan's questions; Recommended is an index into Options, or -1.
type PlanQuestion struct {
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
}

var statusNames = map[plan.Status]string{plan.Todo: "todo", plan.Doing: "doing", plan.Done: "done", plan.Blocked: "blocked"}

// ReadPlan is the task's plan in parts; nil when it has none.
func (c *Core) ReadPlan(id string) *PlanView {
	text := c.Plan(id)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	p := plan.Parse(text)
	v := &PlanView{Title: p.Title, Done: p.Count(plan.Done), Steps: []PlanStep{}, Questions: []PlanQuestion{}}
	v.Approach, v.Background = p.Approach()
	for _, s := range p.Steps {
		v.Steps = append(v.Steps, PlanStep{Text: s.Text, Status: statusNames[s.Status], Notes: strings.Join(s.Notes, "\n")})
	}
	qs, rest := p.Questions()
	for _, q := range qs {
		v.Questions = append(v.Questions, PlanQuestion{Text: q.Text, Options: append([]string{}, q.Options...), Recommended: q.Recommended})
	}
	v.Rest = rest
	return v
}

// SetPlanStep marks step i (from 0) of the task's plan by hand: todo, doing, done or blocked.
func (c *Core) SetPlanStep(id string, i int, status string) error {
	var s plan.Status
	for k, name := range statusNames {
		if name == status {
			s = k
		}
	}
	if s == 0 {
		return errors.New("a step is todo, doing, done or blocked")
	}
	path := filepath.Join(c.cfg.Plans, id+".md")
	text := c.Plan(id)
	if i < 0 || i >= len(plan.Parse(text).Steps) {
		return errors.New("no such step")
	}
	if err := os.WriteFile(path, []byte(plan.SetStatus(text, i, s)), 0o644); err != nil {
		return err
	}
	c.publish(Event{Kind: "task", ID: id})
	return nil
}

// AnswerPlan sends your answers to the plan's questions (answers[i] for question i; "" skips it) to
// Claude, which updates the plan to match.
func (c *Core) AnswerPlan(id string, answers []string) error {
	v := c.ReadPlan(id)
	if v == nil {
		return errors.New("there's no plan")
	}
	var b strings.Builder
	for i, a := range answers {
		if a = strings.TrimSpace(a); a != "" && i < len(v.Questions) {
			fmt.Fprintf(&b, "%d. %s\n→ %s\n", i+1, v.Questions[i].Text, a)
		}
	}
	if b.Len() == 0 {
		return errors.New("answer at least one question")
	}
	return c.PlanTurn(id, "My answers to the plan's questions:\n"+b.String()+"Update the plan to match.")
}
