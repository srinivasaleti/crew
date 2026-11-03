package tasks

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"crew/internal/core"
	"crew/internal/gh"
	"crew/internal/repo"
	task "crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// TASKS: the list, a new task, a task's chat, sending and stopping (all of it core's).
func init() { api.Register(api.Module{Key: "tasks", Routes: tasksRoutes, Run: pollTaskPRs}) }

// taskPR is what the list shows of a task's PR: its number, state, checks and review.
type taskPR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`            // OPEN, MERGED, CLOSED
	Checks string `json:"checks,omitempty"` // pass, fail, pending; "" with none
	Review string `json:"review,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	Draft  bool   `json:"draft,omitempty"`
}

// taskPRs are the tasks' PRs as last read, by task id.
var taskPRs = struct {
	sync.Mutex
	m map[string]*taskPR
}{m: map[string]*taskPR{}}

// pollTaskPRs reads the PR of every live task with a worktree, now and every two minutes (the
// terminal's clock for its sidebar marks), and tells the page when one changed. It reads GitHub
// with gh: no tokens.
func pollTaskPRs(ctx context.Context, c *core.Core) {
	for {
		if c.SectionOn("tasks") && gh.Installed() {
			list, _ := c.List(false)
			for _, v := range list {
				if v.Status == task.Done || !v.Started {
					continue
				}
				t, err := c.Task(v.ID)
				if err != nil {
					continue
				}
				pr, err := gh.View(t.Worktree, repo.Branch(t.Worktree), false)
				if err != nil {
					continue // offline, or gh signed out: keep what we had
				}
				var now *taskPR
				if pr != nil {
					now = &taskPR{Number: pr.Number, URL: pr.URL, State: pr.State, Review: pr.ReviewDecision, Draft: pr.Draft}
					for _, ch := range pr.Checks {
						if now.Checks != gh.Fail && (ch.State == gh.Fail || ch.State == gh.Pending || now.Checks == "") {
							now.Checks = ch.State
						}
					}
				}
				taskPRs.Lock()
				old := taskPRs.m[v.ID]
				taskPRs.m[v.ID] = now
				taskPRs.Unlock()
				if (old == nil) != (now == nil) || old != nil && *old != *now {
					c.Publish(core.Event{Kind: "task", ID: v.ID})
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
		}
	}
}

// listed is a task as the page lists it: core's view, and its PR.
type listed struct {
	core.TaskView
	PR *taskPR `json:"pr,omitempty"`
}

func tasksRoutes(c *core.Core, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		list, err := c.List(r.URL.Query().Get("archived") == "1")
		if list == nil && err != nil {
			api.Fail(w, err, 500)
			return
		}
		out := []listed{}
		taskPRs.Lock()
		for _, v := range list {
			out = append(out, listed{v, taskPRs.m[v.ID]})
		}
		taskPRs.Unlock()
		api.Reply(w, out) // a broken task file is skipped, not fatal
	})
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var n core.NewTask
		if !api.Body(w, r, &n) {
			return
		}
		t, err := c.Create(n)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, t)
	})
	mux.HandleFunc("GET /api/tasks/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		msgs, err := c.Chat(r.PathValue("id"))
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, api.OrEmpty(msgs))
	})
	mux.HandleFunc("POST /api/tasks/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Text string }
		if !api.Body(w, r, &m) {
			return
		}
		if m.Text == "" {
			api.Fail(w, fmt.Errorf("say something"), 400)
			return
		}
		if err := c.Send(r.PathValue("id"), m.Text); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/tasks/statuses", func(w http.ResponseWriter, r *http.Request) {
		type st struct {
			Key   string `json:"key"`
			Label string `json:"label"`
			Icon  string `json:"icon"`
		}
		var out []st
		for _, k := range core.Statuses {
			out = append(out, st{k, task.Labels[k], task.Icons[k]})
		}
		api.Reply(w, out)
	})
	mux.HandleFunc("POST /api/tasks/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Status string }
		if !api.Body(w, r, &m) {
			return
		}
		if err := c.SetStatus(r.PathValue("id"), m.Status); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/tasks/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, map[string]string{"plan": c.Plan(r.PathValue("id"))})
	})
	// The summary, and having Claude write it (crew's summary skill, in the task's chat).
	mux.HandleFunc("GET /api/tasks/{id}/summary", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, map[string]string{"summary": c.Summary(r.PathValue("id"))})
	})
	mux.HandleFunc("POST /api/tasks/{id}/summary", func(w http.ResponseWriter, r *http.Request) {
		if err := c.SummaryTurn(r.PathValue("id")); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, struct{}{})
	})
	// Write or change the plan: Claude does it in the task's chat with crew's plan skill.
	mux.HandleFunc("POST /api/tasks/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Ask string }
		if !api.Body(w, r, &m) {
			return
		}
		if err := c.PlanTurn(r.PathValue("id"), m.Ask); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/tasks/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		diff, summary, err := c.Changes(r.PathValue("id"))
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"diff": diff, "summary": summary})
	})
	mux.HandleFunc("GET /api/tasks/repos", func(w http.ResponseWriter, r *http.Request) { api.Reply(w, api.OrEmpty(c.Repos())) })
	mux.HandleFunc("POST /api/tasks/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Title, Body string }
		if !api.Body(w, r, &m) {
			return
		}
		if err := c.Edit(r.PathValue("id"), m.Title, m.Body); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, struct{}{})
	})
	// Deleting asks first: loss is what the worktree would lose, so the page can say it.
	mux.HandleFunc("GET /api/tasks/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, map[string]string{"loss": c.DeleteLoss(r.PathValue("id"))})
	})
	mux.HandleFunc("POST /api/tasks/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		note, err := c.Delete(r.PathValue("id"))
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, map[string]string{"note": note})
	})
	mux.HandleFunc("POST /api/tasks/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, map[string]bool{"stopped": c.Stop(r.PathValue("id"))})
	})
}
