package app

import (
	"crew/internal/widgets/chat"
	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The chat draws like Claude Code: your message after a "> ", Claude's words after a ⏺, each tool
// call as ⏺ Name(what it was given) with its output under ⎿ (a few lines; ⌘R shows more), a
// subagent's steps indented under its call, and a "✻ Working…" line while Claude works.

var (
	bulletOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("⏺")
	bulletErr  = warn.Render("⏺")
	bulletWait = dim.Render("⏺")
	bulletSay  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("⏺")
	youBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// spinFrames is the working spinner, as in Claude Code.
var spinFrames = look.SpinFrames

// bulleted puts a ⏺ (or another mark) before the first line of body and indents the rest under it.
func bulleted(mark, body, indent string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = indent + mark + " " + l
		} else {
			lines[i] = indent + "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderChat(msgs []chat.Message, w int, agentLabel string, busy bool) string {
	var blocks []string
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		switch {
		case msg.Role == chat.You:
			body := m.youText(msg.Text, w-4)
			if t := stamp(msg.At); t != "" {
				body += dim.Render("  · " + t)
			}
			blocks = append(blocks, bulleted(youBar.Render(">"), body, ""))
		case msg.Role == chat.Agent && !msg.Sub:
			blocks = append(blocks, bulleted(bulletSay, m.md.Render(msg.Text, w-2), ""))
		case msg.Role == chat.Error:
			blocks = append(blocks, bulleted(bulletErr, warn.Render(wrap(msg.Text, w-2)), ""))
		case inRun(msg):
			j := i
			for j < len(msgs) && inRun(msgs[j]) {
				j++
			}
			blocks = append(blocks, m.toolRun(msgs[i:j], w, busy && j == len(msgs)))
			i = j - 1
		default: // crew's own steps (worktree ready, model set, stopped…)
			blocks = append(blocks, dim.Render("  ⎿  "+msg.Text))
		}
	}
	if busy {
		c := m.conv(m.currentID())
		if c.live != "" { // the reply so far, as it's written
			blocks = append(blocks, bulleted(bulletSay, wrap(c.live, w-2), ""))
		}
		took := ""
		if !c.started.IsZero() {
			took = fmt.Sprintf("%ds · ", int(time.Since(c.started).Seconds()))
		}
		blocks = append(blocks, sel.Render(spinFrames[m.spin%len(spinFrames)]+" Working…")+dim.Render(" ("+took+"esc to interrupt)"))
	}
	return strings.Join(blocks, "\n\n")
}

// inRun says whether a message belongs to a run of agent actions: its tool calls, what they
// returned, and everything a subagent did.
func inRun(msg chat.Message) bool {
	return msg.Role == chat.Tool && isToolCall(msg.Text) || msg.Role == chat.Result || msg.Sub
}

// toolRun draws a run of tool calls, each with its output. Results arrive after their calls (and
// parallel calls before any result), so each result goes to the oldest call still waiting for one.
func (m Model) toolRun(run []chat.Message, w int, live bool) string {
	type call struct {
		msg    chat.Message
		result *chat.Message
	}
	var calls []*call
	var said []string // a subagent's own words, kept in order with its calls
	order := []any{}
	for i := range run {
		a := run[i]
		switch a.Role {
		case chat.Tool:
			c := &call{msg: a}
			calls = append(calls, c)
			order = append(order, c)
		case chat.Result:
			for _, c := range calls {
				if c.result == nil && c.msg.Sub == a.Sub {
					c.result = &run[i]
					break
				}
			}
		case chat.Agent:
			said = append(said, a.Text)
			order = append(order, len(said)-1)
		}
	}
	show := 2 // lines of each output
	switch {
	case m.expandActions:
		show = 40
	case live:
		show = 4
	}
	var out []string
	for _, o := range order {
		if i, ok := o.(int); ok { // a subagent's words
			first, _, _ := strings.Cut(said[i], "\n")
			out = append(out, "    "+dim.Render(ansi.Truncate(first, w-6, "…")))
			continue
		}
		c := o.(*call)
		indent := ""
		if c.msg.Sub {
			indent = "    " // under the subagent's call
		}
		name, detail, _ := strings.Cut(c.msg.Text, ": ")
		mark := bulletOK
		switch {
		case c.result == nil && live:
			mark = bulletWait
		case c.result != nil && strings.HasPrefix(c.result.Text, "error: "):
			mark = bulletErr
		}
		head := bold.Render(name)
		if detail != "" {
			head += dim.Render("(") + detail + dim.Render(")")
		}
		out = append(out, bulleted(mark, wrap(head, w-4-len(indent)), indent))
		if c.result == nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(c.result.Text, "\t", "    "), "\n")
		for k, l := range lines[:min(show, len(lines))] {
			lead := "  ⎿  "
			if k > 0 {
				lead = "     "
			}
			out = append(out, indent+dim.Render(lead+ansi.Truncate(l, w-8-len(indent), "…")))
		}
		if n := len(lines) - show; n > 0 {
			out = append(out, indent+dim.Render(keys.Names(fmt.Sprintf("     … +%d line%s (ctrl+r to expand)", n, plural(n)))))
		}
	}
	return strings.Join(out, "\n")
}

// isToolCall tells an agent's tool call ("Bash: go test") from one of crew's own steps
// ("worktree ready: …", "stopped by you").
func isToolCall(text string) bool {
	name, _, ok := strings.Cut(text, ": ")
	if !ok {
		return !strings.Contains(text, " ")
	}
	return !strings.Contains(name, " ")
}

var plural = look.Plural

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
		for _, l := range strings.Split(markdown.Clean(strings.Join(quoted, "\n")), "\n") {
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
			out = append(out, dim.Render(keys.Names(fmt.Sprintf("│ … %d more line%s · ctrl+r shows them", n, plural(n)))))
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
