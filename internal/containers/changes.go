package containers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// What changes things, for both UIs: the docker and kubectl command behind each action, why an action
// can't be done, Cleanup's options, and reading a thing's logs or inspect output. A UI asks before a
// removal and before every change on a production context; it never runs one without your click or key.

// Actions on a thing.
const (
	Restart = "restart" // docker restart; kubectl rollout restart
	Stop    = "stop"
	Start   = "start"
	Remove  = "remove" // docker rm/rmi/volume rm/network rm; kubectl delete
)

// DockerArgs is the docker command for an action on a kind of thing (Container, Image, Volume,
// Network), or nil when the kind doesn't have it.
func DockerArgs(kind, action, id string) []string {
	switch kind + "/" + action {
	case "Container/" + Restart, "Container/" + Stop, "Container/" + Start:
		return []string{action, id}
	case "Container/" + Remove:
		return []string{"rm", "-f", id}
	case "Image/" + Remove:
		return []string{"rmi", id}
	case "Volume/" + Remove:
		return []string{"volume", "rm", id}
	case "Network/" + Remove:
		return []string{"network", "rm", id}
	}
	return nil
}

// WhyNotRemove says why a thing can't be removed (something uses it; it's Docker's own network), or "".
func (d DockerData) WhyNotRemove(kind, id string) string {
	used := func(name string, by []string) string {
		if len(by) == 0 {
			return ""
		}
		return name + " is used by " + strings.Join(by, ", ") + " · remove those first"
	}
	switch kind {
	case "Image":
		if i := slices.IndexFunc(d.Images, func(im Image) bool { return im.ID == id }); i >= 0 {
			return used(d.Images[i].Label(), d.ImageUsers(d.Images[i]))
		}
	case "Volume":
		if i := slices.IndexFunc(d.Volumes, func(v Volume) bool { return v.Name == id }); i >= 0 {
			return used(id, d.VolumeUsers(d.Volumes[i]))
		}
	case "Network":
		if i := slices.IndexFunc(d.Networks, func(n Network) bool { return n.ID == id }); i >= 0 {
			if n := d.Networks[i]; n.Builtin() {
				return n.Name + " is Docker's own network; it can't be removed"
			}
			return used(d.Networks[i].Name, d.NetworkUsers(d.Networks[i]))
		}
	}
	return ""
}

// KubeArgs is the kubectl command for an action on an object (without --context), or nil when its
// kind doesn't have it: a Deployment restarts, a Pod or a Job is deleted.
func KubeArgs(o KObj, action string) []string {
	ns, name := o.Metadata.Namespace, o.Metadata.Name
	switch {
	case action == Restart && o.Kind == "Deployment":
		return []string{"-n", ns, "rollout", "restart", "deployment/" + name}
	case action == Remove && (o.Kind == "Pod" || o.Kind == "Job"):
		return []string{"-n", ns, "delete", strings.ToLower(o.Kind), name, "--wait=false"}
	}
	return nil
}

// Read is a thing's logs (the last 500 lines), or what inspect or describe says about it with
// secret-looking values hidden. kctx is "" for Docker.
func Read(ctx context.Context, kind, id, ns, kctx string, logs bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch {
	case kctx != "" && logs:
		return Kubectl(ctx, kctx, "-n", ns, "logs", strings.ToLower(kind)+"/"+id, "--all-containers", "--prefix", "--tail", "500")
	case kctx != "":
		out, err := Kubectl(ctx, kctx, "-n", ns, "describe", strings.ToLower(kind), id)
		return Redact(out), err
	case logs:
		return Run(ctx, "docker", "logs", "--tail", "500", "--timestamps", id)
	}
	out, err := Run(ctx, "docker", strings.ToLower(kind), "inspect", id)
	return Redact(out), err
}

// Option is one thing Cleanup can remove: what it is, exactly which ones, and how.
type Option struct {
	Label string
	Warn  string   // why it's off by default
	On    bool     // picked to start with
	Names []string // exactly what goes, for the second look
	Size  string
	Run   func(ctx context.Context) error
}

// Has reports whether the option would remove anything.
func (o Option) Has() bool { return len(o.Names) > 0 || o.Size != "" }

