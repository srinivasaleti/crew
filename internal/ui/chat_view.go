package ui

import (
	"crew/internal/chat"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// renderChat lays a task's conversation out as turns: a rule between turns, your messages and
// the agent's replies as blocks with a coloured bar, replies rendered as Markdown, and each run of
// tool calls folded into one summary line (listed in full while the agent is still working on
// it, or always with ctrl+r).
func (m *Model) renderChat(msgs []chat.Message, w int, agentLabel string, busy bool) string {
	var blocks []string
	turn := 0
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		switch {
		case msg.Role == chat.You:
			turn++
			if turn > 1 {
				blocks = append(blocks, dim.Render(fmt.Sprintf("── turn %d %s", turn, strings.Repeat("─", max(w-12, 0)))))
			}
			blocks = append(blocks, m.block(sel, "you", msg.At, m.youText(msg.Text, w-2)))
		case msg.Role == chat.Agent && !msg.Sub:
			blocks = append(blocks, m.block(green, agentLabel, msg.At, m.markdown(msg.Text, w-2)))
		case msg.Role == chat.Error:
			blocks = append(blocks, m.block(warn, "error", msg.At, warn.Render(wrap(msg.Text, w-2))))
		case inRun(msg):
			j := i
			for j < len(msgs) && inRun(msgs[j]) {
				j++
			}
			live := busy && j == len(msgs) // the run the agent is adding to right now
			blocks = append(blocks, m.actions(msgs[i:j], w, live))
			i = j - 1
		default: // crew's own steps (worktree ready, model set, stopped…) always show
			blocks = append(blocks, "  "+action(msg.Text))
		}
	}
	if busy {
		if live := m.conv(m.currentID()).live; live != "" { // the reply so far, as it's written
			blocks = append(blocks, m.block(green, agentLabel, "", wrap(live, w-2)))
		}
		blocks = append(blocks, running.Render("  ● "+agentLabel+" is working…")+dim.Render("  esc to stop"))
	}
	return strings.Join(blocks, "\n\n")
}

// block draws a header ("▌ you  14:32") and a body, both with a bar down the left in the role's colour.
func (m Model) block(style lipgloss.Style, who, at, body string) string {
	bar := style.Render("▌ ")
	header := bar + style.Bold(true).Render(who)
	if t := stamp(at); t != "" {
		header += dim.Render("  " + t)
	}
	lines := []string{header}
	for _, l := range strings.Split(body, "\n") {
		lines = append(lines, bar+l)
	}
	return strings.Join(lines, "\n")
}

// inRun says whether a message belongs to a run of agent actions: its tool calls, what they
// returned, and everything a subagent did.
func inRun(msg chat.Message) bool {
	return msg.Role == chat.Tool && isToolCall(msg.Text) || msg.Role == chat.Result || msg.Sub
}

