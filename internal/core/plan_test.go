package core

import "testing"

// PlanAsk leads with the skill's command (so it runs) and says what to plan or change.
func TestPlanAsk(t *testing.T) {
	for _, c := range []struct {
		exists     bool
		ask, brief string
		want       string
	}{
		{false, "", "Add a board", "/crew:plan Plan file p.md. Goal Add a board"},
		{false, " Just the API ", "Add a board", "/crew:plan Plan file p.md. Goal Just the API"},
		{true, "Drop step 3", "Add a board", "/crew:plan Plan file p.md. Change the plan: Drop step 3"},
		{true, "  ", "Add a board", "/crew:plan Plan file p.md. Goal Add a board"},
	} {
		if got := PlanAsk("p.md", c.exists, c.ask, c.brief); got != c.want {
			t.Errorf("PlanAsk(%v, %q) = %q, want %q", c.exists, c.ask, got, c.want)
		}
	}
}
