package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadPlan checks a plan reads into its approach, steps with their notes and marks, questions
// with the recommended choice, and notes; and that a step can be marked by hand.
func TestReadPlan(t *testing.T) {
	dir := t.TempDir()
	c := New(context.Background(), Config{Plans: dir})
	os.WriteFile(filepath.Join(dir, "t1.md"), []byte(`# Plan: Billing lines

Work out the bill from what's stored.

- [x] 1. Catalog: add priced_per
      core/catalog.go
      Check: go test
- [ ] 2. Domain: Bill()

## Notes

- level decides scope.

## Questions

1. Which price?
   - $39 (recommended: the catalog's)
   - $49
`), 0o644)
	v := c.ReadPlan("t1")
	if v == nil || v.Title != "Billing lines" || !strings.HasPrefix(v.Approach, "Work out") || len(v.Steps) != 2 || v.Done != 1 {
		t.Fatalf("%+v", v)
	}
	if v.Steps[0].Status != "done" || v.Steps[0].Notes != "core/catalog.go\nCheck: go test" || v.Steps[1].Status != "todo" {
		t.Errorf("steps %+v", v.Steps)
	}
	if len(v.Questions) != 1 || len(v.Questions[0].Options) != 2 || v.Questions[0].Recommended != 0 || strings.Contains(v.Rest, "Which price") {
		t.Errorf("questions %+v, rest %q", v.Questions, v.Rest)
	}
	if err := c.SetPlanStep("t1", 1, "doing"); err != nil || c.ReadPlan("t1").Steps[1].Status != "doing" {
		t.Errorf("mark: %v", err)
	}
	if c.SetPlanStep("t1", 5, "done") == nil || c.SetPlanStep("t1", 0, "maybe") == nil {
		t.Error("a bad step or status was accepted")
	}
	if c.ReadPlan("none") != nil {
		t.Error("a missing plan read as one")
	}
}
