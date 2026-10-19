package containers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/widgets/look"
)

// The section's popups: asking before a change, picking a Kubernetes context, and Cleanup. Each
// takes the keys while it's open and hands back what to run.

// popup is an open popup.
type popup interface {
	View() string
	// Key takes a key; closed reports the popup is done, and cmd is what it started.
	Key(msg tea.KeyMsg) (cmd tea.Cmd, closed bool)
}

// doneMsg is a change that ran (a restart, a removal, a cleanup): what to say, or why it failed.
// The section reads everything again after one.
type doneMsg struct {
	sec, note string
	err       error
}

// do runs a change in the background and says what happened.
func do(sec, note string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{sec: sec, note: note, err: f()} }
}

// confirm asks before a change: ⏎ runs it, esc doesn't.
type confirm struct {
	text string
	run  tea.Cmd
}

func (c *confirm) View() string {
	return look.MenuBox.Width(64).Render(c.text + "\n\n" + look.Dim.Render("⏎ yes · esc no"))
}

func (c *confirm) Key(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		return c.run, true
	case "esc", "n":
		return nil, true
	}
	return nil, false
}

// menu picks a Kubernetes context; crew uses it from then on, without changing your kubeconfig.
type menu struct {
	items []string
	sel   int
	pick  func(string) tea.Cmd
}

