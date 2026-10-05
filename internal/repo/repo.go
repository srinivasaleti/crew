// Package repo keeps the list of attached git repositories and manages task worktrees.
package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Store records attached repos as absolute paths, one per line.
type Store struct{ Path string }

func (s Store) List() []string {
	data, _ := os.ReadFile(s.Path)
	var repos []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			repos = append(repos, line)
		}
	}
	return repos
}

// Attach checks that p is inside a git repo and records the repo's top-level folder.
func (s Store) Attach(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	out, err := exec.Command("git", "-C", p, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a git repo", p)
	}
	top := strings.TrimSpace(string(out))
	repos := s.List()
	if slices.Contains(repos, top) {
		return top, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return "", err
	}
	return top, os.WriteFile(s.Path, []byte(strings.Join(append(repos, top), "\n")+"\n"), 0o644)
}

// Resolve maps a repo path or folder name to an attached repo. Unknown paths are attached on the fly.
func (s Store) Resolve(ref string) (string, error) {
	var matches []string
	for _, r := range s.List() {
		if r == ref || filepath.Base(r) == ref {
			matches = append(matches, r)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", fmt.Errorf("%q matches several repos (%s), use the full path", ref, strings.Join(matches, ", "))
	case strings.ContainsAny(ref, "/~"):
		return s.Attach(ref)
	default:
		return "", fmt.Errorf("no attached repo called %q, attach it by path first", ref)
	}
}

// AddWorktree creates the worktree and branch <id> next to the repo, at <repo>.worktrees/<id>,
// or reuses them if they exist.
// ponytail: branches from origin/HEAD as last fetched; add a `git fetch` here if bases go stale.
func AddWorktree(repoPath, id string) (string, error) {
	path := filepath.Join(repoPath+".worktrees", id)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", repoPath}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	base := "HEAD"
	if _, err := git("rev-parse", "--verify", "-q", "origin/HEAD"); err == nil {
		base = "origin/HEAD"
	}
	// --no-track so a later `git push` can't target the base branch.
	out, err := git("worktree", "add", "--no-track", "-b", id, path, base)
	if err != nil {
		// The branch may survive from an earlier worktree; check it out instead.
		out2, err2 := git("worktree", "add", path, id)
		if err2 != nil {
			return "", fmt.Errorf("git worktree add: %s\n%s", out, out2)
		}
	}
	return path, nil
}
