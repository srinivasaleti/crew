package core

import (
	"slices"
	"strings"
	"testing"
)

// The home chat only reads: no file writes, no commands but gh's and acli's reads, and refuses the rest.
func TestHomeArgsReadOnly(t *testing.T) {
	a := HomeArgs("/Users/me/.crew", "")
	for _, want := range []string{"dontAsk", "Write", "Edit", "mcp__*", "Read(//Users/me/.crew/**)"} {
		if !slices.Contains(a, want) {
			t.Errorf("missing %q in %v", want, a)
		}
	}
	for _, x := range a {
		if strings.HasPrefix(x, "Bash(") && !strings.HasPrefix(x, "Bash(gh pr ") && !strings.HasPrefix(x, "Bash(gh search ") &&
			!strings.HasPrefix(x, "Bash(acli jira workitem search") && !strings.HasPrefix(x, "Bash(acli jira workitem view") {
			t.Errorf("a command beyond reads: %s", x)
		}
		if x == "Bash" || x == "bypassPermissions" || x == "auto" {
			t.Errorf("not locked down: %s", x)
		}
	}
}
