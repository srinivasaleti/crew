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

// git runs a git command in dir and returns its trimmed combined output.
func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// AddWorktree creates the worktree and branch <id> next to the repo, at <repo>.worktrees/<id>,
// or reuses them if they exist. base is the commit the branch starts from ("" when reused).
// ponytail: branches from origin/HEAD as last fetched; add a `git fetch` here if bases go stale.
func AddWorktree(repoPath, id string) (path, base string, err error) {
	path = filepath.Join(repoPath+".worktrees", id)
	if _, err := os.Stat(path); err == nil {
		return path, "", nil
	}
	from := "HEAD"
	if _, err := git(repoPath, "rev-parse", "--verify", "-q", "origin/HEAD"); err == nil {
		from = "origin/HEAD"
	}
	base, _ = git(repoPath, "rev-parse", from)
	// --no-track so a later `git push` can't target the base branch.
	out, err := git(repoPath, "worktree", "add", "--no-track", "-b", id, path, from)
	if err != nil {
		// The branch may survive from an earlier worktree; check it out instead.
		out2, err2 := git(repoPath, "worktree", "add", path, id)
		if err2 != nil {
			return "", "", fmt.Errorf("git worktree add: %s\n%s", out, out2)
		}
		base = ""
	}
	return path, base, nil
}

// Diff returns everything changed in worktree wt since base (committed, uncommitted and
// untracked files) as a colored per-file summary followed by the full diff, plus a one-line summary.
// An empty base falls back to the merge-base with origin/HEAD, then to HEAD.
func Diff(wt, base string) (diff, summary string, err error) {
	base = diffBase(wt, base)
	stat, err := git(wt, "diff", "--stat", "--color=always", base)
	if err != nil {
		return "", "", fmt.Errorf("git diff: %s", stat)
	}
	full, _ := git(wt, "diff", "--color=always", base)
	summary, _ = git(wt, "diff", "--shortstat", base)

	untracked, _ := git(wt, "ls-files", "--others", "--exclude-standard")
	var newFiles []string
	if untracked != "" {
		newFiles = strings.Split(untracked, "\n")
	}
	// ponytail: shows the first 50 new files in full; enough for agent work, page it if repos generate more.
	var b strings.Builder
	for i, f := range newFiles {
		stat += "\n new (untracked) " + f
		if i < 50 {
			// --no-index exits 1 when files differ, so the error is expected.
			out, _ := exec.Command("git", "-C", wt, "diff", "--color=always", "--no-index", "--", "/dev/null", f).Output()
			b.Write(out)
		}
	}
	if len(newFiles) > 0 {
		if summary != "" {
			summary += ", "
		}
		summary += fmt.Sprintf("%d new file(s)", len(newFiles))
	}
	if summary == "" {
		return "", "no changes yet", nil
	}
	return stat + "\n\n" + strings.TrimRight(full+"\n"+b.String(), "\n"), strings.TrimSpace(summary), nil
}

// Branch is the branch checked out in worktree wt, or "" when it can't tell (or HEAD is detached).
func Branch(wt string) string {
	b, err := git(wt, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || b == "HEAD" {
		return ""
	}
	return b
}

// diffBase is base, or when it's empty the merge-base with origin/HEAD, else HEAD.
func diffBase(wt, base string) string {
	if base != "" {
		return base
	}
	if mb, err := git(wt, "merge-base", "HEAD", "origin/HEAD"); err == nil {
		return mb
	}
	return "HEAD"
}

// Changed maps each file changed in worktree wt since base (committed or not) to a letter:
// M modified, A added, D deleted, U new and untracked.
func Changed(wt, base string) map[string]byte {
	out := map[string]byte{}
	if s, err := git(wt, "diff", "--name-status", "--no-renames", diffBase(wt, base)); err == nil && s != "" {
		for _, line := range strings.Split(s, "\n") {
			if st, p, ok := strings.Cut(line, "\t"); ok && st != "" {
				out[p] = st[0]
			}
		}
	}
	if s, err := git(wt, "ls-files", "--others", "--exclude-standard"); err == nil && s != "" {
		for _, p := range strings.Split(s, "\n") {
			out[p] = 'U'
		}
	}
	return out
}

// Files lists the repo's files relative to dir: tracked plus untracked ones that aren't ignored.
func Files(dir string) ([]string, error) {
	out, err := git(dir, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %s", out)
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
