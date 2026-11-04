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

// TestModel checks /model sets a task's model without sending anything, by alias or id, and
// default clears it; the web's button reads which choice is on with IsModel.
func TestModel(t *testing.T) {
	dir := t.TempDir()
	c := New(context.Background(), Config{Tasks: tasks.Store{Dir: dir}, Chats: Log{Dir: dir + "/chats"}})
	c.cfg.Tasks.Save(tasks.Task{ID: "t1", Title: "x"})
	for _, tc := range []struct{ text, want string }{{"/model sonnet", "sonnet"}, {"/model claude-opus-5-5", "claude-opus-5-5"}, {"/model default", ""}} {
		if err := c.Send("t1", tc.text); err != nil {
			t.Fatal(err)
		}
		if got, _ := c.Task("t1"); got.Model != tc.want {
			t.Errorf("%q: model %q", tc.text, got.Model)
		}
	}
	if c.Send("t1", "/model") == nil {
		t.Error("/model with nothing was accepted")
	}
	if !IsModel("sonnet", "claude-sonnet-5-5") || IsModel("", "claude-sonnet-5-5") {
		t.Error("IsModel")
	}
	if msgs, _ := c.Chat("t1"); len(msgs) != 3 || !strings.Contains(msgs[0].Text, "Sonnet 5.5") {
		t.Errorf("chat: %+v", msgs)
	}
}
