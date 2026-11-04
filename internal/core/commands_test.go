package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crew/internal/tasks"
)

// TestCrewMessage checks crew's commands become the agent's message, refuse what they can't do, and
// leave anything else as typed.
func TestCrewMessage(t *testing.T) {
	plugin := t.TempDir()
	os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644)
	c := New(context.Background(), Config{Plugin: plugin, Plans: t.TempDir()})
	task := tasks.Task{ID: "t1", Base: "abc", Worktree: "/w"}
	for _, tc := range []struct {
		text, want string // want is a prefix; "" means not a crew command
		err        bool
	}{
		{"/self-review", "/crew:self-review Review. Base abc", false},
		{"/test api", "/crew:test Base abc. Area api", false},
		{"/plan add login", "/crew:plan Plan file ", false},
		{"/debug", "", true},
		{"/review-pr 12", "", false}, // a Claude Code command: sent as typed
	} {
		msg, ok, err := c.crewMessage(task, tc.text)
		if (err != nil) != tc.err || (tc.want == "") == ok && !tc.err || !strings.HasPrefix(msg, tc.want) {
			t.Errorf("%q: %q ok=%v err=%v", tc.text, msg, ok, err)
		}
	}
	if _, _, err := c.crewMessage(tasks.Task{ID: "t2"}, "/pr"); err == nil {
		t.Error("/pr with no worktree was accepted")
	}
}
