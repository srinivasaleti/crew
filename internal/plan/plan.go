// Package plan reads and updates a task's plan: a Markdown checklist that the crew:plan skill
// (plugin/skills/plan) writes and keeps current, one step per "- [ ] N. …" line.
package plan

import (
	"regexp"
	"strings"
)

// Status is the mark between a step's brackets.
type Status byte

const (
	Todo    Status = ' '
	Doing   Status = '~'
	Done    Status = 'x'
	Blocked Status = '!'
)

// Cycle is the order x steps a status through by hand.
var Cycle = []Status{Todo, Doing, Done, Blocked}

type Step struct {
	Text   string
	Status Status
	Notes  []string // the indented lines under it
}

type Plan struct {
	Title string
	Intro string // Markdown between the title and the first step
	Steps []Step
	After string // Markdown after the steps, e.g. notes and open questions
}

var stepLine = regexp.MustCompile(`^\s*[-*] \[([ ~xX!])\] (.*)$`)

func Parse(text string) Plan {
	var p Plan
	var intro, after []string
	for _, line := range strings.Split(text, "\n") {
		if m := stepLine.FindStringSubmatch(line); m != nil {
			s := Status(m[1][0])
			if s == 'X' {
				s = Done
			}
			p.Steps = append(p.Steps, Step{Text: strings.TrimSpace(m[2]), Status: s})
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case p.Title == "" && len(intro) == 0 && strings.HasPrefix(trimmed, "# "):
			p.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "# "), "Plan:"))
		case len(p.Steps) == 0:
			intro = append(intro, line)
		case trimmed == "" && len(after) == 0:
		case len(after) == 0 && (line[0] == ' ' || line[0] == '\t'):
			last := &p.Steps[len(p.Steps)-1]
			last.Notes = append(last.Notes, trimmed)
		default:
			after = append(after, line)
		}
	}
	// A heading right before the steps ("## Steps") introduces them; it isn't part of the intro.
	for len(intro) > 0 && (strings.TrimSpace(intro[len(intro)-1]) == "" || strings.HasPrefix(strings.TrimSpace(intro[len(intro)-1]), "#")) {
		intro = intro[:len(intro)-1]
	}
	p.Intro = strings.TrimSpace(strings.Join(intro, "\n"))
	p.After = strings.TrimSpace(strings.Join(after, "\n"))
	return p
}

// Approach is the intro's first paragraph: what the plan does and why, in a sentence or two.
// Background is the rest of the intro.
func (p Plan) Approach() (approach, background string) {
	lines := strings.Split(p.Intro, "\n")
	for i, l := range lines {
		if t := strings.TrimSpace(l); t == "" || strings.HasPrefix(t, "#") {
			if i == 0 && t != "" {
				return "", p.Intro // the intro starts with a heading: all background
			}
			return strings.Join(lines[:i], " "), strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return strings.Join(lines, " "), ""
}

// Count is how many steps have status s.
func (p Plan) Count(s Status) int {
	n := 0
	for _, st := range p.Steps {
		if st.Status == s {
			n++
		}
	}
	return n
}

// SetStatus returns text with the i-th step (from 0) marked s; the rest is untouched.
func SetStatus(text string, i int, s Status) string {
	lines := strings.Split(text, "\n")
	n := 0
	for j, line := range lines {
		if m := stepLine.FindStringSubmatchIndex(line); m != nil {
			if n == i {
				lines[j] = line[:m[2]] + string(s) + line[m[3]:]
				break
			}
			n++
		}
	}
	return strings.Join(lines, "\n")
}

// Question is something Claude asks you in the plan's "## Questions" section: numbered (or bulleted)
// items, with the choices as indented bullets, one of them marked "(recommended)".
type Question struct {
	Text        string
	Options     []string
	Recommended int // index into Options, or -1
}

var (
	questionsHead = regexp.MustCompile(`(?i)^##\s+(open\s+)?questions\b`)
	questionItem  = regexp.MustCompile(`^(?:\d+[.)]|[-*])\s+(.*)$`)
	optionItem    = regexp.MustCompile(`^\s+(?:[-*]|[a-z][.)])\s+(.*)$`)
)

// Questions splits the questions out of the text after the steps; rest is that text without them.
func (p Plan) Questions() (qs []Question, rest string) { return QuestionsIn(p.After) }

// QuestionsIn splits a "## Questions" section out of any Markdown (a plan, or Claude's reply in a
// chat); rest is the text without it.
func QuestionsIn(text string) (qs []Question, rest string) {
	var keep []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case questionsHead.MatchString(t):
			in = true
			continue
		case in && strings.HasPrefix(t, "#"):
			in = false
		}
		if !in {
			keep = append(keep, line)
			continue
		}
		switch m, o := questionItem.FindStringSubmatch(line), optionItem.FindStringSubmatch(line); {
		case m != nil:
			qs = append(qs, Question{Text: strings.TrimSpace(m[1]), Recommended: -1})
		case o != nil && len(qs) > 0:
			q := &qs[len(qs)-1]
			if strings.Contains(strings.ToLower(o[1]), "(recommended") {
				q.Recommended = len(q.Options)
			}
			q.Options = append(q.Options, strings.TrimSpace(o[1]))
		case t != "" && len(qs) > 0 && len(qs[len(qs)-1].Options) == 0:
			qs[len(qs)-1].Text += " " + t // the question goes on over more lines
		}
	}
	return qs, strings.TrimSpace(strings.Join(keep, "\n"))
}
