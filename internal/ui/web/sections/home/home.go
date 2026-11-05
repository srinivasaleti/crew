package home

import (
	"net/http"
	"os"
	"path/filepath"

	"crew/internal/core"
	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// Home: a chat about anything in crew (core.HomeBrief, read-only), and, on the Settings page, what's
// installed and signed in (setup's own checks, no tokens) and turning sections on or off.
func init() { api.Register(api.Module{Key: "home", Routes: homeRoutes}) }

func homeRoutes(c *core.Core, mux *http.ServeMux) {
	dir := filepath.Join(c.Home(), "homechat")
	c.RegisterChat("home", core.ChatKind{
		Dir:  func(tasks.Task) (string, error) { return dir, os.MkdirAll(dir, 0o755) },
		Args: func(tasks.Task) []string { return core.HomeArgs(c.Home(), c.Plugin()) }, // reads only; MCP writes ask (the gate)
	})
	// chat is the home chat, made the first time: the page talks to it over /api/tasks/<id>/….
	mux.HandleFunc("GET /api/home/chat", func(w http.ResponseWriter, r *http.Request) {
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == "home" },
			tasks.Task{Title: "Home", Kind: "home", Status: tasks.InProgress, Body: "This is crew's home chat."}) // the brief is its system prompt (core.HomeArgs)
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"id": t.ID})
	})
	mux.HandleFunc("GET /api/home/setup", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, setup.Check(r.Context(), filepath.Join(c.Home(), "notify")).Tools())
	})
	mux.HandleFunc("POST /api/home/sections", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Key string
			On  bool
		}
		if !api.Body(w, r, &m) {
			return
		}
		if err := c.SetSectionOn(m.Key, m.On); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
}
