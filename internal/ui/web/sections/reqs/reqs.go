package reqs

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"crew/internal/core"
	"crew/internal/requests"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// REQUESTS: applications, their environments, folders and requests, sending one, the keys (written
// from the page, never read back), and the locked-down chat. Every rule is crew/internal/requests'.
// Key values never leave this file except into the Keychain or the request they're sent with.
func init() { api.Register(api.Module{Key: "reqs", Routes: reqsRoutes}) }

type reqView struct {
	requests.Request
	ID string
}

type keyView struct {
	Name string
	Set  bool // a value is in the Keychain (it's never read here)
}

type envView struct {
	Name, Base, AuthKind, AuthHeader, AuthKey, AuthLabel string
	Keys                                                 []keyView
	Hosts                                                []string // where its keys may go: the base URL's host, then the extra ones
	Extra                                                []string // the extra hosts, as set
	Vars                                                 map[string]string
	Insecure                                             bool
}

type appView struct {
	Name     string
	Current  string // the environment its requests are sent with
	Envs     []envView
	Folders  []string
	Requests []reqView
}

// reqsExe is the crew binary, which the REQUESTS chat's Claude runs as `crew req`.
func reqsExe() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "crew"
}

func reqsRoutes(c *core.Core, mux *http.ServeMux) {
	dir := filepath.Join(c.Home(), "requests")
	c.RegisterChat("requests", core.ChatKind{
		Dir:  func(tasks.Task) (string, error) { return dir, os.MkdirAll(dir, 0o755) },
		Args: func(tasks.Task) []string { return requests.ChatArgs(reqsExe(), c.Plugin()) },
	})

	mux.HandleFunc("GET /api/reqs", func(w http.ResponseWriter, r *http.Request) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			api.Fail(w, err, 500)
			return
		}
		requests.MoveLoose(dir)
		all, err := requests.Load(dir)
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		apps := []appView{}
		for _, name := range requests.Apps(dir) {
			a := appView{Name: name, Current: requests.Current(dir, name), Envs: []envView{}, Folders: requests.Folders(dir, name), Requests: []reqView{}}
			for _, en := range requests.Envs(dir, name) {
				e, err := requests.LoadEnv(dir, name, en)
				if err != nil {
					continue
				}
				v := envView{Name: e.Name, Base: e.BaseURL(), AuthKind: e.Auth.Kind, AuthHeader: e.Auth.Header, AuthKey: e.Auth.Key,
					AuthLabel: e.AuthLabel(), Hosts: e.AllHosts(), Extra: e.Hosts, Vars: e.Vars, Insecure: e.Insecure, Keys: []keyView{}}
				for _, k := range e.Keys {
					v.Keys = append(v.Keys, keyView{k, requests.HasKey(e, k)})
				}
				a.Envs = append(a.Envs, v)
			}
			for _, q := range all {
				if q.App == name {
					a.Requests = append(a.Requests, reqView{q, q.ID()})
				}
			}
			apps = append(apps, a)
		}
		api.Reply(w, map[string]any{"Apps": apps, "Methods": requests.Methods, "DefaultFolder": requests.DefaultFolder})
	})

	mux.HandleFunc("POST /api/reqs/apps", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Name string }
		if api.Body(w, r, &m) {
			reqsDone(w, requests.AddApp(dir, strings.ToLower(strings.TrimSpace(m.Name))))
		}
	})
	mux.HandleFunc("POST /api/reqs/folders", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Name string }
		if api.Body(w, r, &m) {
			reqsDone(w, requests.AddFolder(dir, m.App, strings.ToLower(strings.TrimSpace(m.Name))))
		}
	})
	mux.HandleFunc("POST /api/reqs/folders/delete", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Name string }
		if api.Body(w, r, &m) {
			reqsDone(w, requests.DeleteFolder(dir, m.App, m.Name))
		}
	})

	// An environment, new or changed. Value, when set, is the auth key's value, for the Keychain.
	mux.HandleFunc("POST /api/reqs/envs", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			App, Name, Base, AuthKind, AuthHeader, AuthKey, Hosts, Value string
			Vars                                                         map[string]string
			Insecure, New                                                bool
		}
		if !api.Body(w, r, &m) {
			return
		}
		first := len(requests.Envs(dir, m.App)) == 0
		e := requests.Env{App: m.App, Name: strings.ToLower(strings.TrimSpace(m.Name)), Vars: map[string]string{}, Insecure: m.Insecure}
		if !m.New {
			old, err := requests.LoadEnv(dir, m.App, e.Name)
			if err != nil {
				api.Fail(w, err, 400)
				return
			}
			e.Keys = old.Keys
		}
		for k, v := range m.Vars {
			if k = strings.TrimSpace(k); requests.ValidName(k) {
				e.Vars[k] = strings.TrimSpace(v)
			}
		}
		e.Vars["baseUrl"] = strings.TrimRight(strings.TrimSpace(m.Base), "/")
		for _, h := range strings.Split(m.Hosts, ",") {
			if h = strings.TrimSpace(h); h != "" {
				e.Hosts = append(e.Hosts, h)
			}
		}
		if m.AuthKind != "" {
			e.Auth = requests.Auth{Kind: m.AuthKind, Header: strings.TrimSpace(m.AuthHeader), Key: strings.TrimSpace(m.AuthKey)}
		}
		if err := requests.CheckEnv(dir, &e, m.New); err != nil {
			api.Fail(w, err, 400)
			return
		}
		if err := requests.SaveEnv(dir, e); err != nil {
			api.Fail(w, err, 500)
			return
		}
		if m.Value != "" && e.Auth.Key != "" {
			if err := requests.SetKey(e, e.Auth.Key, m.Value); err != nil {
				api.Fail(w, err, 500)
				return
			}
		}
		if first {
			requests.SetCurrent(dir, m.App, e.Name) // the first one is the one in use
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("POST /api/reqs/envs/delete", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Name string }
		if !api.Body(w, r, &m) {
			return
		}
		e, err := requests.LoadEnv(dir, m.App, m.Name)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		reqsDone(w, requests.DeleteEnv(dir, e))
	})
	mux.HandleFunc("POST /api/reqs/current", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Env string }
		if api.Body(w, r, &m) {
			if !slices.Contains(requests.Envs(dir, m.App), m.Env) {
				api.Fail(w, fmt.Errorf("%s has no environment %q", m.App, m.Env), 400)
				return
			}
			reqsDone(w, requests.SetCurrent(dir, m.App, m.Env))
		}
	})

	// A key's value goes into the Keychain; nothing here ever reads one back to the page. A name the
	// environment doesn't have yet joins its keys.
	mux.HandleFunc("POST /api/reqs/keys", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Env, Name, Value string }
		if !api.Body(w, r, &m) {
			return
		}
		e, err := requests.LoadEnv(dir, m.App, m.Env)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		if m.Name = strings.TrimSpace(m.Name); !requests.ValidName(m.Name) || m.Value == "" {
			api.Fail(w, fmt.Errorf("a key needs a one-word name and a value"), 400)
			return
		}
		if !slices.Contains(e.Keys, m.Name) {
			e.Keys = append(e.Keys, m.Name)
			if err := requests.SaveEnv(dir, e); err != nil {
				api.Fail(w, err, 500)
				return
			}
		}
		reqsDone(w, requests.SetKey(e, m.Name, m.Value))
	})
	mux.HandleFunc("POST /api/reqs/keys/delete", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ App, Env, Name string }
		if !api.Body(w, r, &m) {
			return
		}
		e, err := requests.LoadEnv(dir, m.App, m.Env)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		requests.DeleteKey(e, m.Name)
		api.Reply(w, struct{}{})
	})

	// Saving a request; Folder, when it differs from the request's, moves it there.
	mux.HandleFunc("POST /api/reqs/save", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Request requests.Request
			Folder  string
		}
		if !api.Body(w, r, &m) {
			return
		}
		q := m.Request
		if q.App == "" || !slices.Contains(requests.Apps(dir), q.App) {
			api.Fail(w, fmt.Errorf("no application %q", q.App), 400)
			return
		}
		if strings.TrimSpace(q.Name) == "" {
			q.Name = q.Method + " " + q.URL
		}
		var saved requests.Request
		var err error
		if q.File != "" && m.Folder != q.Folder {
			saved, err = requests.Move(dir, q, m.Folder)
		} else {
			q.Folder = m.Folder
			saved, err = requests.Save(dir, q)
		}
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, reqView{saved, saved.ID()})
	})
	mux.HandleFunc("POST /api/reqs/delete", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Request requests.Request }
		if api.Body(w, r, &m) {
			reqsDone(w, requests.Delete(dir, m.Request))
		}
	})

	// Sending, as the terminal does: the environment's auth, its keys only to its hosts, and their
	// values blanked out of what comes back. Sending with an environment makes it the one in use.
	mux.HandleFunc("POST /api/reqs/send", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Request requests.Request
			Env     string
		}
		if !api.Body(w, r, &m) {
			return
		}
		q := m.Request
		if m.Env == "" {
			m.Env = requests.Current(dir, q.App)
		}
		env, err := requests.LoadEnv(dir, q.App, m.Env)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		requests.SetCurrent(dir, q.App, env.Name)
		keys, err := requests.KeysFor(q, env)
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		res, err := requests.Send(r.Context(), q, env, keys)
		if err != nil {
			api.Fail(w, fmt.Errorf("%s", requests.Redact(err.Error(), keys, false)), 400)
			return
		}
		names := make([]string, 0, len(res.Headers))
		for k := range res.Headers {
			names = append(names, k)
		}
		sort.Strings(names)
		headers := [][2]string{}
		for _, k := range names {
			headers = append(headers, [2]string{k, requests.Redact(strings.Join(res.Headers[k], ", "), keys, false)})
		}
		api.Reply(w, map[string]any{"Status": res.Status, "Code": res.Code, "Ms": res.Took.Milliseconds(), "Size": requests.Size(len(res.Body)),
			"Headers": headers, "Body": requests.Redact(string(res.Body), keys, false), "URL": env.Resolve(q.URL)})
	})

	// The REQUESTS chat: the one task file of kind requests, made on first use, shared with the terminal.
	mux.HandleFunc("GET /api/reqs/chat", func(w http.ResponseWriter, r *http.Request) {
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == "requests" },
			tasks.Task{Title: "Requests", Kind: "requests", Status: tasks.InProgress, Body: requests.ChatBrief(reqsExe())})
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"ID": t.ID})
	})
}

// reqsDone answers an action with nothing to say: {} or its error.
func reqsDone(w http.ResponseWriter, err error) {
	if err != nil {
		api.Fail(w, err, 400)
		return
	}
	api.Reply(w, struct{}{})
}