func (m *menu) View() string {
	var b strings.Builder
	b.WriteString(look.Bold.Render("Context") + look.Dim.Render("  crew's own; your kubeconfig doesn't change") + "\n\n")
	for i, it := range m.items {
		line := it
		if isProd(it) {
			line += look.Warn.Render("  ⚠ production")
		}
		if i == m.sel {
			b.WriteString(look.Sel.Render("▶ ") + look.Bold.Render(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return look.MenuBox.Width(56).Render(b.String() + "\n" + look.Dim.Render("⏎ switches · esc cancel"))
}

func (m *menu) Key(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up":
		m.sel = max(m.sel-1, 0)
	case "down":
		m.sel = min(m.sel+1, len(m.items)-1)
	case "enter":
		if len(m.items) == 0 {
			return nil, true
		}
		return m.pick(m.items[m.sel]), true
	case "esc":
		return nil, true
	}
	return nil, false
}

// option is one kind of thing Cleanup can remove: what it is, how much, and how to remove it.
type option struct {
	label string
	warn  string   // why it's off by default, shown next to it
	on    bool     // picked
	names []string // exactly what goes, for the second look
	size  string
	run   func(ctx context.Context) error
}

// cleanup is the Cleanup popup: pick what to remove (space), ⏎ shows exactly what goes, ⏎ again
// removes it. On KUBERNETES it works on one namespace, picked with ←/→.
type cleanup struct {
	ctx     context.Context
	sec     string
	title   string
	note    string // why there's nothing to pick, when there isn't
	opts    []option
	sel     int
	preview bool
	nss     []string                 // KUBERNETES: the namespaces to pick from
	ns      int                      // the one picked
	build   func(ns string) []option // KUBERNETES: the options in a namespace
}

// pickNS makes the options for the namespace picked.
func (c *cleanup) pickNS() {
	if c.build != nil && len(c.nss) > 0 {
		c.opts, c.sel = c.build(c.nss[c.ns]), 0
	}
}

func (c *cleanup) View() string {
	var b strings.Builder
	b.WriteString(look.Bold.Render(c.title))
	if len(c.nss) > 0 {
		b.WriteString(look.Dim.Render("   namespace ") + look.Sel.Render("‹ "+c.nss[c.ns]+" ›"))
	}
	b.WriteString("\n\n")
	if c.note != "" {
		return look.MenuBox.Width(72).Render(b.String() + c.note + "\n\n" + look.Dim.Render("esc closes"))
	}
	if c.preview {
		n := 0
		for _, o := range c.opts {
			if !o.on || len(o.names) == 0 && o.size == "" {
				continue
			}
			n += len(o.names)
			b.WriteString(look.Bold.Render(o.label) + "\n")
			for _, name := range o.names {
				b.WriteString("  " + name + "\n")
			}
			if len(o.names) == 0 {
				b.WriteString("  " + o.size + "\n")
			}
		}
		return look.MenuBox.Width(72).Render(b.String() + "\n" + look.Warn.Render(fmt.Sprintf("Remove these %d now?", n)) +
			look.Dim.Render("  ⏎ yes · esc back"))
	}
	for i, o := range c.opts {
		box := look.Ready.Render("[x]")
		if !o.on {
			box = look.Dim.Render("[ ]")
		}
		count := ""
		if len(o.names) > 0 || o.size == "" {
			count = fmt.Sprintf("%d", len(o.names))
		}
		line := fmt.Sprintf("%s %-36s %5s  %9s", box, o.label, count, o.size)
		if o.warn != "" {
			line += look.Orange.Render("  " + o.warn)
		}
		if i == c.sel {
			line = look.Sel.Render("▶ ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	keys := "space toggles · ⏎ shows exactly what goes · esc cancel"
	if len(c.nss) > 1 {
		keys = "←/→ namespace · " + keys
	}
	return look.MenuBox.Width(72).Render(b.String() + "\n" + look.Dim.Render(keys))
}

func (c *cleanup) Key(msg tea.KeyMsg) (tea.Cmd, bool) {
	k := msg.String()
	if k == "esc" {
		if c.preview {
			c.preview = false
			return nil, false
		}
		return nil, true
	}
	if c.note != "" {
		return nil, false
	}
	if c.preview {
		if k != "enter" {
			return nil, false
		}
		var runs []func(context.Context) error
		var what []string
		for _, o := range c.opts {
			if o.on && (len(o.names) > 0 || o.size != "") {
				runs, what = append(runs, o.run), append(what, strings.ToLower(o.label))
			}
		}
		ctx := c.ctx
		return do(c.sec, "Cleaned up "+strings.Join(what, ", "), func() error {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			var errs []string
			for _, r := range runs {
				if err := r(ctx); err != nil {
					errs = append(errs, err.Error())
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("%s", strings.Join(errs, "; "))
			}
			return nil
		}), true
	}
	switch k {
	case "up":
		c.sel = max(c.sel-1, 0)
	case "down":
		c.sel = min(c.sel+1, len(c.opts)-1)
	case " ":
		if c.sel < len(c.opts) {
			c.opts[c.sel].on = !c.opts[c.sel].on
		}
	case "left", "right":
		if len(c.nss) > 1 {
			c.ns = (c.ns + map[string]int{"left": -1, "right": 1}[k] + len(c.nss)) % len(c.nss)
			c.pickNS()
		}
	case "enter":
		if slices.ContainsFunc(c.opts, func(o option) bool { return o.on && (len(o.names) > 0 || o.size != "") }) {
			c.preview = true
		}
	}
	return nil, false
}

// dockerCleanup is Cleanup's options for Docker: stopped containers, dangling and unused images,
// the build cache, unused volumes and networks. What can't be undone easily (images you may want,
// volumes, which are usually data) starts unpicked. It removes exactly what it listed, by id.
func dockerCleanup(ctx context.Context, d dockerData) *cleanup {
	var stopped, dangling, unused, vols, nets []string
	var stoppedIDs, danglingIDs, unusedIDs, volIDs, netIDs []string
	var dSize, uSize, vSize float64
	for _, c := range d.containers {
		if !c.State.Running {
			stopped, stoppedIDs = append(stopped, c.name()), append(stoppedIDs, c.ID)
		}
	}
	for _, im := range d.images {
		switch {
		case len(d.imageUsers(im)) > 0:
		case im.dangling():
			dangling, danglingIDs = append(dangling, im.label()+"  "+im.Size), append(danglingIDs, im.ID)
			dSize += parseSize(im.Size)
		default:
			unused, unusedIDs = append(unused, im.label()+"  "+im.Size), append(unusedIDs, im.ID)
			uSize += parseSize(im.Size)
		}
	}
	for _, v := range d.volumes {
		if len(d.volumeUsers(v)) == 0 {
			vols, volIDs = append(vols, v.Name+"  "+v.Size), append(volIDs, v.Name)
			vSize += parseSize(v.Size)
		}
	}
	for _, n := range d.networks {
		if !n.builtin() && len(d.networkUsers(n)) == 0 {
			nets, netIDs = append(nets, n.Name), append(netIDs, n.ID)
		}
	}
	rm := func(args []string, ids []string) func(context.Context) error {
		return func(ctx context.Context) error {
			if len(ids) == 0 {
				return nil
			}
			_, err := run(ctx, "docker", append(args, ids...)...)
			return err
		}
	}
	sizeOf := func(b float64) string {
		if b == 0 {
			return ""
		}
		return size(b)
	}
	cache := d.disk["Build Cache"].Reclaimable
	if parseSize(cache) == 0 {
		cache = ""
	}
	c := &cleanup{ctx: ctx, sec: "docker", title: "Docker cleanup", opts: []option{
		{label: "Stopped containers", on: true, names: stopped, run: rm([]string{"rm"}, stoppedIDs)},
		{label: "Dangling images", on: true, names: dangling, size: sizeOf(dSize), run: rm([]string{"rmi"}, danglingIDs)},
		{label: "Unused images", names: unused, size: sizeOf(uSize), warn: "you may pull them again", run: rm([]string{"rmi"}, unusedIDs)},
		{label: "Build cache", on: cache != "", size: cache, run: func(ctx context.Context) error {
			_, err := run(ctx, "docker", "builder", "prune", "-f")
			return err
		}},
		{label: "Unused volumes", names: vols, size: sizeOf(vSize), warn: "⚠ usually data", run: rm([]string{"volume", "rm"}, volIDs)},
		{label: "Unused networks", on: true, names: nets, run: rm([]string{"network", "rm"}, netIDs)},
	}}
	if dSize+uSize+vSize > 0 || cache != "" {
		c.title = "Docker cleanup · up to " + size(dSize+uSize+vSize+parseSize(cache)) + " can be freed"
	}
	return c
}

// kubeLeftovers are what Cleanup reads on KUBERNETES: pods, jobs and replica sets.
type kubeLeftovers struct {
	kctx            string
	pods, jobs, rss []kobj
	err             error
}

// loadLeftovers reads what Cleanup on KUBERNETES picks from.
func loadLeftovers(ctx context.Context, kctx string) kubeLeftovers {
	l := kubeLeftovers{kctx: kctx}
	if l.pods, l.err = getAll(ctx, kctx, "pods"); l.err != nil {
		return l
	}
	if l.jobs, l.err = getAll(ctx, kctx, "jobs"); l.err != nil {
		return l
	}
	l.rss, l.err = getAll(ctx, kctx, "replicasets")
	return l
}

// kubeCleanup is Cleanup's options for one namespace: what a cluster leaves behind (evicted,
// failed and completed pods, finished jobs, emptied replica sets), never what you deployed. It's
// off on a production context.
func kubeCleanup(ctx context.Context, l kubeLeftovers) *cleanup {
	c := &cleanup{ctx: ctx, sec: "kube", title: "Cleanup · " + l.kctx}
	switch {
	case l.err != nil:
		c.note = l.err.Error()
		return c
	case isProd(l.kctx):
		c.note = look.Warn.Render("⚠ "+l.kctx+" is production.") + " Cleanup is off here: clean it up through your pipeline.\nx on a pod still deletes one, after asking."
		return c
	}
	seen := map[string]bool{}
	for _, objs := range [][]kobj{l.pods, l.jobs, l.rss} {
		for _, o := range objs {
			if !seen[o.Metadata.Namespace] {
				seen[o.Metadata.Namespace] = true
				c.nss = append(c.nss, o.Metadata.Namespace)
			}
		}
	}
	slices.Sort(c.nss)
	if len(c.nss) == 0 {
		c.note = "Nothing to clean up."
		return c
	}
	kctx := l.kctx
	del := func(kind, ns string, names []string) func(context.Context) error {
		return func(ctx context.Context) error {
			if len(names) == 0 {
				return nil
			}
			_, err := kubectl(ctx, kctx, append([]string{"delete", kind, "-n", ns, "--wait=false"}, names...)...)
			return err
		}
	}
	c.build = func(ns string) []option {
		var evicted, failed, done, jobs, rss []string
		for _, p := range l.pods {
			switch {
			case p.Metadata.Namespace != ns:
			case p.Status.Reason == "Evicted":
				evicted = append(evicted, p.Metadata.Name)
			case p.Status.Phase == "Failed":
				failed = append(failed, p.Metadata.Name)
			case p.Status.Phase == "Succeeded" && p.owner() != "Job": // a job's pods go with the job
				done = append(done, p.Metadata.Name)
			}
		}
		for _, j := range l.jobs {
			finished := j.Status.Active == 0 && j.Status.CompletionTime != nil && time.Since(*j.Status.CompletionTime) > 24*time.Hour
			if j.Metadata.Namespace == ns && j.owner() != "CronJob" && (finished || j.jobState() == "Failed" && time.Since(j.Metadata.CreationTimestamp) > 24*time.Hour) {
				jobs = append(jobs, j.Metadata.Name)
			}
		}
		for _, r := range l.rss {
			if r.Metadata.Namespace == ns && r.owner() == "Deployment" && r.Spec.Replicas != nil && *r.Spec.Replicas == 0 {
				rss = append(rss, r.Metadata.Name)
			}
		}
		return []option{
			{label: "Evicted pods", on: true, names: evicted, run: del("pod", ns, evicted)},
			{label: "Failed pods", on: true, names: failed, run: del("pod", ns, failed)},
			{label: "Completed pods (not from a Job)", on: true, names: done, run: del("pod", ns, done)},
			{label: "Finished jobs older than a day", on: true, names: jobs, warn: "not a CronJob's", run: del("job", ns, jobs)},
			{label: "Old replica sets (0 replicas)", names: rss, warn: "rollback history", run: del("replicaset", ns, rss)},
		}
	}
	c.pickNS()
	return c
}