// actions shows a run of tool calls. While the agent works (or with ⌘R) each call shows in full
// with the start of what it returned, and a subagent's steps nested under its call; otherwise the
// run folds into one summary line.
func (m Model) actions(run []chat.Message, w int, live bool) string {
	if live || m.expandActions {
		show := 4 // lines of each result
		if m.expandActions {
			show = 40
		}
		var lines []string
		for _, a := range run {
			indent := "  "
			if a.Sub {
				indent = "  " + dim.Render("│ ") + " "
			}
			switch a.Role {
			case chat.Tool:
				for _, l := range strings.Split(wrap(action(a.Text), w-6), "\n") {
					lines = append(lines, indent+l)
				}
			case chat.Agent: // a subagent's own words
				said := strings.Split(wrap(a.Text, w-8), "\n")
				for _, l := range said[:min(3, len(said))] {
					lines = append(lines, indent+dim.Render(l))
				}
			case chat.Result:
				out := strings.Split(a.Text, "\n")
				for _, l := range out[:min(show, len(out))] {
					lines = append(lines, indent+dim.Render("    "+ansi.Truncate(strings.ReplaceAll(l, "\t", "    "), w-12, "…")))
				}
				if n := len(out) - show; n > 0 {
					lines = append(lines, indent+dim.Render(keyNames(fmt.Sprintf("    … %d more line%s · ctrl+r shows more", n, plural(n)))))
				}
			}
		}
		return strings.Join(lines, "\n")
	}
	counts, order, n := map[string]int{}, []string{}, 0
	for _, a := range run {
		if a.Role != chat.Tool {
			continue
		}
		n++
		k := actionKind(a.Text)
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	parts := []string{fmt.Sprintf("%d action%s", n, plural(n))}
	for _, k := range order {
		st := kindStyles[k]
		parts = append(parts, lipgloss.NewStyle().Foreground(st.color).Render(fmt.Sprintf("%s %d %s%s", st.icon, counts[k], k, plural(counts[k]))))
	}
	return dim.Render("  ▸ ") + strings.Join(parts, dim.Render(" · ")) + dim.Render(keyNames("   ctrl+r shows them with their output"))
}

// kindStyles names each kind of tool call for summaries, with its icon and colour.
var kindStyles = map[string]struct {
	icon  string
	color lipgloss.Color
}{
	"command": {"❯", "11"}, "edit": {"✎", "13"}, "read": {"≡", "14"}, "search": {"⌕", "14"},
	"web call": {"↗", "12"}, "subagent": {"◆", "10"}, "other": {"▸", "8"},
}

func actionKind(text string) string {
	name, _, _ := strings.Cut(text, ": ")
	for _, k := range []struct{ kind, prefix string }{
		{"command", "Bash"}, {"edit", "Edit"}, {"edit", "MultiEdit"}, {"edit", "Write"}, {"edit", "NotebookEdit"},
		{"read", "Read"}, {"read", "LS"}, {"search", "Grep"}, {"search", "Glob"},
		{"web call", "WebFetch"}, {"web call", "WebSearch"}, {"subagent", "Task"}, {"subagent", "Agent"},
	} {
		if strings.HasPrefix(name, k.prefix) {
			return k.kind
		}
	}
	return "other"
}

// isToolCall tells an agent's tool call ("Bash: go test") from one of crew's own steps
// ("worktree ready: …", "stopped by you"), matching how action() draws them.
func isToolCall(text string) bool {
	name, _, ok := strings.Cut(text, ": ")
	if !ok {
		return !strings.Contains(text, " ")
	}
	return !strings.Contains(name, " ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// stamp formats a message time as 14:32 today, or Oct 5 14:32 on other days.
func stamp(at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	if y, mo, d := t.Date(); y == time.Now().Year() && mo == time.Now().Month() && d == time.Now().Day() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// youText draws your message. Quoted blocks (the feedback the PR tab attaches to a question) are
// cleaned of GitHub's HTML and folded to their first lines; ctrl+r shows them in full. The agent
// was sent the whole text either way.
func (m *Model) youText(text string, w int) string {
	var out, quoted []string
	flush := func() {
		var lines []string
		for _, l := range strings.Split(cleanMD(strings.Join(quoted, "\n")), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, strings.ReplaceAll(l, "**", "")) // plain text: no Markdown markers
			}
		}
		show := lines
		if !m.expandActions && len(lines) > 3 {
			show = lines[:3]
		}
		for _, l := range show {
			for _, wl := range strings.Split(wrap(l, w-2), "\n") {
				out = append(out, dim.Render("│ "+wl))
			}
		}
		if n := len(lines) - len(show); n > 0 {
			out = append(out, dim.Render(fmt.Sprintf("│ … %d more line%s · ctrl+r shows them", n, plural(n))))
		}
		quoted = nil
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, ">") {
			quoted = append(quoted, strings.TrimPrefix(strings.TrimPrefix(l, ">"), " "))
			continue
		}
		if len(quoted) > 0 {
			flush()
		}
		out = append(out, wrap(l, w))
	}
	if len(quoted) > 0 {
		flush()
	}
	return strings.Join(out, "\n")
}
