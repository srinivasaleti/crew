package containers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	ctr "crew/internal/containers"
	"crew/internal/ui/terminal/widgets/look"
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
		if ctr.IsProd(it) {
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

// cleanup is the Cleanup popup: pick what to remove (space), ⏎ shows exactly what goes, ⏎ again
// removes it. On KUBERNETES it works on one namespace, picked with ←/→.
type cleanup struct {
	ctx     context.Context
	sec     string
	title   string
	note    string // why there's nothing to pick, when there isn't
	opts    []ctr.Option
	sel     int
	preview bool
	nss     []string                     // KUBERNETES: the namespaces to pick from
	ns      int                          // the one picked
	build   func(ns string) []ctr.Option // KUBERNETES: the options in a namespace
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
			if !o.On || len(o.Names) == 0 && o.Size == "" {
				continue
			}
			n += len(o.Names)
			b.WriteString(look.Bold.Render(o.Label) + "\n")
			for _, name := range o.Names {
				b.WriteString("  " + name + "\n")
			}
			if len(o.Names) == 0 {
				b.WriteString("  " + o.Size + "\n")
			}
		}
		return look.MenuBox.Width(72).Render(b.String() + "\n" + look.Warn.Render(fmt.Sprintf("Remove these %d now?", n)) +
			look.Dim.Render("  ⏎ yes · esc back"))
	}
	for i, o := range c.opts {
		box := look.Ready.Render("[x]")
		if !o.On {
			box = look.Dim.Render("[ ]")
		}
		count := ""
		if len(o.Names) > 0 || o.Size == "" {
			count = fmt.Sprintf("%d", len(o.Names))
		}
		line := fmt.Sprintf("%s %-36s %5s  %9s", box, o.Label, count, o.Size)
		if o.Warn != "" {
			line += look.Orange.Render("  " + o.Warn)
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
		var picked []ctr.Option
		var what []string
		for _, o := range c.opts {
			if o.On && o.Has() {
				picked, what = append(picked, o), append(what, strings.ToLower(o.Label))
			}
		}
		ctx := c.ctx
		return do(c.sec, "Cleaned up "+strings.Join(what, ", "), func() error { return ctr.RunAll(ctx, picked) }), true
	}
	switch k {
	case "up":
		c.sel = max(c.sel-1, 0)
	case "down":
		c.sel = min(c.sel+1, len(c.opts)-1)
	case " ":
		if c.sel < len(c.opts) {
			c.opts[c.sel].On = !c.opts[c.sel].On
		}
	case "left", "right":
		if len(c.nss) > 1 {
			c.ns = (c.ns + map[string]int{"left": -1, "right": 1}[k] + len(c.nss)) % len(c.nss)
			c.pickNS()
		}
	case "enter":
		if slices.ContainsFunc(c.opts, func(o ctr.Option) bool { return o.On && o.Has() }) {
			c.preview = true
		}
	}
	return nil, false
}

// dockerCleanup is the Cleanup popup for Docker (ctr.DockerCleanup).
func dockerCleanup(ctx context.Context, d ctr.DockerData) *cleanup {
	title, opts := ctr.DockerCleanup(d)
	return &cleanup{ctx: ctx, sec: "docker", title: title, opts: opts}
}

// kubeCleanup is the Cleanup popup for a context, one namespace at a time (ctr.KubeLeftovers.Cleanup).
// It's off on a production context.
func kubeCleanup(ctx context.Context, l ctr.KubeLeftovers) *cleanup {
	c := &cleanup{ctx: ctx, sec: "kube", title: "Cleanup · " + l.Context}
	switch {
	case l.Err != nil:
		c.note = l.Err.Error()
		return c
	case ctr.IsProd(l.Context):
		c.note = look.Warn.Render("⚠ "+l.Context+" is production.") + " Cleanup is off here: clean it up through your pipeline.\nx on a pod still deletes one, after asking."
		return c
	}
	if c.nss = l.Namespaces(); len(c.nss) == 0 {
		c.note = "Nothing to clean up."
		return c
	}
	c.build = l.Cleanup
	c.pickNS()
	return c
}
