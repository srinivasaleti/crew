package containers

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	ctr "crew/internal/containers"
	"crew/internal/ui/terminal/section"
	"crew/internal/ui/terminal/widgets/look"
)

// What the tables' keys do: DOCKER's on containers, images, volumes and networks, KUBERNETES' on
// its objects. A removal asks first, and so does every change on a production context.

// dockerAct does a key's action on a row of DOCKER's open tab.
func (s *Section) dockerAct(h section.Host, id, action string) tea.Cmd {
	ctx, d := h.Ctx(), s.docker
	docker := func(note string, args ...string) tea.Cmd {
		return do("docker", note, func() error { _, err := ctr.Run(ctx, "docker", args...); return err })
	}
	switch s.tab {
	case 0:
		i := slices.IndexFunc(d.Containers, func(c ctr.Container) bool { return c.ID == id })
		if i < 0 {
			return nil
		}
		c := d.Containers[i]
		name := c.Label()
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
			return docker("Restarted "+name, ctr.DockerArgs("Container", ctr.Restart, c.ID)...)
		case Stop:
			h.Notice("Stopping " + name + "…")
			return docker("Stopped "+name, ctr.DockerArgs("Container", ctr.Stop, c.ID)...)
		case Start:
			return docker("Started "+name, ctr.DockerArgs("Container", ctr.Start, c.ID)...)
		case Remove:
			text := "Remove container " + look.Bold.Render(name) + "?"
			if c.State.Running {
				text += "\nIt's running; it's stopped first."
			}
			return s.ask(h, text, docker("Removed "+name, ctr.DockerArgs("Container", ctr.Remove, c.ID)...))
		}
	case 1:
		i := slices.IndexFunc(d.Images, func(im ctr.Image) bool { return im.ID == id })
		if i < 0 {
			return nil
		}
		im := d.Images[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Image", id: im.ID, name: im.Label()}, action)
		case Remove:
			if why := d.WhyNotRemove("Image", im.ID); why != "" {
				h.Notice(why)
				return nil
			}
			return s.ask(h, "Remove image "+look.Bold.Render(im.Label())+" ("+im.Size+")?", docker("Removed "+im.Label(), ctr.DockerArgs("Image", ctr.Remove, im.ID)...))
		}
	case 2:
		i := slices.IndexFunc(d.Volumes, func(v ctr.Volume) bool { return v.Name == id })
		if i < 0 {
			return nil
		}
		v := d.Volumes[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Volume", id: v.Name, name: v.Name}, action)
		case Remove:
			if why := d.WhyNotRemove("Volume", v.Name); why != "" {
				h.Notice(why)
				return nil
			}
			return s.ask(h, look.Warn.Render("⚠ Remove volume ")+look.Bold.Render(v.Name)+look.Warn.Render(" and the data in it?")+
				"\nA volume usually holds a database or other data; this can't be undone.", docker("Removed volume "+v.Name, ctr.DockerArgs("Volume", ctr.Remove, v.Name)...))
		}
	case 3:
		i := slices.IndexFunc(d.Networks, func(n ctr.Network) bool { return n.ID == id })
		if i < 0 {
			return nil
		}
		n := d.Networks[i]
		switch action {
		case Open, Ask:
			return s.openThing(h, &thing{kind: "Network", id: n.ID, name: n.Name}, action)
		case Remove:
			if why := d.WhyNotRemove("Network", n.ID); why != "" {
				h.Notice(why)
				return nil
			}
			return s.ask(h, "Remove network "+look.Bold.Render(n.Name)+"?", docker("Removed network "+n.Name, ctr.DockerArgs("Network", ctr.Remove, n.ID)...))
		}
	}
	return nil
}

// anyShell starts bash in a container when it has it, else sh.
const anyShell = `sh -c 'command -v bash >/dev/null && exec bash || exec sh'`

// kubeAct does a key's action on a row of KUBERNETES' open tab.
func (s *Section) kubeAct(h section.Host, id, action string) tea.Cmd {
	i := slices.IndexFunc(s.kd.Objs, func(o ctr.KObj) bool { return o.Kind+"/"+o.Metadata.Namespace+"/"+o.Metadata.Name == id })
	if i < 0 {
		return nil
	}
	o, ctx, kctx := s.kd.Objs[i], h.Ctx(), s.kd.Context
	ns, name := o.Metadata.Namespace, o.Metadata.Name
	if o.Kind == "Event" && action == Open { // an event opens what it's about
		o.Kind, name = o.InvolvedObject.Kind, o.InvolvedObject.Name
	}
	prod := ctr.IsProd(kctx)
	where := " in " + ns
	if prod {
		where += look.Warn.Render(" on " + kctx + " (production)")
	}
	kube := func(note string, args ...string) tea.Cmd {
		return do("kube", note, func() error { _, err := ctr.Kubectl(ctx, kctx, args...); return err })
	}
	base := "kubectl --context " + quote(kctx) + " -n " + quote(ns) + " "
	switch action {
	case Open, Ask:
		return s.openThing(h, &thing{kind: o.Kind, id: name, name: name, ns: ns, kctx: kctx}, action)
	case Shell:
		if o.PodStatus() != "Running" {
			h.Notice(name + " isn't running · ⏎ shows its logs and why")
			return nil
		}
		run := h.RunShell(base+"exec -it "+quote(name)+" -- "+anyShell, "a shell in "+name)
		if prod {
			return s.ask(h, look.Warn.Render("⚠ "+kctx+" is production.")+"\nOpen a shell in "+look.Bold.Render(name)+where+"?", run)
		}
		return run
	case Forward:
		port := o.Port()
		if port == 0 {
			h.Notice(name + " doesn't say which port it listens on · forward it from a shell with kubectl port-forward")
			return nil
		}
		return h.RunShell(base+fmt.Sprintf("port-forward %s %d:%d", ctr.Ref(o), port, port),
			fmt.Sprintf("localhost:%d goes to %s · close the shell (⌘W) to stop", port, name))
	case Restart:
		note := "Restarting " + name + "'s pods, one by one"
		if prod {
			return s.ask(h, look.Warn.Render("⚠ "+kctx+" is production.")+"\nRollout restart deployment "+look.Bold.Render(name)+where+"?",
				kube(note, ctr.KubeArgs(o, ctr.Restart)...))
		}
		return kube(note, ctr.KubeArgs(o, ctr.Restart)...)
	case Remove:
		what := map[string]string{"Pod": "pod", "Job": "job"}[o.Kind]
		if what == "" {
			return nil
		}
		text := "Delete " + what + " " + look.Bold.Render(name) + where + "?"
		if o.Kind == "Pod" && o.Owner() != "" {
			text += "\nIts " + o.Owner() + " makes a new one."
		}
		if prod {
			text = look.Warn.Render("⚠ "+kctx+" is production.") + "\n" + text
		}
		return s.ask(h, text, kube("Deleted "+what+" "+name, ctr.KubeArgs(o, ctr.Remove)...))
	}
	return nil
}

// quote quotes a word for sh.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
