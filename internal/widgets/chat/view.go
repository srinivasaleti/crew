package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/keys"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
)

// The chat draws like Claude Code: your message after a "> ", Claude's words after a ⏺, each tool
// call as ⏺ Name(what it was given) with its output under ⎿ (a few lines; ⌘R shows more), a
// subagent's steps indented under its call, and a "✻ Working…" line while Claude works.

var (
	bulletOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("⏺")
	bulletErr  = look.Warn.Render("⏺")
	bulletWait = look.Dim.Render("⏺")
	bulletSay  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("⏺")
	youBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// Bulleted puts a ⏺ (or another mark) before the first line of body and indents the rest under it.
func Bulleted(mark, body, indent string) string {
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

// Render draws a chat w wide; spin is the working spinner's frame.
func (cs *Chats) Render(id string, w, spin int) string {
	c := cs.Conv(id)
	msgs, busy := c.Msgs, c.Busy
	var blocks []string
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		switch {
		case msg.Role == You:
			body := cs.youText(msg.Text, w-4)
			if t := stamp(msg.At); t != "" {
				body += look.Dim.Render("  · " + t)
			}
			blocks = append(blocks, Bulleted(youBar.Render(">"), body, ""))
		case msg.Role == Agent && !msg.Sub:
			blocks = append(blocks, Bulleted(bulletSay, cs.md.Render(msg.Text, w-2), ""))
		case msg.Role == Error:
			blocks = append(blocks, Bulleted(bulletErr, look.Warn.Render(look.Wrap(msg.Text, w-2)), ""))
		case inRun(msg):
			j := i
			for j < len(msgs) && inRun(msgs[j]) {
				j++
			}
			blocks = append(blocks, cs.toolRun(msgs[i:j], w, busy && j == len(msgs)))
			i = j - 1
		default: // crew's own steps (worktree ready, model set, stopped…)
			blocks = append(blocks, look.Dim.Render("  ⎿  "+msg.Text))
		}
	}
	if busy {
		if c.live != "" { // the reply so far, as it's written
			blocks = append(blocks, Bulleted(bulletSay, look.Wrap(c.live, w-2), ""))
		}
		took := ""
		if !c.started.IsZero() {
			took = fmt.Sprintf("%ds · ", int(time.Since(c.started).Seconds()))
		}
		blocks = append(blocks, look.Sel.Render(look.SpinFrames[spin%len(look.SpinFrames)]+" Working…")+look.Dim.Render(" ("+took+"esc to interrupt)"))
	}
	return strings.Join(blocks, "\n\n")
}

// inRun says whether a message belongs to a run of agent actions: its tool calls, what they
// returned, and everything a subagent did.
func inRun(msg Message) bool {
	return msg.Role == Tool && isToolCall(msg.Text) || msg.Role == Result || msg.Sub
}

// toolRun draws a run of tool calls, each with its output. Results arrive after their calls (and
// parallel calls before any result), so each result goes to the oldest call still waiting for one.
func (cs *Chats) toolRun(run []Message, w int, live bool) string {
	type call struct {
		msg    Message
		result *Message
	}
	var calls []*call
	var said []string // a subagent's own words, kept in order with its calls
	order := []any{}
	for i := range run {
		a := run[i]
		switch a.Role {
		case Tool:
			c := &call{msg: a}
			calls = append(calls, c)
			order = append(order, c)
		case Result:
			for _, c := range calls {
				if c.result == nil && c.msg.Sub == a.Sub {
					c.result = &run[i]
					break
				}
			}
		case Agent:
			said = append(said, a.Text)
			order = append(order, len(said)-1)
		}
	}
	show := 2 // lines of each output
	switch {
	case cs.Expand:
		show = 40
	case live:
		show = 4
	}
	var out []string
	for _, o := range order {
		if i, ok := o.(int); ok { // a subagent's words
			first, _, _ := strings.Cut(said[i], "\n")
			out = append(out, "    "+look.Dim.Render(ansi.Truncate(first, w-6, "…")))
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
		head := look.Bold.Render(name)
		if detail != "" {
			head += look.Dim.Render("(") + detail + look.Dim.Render(")")
		}
		out = append(out, Bulleted(mark, look.Wrap(head, w-4-len(indent)), indent))
		if c.result == nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(c.result.Text, "\t", "    "), "\n")
		for k, l := range lines[:min(show, len(lines))] {
			lead := "  ⎿  "
			if k > 0 {
				lead = "     "
			}
			out = append(out, indent+look.Dim.Render(lead+ansi.Truncate(l, w-8-len(indent), "…")))
		}
		if n := len(lines) - show; n > 0 {
			out = append(out, indent+look.Dim.Render(keys.Names(fmt.Sprintf("     … +%d line%s (ctrl+r to expand)", n, look.Plural(n)))))
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
func (cs *Chats) youText(text string, w int) string {
	var out, quoted []string
	flush := func() {
		var lines []string
		for _, l := range strings.Split(markdown.Clean(strings.Join(quoted, "\n")), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, strings.ReplaceAll(l, "**", "")) // plain text: no Markdown markers
			}
		}
		show := lines
		if !cs.Expand && len(lines) > 3 {
			show = lines[:3]
		}
		for _, l := range show {
			for _, wl := range strings.Split(look.Wrap(l, w-2), "\n") {
				out = append(out, look.Dim.Render("│ "+wl))
			}
		}
		if n := len(lines) - len(show); n > 0 {
			out = append(out, look.Dim.Render(keys.Names(fmt.Sprintf("│ … %d more line%s · ctrl+r shows them", n, look.Plural(n)))))
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
		out = append(out, look.Wrap(l, w))
	}
	if len(quoted) > 0 {
		flush()
	}
	return strings.Join(out, "\n")
}
