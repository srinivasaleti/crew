package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/widgets/chat"
)

// The Changes tab: the task's diff with a line cursor, so you review the agent's work the way you'd
// review a colleague's PR. c writes a comment on the selected line; comments collect (✎ beside
// their line) until s sends them all to the task's agent in one message, which asks it to address
// each and commit. Nothing goes to GitHub.

// diffLine is one line of the diff, and where it is in the code.
type diffLine struct {
	text string // as git coloured it
	path string // the file it's in; "" on the summary above the first file
	line int    // its line in the new file (0 for a removed line or a header)
	old  int    // a removed line's line in the old file
}

// diffNote is your comment on a line of the diff, waiting to be sent.
type diffNote struct {
	path       string
	line, old  int
	code, text string
}

// at is where the note is, "path:12" (a removed line: "path:-12", its old line).
func (n diffNote) at() string {
	if n.line == 0 {
		return n.path + ":-" + strconv.Itoa(n.old)
	}
	return n.path + ":" + strconv.Itoa(n.line)
}

// changesView is one task's Changes tab: its diff as last read, the selected line, your comments.
type changesView struct {
	lines   []diffLine
	sel     int
	notes   []diffNote
	writing bool // the message box is a comment on the selected line
}

func (m Model) changesOf(id string) *changesView {
	if m.changes[id] == nil {
		m.changes[id] = &changesView{}
	}
	return m.changes[id]
}

var hunkHead = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseDiff splits git's coloured diff into lines that know their file and line numbers.
func parseDiff(colored string) []diffLine {
	var out []diffLine
	path, newLn, oldLn, inHunk := "", 0, 0, false
	for _, l := range strings.Split(colored, "\n") {
		p := ansi.Strip(l)
		d := diffLine{text: l}
		switch {
		case strings.HasPrefix(p, "diff --git "):
			if i := strings.LastIndex(p, " b/"); i >= 0 {
				path = p[i+3:]
			}
			inHunk = false
		case strings.HasPrefix(p, "+++ b/"):
			path = strings.TrimPrefix(p, "+++ b/")
		case strings.HasPrefix(p, "+++ ") || strings.HasPrefix(p, "--- "):
		case hunkHead.MatchString(p):
			m := hunkHead.FindStringSubmatch(p)
			oldLn, _ = strconv.Atoi(m[1])
			newLn, _ = strconv.Atoi(m[2])
			inHunk = true
		case inHunk && strings.HasPrefix(p, "+"):
			d.line = newLn
			newLn++
		case inHunk && strings.HasPrefix(p, "-"):
			d.old = oldLn
			oldLn++
		case inHunk && strings.HasPrefix(p, " "):
			d.line = newLn
			newLn++
			oldLn++
		}
		d.path = path
		out = append(out, d)
	}
	return out
}

// noteOn is the index of the comment on line i, or -1.
func (cv *changesView) noteOn(i int) int {
	d := cv.lines[i]
	for j, n := range cv.notes {
		if n.path == d.path && n.line == d.line && n.old == d.old && (d.line > 0 || d.old > 0) {
			return j
		}
	}
	return -1
}

// renderChanges fills the viewport with the diff: the selected line marked ▶, commented ones ✎, and
// the selection kept in view.
func (m *Model) renderChanges() {
	cv := m.changesOf(m.currentID())
	if len(cv.lines) == 0 {
		return
	}
	cv.sel = min(max(cv.sel, 0), len(cv.lines)-1)
	w := m.vp.Width
	var b strings.Builder
	for i, d := range cv.lines {
		gutter := "  "
		switch {
		case i == cv.sel:
			gutter = sel.Render("▶ ")
		case cv.noteOn(i) >= 0:
			gutter = claude.Render("✎ ")
		}
		b.WriteString(gutter + ansi.Truncate(d.text, w-2, "…"))
		if j := cv.noteOn(i); j >= 0 {
			b.WriteString("\n    " + claude.Render("✎ "+ansi.Truncate(cv.notes[j].text, w-6, "…")))
		}
		if i < len(cv.lines)-1 {
			b.WriteString("\n")
		}
	}
	m.vp.SetContent(b.String())
	row := cv.sel // the selection's screen row: notes above it add a line each
	for i := 0; i < cv.sel; i++ {
		if cv.noteOn(i) >= 0 {
			row++
		}
	}
	switch {
	case row < m.vp.YOffset:
		m.vp.SetYOffset(row)
	case row >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(row - m.vp.Height + 1)
	}
}

