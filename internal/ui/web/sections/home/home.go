package home

import (
	"net/http"
	"path/filepath"

	"crew/internal/core"
	"crew/internal/setup"
	"crew/internal/ui/web/api"
)

// Home: what's installed and signed in (setup's own checks, no tokens), and turning sections on or off.
func init() { api.Register(api.Module{Key: "home", Routes: homeRoutes}) }

func homeRoutes(c *core.Core, mux *http.ServeMux) {
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
