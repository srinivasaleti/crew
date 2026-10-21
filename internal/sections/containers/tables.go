package containers

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// The tables DOCKER and KUBERNETES show, one per tab, built from what was last read (no CLI calls).

// What a table's keys ask for on a row.
const (
	Open    = ""        // its page (logs, inspect, chat)
	Ask     = "ask"     // its page on the Chat tab
	Shell   = "shell"   // a shell inside it
	Restart = "restart" // docker restart; kubectl rollout restart
	Stop    = "stop"
	Start   = "start"
	Remove  = "remove" // docker rm/rmi/volume rm/network rm; kubectl delete
	Forward = "forward"
	Cleanup = "cleanup" // the Cleanup popup
	Context = "context" // the context menu (KUBERNETES)
)

// dockerTabs and kubeTabs are each section's tabs, ⌘1….
var (
	dockerTabs = []string{"Containers", "Images", "Volumes", "Networks"}
	kubeTabs   = []string{"Pods", "Deployments", "Services", "Jobs", "Config", "Events"}
)

// state draws a status in its colour: green when fine, red when broken, dim otherwise.
func state(s string, ok, bad bool) string {
	switch {
	case bad:
		return look.Warn.Render("✗ " + s)
	case ok:
		return look.Running.Render("● " + s)
	}
	return look.Dim.Render("○ " + s)
}

// dash is an empty cell.
var dash = look.Dim.Render("–")

// or is s, or a dash when it's empty.
func or(s string) string {
	if s == "" {
		return dash
	}
	return s
}

// users says what uses an image, volume or network: one name, a count, or unused.
func users(names []string) string {
	switch len(names) {
	case 0:
		return look.Dim.Render("– unused")
	case 1:
		return names[0]
	}
	return fmt.Sprintf("%d containers", len(names))
}

