// Package look is crew's styles and small text helpers, shared by the ui package and the screens
// that live in their own packages (requests), so everything looks the same.
package look

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	Dim  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	Bold = lipgloss.NewStyle().Bold(true)
	// Accent is Claude's coral: the focused frame, selections, markers and the working spinner.
	Accent  = lipgloss.Color("#D97757")
	Sel     = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Warn    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	Notice  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	Green   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	Running = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	Ready   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	Orange  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Claude  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	// FocusBar marks the selected row of the area that has the keys.
	FocusBar = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	// Active is a selected tab or pressed-looking button.
	Active = lipgloss.NewStyle().Background(Accent).Foreground(lipgloss.Color("0")).Bold(true)
	// MenuBox frames a menu or a small form over the main pane.
	MenuBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(1, 2).Width(32)
)

// Button is an action button, "[ Open enter ]"; one that adds ("+ Task") is in the accent.
func Button(label, key string) string {
	if strings.HasPrefix(label, "+") {
		return Sel.Render("[ "+label) + " " + Dim.Render(key+" ]")
	}
	return Dim.Render("[") + " " + label + " " + Dim.Render(key+" ]")
}

// KeyHints colours a hint line ("⏎ opens · n new task", "⌘F search  esc back"): the key that
// starts each part (after a "·" or two spaces), and one after "or", in the accent; the rest dim.
func KeyHints(s string) string {
	words := strings.Split(s, " ")
	out, start := make([]string, len(words)), true
	for i, w := range words {
		switch {
		case w == "" || w == "·":
			out[i], start = Dim.Render(w), true
			continue
		case (start || i > 0 && words[i-1] == "or") && isKey(w):
			out[i] = Sel.Render(w)
		default:
			out[i] = Dim.Render(w)
		}
		start = false
	}
	return strings.Join(out, " ")
}

// isKey reports whether a hint's word names a key: ⌘F, ⏎, ↑↓, alt+⏎, /search, pgup/pgdn, n, a/c/r.
func isKey(w string) bool {
	w = strings.TrimRight(w, ",:")
	switch {
	case w == "":
		return false
	case len(w) == 1, strings.HasPrefix(w, "/"), strings.Contains(w, "+"):
		return true
	}
	for _, r := range w {
		if r > 127 {
			return true
		}
	}
	for _, p := range strings.Split(w, "/") {
		if len(p) != 1 && !keyWords[p] {
			return false
		}
	}
	return true
}

var keyWords = map[string]bool{"esc": true, "tab": true, "enter": true, "space": true, "pgup": true, "pgdn": true, "home": true, "end": true}

// SpinFrames is the working spinner, as in Claude Code.
var SpinFrames = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// Wrap wraps text to w columns.
func Wrap(s string, w int) string { return lipgloss.NewStyle().Width(max(w, 10)).Render(s) }

// Plural is "s" unless n is 1.
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Ago is how long ago t was: "just now", "5m ago", "3h ago", "2d ago".
func Ago(t time.Time) string {
	switch d := time.Since(t); {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// Tildify writes a path under your home folder with ~.
func Tildify(p string) string {
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// Clip cuts each line to width so long lines don't wrap and break the layout.
func Clip(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}

// Tokens formats a count compactly: 950, 12.3k, 1.2M.
func Tokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return strconv.Itoa(n)
	}
}
