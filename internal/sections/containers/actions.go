package containers

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/section"
	"crew/internal/widgets/look"
)

// What the tables' keys do: DOCKER's on containers, images, volumes and networks, KUBERNETES' on
// its objects. A removal asks first, and so does every change on a production context.

// dockerAct does a key's action on a row of DOCKER's open tab.
func (s *Section) dockerAct(h section.Host, id, action string) tea.Cmd {
	ctx, d := h.Ctx(), s.docker
	docker := func(note string, args ...string) tea.Cmd {
		return do("docker", note, func() error { _, err := run(ctx, "docker", args...); return err })
	}
	switch s.tab {
	case 0:
		i := slices.IndexFunc(d.containers, func(c container) bool { return c.ID == id })
		if i < 0 {
			return nil
		}
		c := d.containers[i]
		name := c.name()
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Container", id: c.ID, name: name}, action)
		case Shell:
			if !c.State.Running {
				h.Notice(name + " isn't running · ⏎ shows its logs, S starts it")
				return nil
			}
			return h.RunShell("docker exec -it "+c.ID+" "+anyShell, "a shell in "+name)
		case Restart:
			h.Notice("Restarting " + name + "…")
			return docker("Restarted "+name, "restart", c.ID)
		case Stop:
			h.Notice("Stopping " + name + "…")
			return docker("Stopped "+name, "stop", c.ID)
		case Start:
			return docker("Started "+name, "start", c.ID)
		case Remove:
			text := "Remove container " + look.Bold.Render(name) + "?"
			if c.State.Running {
				text += "\nIt's running; it's stopped first."
			}
			return s.ask(h, text, docker("Removed "+name, "rm", "-f", c.ID))
		}
	case 1:
		i := slices.IndexFunc(d.images, func(im image) bool { return im.ID == id })
		if i < 0 {
			return nil
		}
		im := d.images[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Image", id: im.ID, name: im.label()}, action)
		case Remove:
			if by := d.imageUsers(im); len(by) > 0 {
				h.Notice(im.label() + " is used by " + strings.Join(by, ", ") + " · remove those first")
				return nil
			}
			return s.ask(h, "Remove image "+look.Bold.Render(im.label())+" ("+im.Size+")?", docker("Removed "+im.label(), "rmi", im.ID))
		}
	case 2:
		i := slices.IndexFunc(d.volumes, func(v volume) bool { return v.Name == id })
		if i < 0 {
			return nil
		}
		v := d.volumes[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Volume", id: v.Name, name: v.Name}, action)
		case Remove:
			if by := d.volumeUsers(v); len(by) > 0 {
				h.Notice(v.Name + " is used by " + strings.Join(by, ", ") + " · remove those first")
				return nil
			}
			return s.ask(h, look.Warn.Render("⚠ Remove volume ")+look.Bold.Render(v.Name)+look.Warn.Render(" and the data in it?")+
				"\nA volume usually holds a database or other data; this can't be undone.", docker("Removed volume "+v.Name, "volume", "rm", v.Name))
		}
	case 3:
		i := slices.IndexFunc(d.networks, func(n network) bool { return n.ID == id })
		if i < 0 {
			return nil
		}
		n := d.networks[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Network", id: n.ID, name: n.Name}, action)
		case Remove:
			switch by := d.networkUsers(n); {
			case n.builtin():
				h.Notice(n.Name + " is Docker's own network; it can't be removed")
				return nil
			case len(by) > 0:
				h.Notice(n.Name + " is used by " + strings.Join(by, ", ") + " · remove those first")
				return nil
			}
			return s.ask(h, "Remove network "+look.Bold.Render(n.Name)+"?", docker("Removed network "+n.Name, "network", "rm", n.ID))
		}
	}
	return nil
}

// anyShell starts bash in a container when it has it, else sh.
const anyShell = `sh -c 'command -v bash >/dev/null && exec bash || exec sh'`

// kubeAct does a key's action on a row of KUBERNETES' open tab.
func (s *Section) kubeAct(h section.Host, id, action string) tea.Cmd {
	i := slices.IndexFunc(s.kd.objs, func(o kobj) bool { return o.Kind+"/"+o.Metadata.Namespace+"/"+o.Metadata.Name == id })
	if i < 0 {
		return nil
	}
	o, ctx, kctx := s.kd.objs[i], h.Ctx(), s.kd.kctx
	ns, name := o.Metadata.Namespace, o.Metadata.Name
	if o.Kind == "Event" && action == Open { // an event opens what it's about
		o.Kind, name = o.InvolvedObject.Kind, o.InvolvedObject.Name
	}
	prod := isProd(kctx)
	where := " in " + ns
	if prod {
		where += look.Warn.Render(" on " + kctx + " (production)")
	}
	kube := func(note string, args ...string) tea.Cmd {
		return do("kube", note, func() error { _, err := kubectl(ctx, kctx, args...); return err })
	}
	base := "kubectl --context " + quote(kctx) + " -n " + quote(ns) + " "
	switch action {
	case Open, Ask:
		return s.openThing(h, &thing{kind: o.Kind, id: name, name: name, ns: ns, kctx: kctx}, action)
	case Shell:
		if o.podStatus() != "Running" {
			h.Notice(name + " isn't running · ⏎ shows its logs and why")
			return nil
		}
		run := h.RunShell(base+"exec -it "+quote(name)+" -- "+anyShell, "a shell in "+name)
		if prod {
			return s.ask(h, look.Warn.Render("⚠ "+kctx+" is production.")+"\nOpen a shell in "+look.Bold.Render(name)+where+"?", run)
		}
		return run
	case Forward:
		port := o.port()
		if port == 0 {
			h.Notice(name + " doesn't say which port it listens on · forward it from a shell with kubectl port-forward")
			return nil
		}
		return h.RunShell(base+fmt.Sprintf("port-forward %s %d:%d", ref(o), port, port),
			fmt.Sprintf("localhost:%d goes to %s · close the shell (⌘W) to stop", port, name))
	case Restart:
		note := "Restarting " + name + "'s pods, one by one"
		if prod {
			return s.ask(h, look.Warn.Render("⚠ "+kctx+" is production.")+"\nRollout restart deployment "+look.Bold.Render(name)+where+"?",
				kube(note, "-n", ns, "rollout", "restart", "deployment/"+name))
		}
		return kube(note, "-n", ns, "rollout", "restart", "deployment/"+name)
	case Remove:
		what := map[string]string{"Pod": "pod", "Job": "job"}[o.Kind]
		if what == "" {
			return nil
		}
		text := "Delete " + what + " " + look.Bold.Render(name) + where + "?"
		if o.Kind == "Pod" && o.owner() != "" {
			text += "\nIts " + o.owner() + " makes a new one."
		}
		if prod {
			text = look.Warn.Render("⚠ "+kctx+" is production.") + "\n" + text
		}
		return s.ask(h, text, kube("Deleted "+what+" "+name, "-n", ns, "delete", what, name, "--wait=false"))
	}
	return nil
}

// quote quotes a word for sh.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
