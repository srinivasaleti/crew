package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	ctr "crew/internal/containers"
	"crew/internal/core"
	"crew/internal/tasks"
)

// DOCKER: its tabs read with the docker CLI (internal/containers), a thing's logs and inspect output,
// the changes (each on your click, after the page asks), Cleanup, and a chat locked down to crew ctr.
func init() { modules = append(modules, module{key: "docker", routes: dockerRoutes}) }

// thingChat is the chat about a container or Kubernetes object: found again by the brief it was
// given (the terminal's too, made with the same brief), else made. It runs in its own folder,
// locked down to crew ctr, which only reads.
func thingChat(c *core.Core, key, kind, id, name, ns, kctx string) (tasks.Task, error) {
	exe := crewExe()
	brief := ctr.ChatBrief(exe, kind, id, name, ns, kctx)
	termID := tasks.Slugify("ctr " + key) // the id the terminal gives it
	c.RegisterChat("containers", core.ChatKind{
		Dir: func(t tasks.Task) (string, error) {
			dir := filepath.Join(c.Home(), "containers", t.ID)
			return dir, os.MkdirAll(dir, 0o755)
		},
		Args: func(tasks.Task) []string { return ctr.ChatArgs(exe) },
	})
	return c.FindChat(func(t tasks.Task) bool { return t.Kind == "containers" && (t.ID == termID || t.Body == brief) },
		tasks.Task{Title: name, Kind: "containers", Status: tasks.InProgress, Body: brief})
}

// crewExe is the crew binary, which a container chat's Claude runs as crew ctr.
func crewExe() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "crew"
}

// cleanups are the Cleanup options last shown, by section, so a cleanup removes exactly what the
// page listed and you picked, not what's there by the time you click.
var cleanups = struct {
	sync.Mutex
	m map[string][]ctr.Option
}{m: map[string][]ctr.Option{}}

// keepCleanup remembers what Cleanup listed for a section and answers the page with it.
func keepCleanup(w http.ResponseWriter, sec, title, note string, nss []string, opts []ctr.Option) {
	cleanups.Lock()
	cleanups.m[sec] = opts
	cleanups.Unlock()
	type opt struct {
		Label, Warn, Size string
		On                bool
		Names             []string
	}
	out := struct {
		Title, Note string
		Namespaces  []string
		Opts        []opt
	}{Title: title, Note: note, Namespaces: orEmpty(nss), Opts: []opt{}}
	for _, o := range opts {
		out.Opts = append(out.Opts, opt{o.Label, o.Warn, o.Size, o.On, orEmpty(o.Names)})
	}
	reply(w, out)
}

// runCleanup removes the picked options (by label) of what Cleanup last listed for a section.
func runCleanup(ctx context.Context, w http.ResponseWriter, sec string, labels []string) {
	cleanups.Lock()
	opts := cleanups.m[sec]
	delete(cleanups.m, sec) // one listing, one cleanup
	cleanups.Unlock()
	var picked []ctr.Option
	for _, o := range opts {
		if slices.Contains(labels, o.Label) && o.Has() {
			picked = append(picked, o)
		}
	}
	if len(picked) == 0 {
		fail(w, fmt.Errorf("nothing picked, or the list is old: open Cleanup again"), 400)
		return
	}
	if err := ctr.RunAll(ctx, picked); err != nil {
		fail(w, err, 500)
		return
	}
	reply(w, map[string]int{"removed": len(picked)})
}

// missing says why a CLI can't be used, or "".
func missing(cli string) string {
	if _, err := exec.LookPath(cli); err != nil {
		return cli + " isn't installed, or crew can't find it on your PATH"
	}
	return ""
}

