package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Kubernetes, read with kubectl. crew keeps its own choice of context and passes --context on
// every call, so switching here never changes your kubeconfig (or another terminal's kubectl).

// kubectl runs kubectl against context kctx.
func kubectl(ctx context.Context, kctx string, args ...string) (string, error) {
	if kctx != "" {
		args = append([]string{"--context", kctx}, args...)
	}
	return run(ctx, "kubectl", args...)
}

// prodName is what makes a context production: its name, unless CREW_PROD_CONTEXTS lists them.
var prodName = regexp.MustCompile(`(?i)prod`)

// isProd reports whether a context is production: changes there ask first, and Cleanup is off.
// CREW_PROD_CONTEXTS (comma-separated names) adds contexts whose names don't say so.
func isProd(kctx string) bool {
	for _, n := range strings.Split(os.Getenv("CREW_PROD_CONTEXTS"), ",") {
		if strings.TrimSpace(n) == kctx && kctx != "" {
			return true
		}
	}
	return prodName.MatchString(kctx)
}

// skip reads a JSON value and keeps nothing: a Secret's data is read as its keys only, so its
// values never reach crew's memory past the decoder.
type skip struct{}

func (*skip) UnmarshalJSON([]byte) error { return nil }

// kobj is one Kubernetes object, the fields crew shows of every kind it lists.
type kobj struct {
	Kind     string
	Metadata struct {
		Name, Namespace   string
		CreationTimestamp time.Time
		OwnerReferences   []struct{ Kind, Name string }
	}
	Spec struct {
		NodeName   string
		Containers []kcontainer
		Template   struct {
			Spec struct{ Containers []kcontainer }
		} // a Deployment's or Job's pods
		Replicas    *int
		Completions *int
		Type        string
		ClusterIP   string `json:"clusterIP"`
		Ports       []struct {
			Port     int
			NodePort int
			Protocol string
		}
	}
	Status struct {
		Phase, Reason     string
		ContainerStatuses []struct {
			Ready        bool
			RestartCount int
			State        struct {
				Waiting    *struct{ Reason string }
				Terminated *struct{ Reason string }
			}
		}
		Replicas, ReadyReplicas, UpdatedReplicas, AvailableReplicas int
		Succeeded, Failed, Active                                   int
		CompletionTime                                              *time.Time
	}
	Data           map[string]*skip // ConfigMaps and Secrets: the keys only
	Type           string           // a Secret's type; an Event's Normal or Warning
	Reason         string           // an Event's
	Message        string
	Count          int
	LastTimestamp  *time.Time
	EventTime      *time.Time
	InvolvedObject struct{ Kind, Name string }
}

// kcontainer is a container in a pod's spec: its name and the ports it says it listens on.
type kcontainer struct {
	Name  string
	Ports []struct{ ContainerPort int }
}

// owner is the kind of what made the object (a Job, a ReplicaSet), or "".
func (o kobj) owner() string {
	if len(o.Metadata.OwnerReferences) > 0 {
		return o.Metadata.OwnerReferences[0].Kind
	}
	return ""
}

// podStatus is a pod's state as kubectl get shows it: a waiting or terminated reason
// (CrashLoopBackOff, Error), Evicted, else its phase.
func (o kobj) podStatus() string {
	for _, c := range o.Status.ContainerStatuses {
		if w := c.State.Waiting; w != nil && w.Reason != "" {
			return w.Reason
		}
	}
	if o.Status.Reason != "" {
		return o.Status.Reason
	}
	if o.Status.Phase == "Succeeded" {
		return "Completed"
	}
	return o.Status.Phase
}

// podReady is "1/2": ready containers of all.
func (o kobj) podReady() string {
	n := 0
	for _, c := range o.Status.ContainerStatuses {
		if c.Ready {
			n++
		}
	}
	return fmt.Sprintf("%d/%d", n, len(o.Spec.Containers))
}

// restarts adds up a pod's container restarts.
func (o kobj) restarts() int {
	n := 0
	for _, c := range o.Status.ContainerStatuses {
		n += c.RestartCount
	}
	return n
}

