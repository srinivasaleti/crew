// Package look is crew's styles and small text helpers, shared by the ui package and the screens
// that live in their own packages (requests), so everything looks the same.
package look

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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
)

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
