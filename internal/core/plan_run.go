package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"crew/internal/gh"
	"crew/internal/plan"
	"crew/internal/proc"
	"crew/internal/repo"
	"crew/internal/tasks"
)

// Running a plan, and opening its PRs. Run plan has the agent work through the plan's steps with
// one commit per step, titled "Step N: …" (crew's plan skill). Then you open either one PR for the
// task's branch (OpenPR, as P does), or a stack of PRs, one per step (OpenStack): crew points a
// branch at each step's last commit, pushes it and opens its PR on top of the step before. Both
// run the task's checks first (repo.Checks), and both happen only on your click: agents never push.

// RunPlan has the task's agent work through its plan: every step that isn't done, or with oneStep
// only the next one. stacked says how its PRs will open (a PR per step, or one), kept on the task.
func (c *Core) RunPlan(id string, stacked, oneStep bool) error {
	t, err := c.task(id)
	if err != nil {
		return err
	}
	if err := c.skillOK(t, "plan"); err != nil {
		return err
	}
	p := plan.Parse(c.Plan(id))
	if len(p.Steps) == 0 {
		return errors.New("there's no plan to run · Write plan first")
	}
	if p.Count(plan.Done) == len(p.Steps) {
		return errors.New("every step is done")
	}
	if t.Stacked != stacked {
		t.Stacked = stacked
		if err := c.cfg.Tasks.Save(t); err != nil {
			return err
		}
	}
	return c.Send(id, RunAsk(filepath.Join(c.cfg.Plans, id+".md"), oneStep))
}

// RunAsk is the message that has the plan skill work through the plan at path.
func RunAsk(path string, oneStep bool) string {
	msg := "/crew:plan Plan file " + path + ". Run the plan: start with the first step that isn't done, " +
		"and commit each step as its own commit titled `Step N: <the step's title>`."
	if oneStep {
		return msg + " Do only that one step, then stop and say in a few lines what you did and how you checked it."
	}
	return msg + " Go on through the steps in order until every one is done, or you're blocked or need my answer."
}

var stepCommit = regexp.MustCompile(`^Step (\d+):`)

// StepTips are the last commit of each step on the task's branch since it started (step number from
// 1 → hash), found by their "Step N:" titles. A step's branch in the stack points there, so the
// steps' last commits must come in order: a fix to step 1 made after step 2 can't be split out.
func StepTips(wt, base string) (map[int]string, error) {
	cs, err := repo.Commits(wt, base)
	if err != nil {
		return nil, err
	}
	tips, at := map[int]string{}, map[int]int{}
	for i, c := range cs {
		if m := stepCommit.FindStringSubmatch(c.Subject); m != nil {
			n, _ := strconv.Atoi(m[1])
			tips[n], at[n] = c.Hash, i
		}
	}
	steps := slices.Sorted(maps.Keys(at))
	last, lastN := -1, 0
	for _, n := range steps {
		i := at[n]
		if i < last {
			return nil, fmt.Errorf("step %d has a commit after step %d's, so they can't be split into PRs in order · ask the agent to move it (git rebase) or open one PR", lastN, n)
		}
		last, lastN = i, n
	}
	return tips, nil
}

// StackPR is one step's PR in a stack: the step, its branch, and the PR's URL once it's open.
type StackPR struct {
	Step   int    `json:"step"`
	Branch string `json:"branch"`
	URL    string `json:"url"`
}

func (c *Core) stackPath(id string) string { return filepath.Join(c.cfg.Plans, id+".stack.json") }

// Stack is the task's stacked PRs as crew last opened them.
func (c *Core) Stack(id string) []StackPR {
	var s []StackPR
	b, _ := os.ReadFile(c.stackPath(id))
	json.Unmarshal(b, &s)
	return s
}

// OpenPR runs the task's checks, then pushes its branch and opens its PR (titled from /pr's
// description when there is one), or pushes new commits to its open PR. It works in the background
// and says how it went in the task's chat.
func (c *Core) OpenPR(id string) error {
	t, err := c.pushable(id)
	if err != nil {
		return err
	}
	go c.pushing(t, func(say func(string)) error {
		branch := repo.Branch(t.Worktree)
		if branch == "" {
			return errors.New("the worktree has no branch checked out")
		}
		if pr, _ := gh.View(t.Worktree, branch, false); pr != nil && pr.State == "OPEN" {
			if err := gh.Push(t.Worktree, branch); err != nil {
				return err
			}
			say("pushed " + branch + " to its PR: " + pr.URL)
			return nil
		}
		desc := filepath.Join(c.cfg.Plans, t.ID+".pr.md")
		if _, err := os.Stat(desc); err != nil {
			desc = ""
		}
		url, err := gh.Create(t.Worktree, branch, desc)
		if err != nil {
			return err
		}
		say("opened " + url)
		return nil
	})
	return nil
}