// DockerCleanup is Cleanup's options for Docker: stopped containers, dangling and unused images,
// the build cache, unused volumes and networks. What can't be undone easily (images you may want,
// volumes, which are usually data) starts unpicked. It removes exactly what it listed, by id.
func DockerCleanup(d DockerData) (title string, opts []Option) {
	var stopped, dangling, unused, vols, nets []string
	var stoppedIDs, danglingIDs, unusedIDs, volIDs, netIDs []string
	var dSize, uSize, vSize float64
	for _, c := range d.Containers {
		if !c.State.Running {
			stopped, stoppedIDs = append(stopped, c.Label()), append(stoppedIDs, c.ID)
		}
	}
	for _, im := range d.Images {
		switch {
		case len(d.ImageUsers(im)) > 0:
		case im.Dangling():
			dangling, danglingIDs = append(dangling, im.Label()+"  "+im.Size), append(danglingIDs, im.ID)
			dSize += ParseSize(im.Size)
		default:
			unused, unusedIDs = append(unused, im.Label()+"  "+im.Size), append(unusedIDs, im.ID)
			uSize += ParseSize(im.Size)
		}
	}
	for _, v := range d.Volumes {
		if len(d.VolumeUsers(v)) == 0 {
			vols, volIDs = append(vols, v.Name+"  "+v.Size), append(volIDs, v.Name)
			vSize += ParseSize(v.Size)
		}
	}
	for _, n := range d.Networks {
		if !n.Builtin() && len(d.NetworkUsers(n)) == 0 {
			nets, netIDs = append(nets, n.Name), append(netIDs, n.ID)
		}
	}
	rm := func(args []string, ids []string) func(context.Context) error {
		return func(ctx context.Context) error {
			if len(ids) == 0 {
				return nil
			}
			_, err := Run(ctx, "docker", append(args, ids...)...)
			return err
		}
	}
	sizeOf := func(b float64) string {
		if b == 0 {
			return ""
		}
		return Size(b)
	}
	cache := d.Disk["Build Cache"].Reclaimable
	if ParseSize(cache) == 0 {
		cache = ""
	}
	opts = []Option{
		{Label: "Stopped containers", On: true, Names: stopped, Run: rm([]string{"rm"}, stoppedIDs)},
		{Label: "Dangling images", On: true, Names: dangling, Size: sizeOf(dSize), Run: rm([]string{"rmi"}, danglingIDs)},
		{Label: "Unused images", Names: unused, Size: sizeOf(uSize), Warn: "you may pull them again", Run: rm([]string{"rmi"}, unusedIDs)},
		{Label: "Build cache", On: cache != "", Size: cache, Run: func(ctx context.Context) error {
			_, err := Run(ctx, "docker", "builder", "prune", "-f")
			return err
		}},
		{Label: "Unused volumes", Names: vols, Size: sizeOf(vSize), Warn: "⚠ usually data", Run: rm([]string{"volume", "rm"}, volIDs)},
		{Label: "Unused networks", On: true, Names: nets, Run: rm([]string{"network", "rm"}, netIDs)},
	}
	title = "Docker cleanup"
	if dSize+uSize+vSize > 0 || cache != "" {
		title = "Docker cleanup · up to " + Size(dSize+uSize+vSize+ParseSize(cache)) + " can be freed"
	}
	return title, opts
}

// KubeLeftovers are what Cleanup reads on KUBERNETES: pods, jobs and replica sets.
type KubeLeftovers struct {
	Context         string
	Pods, Jobs, RSs []KObj
	Err             error
}

// LoadLeftovers reads what Cleanup on KUBERNETES picks from.
func LoadLeftovers(ctx context.Context, kctx string) KubeLeftovers {
	l := KubeLeftovers{Context: kctx}
	if l.Pods, l.Err = GetAll(ctx, kctx, "pods"); l.Err != nil {
		return l
	}
	if l.Jobs, l.Err = GetAll(ctx, kctx, "jobs"); l.Err != nil {
		return l
	}
	l.RSs, l.Err = GetAll(ctx, kctx, "replicasets")
	return l
}

// Namespaces are the namespaces the leftovers are in, sorted.
func (l KubeLeftovers) Namespaces() []string {
	var nss []string
	for _, objs := range [][]KObj{l.Pods, l.Jobs, l.RSs} {
		for _, o := range objs {
			if !slices.Contains(nss, o.Metadata.Namespace) {
				nss = append(nss, o.Metadata.Namespace)
			}
		}
	}
	slices.Sort(nss)
	return nss
}

// Cleanup is Cleanup's options for one namespace: what a cluster leaves behind (evicted, failed and
// completed pods, finished jobs, emptied replica sets), never what you deployed. A UI turns it off on
// a production context (IsProd).
func (l KubeLeftovers) Cleanup(ns string) []Option {
	kctx := l.Context
	del := func(kind string, names []string) func(context.Context) error {
		return func(ctx context.Context) error {
			if len(names) == 0 {
				return nil
			}
			_, err := Kubectl(ctx, kctx, append([]string{"delete", kind, "-n", ns, "--wait=false"}, names...)...)
			return err
		}
	}
	var evicted, failed, done, jobs, rss []string
	for _, p := range l.Pods {
		switch {
		case p.Metadata.Namespace != ns:
		case p.Status.Reason == "Evicted":
			evicted = append(evicted, p.Metadata.Name)
		case p.Status.Phase == "Failed":
			failed = append(failed, p.Metadata.Name)
		case p.Status.Phase == "Succeeded" && p.Owner() != "Job": // a job's pods go with the job
			done = append(done, p.Metadata.Name)
		}
	}
	for _, j := range l.Jobs {
		finished := j.Status.Active == 0 && j.Status.CompletionTime != nil && time.Since(*j.Status.CompletionTime) > 24*time.Hour
		if j.Metadata.Namespace == ns && j.Owner() != "CronJob" && (finished || j.JobState() == "Failed" && time.Since(j.Metadata.CreationTimestamp) > 24*time.Hour) {
			jobs = append(jobs, j.Metadata.Name)
		}
	}
	for _, r := range l.RSs {
		if r.Metadata.Namespace == ns && r.Owner() == "Deployment" && r.Spec.Replicas != nil && *r.Spec.Replicas == 0 {
			rss = append(rss, r.Metadata.Name)
		}
	}
	return []Option{
		{Label: "Evicted pods", On: true, Names: evicted, Run: del("pod", evicted)},
		{Label: "Failed pods", On: true, Names: failed, Run: del("pod", failed)},
		{Label: "Completed pods (not from a Job)", On: true, Names: done, Run: del("pod", done)},
		{Label: "Finished jobs older than a day", On: true, Names: jobs, Warn: "not a CronJob's", Run: del("job", jobs)},
		{Label: "Old replica sets (0 replicas)", Names: rss, Warn: "rollback history", Run: del("replicaset", rss)},
	}
}

// RunAll runs the picked options and joins what failed.
func RunAll(ctx context.Context, opts []Option) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var errs []string
	for _, o := range opts {
		if err := o.Run(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
