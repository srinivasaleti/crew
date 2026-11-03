package core

import (
	"testing"

	"crew/internal/tasks"
)

// TestBudgetOf checks a task's own budget wins, -1 turns it off, and CREW_TASK_BUDGET is the default.
func TestBudgetOf(t *testing.T) {
	t.Setenv("CREW_TASK_BUDGET", "5")
	for _, tc := range []struct {
		t    tasks.Task
		want float64
	}{
		{tasks.Task{}, 5},
		{tasks.Task{Budget: 2}, 2},
		{tasks.Task{Budget: -1}, 0},
		{tasks.Task{Kind: "requests"}, 0}, // a section's chat has none
	} {
		if got := BudgetOf(tc.t); got != tc.want {
			t.Errorf("%+v: %v, want %v", tc.t, got, tc.want)
		}
	}
}

// TestUIMode checks the UI choice defaults to the terminal and keeps what's set.
func TestUIMode(t *testing.T) {
	home := t.TempDir()
	if m := UIMode(home); m != "terminal" {
		t.Errorf("default %q, want terminal", m)
	}
	if err := SetUIMode(home, "web"); err != nil || UIMode(home) != "web" {
		t.Errorf("after setting web: %q, %v", UIMode(home), err)
	}
	if SetUIMode(home, "gui") == nil {
		t.Error("an unknown UI was accepted")
	}
}
