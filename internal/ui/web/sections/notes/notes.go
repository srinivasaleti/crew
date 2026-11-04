package notes

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crew/internal/core"
	nt "crew/internal/notes"
	"crew/internal/pins"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// NOTES: notes (HTML files) in ~/.crew/notes, read, written, searched, starred, moved and trashed, and
// a Claude chat locked down to that folder. The rules are the notes service's.
func init() { api.Register(api.Module{Key: "notes", Routes: notesRoutes}) }

func notesRoutes(c *core.Core, mux *http.ServeMux) {
	dir := nt.Dir(filepath.Join(c.Home(), "notes"))
	pinsPath := filepath.Join(c.Home(), "notes.json") // starred notes, as prs.json keeps starred repos
	c.RegisterChat(nt.ChatKind, core.ChatKind{
		Dir:  func(tasks.Task) (string, error) { return string(dir), os.MkdirAll(string(dir), 0o755) },
		Args: func(tasks.Task) []string { return nt.ChatArgs() }, // only files under the notes folder
	})
	changed := func() { c.Publish(core.Event{Kind: "notes"}) }
	reply := func(w http.ResponseWriter, v any, err error) {
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, v)
	}
	ok := struct{}{}

	mux.HandleFunc("GET /api/notes", func(w http.ResponseWriter, r *http.Request) {
		api.Reply(w, map[string]any{"notes": api.OrEmpty(dir.List(r.URL.Query().Get("q"))),
			"folders": api.OrEmpty(dir.Folders()), "starred": api.OrEmpty(pins.Load(pinsPath).Starred), "dir": string(dir)})
	})
	mux.HandleFunc("GET /api/notes/note", func(w http.ResponseWriter, r *http.Request) {
		text, path, err := dir.Read(r.URL.Query().Get("path")) // a Markdown note comes back as HTML, at a new path
		reply(w, map[string]string{"text": text, "path": path}, err)
	})
	mux.HandleFunc("PUT /api/notes/note", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Path, Text string }
		if !api.Body(w, r, &in) {
			return
		}
		// Only a note that's there: a save that lands after a delete mustn't bring the note back.
		if !strings.HasSuffix(in.Path, ".html") || func() bool { _, _, err := dir.Read(in.Path); return err != nil }() {
			api.Fail(w, errors.New("this note was deleted or moved"), 409)
			return
		}
		err := dir.Write(in.Path, in.Text)
		if err == nil {
			changed()
		}
		reply(w, ok, err)
	})
	mux.HandleFunc("POST /api/notes", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Folder string }
		if !api.Body(w, r, &in) {
			return
		}
		p, err := dir.New(in.Folder, time.Now())
		if err == nil {
			changed()
		}
		reply(w, map[string]string{"path": p}, err)
	})
	// action is move ({To}), trash, restore or star ({On}) on the note at {Path}.
	mux.HandleFunc("POST /api/notes/{action}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path, To string
			On       bool
		}
		if !api.Body(w, r, &in) {
			return
		}
		var err error
		p := pins.Load(pinsPath)
		switch r.PathValue("action") {
		case "move":
			if err = dir.Move(in.Path, in.To); err == nil && p.IsStarred(in.Path) {
				p.Star(in.Path, false)
				p.Star(in.To, true)
				err = p.Save(pinsPath)
			}
		case "trash":
			err = dir.Trash(in.Path)
		case "restore":
			err = dir.Restore(in.Path)
		case "star":
			p.Star(in.Path, in.On)
			err = p.Save(pinsPath)
		default:
			api.Fail(w, os.ErrInvalid, 404)
			return
		}
		if err == nil {
			changed()
		}
		reply(w, ok, err)
	})
	// chat is NOTES' chat, made the first time: the page talks to it over /api/tasks/<id>/….
	mux.HandleFunc("GET /api/notes/chat", func(w http.ResponseWriter, r *http.Request) {
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == nt.ChatKind },
			tasks.Task{Title: "Notes", Kind: nt.ChatKind, Status: tasks.InProgress, Body: nt.ChatBrief()})
		if err == nil && t.Body != nt.ChatBrief() { // a chat made before the brief changed (notes were Markdown)
			err = c.Edit(t.ID, t.Title, nt.ChatBrief())
		}
		reply(w, map[string]string{"id": t.ID}, err)
	})
}
