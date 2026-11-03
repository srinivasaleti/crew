package containers

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	ctr "crew/internal/containers"
	"crew/internal/core"
	"crew/internal/ui/web/api"
)

// KUBERNETES: a tab's objects in every namespace, read with kubectl in crew's own context (passed as
// --context on every call, so your kubeconfig never changes), an object's logs and describe output,
// the changes, Cleanup (off on production), and a chat locked down to crew ctr. On a production
// context every change needs the context's name typed, and the server checks it, not just the page.
func init() { api.Register(api.Module{Key: "kube", Routes: kubeRoutes}) }

// kubeContext is the context crew uses while the web UI runs: "" until the first read picks
// kubectl's current one.
var kubeContext struct {
	sync.Mutex
	name string
}

// prodOK reports whether a change on kctx may go: anywhere but production, or with its name typed.
func prodOK(kctx, typed string) error {
	if ctr.IsProd(kctx) && strings.TrimSpace(typed) != kctx {
		return fmt.Errorf("%s is production: type its name to go ahead", kctx)
	}
	return nil
}

// kubeRoutes are KUBERNETES' calls.
func kubeRoutes(c *core.Core, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kube", func(w http.ResponseWriter, r *http.Request) {
		if why := missing("kubectl"); why != "" {
			api.Reply(w, map[string]string{"error": why, "missing": "kubectl"})
			return
		}
		q := r.URL.Query()
		tab := 0
		fmt.Sscan(q.Get("tab"), &tab)
		if tab < 0 || tab >= len(ctr.KubeKinds) {
			tab = 0
		}
		kubeContext.Lock()
		if q.Get("context") != "" {
			kubeContext.name = q.Get("context")
		}
		kctx := kubeContext.name
		kubeContext.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		d, err := ctr.LoadKube(ctx, kctx, tab)
		if kctx == "" && d.Context != "" {
			kubeContext.Lock()
			kubeContext.name = d.Context
			kubeContext.Unlock()
		}
		v := kubeView(d)
		if err != nil {
			v["error"] = err.Error()
		}
		api.Reply(w, v)
	})
	mux.HandleFunc("GET /api/kube/thing", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("context") == "" {
			api.Fail(w, fmt.Errorf("say which context"), 400)
			return
		}
		out, err := ctr.Read(r.Context(), q.Get("kind"), q.Get("name"), q.Get("ns"), q.Get("context"), q.Get("logs") != "")
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"out": out})
	})
	mux.HandleFunc("POST /api/kube/act", func(w http.ResponseWriter, r *http.Request) {
		var a struct{ Context, Kind, NS, Name, Action, Confirm string }
		if !api.Body(w, r, &a) {
			return
		}
		var o ctr.KObj
		o.Kind, o.Metadata.Namespace, o.Metadata.Name = a.Kind, a.NS, a.Name
		args := ctr.KubeArgs(o, a.Action)
		switch {
		case a.Context == "":
			api.Fail(w, fmt.Errorf("say which context"), 400)
			return
		case args == nil:
			api.Fail(w, fmt.Errorf("%s has no %s", strings.ToLower(a.Kind), a.Action), 400)
			return
		}
		if err := prodOK(a.Context, a.Confirm); err != nil {
			api.Fail(w, err, 403)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if _, err := ctr.Kubectl(ctx, a.Context, args...); err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/kube/cleanup", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		kctx := q.Get("context")
		if ctr.IsProd(kctx) {
			keepCleanup(w, "kube", "Cleanup · "+kctx, kctx+" is production. Cleanup is off here: clean it up through your pipeline. Deleting one pod still works, after asking.", nil, nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		l := ctr.LoadLeftovers(ctx, kctx)
		if l.Err != nil {
			keepCleanup(w, "kube", "Cleanup · "+kctx, l.Err.Error(), nil, nil)
			return
		}
		nss := l.Namespaces()
		if len(nss) == 0 {
			keepCleanup(w, "kube", "Cleanup · "+kctx, "Nothing to clean up.", nil, nil)
			return
		}
		ns := q.Get("ns")
		if ns == "" || !slices.Contains(nss, ns) {
			ns = nss[0]
		}
		keepCleanup(w, "kube", "Cleanup · "+kctx+" · "+ns, "", nss, l.Cleanup(ns))
	})
	mux.HandleFunc("POST /api/kube/cleanup", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Context string
			Labels  []string
		}
		if !api.Body(w, r, &p) {
			return
		}
		if ctr.IsProd(p.Context) {
			api.Fail(w, fmt.Errorf("Cleanup is off on production"), 403)
			return
		}
		runCleanup(r.Context(), w, "kube", p.Labels)
	})
	mux.HandleFunc("POST /api/kube/chat", func(w http.ResponseWriter, r *http.Request) {
		var t struct{ Context, Kind, NS, Name string }
		if !api.Body(w, r, &t) {
			return
		}
		ch, err := thingChat(c, "k:"+t.Context+"/"+t.Kind+"/"+t.NS+"/"+t.Name, t.Kind, t.Name, t.Name, t.NS, t.Context)
		if err != nil {
			api.Fail(w, err, 500)
			return
		}
		api.Reply(w, map[string]string{"id": ch.ID})
	})
}

