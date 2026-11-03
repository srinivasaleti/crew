package reminders

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crew/internal/core"
	rem "crew/internal/reminders"
	"crew/internal/tasks"
	"crew/internal/ui/web/api"
)

// REMINDERS: the list, adding (one line, or what/when/repeats), editing, delete/restore/pause/resume,
// its locked-down chat, and firing what's due while the web UI is open. All of it is the reminders
// service's; this only carries it.
func init() {
	api.Register(api.Module{Key: "reminders", Routes: remindersRoutes, Run: remindersRun})
}

// remindersStore is REMINDERS' file, in crew's folder as the terminal keeps it.
func remindersStore(c *core.Core) rem.Store {
	return rem.Store{Path: filepath.Join(c.Home(), "reminders", "reminders.json")}
}

// reminderView is a reminder as the page shows it: with how it reads now.
type reminderView struct {
	rem.Reminder
	When    string `json:"when"`           // "Today 4:00pm"
	Repeats string `json:"repeats"`        // "every weekday", "—"
	Next    string `json:"next,omitempty"` // a repeating one: the time after this one
}

func viewOf(r rem.Reminder, now time.Time) reminderView {
	v := reminderView{Reminder: r, When: rem.When(r.At, now), Repeats: rem.Describe(r.Every, r.At)}
	if r.Every != "" {
		v.Next = rem.When(rem.Next(r.At, r.Every), now)
	}
	return v
}

// reminderInput is a new or changed reminder: Quick is one line ("stand-up at 4pm every weekday");
// otherwise Text, When ("4pm", "" keeps the time) and Every ("weekday", "never").
type reminderInput struct{ Quick, Text, When, Every string }

// build is the reminder in, on top of r (a zero r for a new one).
func (in reminderInput) build(r rem.Reminder, now time.Time) (rem.Reminder, error) {
	if strings.TrimSpace(in.Quick) != "" {
		q, err := rem.ParseQuick(in.Quick, now)
		r.Text, r.At, r.Every = q.Text, q.At, q.Every
		return r, err
	}
	if t := strings.TrimSpace(in.Text); t != "" {
		r.Text = t
	}
	r, err := rem.Build(r, in.When, in.Every, now)
	if err == nil && r.Text == "" {
		err = errors.New("say what to remind you of")
	}
	return r, err
}

func remindersRoutes(c *core.Core, mux *http.ServeMux) {
	exe, _ := os.Executable()
	dir := filepath.Join(c.Home(), "reminders")
	c.RegisterChat(rem.ChatKind, core.ChatKind{
		Dir:  func(tasks.Task) (string, error) { return dir, os.MkdirAll(dir, 0o755) },
		Args: func(tasks.Task) []string { return rem.ChatArgs(exe) }, // only crew remind, as in the terminal
	})
	changed := func() { c.Publish(core.Event{Kind: "reminders"}) }

	mux.HandleFunc("GET /api/reminders", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		var out []reminderView
		for _, x := range remindersStore(c).Load() {
			if !x.Deleted {
				out = append(out, viewOf(x, now))
			}
		}
		api.Reply(w, map[string]any{"now": now, "reminders": api.OrEmpty(out)})
	})
	// preview is what a line would make, without saving: the page shows it as you type.
	mux.HandleFunc("POST /api/reminders/preview", func(w http.ResponseWriter, r *http.Request) {
		var in reminderInput
		if !api.Body(w, r, &in) {
			return
		}
		x, err := in.build(rem.Reminder{}, time.Now())
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, viewOf(x, time.Now()))
	})
	mux.HandleFunc("POST /api/reminders", func(w http.ResponseWriter, r *http.Request) {
		var in reminderInput
		if !api.Body(w, r, &in) {
			return
		}
		var added rem.Reminder
		err := remindersStore(c).Change(func(rs []rem.Reminder) ([]rem.Reminder, error) {
			x, err := in.build(rem.Reminder{ID: rem.NextID(rs)}, time.Now())
			added = x
			return append(rs, x), err
		})
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		changed()
		api.Reply(w, viewOf(added, time.Now()))
	})
	mux.HandleFunc("PUT /api/reminders/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in reminderInput
		if !api.Body(w, r, &in) {
			return
		}
		err := changeReminder(c, r.PathValue("id"), func(x *rem.Reminder) error {
			y, err := in.build(*x, time.Now())
			if err != nil {
				return err
			}
			x.Text, x.At, x.Every, x.Done = y.Text, y.At, y.Every, false // as the terminal's form saves it
			return nil
		})
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		changed()
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("POST /api/reminders/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		err := changeReminder(c, r.PathValue("id"), func(x *rem.Reminder) error {
			switch r.PathValue("action") {
			case "delete":
				x.Deleted = true
			case "restore":
				x.Deleted = false
			case "pause":
				x.Paused, x.Until = true, time.Time{}
			case "resume":
				rem.Resume(x, time.Now())
			default:
				return errors.New("delete, restore, pause or resume")
			}
			return nil
		})
		if err != nil {
			api.Fail(w, err, 400)
			return
		}
		changed()
		api.Reply(w, struct{}{})
	})
	// chat is REMINDERS' chat, made the first time: the page talks to it over /api/tasks/<id>/….
	mux.HandleFunc("GET /api/reminders/chat", func(w http.ResponseWriter, r *http.Request) {
		t, err := c.FindChat(func(t tasks.Task) bool { return t.Kind == rem.ChatKind },
			tasks.Task{Title: "Reminders", Kind: rem.ChatKind, Status: tasks.InProgress, Body: rem.ChatBrief(exe, time.Now())})
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"id": t.ID})
	})
}

// changeReminder changes the reminder with this id and saves it.
func changeReminder(c *core.Core, id string, f func(*rem.Reminder) error) error {
	n, _ := strconv.Atoi(id)
	return remindersStore(c).Change(func(rs []rem.Reminder) ([]rem.Reminder, error) {
		x := rem.Find(rs, n)
		if x == nil {
			return nil, errors.New("no reminder " + id)
		}
		return rs, f(x)
	})
}

// remindersRun fires what's due every 15 seconds while REMINDERS is on, as the terminal does: a
// sound and a notification each, and the page hears it ("reminders", what fired in Text).
func remindersRun(ctx context.Context, c *core.Core) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		if c.SectionOn("reminders") {
			if fired, _ := remindersStore(c).Due(time.Now()); len(fired) > 0 {
				rem.Ring(fired)
				var what []string
				for _, r := range fired {
					what = append(what, r.Text)
				}
				// ponytail: what fired goes as Text, one a line; core.Event has no field for a section's data yet.
				c.Publish(core.Event{Kind: "reminders", Text: strings.Join(what, "\n")})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
