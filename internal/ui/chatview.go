package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/chat"
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
			blocks = append(blocks, m.block(sel, "you", msg.At, wrap(msg.Text, w-2)))
		case msg.Role == chat.Agent:
			blocks = append(blocks, m.block(green, agentLabel, msg.At, m.markdown(msg.Text, w-2)))
		case msg.Role == chat.Error:
			blocks = append(blocks, m.block(warn, "error", msg.At, warn.Render(wrap(msg.Text, w-2))))
		case isToolCall(msg.Text):
			j := i
			for j < len(msgs) && msgs[j].Role == chat.Tool && isToolCall(msgs[j].Text) {
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

// actions shows a run of tool calls: one summary line, or each call when live or expanded.
func (m Model) actions(run []chat.Message, w int, live bool) string {
	if live || m.expandActions {
		var lines []string
		for _, a := range run {
			lines = append(lines, "  "+ansi.Truncate(action(a.Text), w-2, "…"))
		}
		return strings.Join(lines, "\n")
	}
	counts, order := map[string]int{}, []string{}
	for _, a := range run {
		k := actionKind(a.Text)
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	parts := []string{fmt.Sprintf("%d action%s", len(run), plural(len(run)))}
	for _, k := range order {
		st := kindStyles[k]
		parts = append(parts, lipgloss.NewStyle().Foreground(st.color).Render(fmt.Sprintf("%s %d %s%s", st.icon, counts[k], k, plural(counts[k]))))
	}
	return dim.Render("  ▸ ") + strings.Join(parts, dim.Render(" · ")) + dim.Render("   ctrl+r to expand")
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

// markdown renders an agent reply, caching by text and width since the chat redraws often.
func (m *Model) markdown(text string, w int) string {
	key := fmt.Sprintf("%d\x00%s", w, text)
	if out, ok := m.mdCache[key]; ok {
		return out
	}
	if m.md == nil || m.mdWidth != w {
		m.md, _ = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(max(w-4, 20)))
		m.mdWidth = w
	}
	out := wrap(text, w)
	if m.md != nil {
		if r, err := m.md.Render(text); err == nil {
			var lines []string
			for _, l := range strings.Split(strings.Trim(r, "\n"), "\n") {
				lines = append(lines, ansi.Truncate(strings.TrimRight(l, " "), w, "…"))
			}
			out = strings.Join(lines, "\n")
		}
	}
	m.mdCache[key] = out
	return out
}

func wrap(s string, w int) string { return lipgloss.NewStyle().Width(max(w, 10)).Render(s) }

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