// kubeView is what the page shows of one tab: rows with what kubectl get would print (ready,
// status, restarts…) worked out here, the contexts, and whether this one is production.
func kubeView(d ctr.KubeData) map[string]any {
	type row struct {
		Kind, Name, NS, Status, Ready, Node, Type, IP, Ports, From, Reason, Object, Message string
		Restarts, Count                                                                     int
		Healthy, Bad, Warning, Logs                                                         bool
		Created, Last                                                                       time.Time
		Keys                                                                                []string
		Port                                                                                int
	}
	var rows []row
	for _, o := range d.Objs {
		x := row{Kind: o.Kind, Name: o.Metadata.Name, NS: o.Metadata.Namespace, Created: o.Metadata.CreationTimestamp, Logs: ctr.HasLogs(o.Kind), Port: o.Port()}
		switch o.Kind {
		case "Pod":
			x.Status, x.Ready, x.Restarts, x.Node, x.Healthy = o.PodStatus(), o.PodReady(), o.Restarts(), o.Spec.NodeName, o.Healthy()
			x.Bad = !x.Healthy
		case "Deployment":
			want := 1
			if o.Spec.Replicas != nil {
				want = *o.Spec.Replicas
			}
			x.Ready = fmt.Sprintf("%d/%d", o.Status.ReadyReplicas, want)
			x.Status = fmt.Sprintf("%d up to date · %d available", o.Status.UpdatedReplicas, o.Status.AvailableReplicas)
			x.Bad, x.Healthy = o.Status.ReadyReplicas < want, o.Status.ReadyReplicas >= want
		case "Service":
			var ps []string
			for _, p := range o.Spec.Ports {
				s := fmt.Sprint(p.Port)
				if p.NodePort > 0 {
					s += fmt.Sprintf(":%d", p.NodePort)
				}
				ps = append(ps, s+"/"+p.Protocol)
			}
			x.Type, x.IP, x.Ports = o.Spec.Type, o.Spec.ClusterIP, strings.Join(ps, ",")
		case "Job":
			want := 1
			if o.Spec.Completions != nil {
				want = *o.Spec.Completions
			}
			x.Ready, x.Status, x.From = fmt.Sprintf("%d/%d", o.Status.Succeeded, want), o.JobState(), o.Owner()
			x.Bad, x.Healthy = x.Status == "Failed", x.Status == "Running"
		case "ConfigMap", "Secret":
			x.Type, x.Keys = o.Type, api.OrEmpty(o.Keys())
		case "Event":
			x.Reason, x.Message, x.Count, x.Last, x.Warning = o.Reason, o.Message, max(o.Count, 1), o.When(), o.Type == "Warning"
			x.Object = strings.ToLower(o.InvolvedObject.Kind) + "/" + o.InvolvedObject.Name
			x.Type = o.InvolvedObject.Kind
		}
		rows = append(rows, x)
	}
	problems := 0
	for _, p := range d.Pods {
		if !p.Healthy() {
			problems++
		}
	}
	return map[string]any{"context": d.Context, "contexts": api.OrEmpty(d.Contexts), "prod": ctr.IsProd(d.Context), "tab": d.Tab,
		"objs": api.OrEmpty(rows), "pods": len(d.Pods), "problems": problems}
}
