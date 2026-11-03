// Package api is what every section of the web UI's server shares: the registry each section adds
// its routes to, and the helpers that read requests and write replies.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"crew/internal/core"
)

// A Module is one section's part of the API: its routes, and the background work crew serve runs for
// it while the web UI is open (firing reminders, polling PRs). Each section's package adds its own in
// init (api.Register), so sections never touch each other's files.
type Module struct {
	Key    string // the section's key (core.SectionOrder): its routes are under /api/<key>/
	Routes func(c *core.Core, mux *http.ServeMux)
	Run    func(ctx context.Context, c *core.Core) // nil when it has nothing to do in the background
}

var modules []Module

// Register adds a section's module; call it from the section's init.
func Register(m Module) { modules = append(modules, m) }

// Modules are the registered modules.
func Modules() []Module { return modules }

// Body reads a request's JSON into v, or answers 400 and reports false.
func Body(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Fail(w, fmt.Errorf("bad request: %v", err), 400)
		return false
	}
	return true
}

// Reply answers with v as JSON.
func Reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Fail answers with err and the status code.
func Fail(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// OrEmpty makes a nil list [] in JSON, which the page can loop over.
func OrEmpty[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}
