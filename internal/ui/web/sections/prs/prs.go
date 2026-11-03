package prs

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"crew/internal/core"
	"crew/internal/gh"
	"crew/internal/pulls"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// PULL REQUESTS: the list (read on pulls.Every while the web UI is open, and on refresh), a PR read
// with gh, its diff, its chat, and posting a comment or a review. Only the chat sends to an agent.
func init() { api.Register(api.Module{Key: "prs", Routes: prsRoutes, Run: prsRun}) }

// prefsMu keeps two changes to your stars and hidden repos from writing over each other.
var prefsMu sync.Mutex

// prsList is the list as last read, shared by the routes and the clock.
var prsList struct {
	sync.Mutex
	items   []gh.Item
	err     string
	loading bool
	read    time.Time
}

func prsRepos(c *core.Core) repo.Store { return repo.Store{Path: filepath.Join(c.Home(), "repos")} }

// prsLoad reads the list now and tells the page.
func prsLoad(c *core.Core) {
	prsList.Lock()
	if prsList.loading {
		prsList.Unlock()
		return
	}
	prsList.loading = true
	prsList.Unlock()
	c.Publish(core.Event{Kind: "prs"})
	items, err := pulls.List(prsRepos(c))
	prsList.Lock()
	prsList.loading, prsList.read, prsList.err = false, time.Now(), ""
	if err != nil {
		prsList.err = err.Error()
	}
	if items != nil || err == nil { // a failed read keeps the last list
		prsList.items = items
	}
	prsList.Unlock()
	c.Publish(core.Event{Kind: "prs"})
}

// prsRun reads the list on the terminal's clock while PULL REQUESTS is on.
func prsRun(ctx context.Context, c *core.Core) {
	for {
		if c.SectionOn("prs") && gh.Installed() {
			prsLoad(c)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pulls.Every):
		}
	}
}

func prsRoutes(c *core.Core, mux *http.ServeMux) {
	c.RegisterChat("pr", core.ChatKind{Dir: func(t tasks.Task) (string, error) {
		return pulls.ChatDir(t, filepath.Join(c.Home(), "prchats"))
	}}) // Args nil: a PR's chat gets what a task's does, as in the terminal
	mux.HandleFunc("GET /api/prs", func(w http.ResponseWriter, r *http.Request) {
		prefsMu.Lock()
		p := pulls.Prefs(c.Home())
		prefsMu.Unlock()
		prsList.Lock()
		defer prsList.Unlock()
		shown, hidden := pulls.Visible(p, prsList.items)
		api.Reply(w, map[string]any{"items": api.OrEmpty(shown), "hidden": hidden, "prefs": p, "err": prsList.err, "loading": prsList.loading,
			"read": prsList.read, "gh": gh.Installed()})
	})
	// Star a repo, or hide a repo or an org (and undo either); the page reads the list again.
	mux.HandleFunc("POST /api/prs/prefs", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Star, Hide string // a repo to star, or a repo or org to hide
			On         bool
		}
		if !api.Body(w, r, &m) {
			return
		}
		prefsMu.Lock()
		p := pulls.Prefs(c.Home())
		if m.Star != "" {
			p.Star(m.Star, m.On)
		}
		if m.Hide != "" {
			p.Hide(m.Hide, m.On)
		}
		err := p.Save(pulls.PrefsPath(c.Home()))
		prefsMu.Unlock()
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		c.Publish(core.Event{Kind: "prs"})
		api.Reply(w, p)
	})
	mux.HandleFunc("POST /api/prs/refresh", func(w http.ResponseWriter, r *http.Request) {
		go prsLoad(c)
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/prs/view", func(w http.ResponseWriter, r *http.Request) {
		pr, err := gh.View("", r.URL.Query().Get("url"), true)
		if err != nil || pr == nil {
			api.Fail(w, fmt.Errorf("couldn't read the PR: %v", err), 500)
			return
		}
		api.Reply(w, pr)
	})
	mux.HandleFunc("GET /api/prs/diff", func(w http.ResponseWriter, r *http.Request) {
		diff, err := gh.Diff(r.URL.Query().Get("url"))
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"diff": diff})
	})
	// The chat about a PR: found, or made (a task file with pr:, kept out of the task list).
	mux.HandleFunc("POST /api/prs/chat", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ URL string }
		if !api.Body(w, r, &m) {
			return
		}
		prsList.Lock()
		i := slices.IndexFunc(prsList.items, func(it gh.Item) bool { return it.URL == m.URL })
		var it gh.Item
		if i >= 0 {
			it = prsList.items[i]
		}
		prsList.Unlock()
		if i < 0 {
			api.Fail(w, fmt.Errorf("that PR isn't in the list"), 400)
			return
		}
		t, err := c.FindChat(func(t tasks.Task) bool { return t.PR == m.URL }, pulls.ChatTask(it, prsRepos(c)))
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]any{"id": t.ID, "attached": t.Repo != ""})
	})
	// Outward: these post as you, only when you click (the page asks first).
	mux.HandleFunc("POST /api/prs/comment", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ URL, Body string }
		if !api.Body(w, r, &m) {
			return
		}
		if m.Body == "" {
			api.Fail(w, fmt.Errorf("write something"), 400)
			return
		}
		if err := gh.AddComment("", m.URL, m.Body); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
	// A reply on a line-comment thread, as you, and resolving or reopening one.
	mux.HandleFunc("POST /api/prs/reply", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ URL, Thread, Body string }
		if !api.Body(w, r, &m) {
			return
		}
		if strings.TrimSpace(m.Body) == "" || m.Thread == "" {
			api.Fail(w, fmt.Errorf("write something"), 400)
			return
		}
		if err := gh.ReplyThread("", m.URL, m.Thread, m.Body); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("POST /api/prs/resolve", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			URL, Thread string
			Resolved    bool
		}
		if !api.Body(w, r, &m) {
			return
		}
		if err := gh.SetResolved("", m.URL, m.Thread, m.Resolved); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
	// A comment on one line of the diff, as you: a review with just that comment. Side is RIGHT for
	// the new code (an added or unchanged line), LEFT for a removed one.
	mux.HandleFunc("POST /api/prs/line", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			URL, Path, Side, Body string
			Line                  int
		}
		if !api.Body(w, r, &m) {
			return
		}
		switch {
		case strings.TrimSpace(m.Body) == "":
			api.Fail(w, fmt.Errorf("say something"), 400)
			return
		case m.Path == "" || m.Line < 1 || (m.Side != "LEFT" && m.Side != "RIGHT"):
			api.Fail(w, fmt.Errorf("pick a line of the diff"), 400)
			return
		}
		if err := gh.PostReview(m.URL, "COMMENT", "", []gh.ReviewComment{{Path: m.Path, Line: m.Line, Side: m.Side, Body: m.Body}}); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("POST /api/prs/review", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ URL, Event, Body string }
		if !api.Body(w, r, &m) {
			return
		}
		if !slices.Contains([]string{"APPROVE", "COMMENT", "REQUEST_CHANGES"}, m.Event) {
			api.Fail(w, fmt.Errorf("unknown review %q", m.Event), 400)
			return
		}
		if m.Event != "APPROVE" && m.Body == "" {
			api.Fail(w, fmt.Errorf("say why"), 400)
			return
		}
		if err := gh.PostReview(m.URL, m.Event, m.Body, nil); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
}