// healthy reports whether a pod is fine: running with every container ready, or completed.
func (o kobj) healthy() bool {
	s := o.podStatus()
	return s == "Completed" || s == "Running" && o.podReady() == fmt.Sprintf("%d/%d", len(o.Spec.Containers), len(o.Spec.Containers))
}

// jobState is a Job's Running, Complete or Failed.
func (o kobj) jobState() string {
	switch {
	case o.Status.Active > 0:
		return "Running"
	case o.Status.Failed > 0 && o.Status.Succeeded == 0:
		return "Failed"
	}
	return "Complete"
}

// port is the first port the object listens on, for port-forward; 0 when it has none.
func (o kobj) port() int {
	for _, c := range append(o.Spec.Containers, o.Spec.Template.Spec.Containers...) {
		for _, p := range c.Ports {
			return p.ContainerPort
		}
	}
	for _, p := range o.Spec.Ports {
		return p.Port
	}
	return 0
}

// when is when an Event last happened.
func (o kobj) when() time.Time {
	switch {
	case o.LastTimestamp != nil:
		return *o.LastTimestamp
	case o.EventTime != nil:
		return *o.EventTime
	}
	return o.Metadata.CreationTimestamp
}

// keys are a ConfigMap's or Secret's keys, sorted.
func (o kobj) keys() []string {
	var ks []string
	for k := range o.Data {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// kubeData is what KUBERNETES shows: the contexts, and one tab's objects in every namespace, with
// the pods always (the row's summary counts them).
type kubeData struct {
	kctx     string   // the context read
	contexts []string // every context in your kubeconfig
	tab      int
	objs     []kobj
	pods     []kobj
	at       time.Time
}

// kubeKinds are what each of KUBERNETES' tabs reads.
var kubeKinds = [][]string{{"pods"}, {"deployments"}, {"services"}, {"jobs"}, {"configmaps", "secrets"}, {"events"}}

// getAll reads every object of one kind in every namespace.
// ponytail: -A reads every namespace; on a big cluster, read only the facet's namespace instead.
func getAll(ctx context.Context, kctx, kind string) ([]kobj, error) {
	out, err := kubectl(ctx, kctx, "get", kind, "-A", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list struct{ Items []kobj }
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}
	single := map[string]string{"pods": "Pod", "deployments": "Deployment", "services": "Service", "jobs": "Job",
		"configmaps": "ConfigMap", "secrets": "Secret", "events": "Event", "replicasets": "ReplicaSet"}[kind]
	for i := range list.Items {
		list.Items[i].Kind = single // kubectl's list leaves it out of the items
	}
	return list.Items, nil
}

// loadKube reads the contexts and tab's objects in context kctx ("" is kubectl's current one).
func loadKube(ctx context.Context, kctx string, tab int) (kubeData, error) {
	d := kubeData{tab: tab, at: time.Now()}
	if out, err := kubectl(ctx, "", "config", "get-contexts", "-o", "name"); err == nil {
		d.contexts = strings.Fields(out)
	}
	if kctx == "" {
		cur, err := kubectl(ctx, "", "config", "current-context")
		if err != nil {
			return d, err
		}
		kctx = cur
	}
	d.kctx = kctx
	pods, err := getAll(ctx, kctx, "pods")
	if err != nil {
		return d, err
	}
	d.pods = pods
	if tab == 0 {
		d.objs = pods
		return d, nil
	}
	for _, kind := range kubeKinds[tab] {
		objs, err := getAll(ctx, kctx, kind)
		if err != nil {
			return d, err
		}
		d.objs = append(d.objs, objs...)
	}
	return d, nil
}

// ref is how kubectl names an object: pod/api-7c9f, deployment/api.
func ref(o kobj) string { return strings.ToLower(o.Kind) + "/" + o.Metadata.Name }

// hasLogs reports whether kubectl logs can read the object.
func hasLogs(kind string) bool { return kind == "Pod" || kind == "Deployment" || kind == "Job" }
