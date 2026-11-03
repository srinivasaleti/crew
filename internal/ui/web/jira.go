package web

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"crew/internal/core"
	"crew/internal/jira"
	"crew/internal/setup"
	"crew/internal/tasks"
)

// JIRA: your open issues read with acli on CREW_JIRA_EVERY, an issue's card, the changes you make to
// one (each only on its own click: a comment, an edit, a new status), Claude's draft of a comment,
// working on an issue in a task, and the chat about it.
func init() { modules = append(modules, module{key: "jira", routes: jiraRoutes, run: jiraRun}) }

// jiraList is the issues as last read, which the page and the card's statuses share.
var jiraList struct {
	sync.Mutex
	issues  []jira.Issue
	spaces  map[string]string // the spaces' names by key, read once
	site    string            // the site acli is signed in to, read once
	err     string            // why the last read failed
	ok      bool              // the last read worked: acli is signed in
	loading bool
	read    bool // a read has finished, so an empty list means none
}

// jiraNow asks the background loop to read the list now (refresh, a change).
var jiraNow = make(chan struct{}, 1)

func jiraWake() {
	select {
	case jiraNow <- struct{}{}:
	default:
	}
}

// jiraLoad reads your issues (and the spaces' names and the site the first time), and tells the
// page when something changed.
func jiraLoad(ctx context.Context, c *core.Core) {
	if _, err := exec.LookPath("acli"); err != nil {
		return
	}
	jiraList.Lock()
	jiraList.loading = true
	first := jiraList.spaces == nil
	jiraList.Unlock()
	issues, err := jira.Mine(ctx)
	var spaces map[string]string
	site := ""
	if first && err == nil {
		spaces, _ = jira.Spaces(ctx) // without them, headings show the key
		site = jira.Site(ctx)
	}
	jiraList.Lock()
	jiraList.loading, jiraList.read, jiraList.ok, jiraList.err = false, true, err == nil, ""
	if err != nil {
		jiraList.err = err.Error()
	} else {
		if spaces != nil {
			jiraList.spaces, jiraList.site = spaces, site
		}
		for i := range issues {
			if jiraList.site != "" { // the browser's address for it, not Jira's internal one
				issues[i].URL = jira.BrowseURL(jiraList.site, issues[i].Key)
			}
		}
		jiraList.issues = issues
	}
	jiraList.Unlock()
	c.Publish(core.Event{Kind: "jira"})
}

// jiraRun reads the list now, then on JIRA's clock or when asked, while the section is on.
func jiraRun(ctx context.Context, c *core.Core) {
	c.RegisterChat(jira.ChatKind, core.ChatKind{Dir: func(t tasks.Task) (string, error) {
		dir := filepath.Join(c.Home(), "jirachats", t.ID) // where the terminal runs it too
		return dir, os.MkdirAll(dir, 0o755)
	}})
	for {
		if c.SectionOn("jira") {
			jiraLoad(ctx, c)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(jira.Every()):
		case <-jiraNow:
		}
	}
}

// jiraFind is one of your listed issues, by key.
func jiraFind(key string) (jira.Issue, bool) {
	jiraList.Lock()
	defer jiraList.Unlock()
	for _, i := range jiraList.issues {
		if i.Key == key {
			return i, true
		}
	}
	return jira.Issue{}, false
}

// jiraIssue reads an issue now, keeping what only the list knows (its space and link).
func jiraIssue(ctx context.Context, key string) (jira.Issue, jira.Detail, []jira.Comment, error) {
	i, d, comments, err := jira.Read(ctx, key)
	project, _, _ := strings.Cut(key, "-")
	i.Project = project
	if l, ok := jiraFind(key); ok {
		i.URL = l.URL
	} else {
		jiraList.Lock()
		if jiraList.site != "" {
			i.URL = jira.BrowseURL(jiraList.site, key)
		}
		jiraList.Unlock()
	}
	return i, d, comments, err
}

