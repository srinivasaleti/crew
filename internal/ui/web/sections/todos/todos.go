package todos

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"crew/internal/core"
	td "crew/internal/todos"
	"crew/internal/ui/web/api"
)

// TODO: the lists and items, quick add ("call the bank tomorrow !high #Home"), ticking off, editing,
// reordering, and remembering the crew task made from an item. The rules are the todos service's.
func init() { api.Register(api.Module{Key: "todos", Routes: todosRoutes}) }

// itemInput is a change to an item; a nil field is left as it is.
type itemInput struct {
	Text, List, Due, Note, Task *string
	Priority                    *int
	Done                        *bool
}

func todosRoutes(c *core.Core, mux *http.ServeMux) {
	store := td.Store{Path: filepath.Join(c.Home(), "todos", "todos.json")}
	// change runs f on the lists and answers; the page hears "todos" and reads them again.
	change := func(w http.ResponseWriter, f func(t *td.Todos) (any, error)) {
		var out any
		err := store.Change(func(t *td.Todos) (err error) { out, err = f(t); return err })
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		c.Publish(core.Event{Kind: "todos"})
		api.Reply(w, out)
	}
	ok := struct{}{}

	mux.HandleFunc("GET /api/todos", func(w http.ResponseWriter, r *http.Request) {
		t := store.Load()
		t.Items = api.OrEmpty(t.Items)
		api.Reply(w, map[string]any{"lists": t.Lists, "items": t.Items, "today": time.Now().Format("2006-01-02")})
	})
	// preview is what a quick-add line would make, without saving: the page shows it as you type.
	mux.HandleFunc("POST /api/todos/preview", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Quick string }
		if api.Body(w, r, &in) {
			api.Reply(w, td.Parse(in.Quick, time.Now()))
		}
	})
	mux.HandleFunc("POST /api/todos", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Quick, List, Due string } // List and Due when the line names none
		if !api.Body(w, r, &in) {
			return
		}
		change(w, func(t *td.Todos) (any, error) {
			it := td.Parse(in.Quick, time.Now())
			if it.Text == "" {
				return nil, errors.New("say what to do")
			}
			if it.List == "" {
				it.List = in.List
			}
			if it.Due == "" {
				it.Due = in.Due
			}
			return t.Add(it), nil
		})
	})
	mux.HandleFunc("PUT /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in itemInput
		if !api.Body(w, r, &in) {
			return
		}
		id, _ := strconv.Atoi(r.PathValue("id"))
		change(w, func(t *td.Todos) (any, error) {
			it := t.Find(id)
			if it == nil {
				return nil, errors.New("no such item")
			}
			if in.Text != nil {
				if it.Text = strings.TrimSpace(*in.Text); it.Text == "" {
					return nil, errors.New("say what to do")
				}
			}
			if in.List != nil && *in.List != "" {
				it.List = *in.List
				t.AddList(it.List)
			}
			if in.Due != nil {
				it.Due = *in.Due
			}
			if in.Note != nil {
				it.Note = *in.Note
			}
			if in.Task != nil {
				it.Task = *in.Task
			}
			if in.Priority != nil {
				it.Priority = min(max(*in.Priority, 0), 3)
			}
			if in.Done != nil {
				it.SetDone(*in.Done, time.Now())
			}
			return *it, nil
		})
	})
	mux.HandleFunc("DELETE /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		change(w, func(t *td.Todos) (any, error) {
			n := len(t.Items)
			t.Items = slices.DeleteFunc(t.Items, func(it td.Item) bool { return it.ID == id })
			if len(t.Items) == n {
				return nil, errors.New("no such item")
			}
			return ok, nil
		})
	})
	// restore puts back an item just deleted (the page's Undo), where it was.
	mux.HandleFunc("POST /api/todos/restore", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Item   td.Item
			Before int
		}
		if !api.Body(w, r, &in) {
			return
		}
		change(w, func(t *td.Todos) (any, error) {
			t.AddList(in.Item.List)
			t.Items = append(t.Items, in.Item)
			return ok, t.Move(in.Item.ID, in.Before, "")
		})
	})
	// move puts an item before another (Before 0: at the end), into List ("" keeps its list).
	mux.HandleFunc("POST /api/todos/{id}/move", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Before int
			List   string
		}
		if !api.Body(w, r, &in) {
			return
		}
		id, _ := strconv.Atoi(r.PathValue("id"))
		change(w, func(t *td.Todos) (any, error) { return ok, t.Move(id, in.Before, in.List) })
	})
	mux.HandleFunc("POST /api/todos/clear", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ List string }
		if api.Body(w, r, &in) {
			change(w, func(t *td.Todos) (any, error) { return map[string]int{"cleared": t.ClearDone(in.List)}, nil })
		}
	})
	mux.HandleFunc("POST /api/todos/lists", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !api.Body(w, r, &in) {
			return
		}
		change(w, func(t *td.Todos) (any, error) {
			if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
				return nil, errors.New("give the list a name")
			}
			t.AddList(in.Name)
			return ok, nil
		})
	})
	mux.HandleFunc("PUT /api/todos/lists/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if api.Body(w, r, &in) {
			change(w, func(t *td.Todos) (any, error) { return ok, t.RenameList(r.PathValue("name"), in.Name) })
		}
	})
	mux.HandleFunc("DELETE /api/todos/lists/{name}", func(w http.ResponseWriter, r *http.Request) {
		change(w, func(t *td.Todos) (any, error) { return ok, t.DeleteList(r.PathValue("name")) })
	})
}
