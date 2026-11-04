package core

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"crew/internal/repo"
	"crew/internal/tasks"
)

// TestSetRepo moves a task that hasn't started to another attached repo, dropping the old repo's
// session, branch and PR, and refuses a repo that isn't there.
func TestSetRepo(t *testing.T) {
	dir := t.TempDir()
	var repos []string
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(dir, n)
		if out, err := exec.Command("git", "init", "-q", p).CombinedOutput(); err != nil {
			t.Skip("no git:", err, string(out))
		}
		repos = append(repos, p)
	}
	c := New(context.Background(), Config{Home: dir, Tasks: tasks.Store{Dir: dir + "/tasks"}, Repos: repo.Store{Path: dir + "/repos"}, Plans: dir + "/plans"})
	for _, p := range repos {
		if _, err := c.cfg.Repos.Attach(p); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := c.cfg.Repos.Resolve("a")
	if err := c.cfg.Tasks.Save(tasks.Task{ID: "t1", Title: "x", Repo: a, Session: "s1", Branch: "feat", PR: "https://x/pull/1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetRepo("t1", "nope"); err == nil {
		t.Error("moved to a repo that isn't attached")
	}
	if err := c.SetRepo("t1", "b"); err != nil {
		t.Fatal(err)
	}
	got, _ := c.task("t1")
	if filepath.Base(got.Repo) != "b" || got.Session != "" || got.Branch != "" || got.PR != "" || len(got.Previous) != 1 {
		t.Errorf("after SetRepo: %+v", got)
	}
}
