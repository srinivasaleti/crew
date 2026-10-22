package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGuard checks crew's PreToolUse hook (plugin/hooks/guard.sh): agents can't write crew's task
// files, push or post to GitHub; everything else goes through.
func TestGuard(t *testing.T) {
	cases := map[string]bool{ // tool input -> blocked
		`{"tool_name":"Bash","tool_input":{"command":"git push -u origin feat"}}`:                    true,
		`{"tool_name":"Bash","tool_input":{"command":"make test && git -C /x push"}}`:                true,
		`{"tool_name":"Bash","tool_input":{"command":"gh pr create --fill"}}`:                        true,
		`{"tool_name":"Bash","tool_input":{"command":"gh pr comment 3 -b hi"}}`:                      true,
		`{"tool_name":"Bash","tool_input":{"command":"gh api -X POST repos/o/r/issues/1/comments"}}`: true,
		`{"tool_name":"Bash","tool_input":{"command":"echo x > ~/.crew/tasks/a.md"}}`:                true,
		`{"tool_name":"Write","tool_input":{"file_path":"/Users/me/.crew/tasks/a.md"}}`:              true,
		`{"tool_name":"Bash","tool_input":{"command":"git commit -m \"push the button\""}}`:          false,
		`{"tool_name":"Bash","tool_input":{"command":"gh pr view 12 --json state"}}`:                 false,
		`{"tool_name":"Bash","tool_input":{"command":"gh api repos/o/r/pulls/1/comments"}}`:          false,
		`{"tool_name":"Write","tool_input":{"file_path":"/x/notes.md","content":"git push"}}`:        false,
		`{"tool_name":"Write","tool_input":{"file_path":"/Users/me/.crew/splits/a.md"}}`:             false,
	}
	for in, blocked := range cases {
		cmd := exec.Command("sh", "plugin/hooks/guard.sh")
		cmd.Stdin = strings.NewReader(in)
		err := cmd.Run()
		if got := err != nil; got != blocked {
			t.Errorf("blocked=%v, want %v: %s", got, blocked, in)
		}
	}
}
