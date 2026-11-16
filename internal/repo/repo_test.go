package repo

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAddWorktreeNoCommits checks that a repo with no commits gets a plain message, not git's.
func TestAddWorktreeNoCommits(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	_, _, err := AddWorktree(dir, "t1", "")
	if err == nil || !strings.Contains(err.Error(), "no commits yet") {
		t.Fatalf("want a no-commits error, got %v", err)
	}
}