func jiraRoutes(c *core.Core, mux *http.ServeMux) {
	// The list, with what crew knows about each issue: the task working on it.
	mux.HandleFunc("GET /api/jira", func(w http.ResponseWriter, r *http.Request) {
		_, lookErr := exec.LookPath("acli")
		all, _ := c.List(true)
		work := map[string]core.TaskView{}
		for _, t := range all {
			if t.Issue != "" {
				work[t.Issue] = t
			}
		}
		jiraList.Lock()
		defer jiraList.Unlock()
		reply(w, map[string]any{
			"issues": orEmpty(jiraList.issues), "spaces": jiraList.spaces, "tasks": work,
			"installed": lookErr == nil, "ok": jiraList.ok, "read": jiraList.read, "loading": jiraList.loading, "err": jiraList.err,
			"install": setup.AcliInstall, "signIn": "acli jira auth login --web",
		})
	})
	mux.HandleFunc("POST /api/jira/refresh", func(w http.ResponseWriter, r *http.Request) {
		jiraWake()
		reply(w, struct{}{})
	})
	// An issue's card: its details and comments, the statuses it can move to, and its chat if it has one.
	mux.HandleFunc("GET /api/jira/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		i, d, comments, err := jiraIssue(r.Context(), key)
		if err != nil && i.Summary == "" {
			fail(w, err, 502)
			return
		}
		jiraList.Lock()
		statuses := jira.Statuses(jiraList.issues, i.Project)
		jiraList.Unlock()
		errText := ""
		if err != nil {
			errText = err.Error() // the issue read, its comments didn't
		}
		reply(w, map[string]any{"issue": i, "detail": d, "comments": orEmpty(comments), "statuses": statuses, "err": errText})
	})
	mux.HandleFunc("POST /api/jira/{key}/comment", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Body string }
		if !body(w, r, &m) {
			return
		}
		if strings.TrimSpace(m.Body) == "" {
			fail(w, errors.New("write the comment first"), 400)
			return
		}
		if err := jira.PostComment(r.Context(), r.PathValue("key"), strings.TrimSpace(m.Body)); err != nil {
			fail(w, err, 502)
			return
		}
		reply(w, struct{}{})
	})
	// An edit sends only what differs from the issue as Jira has it now.
	mux.HandleFunc("POST /api/jira/{key}/edit", func(w http.ResponseWriter, r *http.Request) {
		var now struct {
			Summary, Description, Labels string
		}
		if !body(w, r, &now) {
			return
		}
		key := r.PathValue("key")
		i, d, _, err := jira.Read(r.Context(), key)
		if err != nil {
			fail(w, err, 502)
			return
		}
		args, err := jira.EditArgs(key, jira.Fields{Summary: i.Summary, Description: i.Description, Labels: d.Labels},
			jira.Fields{Summary: now.Summary, Description: now.Description, Labels: jira.SplitLabels(now.Labels)})
		if err != nil {
			fail(w, err, 400)
			return
		}
		if args == nil {
			reply(w, map[string]bool{"changed": false})
			return
		}
		if err := jira.Run(r.Context(), args); err != nil {
			fail(w, err, 502)
			return
		}
		jiraWake()
		reply(w, map[string]bool{"changed": true})
	})
	mux.HandleFunc("POST /api/jira/{key}/status", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ To string }
		if !body(w, r, &m) {
			return
		}
		if strings.TrimSpace(m.To) == "" {
			fail(w, errors.New("pick a status"), 400)
			return
		}
		if err := jira.Run(r.Context(), jira.Transition(r.PathValue("key"), strings.TrimSpace(m.To))); err != nil {
			fail(w, err, 502)
			return
		}
		jiraWake()
		reply(w, struct{}{})
	})
	// Claude's draft of a comment, for the box: nothing is posted.
	mux.HandleFunc("POST /api/jira/{key}/draft", func(w http.ResponseWriter, r *http.Request) {
		i, d, comments, err := jiraIssue(r.Context(), r.PathValue("key"))
		if err != nil && i.Summary == "" {
			fail(w, err, 502)
			return
		}
		text, err := jira.DraftComment(r.Context(), jira.CardText(i, d, comments))
		if err != nil {
			fail(w, err, 502)
			return
		}
		reply(w, map[string]string{"text": text})
	})
	// Work on it: the task already working on the issue, or a new one made from it in repo.
	mux.HandleFunc("POST /api/jira/{key}/work", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Repo, Branch string }
		if !body(w, r, &m) {
			return
		}
		key := r.PathValue("key")
		all, _ := c.List(true)
		for _, t := range all {
			if t.Issue == key {
				reply(w, t)
				return
			}
		}
		i, ok := jiraFind(key)
		if !ok {
			var err error
			if i, _, _, err = jiraIssue(r.Context(), key); err != nil {
				fail(w, err, 502)
				return
			}
		}
		t, err := c.Create(core.NewTask{Title: i.Key + " " + i.Summary, Repo: m.Repo, Branch: m.Branch, Body: i.Brief(), Issue: key})
		if err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, t)
	})
	// The chat about an issue: made the first time it's opened.
	mux.HandleFunc("POST /api/jira/{key}/chat", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		i, ok := jiraFind(key)
		if !ok {
			i = jira.Issue{Key: key}
		}
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == jira.ChatKind && t.Issue == key }, tasks.Task{
			Title: i.Key + " " + i.Summary, Kind: jira.ChatKind, Issue: key, Status: tasks.InProgress, Body: jira.ChatBrief(i),
		})
		if err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, map[string]string{"id": t.ID})
	})
}