// dockerRoutes are DOCKER's calls.
func dockerRoutes(c *core.Core, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/docker", func(w http.ResponseWriter, r *http.Request) {
		if why := missing("docker"); why != "" {
			reply(w, map[string]string{"error": why, "missing": "docker"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		d, err := ctr.LoadDocker(ctx, r.URL.Query().Get("stats") != "")
		if err != nil {
			reply(w, map[string]string{"error": err.Error()})
			return
		}
		reply(w, dockerView(d))
	})
	mux.HandleFunc("GET /api/docker/thing", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := ctr.Read(r.Context(), q.Get("kind"), q.Get("id"), "", "", q.Get("logs") != "")
		if err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, map[string]string{"out": out})
	})
	mux.HandleFunc("POST /api/docker/act", func(w http.ResponseWriter, r *http.Request) {
		var a struct{ Kind, ID, Action string }
		if !body(w, r, &a) {
			return
		}
		args := ctr.DockerArgs(a.Kind, a.Action, a.ID)
		if args == nil {
			fail(w, fmt.Errorf("%s has no %s", strings.ToLower(a.Kind), a.Action), 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if a.Action == ctr.Remove && a.Kind != "Container" {
			d, err := ctr.LoadDocker(ctx, false)
			if err == nil {
				err = errorIf(d.WhyNotRemove(a.Kind, a.ID))
			}
			if err != nil {
				fail(w, err, 400)
				return
			}
		}
		if _, err := ctr.Run(ctx, "docker", args...); err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/docker/cleanup", func(w http.ResponseWriter, r *http.Request) {
		d, err := ctr.LoadDocker(r.Context(), false)
		if err != nil {
			fail(w, err, 500)
			return
		}
		title, opts := ctr.DockerCleanup(d)
		keepCleanup(w, "docker", title, "", nil, opts)
	})
	mux.HandleFunc("POST /api/docker/cleanup", func(w http.ResponseWriter, r *http.Request) {
		var p struct{ Labels []string }
		if body(w, r, &p) {
			runCleanup(r.Context(), w, "docker", p.Labels)
		}
	})
	mux.HandleFunc("POST /api/docker/chat", func(w http.ResponseWriter, r *http.Request) {
		var t struct{ Kind, ID, Name string }
		if !body(w, r, &t) {
			return
		}
		ch, err := thingChat(c, "d:"+t.Kind+":"+t.ID, t.Kind, t.ID, t.Name, "", "")
		if err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, map[string]string{"id": ch.ID})
	})
}

// errorIf is why as an error, or nil when it's "".
func errorIf(why string) error {
	if why == "" {
		return nil
	}
	return fmt.Errorf("%s", why)
}

// dockerView is what the page shows of DOCKER: each tab's rows, with what the page can't work out
// itself (who uses what, a container's state and ports) worked out here.
func dockerView(d ctr.DockerData) map[string]any {
	type ctrRow struct {
		ID, Name, Image, State, Ports, CPU, Mem, Project string
		Running                                          bool
		ExitCode                                         int
		Since                                            time.Time // started, or finished when it's stopped
	}
	type usedRow struct {
		ID, Name, Tag, Size, Created, Driver, Scope, Project string
		UsedBy                                               []string
		Dangling, Builtin, Anonymous                         bool
	}
	var cs []ctrRow
	for _, x := range d.Containers {
		st, since := x.State.Status, x.State.FinishedAt
		if x.State.Running {
			since = x.State.StartedAt
		} else if st == "exited" {
			st = fmt.Sprintf("exited (%d)", x.State.ExitCode)
		}
		cs = append(cs, ctrRow{x.ID, x.Label(), x.Config.Image, st, x.Ports(), x.CPU, x.Mem, x.Project(), x.State.Running, x.State.ExitCode, since})
	}
	var ims, vols, nets []usedRow
	for _, im := range d.Images {
		ims = append(ims, usedRow{ID: im.ID, Name: im.Label(), Tag: im.Tag, Size: im.Size, Created: im.CreatedSince, UsedBy: orEmpty(d.ImageUsers(im)), Dangling: im.Dangling()})
	}
	for _, v := range d.Volumes {
		vols = append(vols, usedRow{ID: v.Name, Name: v.Name, Size: v.Size, Driver: v.Driver, Project: v.Project(), UsedBy: orEmpty(d.VolumeUsers(v)), Anonymous: len(v.Name) == 64})
	}
	for _, n := range d.Networks {
		nets = append(nets, usedRow{ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, UsedBy: orEmpty(d.NetworkUsers(n)), Builtin: n.Builtin()})
	}
	return map[string]any{"containers": orEmpty(cs), "images": orEmpty(ims), "volumes": orEmpty(vols), "networks": orEmpty(nets), "disk": d.Disk}
}
