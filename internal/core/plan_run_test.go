package core

import (
	"os/exec"
	"strings"
	"testing"
)

// TestStepTips checks each step's branch point is its last "Step N:" commit, and that a fix to an
// earlier step made after a later one is refused (it can't be split into PRs in order).
func TestStepTips(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(msg string) string {
		git("commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q")
	base := commit("start")
	commit("Step 1: catalog")
	s1 := commit("Step 1: fix the test")
	commit("tidy") // not a step's: goes with the step after it
	s2 := commit("Step 2: domain")
	tips, err := StepTips(dir, base)
	if err != nil || len(tips) != 2 || tips[1] != s1 || tips[2] != s2 {
		t.Fatalf("%v %v", tips, err)
	}
	commit("Step 1: late fix")
	if _, err := StepTips(dir, base); err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Errorf("a late fix to step 1 was accepted: %v", err)
	}
	if !strings.Contains(RunAsk("/p.md", true), "only that one step") || !strings.HasPrefix(RunAsk("/p.md", false), "/crew:plan ") {
		t.Error("RunAsk")
	}
}