// OpenStack runs the task's checks, then opens a PR per step of its plan that has a commit: each
// step's branch (<branch>-step-N) points at its last commit and is pushed, and its PR goes on top of
// the step before's (the first on the repo's default branch). A PR that's open already just gets
// the push, so this also updates the stack after the agent changes it.
func (c *Core) OpenStack(id string) error {
	t, err := c.pushable(id)
	if err != nil {
		return err
	}
	tips, err := StepTips(t.Worktree, t.Base)
	if err != nil {
		return err
	}
	if len(tips) == 0 {
		return errors.New("no step has a commit yet · Run plan commits each step as \"Step N: …\"")
	}
	p := plan.Parse(c.Plan(id))
	go c.pushing(t, func(say func(string)) error {
		branch := repo.Branch(t.Worktree)
		if branch == "" {
			branch = t.ID
		}
		var stack []StackPR
		prev, prevURL := "", ""
		for n := 1; n <= len(p.Steps); n++ {
			tip, ok := tips[n]
			if !ok {
				continue
			}
			b := fmt.Sprintf("%s-step-%d", branch, n)
			if err := repo.SetBranch(t.Worktree, b, tip); err != nil {
				return err
			}
			if err := gh.PushForce(t.Worktree, b); err != nil {
				return err
			}
			url := ""
			if pr, _ := gh.View(t.Worktree, b, false); pr != nil && pr.State == "OPEN" {
				url = pr.URL
			} else {
				title, body := stepPR(t, p, n, prevURL)
				if url, err = gh.CreateOn(t.Worktree, b, prev, title, body); err != nil {
					return fmt.Errorf("step %d's PR: %w", n, err)
				}
			}
			stack = append(stack, StackPR{n, b, url})
			prev, prevURL = b, url
		}
		data, _ := json.MarshalIndent(stack, "", "  ")
		os.WriteFile(c.stackPath(t.ID), data, 0o644)
		var lines []string
		for _, s := range stack {
			lines = append(lines, fmt.Sprintf("step %d: %s", s.Step, s.URL))
		}
		say(fmt.Sprintf("%d stacked PRs, each on the one before:\n%s", len(stack), strings.Join(lines, "\n")))
		return nil
	})
	return nil
}

// stepPR is a step's PR title and description: the task and step, what the step does, and the PR
// it's stacked on.
func stepPR(t tasks.Task, p plan.Plan, n int, under string) (title, body string) {
	s := p.Steps[n-1]
	text := strings.TrimSpace(strings.TrimPrefix(s.Text, strconv.Itoa(n)+"."))
	title = fmt.Sprintf("%s (%d/%d): %s", t.Title, n, len(p.Steps), text)
	approach, _ := p.Approach()
	body = fmt.Sprintf("Step %d of %d of **%s**.\n\n%s\n", n, len(p.Steps), t.Title, approach)
	if len(s.Notes) > 0 {
		body += "\n" + strings.Join(s.Notes, "\n") + "\n"
	}
	if under != "" {
		body += "\nStacked on " + under + ": review and merge that one first."
	}
	return title, body
}

// pushable is the task, when it can be pushed: it has a worktree, gh is there, its agent isn't
// working, and nothing is being pushed for it already.
func (c *Core) pushable(id string) (tasks.Task, error) {
	t, err := c.task(id)
	switch {
	case err != nil:
		return t, err
	case t.Worktree == "":
		return t, errors.New("nothing to push yet · the worktree is made with the first message")
	case !gh.Installed():
		return t, errors.New("needs the GitHub CLI (gh): make setup")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs[id] != nil {
		return t, errors.New("wait for the agent to finish")
	}
	if c.pushes[id] {
		return t, errors.New("it's being pushed already")
	}
	c.pushes[id] = true
	return t, nil
}

// pushing runs the task's checks and then push, saying each part in its chat, and lets the task be
// pushed again when it's done.
func (c *Core) pushing(t tasks.Task, push func(say func(string)) error) {
	defer func() {
		c.mu.Lock()
		delete(c.pushes, t.ID)
		c.mu.Unlock()
		c.publish(Event{Kind: "task", ID: t.ID})
	}()
	c.publish(Event{Kind: "task", ID: t.ID}) // the page shows it's pushing
	say := func(s string) { c.say(t.ID, Tool, s, false) }
	if cmd := repo.Checks(t.Worktree); cmd != "" {
		say("running the checks first: " + cmd)
		ctx, cancel := context.WithTimeout(c.ctx, 20*time.Minute)
		run := proc.Command(ctx, "sh", "-c", cmd)
		run.Dir = t.Worktree
		out, err := run.CombinedOutput()
		cancel()
		if err != nil {
			c.say(t.ID, Error, "the checks failed, so nothing was pushed · fix them (or ask the agent), then try again\n\n```\n"+tail(string(out), 30)+"\n```", false)
			return
		}
	}
	if err := push(say); err != nil {
		c.say(t.ID, Error, err.Error(), false)
	}
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}
