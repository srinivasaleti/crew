package web

import (
	"fmt"
	"net/http"

	"crew/internal/core"
)

// TASKS: the list, a new task, a task's chat, sending and stopping (all of it core's).
func init() { modules = append(modules, module{key: "tasks", routes: tasksRoutes}) }

func tasksRoutes(c *core.Core, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		list, err := c.List()
		if list == nil && err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, orEmpty(list)) // a broken task file is skipped, not fatal
	})
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var n core.NewTask
		if !body(w, r, &n) {
			return
		}
		t, err := c.Create(n)
		if err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, t)
	})
	mux.HandleFunc("GET /api/tasks/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		msgs, err := c.Chat(r.PathValue("id"))
		if err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, orEmpty(msgs))
	})
	mux.HandleFunc("POST /api/tasks/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Text string }
		if !body(w, r, &m) {
			return
		}
		if m.Text == "" {
			fail(w, fmt.Errorf("say something"), 400)
			return
		}
		if err := c.Send(r.PathValue("id"), m.Text); err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, struct{}{})
	})
	mux.HandleFunc("POST /api/tasks/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"stopped": c.Stop(r.PathValue("id"))})
	})
}