// dockerTable is DOCKER's table on tab i.
func dockerTable(d dockerData, i int, err string) table.Table {
	cleanup := table.Act{Label: "+ Cleanup", Key: "C"}
	t := table.Table{Facet: "project", Global: map[string]bool{"C": true}, Keys: map[string]string{"C": Cleanup, "x": Remove, "a": Ask}}
	switch i {
	case 0:
		t.Cols = []table.Col{{Name: "name"}, {Name: "image", Width: 26}, {Name: "state", Width: 16}, {Name: "ports", Width: 16},
			{Name: "cpu", Width: 7}, {Name: "mem", Width: 8}, {Name: "up", Width: 9}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "running", Label: "Running"}, {ID: "exited", Label: "Exited"}}
		t.Keys["e"], t.Keys["R"], t.Keys["s"], t.Keys["S"] = Shell, Restart, Stop, Start
		t.Acts = []table.Act{{Label: "Logs", Key: "enter"}, {Label: "Shell", Key: "e"}, {Label: "Restart", Key: "R"}, {Label: "Stop", Key: "s"},
			{Label: "Start", Key: "S"}, {Label: "Remove", Key: "x"}, cleanup, {Label: "Ask", Key: "a"}}
		t.Hint = "⏎ logs · e shell · R restart · s stop · S start · x remove · C cleanup · a ask Claude · tab filter · r project · / search"
		running, exited := 0, 0
		for _, c := range d.containers {
			tag, up := "exited", look.Ago(c.State.FinishedAt)
			if c.State.Running {
				tag, up = "running", strings.TrimSuffix(look.Ago(c.State.StartedAt), " ago")
				running++
			} else {
				exited++
			}
			st := c.State.Status
			if !c.State.Running && st == "exited" {
				st = fmt.Sprintf("exited (%d)", c.State.ExitCode)
			}
			t.Rows = append(t.Rows, table.Row{ID: c.ID, Facet: c.project(), Tags: []string{"all", tag},
				Cells:  []string{c.name(), c.Config.Image, state(st, c.State.Running, !c.State.Running && c.State.ExitCode != 0), or(c.ports()), or(c.cpu), or(c.mem), look.Dim.Render(up)},
				Search: c.name() + " " + c.Config.Image + " " + short(c.ID)})
		}
		t.Summary = fmt.Sprintf("%d running · %d stopped", running, exited)
		t.Empty = "No containers"
	case 1:
		t.Cols = []table.Col{{Name: "image"}, {Name: "tag", Width: 14}, {Name: "size", Width: 9}, {Name: "used by", Width: 20}, {Name: "created", Width: 14}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "used", Label: "In use"}, {ID: "unused", Label: "Unused"}, {ID: "dangling", Label: "Dangling"}}
		t.Acts = []table.Act{{Label: "Inspect", Key: "enter"}, {Label: "Remove", Key: "x"}, cleanup, {Label: "Ask", Key: "a"}}
		t.Hint = "⏎ inspect · x remove · C cleanup · a ask Claude · tab filter · / search"
		for _, im := range d.images {
			by, tags := d.imageUsers(im), []string{"all", "used"}
			used := users(by)
			switch {
			case im.dangling() && len(by) == 0:
				tags, used = []string{"all", "unused", "dangling"}, look.Dim.Render("– dangling")
			case len(by) == 0:
				tags = []string{"all", "unused"}
			}
			repo := im.Repository
			if im.dangling() {
				repo = look.Dim.Render(short(im.ID))
			}
			t.Rows = append(t.Rows, table.Row{ID: im.ID, Tags: tags, Cells: []string{repo, im.Tag, im.Size, used, look.Dim.Render(im.CreatedSince)},
				Search: im.label() + " " + short(im.ID)})
		}
		u := d.disk["Images"]
		t.Summary = fmt.Sprintf("%d images · %s · %s reclaimable", len(d.images), u.Size, u.Reclaimable)
		t.Empty = "No images"
	case 2:
		t.Cols = []table.Col{{Name: "volume"}, {Name: "size", Width: 9}, {Name: "used by", Width: 22}, {Name: "driver", Width: 8}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "used", Label: "In use"}, {ID: "unused", Label: "Unused"}}
		t.Acts = []table.Act{{Label: "Inspect", Key: "enter"}, {Label: "Remove", Key: "x"}, cleanup, {Label: "Ask", Key: "a"}}
		t.Hint = "⏎ inspect · x remove (asks: a volume is usually data) · C cleanup · a ask Claude · tab filter · r project · / search"
		unused := 0
		for _, v := range d.volumes {
			by, tag := d.volumeUsers(v), "used"
			if len(by) == 0 {
				tag = "unused"
				unused++
			}
			name := v.Name
			if len(name) == 64 { // an anonymous volume's name is its hash
				name = look.Dim.Render(short(name) + "  (anonymous)")
			}
			t.Rows = append(t.Rows, table.Row{ID: v.Name, Facet: v.project(), Tags: []string{"all", tag},
				Cells: []string{name, or(v.Size), users(by), look.Dim.Render(v.Driver)}, Search: v.Name})
		}
		t.Summary = fmt.Sprintf("%d volumes · %s · %d unused", len(d.volumes), d.disk["Local Volumes"].Size, unused)
		t.Empty = "No volumes"
	case 3:
		t.Cols = []table.Col{{Name: "network"}, {Name: "driver", Width: 9}, {Name: "scope", Width: 7}, {Name: "used by", Width: 22}}
		t.Acts = []table.Act{{Label: "Inspect", Key: "enter"}, {Label: "Remove", Key: "x"}, cleanup, {Label: "Ask", Key: "a"}}
		t.Hint = "⏎ inspect · x remove · C cleanup · a ask Claude · / search"
		for _, n := range d.networks {
			by := users(d.networkUsers(n))
			if n.builtin() {
				by = look.Dim.Render("built in")
			}
			t.Rows = append(t.Rows, table.Row{ID: n.ID, Cells: []string{n.Name, n.Driver, look.Dim.Render(n.Scope), by}, Search: n.Name})
		}
		t.Summary = fmt.Sprintf("%d networks", len(d.networks))
		t.Empty = "No networks"
	}
	if err != "" {
		t.Rows, t.Empty, t.Summary = nil, err, "not reachable"
	}
	return t
}

