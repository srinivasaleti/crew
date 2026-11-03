package web

import (
	"net/http"
	"path/filepath"

	"crew/internal/core"
	"crew/internal/setup"
)

// Home: what's installed and signed in (setup's own checks, no tokens), and turning sections on or off.
func init() { modules = append(modules, module{key: "home", routes: homeRoutes}) }

func homeRoutes(c *core.Core, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/home/setup", func(w http.ResponseWriter, r *http.Request) {
		reply(w, setup.Check(r.Context(), filepath.Join(c.Home(), "notify")).Tools())
	})
	mux.HandleFunc("POST /api/home/sections", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Key string
			On  bool
		}
		if !body(w, r, &m) {
			return
		}
		if err := c.SetSectionOn(m.Key, m.On); err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, struct{}{})
	})
}