// updateChanges handles the Changes tab's own keys; ok is false for the ones it leaves to the
// generic tab keys (tab, esc, r).
func (m Model) updateChanges(msg tea.KeyMsg) (_ Model, _ tea.Cmd, ok bool) {
	t, cv := m.current(), m.changesOf(m.currentID())
	k := msg.String()
	if cv.writing {
		switch k {
		case "esc":
			cv.writing = false
			m.input.Reset()
			m.input.Blur()
			m.layout()
			m.renderChanges()
		case "enter":
			if text := strings.TrimSpace(m.input.Value()); text != "" {
				d := cv.lines[cv.sel]
				if j := cv.noteOn(cv.sel); j >= 0 {
					cv.notes = append(cv.notes[:j], cv.notes[j+1:]...) // a second comment replaces the first
				}
				cv.notes = append(cv.notes, diffNote{path: d.path, line: d.line, old: d.old, code: strings.TrimSpace(ansi.Strip(d.text)), text: text})
			}
			cv.writing = false
			m.input.Reset()
			m.input.Blur()
			m.layout()
			m.renderChanges()
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.layout()
			return m, cmd, true
		}
		return m, nil, true
	}
	if len(cv.lines) == 0 {
		return m, nil, false
	}
	switch k {
	case "up":
		cv.sel--
	case "down":
		cv.sel++
	case "pgup":
		cv.sel -= m.vp.Height
	case "pgdown":
		cv.sel += m.vp.Height
	case "home":
		cv.sel = 0
	case "end":
		cv.sel = len(cv.lines) - 1
	case "c":
		d := cv.lines[cv.sel]
		if d.line == 0 && d.old == 0 {
			m.notice = "Pick a line of code to comment on (↑↓)"
			return m, nil, true
		}
		cv.writing = true
		m.input.Reset()
		m.input.Placeholder = "What should " + chat.AgentName(*t) + " change here?"
		if j := cv.noteOn(cv.sel); j >= 0 {
			m.input.SetValue(cv.notes[j].text)
		}
		m.layout()
		return m, m.input.Focus(), true
	case "x":
		if j := cv.noteOn(cv.sel); j >= 0 {
			cv.notes = append(cv.notes[:j], cv.notes[j+1:]...)
		}
	case "s":
		if len(cv.notes) == 0 {
			m.notice = "No comments yet · c on a line writes one"
			return m, nil, true
		}
		text := reviewMessage(cv.notes)
		n := len(cv.notes)
		cv.notes = nil
		m.notice = fmt.Sprintf("Sent %d comment%s to %s · the Chat tab shows it working", n, plural(n), chat.AgentName(*t))
		m.renderChanges()
		return m, m.send(*t, chat.Outgoing{Text: text}), true
	default:
		return m, nil, false
	}
	m.renderChanges()
	return m, nil, true
}

// reviewMessage is your comments as one message to the agent, like a review on its PR.
func reviewMessage(notes []diffNote) string {
	var b strings.Builder
	b.WriteString("I reviewed your changes (the Changes tab). Address each comment the simplest way that's correct; " +
		"if you disagree with one, say why instead of changing it. Commit when you're done, and don't push.\n")
	for i, n := range notes {
		fmt.Fprintf(&b, "\n%d. `%s`\n   > %s\n   %s\n", i+1, n.at(), n.code, strings.ReplaceAll(n.text, "\n", "\n   "))
	}
	return b.String()
}

// changesInputView is the bottom of the Changes tab: the comment box while you write one, else its keys.
func (m Model) changesInputView() string {
	cv := m.changesOf(m.currentID())
	if cv.writing && cv.sel < len(cv.lines) {
		d := cv.lines[cv.sel]
		at := diffNote{path: d.path, line: d.line, old: d.old}.at()
		return claude.Render("  ↳ Comment on "+at) + dim.Render(" · ⏎ adds it · esc cancel") + "\n" + m.input.View()
	}
	h := "  ↑↓ pick a line · c comment for the agent · r refresh"
	if n := len(cv.notes); n > 0 {
		h = fmt.Sprintf("  ↑↓ pick a line · c comment · x removes one · s sends %d comment%s to the agent · r refresh", n, plural(n))
	}
	return hint(h)
}