// kubeTable is KUBERNETES' table on tab i: every namespace, the namespace as the facet.
func kubeTable(d kubeData, i int, err string) table.Table {
	t := table.Table{Facet: "namespace", Global: map[string]bool{"c": true, "C": true},
		Keys: map[string]string{"c": Context, "C": Cleanup, "a": Ask}}
	ctxBtn, cleanup := table.Act{Label: "+ Context", Key: "c"}, table.Act{Label: "+ Cleanup", Key: "C"}
	ask := table.Act{Label: "Ask", Key: "a"}
	age := func(o kobj) string {
		return look.Dim.Render(strings.TrimSuffix(look.Ago(o.Metadata.CreationTimestamp), " ago"))
	}
	row := func(o kobj, tags []string, cells ...string) {
		t.Rows = append(t.Rows, table.Row{ID: o.Kind + "/" + o.Metadata.Namespace + "/" + o.Metadata.Name, Facet: o.Metadata.Namespace,
			Tags: append([]string{"all"}, tags...), Cells: cells, Search: o.Metadata.Name + " " + o.Metadata.Namespace})
	}
	objs := slices.Clone(d.objs)
	if d.tab != i {
		objs = nil // read for another tab; this one's read is on its way
	}
	slices.SortStableFunc(objs, func(a, b kobj) int {
		if i == 5 { // events: the latest first
			return b.when().Compare(a.when())
		}
		return strings.Compare(a.Metadata.Namespace+"/"+a.Metadata.Name, b.Metadata.Namespace+"/"+b.Metadata.Name)
	})
	switch i {
	case 0:
		t.Cols = []table.Col{{Name: "pod"}, {Name: "ready", Width: 6}, {Name: "status", Width: 20}, {Name: "restarts", Width: 9}, {Name: "node", Width: 14}, {Name: "age", Width: 6}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "problems", Label: "Problems"}, {ID: "running", Label: "Running"}, {ID: "done", Label: "Completed"}}
		t.Keys["e"], t.Keys["p"], t.Keys["x"] = Shell, Forward, Remove
		t.Acts = []table.Act{{Label: "Logs", Key: "enter"}, {Label: "Shell", Key: "e"}, {Label: "Forward", Key: "p"}, {Label: "Delete pod", Key: "x"}, ctxBtn, cleanup, ask}
		t.Hint = "⏎ logs · e shell · p port-forward · x deletes the pod · c context · C cleanup · a ask · tab filter · r namespace · / search"
		for _, o := range objs {
			s, tags := o.podStatus(), []string{}
			switch {
			case !o.healthy():
				tags = append(tags, "problems")
			case s == "Completed":
				tags = append(tags, "done")
			default:
				tags = append(tags, "running")
			}
			restarts := strconv.Itoa(o.restarts())
			if o.restarts() > 0 {
				restarts = look.Orange.Render(restarts)
			}
			row(o, tags, o.Metadata.Name, o.podReady(), state(s, o.healthy() && s != "Completed", !o.healthy()), restarts, look.Dim.Render(o.Spec.NodeName), age(o))
		}
	case 1:
		t.Cols = []table.Col{{Name: "deployment"}, {Name: "ready", Width: 8}, {Name: "up to date", Width: 11}, {Name: "available", Width: 10}, {Name: "age", Width: 6}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "notready", Label: "Not ready"}}
		t.Keys["R"], t.Keys["p"] = Restart, Forward
		t.Acts = []table.Act{{Label: "Logs", Key: "enter"}, {Label: "Restart", Key: "R"}, {Label: "Forward", Key: "p"}, ctxBtn, ask}
		t.Hint = "⏎ logs · R rollout restart · p port-forward · c context · a ask · tab filter · r namespace · / search"
		for _, o := range objs {
			want := 1
			if o.Spec.Replicas != nil {
				want = *o.Spec.Replicas
			}
			ready := fmt.Sprintf("%d/%d", o.Status.ReadyReplicas, want)
			var tags []string
			if o.Status.ReadyReplicas < want {
				tags, ready = []string{"notready"}, look.Warn.Render(ready)
			}
			row(o, tags, o.Metadata.Name, ready, strconv.Itoa(o.Status.UpdatedReplicas), strconv.Itoa(o.Status.AvailableReplicas), age(o))
		}
	case 2:
		t.Cols = []table.Col{{Name: "service"}, {Name: "type", Width: 13}, {Name: "cluster ip", Width: 16}, {Name: "ports", Width: 18}, {Name: "age", Width: 6}}
		t.Keys["p"] = Forward
		t.Acts = []table.Act{{Label: "Describe", Key: "enter"}, {Label: "Forward", Key: "p"}, ctxBtn, ask}
		t.Hint = "⏎ describe · p port-forward · c context · a ask · r namespace · / search"
		for _, o := range objs {
			var ps []string
			for _, p := range o.Spec.Ports {
				s := strconv.Itoa(p.Port)
				if p.NodePort > 0 {
					s += ":" + strconv.Itoa(p.NodePort)
				}
				ps = append(ps, s+"/"+p.Protocol)
			}
			row(o, nil, o.Metadata.Name, o.Spec.Type, look.Dim.Render(o.Spec.ClusterIP), strings.Join(ps, ","), age(o))
		}
	case 3:
		t.Cols = []table.Col{{Name: "job"}, {Name: "completions", Width: 12}, {Name: "status", Width: 12}, {Name: "from", Width: 9}, {Name: "age", Width: 6}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "Running", Label: "Running"}, {ID: "Failed", Label: "Failed"}, {ID: "Complete", Label: "Complete"}}
		t.Keys["x"] = Remove
		t.Acts = []table.Act{{Label: "Logs", Key: "enter"}, {Label: "Delete", Key: "x"}, ctxBtn, cleanup, ask}
		t.Hint = "⏎ logs · x deletes the job · c context · C cleanup · a ask · tab filter · r namespace · / search"
		for _, o := range objs {
			want := 1
			if o.Spec.Completions != nil {
				want = *o.Spec.Completions
			}
			s := o.jobState()
			row(o, []string{s}, o.Metadata.Name, fmt.Sprintf("%d/%d", o.Status.Succeeded, want), state(s, s == "Running", s == "Failed"), look.Dim.Render(or(o.owner())), age(o))
		}
	case 4:
		t.Cols = []table.Col{{Name: "name"}, {Name: "kind", Width: 10}, {Name: "type", Width: 20}, {Name: "keys"}, {Name: "age", Width: 6}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "ConfigMap", Label: "ConfigMaps"}, {ID: "Secret", Label: "Secrets"}}
		t.Acts = []table.Act{{Label: "Describe", Key: "enter"}, ctxBtn, ask}
		t.Hint = "⏎ describe (a secret's values never show) · c context · a ask · tab filter · r namespace · / search"
		for _, o := range objs {
			row(o, []string{o.Kind}, o.Metadata.Name, o.Kind, look.Dim.Render(or(o.Type)), look.Dim.Render(strings.Join(o.keys(), ", ")), age(o))
		}
	case 5:
		t.Cols = []table.Col{{Name: "reason", Width: 18}, {Name: "object", Width: 30}, {Name: "message"}, {Name: "count", Width: 6}, {Name: "last", Width: 9}}
		t.Chips = []table.Chip{{ID: "all", Label: "All"}, {ID: "Warning", Label: "Warnings"}}
		t.Acts = []table.Act{ctxBtn, ask}
		t.Hint = "c context · a ask Claude about an event · tab filter · r namespace · / search"
		for _, o := range objs {
			reason := look.Dim.Render(o.Reason)
			if o.Type == "Warning" {
				reason = look.Orange.Render(o.Reason)
			}
			obj := strings.ToLower(o.InvolvedObject.Kind) + "/" + o.InvolvedObject.Name
			row(o, []string{o.Type}, reason, obj, o.Message, strconv.Itoa(max(o.Count, 1)), look.Dim.Render(look.Ago(o.when())))
		}
	}
	problems := 0
	for _, p := range d.pods {
		if !p.healthy() {
			problems++
		}
	}
	t.Summary = d.kctx + " · " + fmt.Sprintf("%d pods", len(d.pods))
	if isProd(d.kctx) {
		t.Summary = look.Warn.Render("⚠ "+d.kctx+" (production)") + fmt.Sprintf(" · %d pods", len(d.pods))
	}
	if problems > 0 {
		t.Summary += " · " + look.Warn.Render(fmt.Sprintf("%d not healthy", problems))
	}
	t.Empty = "Nothing here in " + or(d.kctx)
	if err != "" {
		t.Rows, t.Empty, t.Summary = nil, err, "not reachable"
	}
	return t
}

// secretish matches the names of values that shouldn't be shown: env vars and keys like
// DB_PASSWORD, API_TOKEN, aws_secret_access_key.
var secretish = regexp.MustCompile(`(?i)(pass|secret|token|key|auth|credential|private|cookie|session)`)

// envLine is NAME=value (docker inspect's Env) or NAME: value (kubectl describe), with the quote
// and indent around it.
var envLine = regexp.MustCompile(`^(\s*"?)([A-Za-z_][A-Za-z0-9_.-]*)(=|:\s+)(.+?)("?,?)$`)

// redact hides the values of secret-looking variables in inspect and describe output.
func redact(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if m := envLine.FindStringSubmatch(l); m != nil && secretish.MatchString(m[2]) && !strings.HasPrefix(m[4], "<set to the key") {
			lines[i] = m[1] + m[2] + m[3] + "•••" + m[5]
		}
	}
	return strings.Join(lines, "\n")
}
