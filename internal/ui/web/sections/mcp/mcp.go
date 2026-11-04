package mcp

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crew/internal/core"
	m "crew/internal/mcp"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// MCP: the MCP servers Claude Code has (connectors, plugins' and yours), how to sign in to each,
// adding and removing your own, and a chat that uses them, where a call that changes something waits
// for your OK (the gate). Reading the list is `claude mcp list`: no tokens.
func init() { api.Register(api.Module{Key: "mcp", Routes: mcpRoutes}) }

// serverView is a server as the page shows it, with how to sign in when it needs to.
type serverView struct {
	m.Server
	ConnectURL string `json:"connectUrl,omitempty"`
	ConnectHow string `json:"connectHow,omitempty"`
}

// cache keeps the last list a minute: `claude mcp list` checks every server, which takes seconds.
var cache struct {
	sync.Mutex
	list []serverView
	at   time.Time
}

func mcpRoutes(c *core.Core, mux *http.ServeMux) {
	dir := filepath.Join(c.Home(), "mcpchat")
	c.RegisterChat("mcp", core.ChatKind{
		Dir:  func(tasks.Task) (string, error) { return dir, os.MkdirAll(dir, 0o755) },
		Args: func(tasks.Task) []string { return m.ChatArgs(c.Plugin()) }, // MCP servers, no commands or edits; writes ask (the gate)
	})

	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		cache.Lock()
		defer cache.Unlock()
		if r.URL.Query().Get("fresh") != "" || cache.list == nil || time.Since(cache.at) > time.Minute {
			list, err := m.List()
			if err != nil {
				api.Fail(w, err, 500)
				return
			}
			cache.list = nil
			for _, s := range list {
				v := serverView{Server: s}
				if s.Status != "connected" {
					v.ConnectURL, v.ConnectHow = m.Connect(s)
				}
				cache.list = append(cache.list, v)
			}
			cache.at = time.Now()
		}
		api.Reply(w, map[string]any{"servers": api.OrEmpty(cache.list), "checked": cache.at})
	})
	mux.HandleFunc("POST /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Target string }
		if !api.Body(w, r, &in) {
			return
		}
		if err := m.Add(in.Name, in.Target); err != nil {
			api.Fail(w, err, 400)
			return
		}
		cache.Lock()
		cache.list = nil
		cache.Unlock()
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("DELETE /api/mcp/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.Remove(r.PathValue("name")); err != nil {
			api.Fail(w, err, 400)
			return
		}
		cache.Lock()
		cache.list = nil
		cache.Unlock()
		api.Reply(w, struct{}{})
	})
	// chat is the MCP chat, made the first time: the page talks to it over /api/tasks/<id>/….
	mux.HandleFunc("GET /api/mcp/chat", func(w http.ResponseWriter, r *http.Request) {
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == "mcp" },
			tasks.Task{Title: "MCP", Kind: "mcp", Status: tasks.InProgress, Body: m.ChatBrief()})
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"id": t.ID})
	})
}
