// Package repo keeps the list of attached git repositories and manages task worktrees.
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"crew/internal/proc"
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
	out, err := proc.Command(context.Background(), "git", "-C", p, "rev-parse", "--show-toplevel").Output()
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
	out, err := proc.Command(context.Background(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// AddWorktree creates the worktree and branch <id> next to the repo, at <repo>.worktrees/<id>,
// or reuses them if they exist. The branch starts from from (a subtask's parent branch), or from
// origin/HEAD, fetched first so a new task starts from today's code (offline, it starts from the
// last fetch). base is the commit the branch starts from ("" when reused).
func AddWorktree(repoPath, id, from string) (path, base string, err error) {
	path = filepath.Join(repoPath+".worktrees", id)
	if _, err := os.Stat(path); err == nil {
		return path, "", nil
	}
	if from == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		proc.Command(ctx, "git", "-C", repoPath, "fetch", "--quiet", "origin").Run()
		cancel()
		from = "HEAD"
		if _, err := git(repoPath, "rev-parse", "--verify", "-q", "origin/HEAD"); err == nil {
			from = "origin/HEAD"
		}
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

// WorktreeOf is the worktree that has branch checked out (the repo's own folder included), or "".
func WorktreeOf(repoPath, branch string) string {
	out, err := git(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		}
		if line == "branch refs/heads/"+branch {
			return path
		}
	}
	return ""
}

// UseBranch is the worktree for an existing branch, or a PR's ("#1330" or "1330", read with gh): the
// one that already has it checked out, else a new one at <repo>.worktrees/<branch> (slashes become
// dashes) with the branch from this repo, origin, or the PR's head. Git won't check one branch out
// twice, so a task on a branch that has a worktree works in that worktree.
func UseBranch(repoPath, ref string) (path, branch string, err error) {
	branch = strings.TrimSpace(ref)
	pr, _ := strconv.Atoi(strings.TrimPrefix(branch, "#"))
	if pr > 0 {
		cmd := proc.Command(context.Background(), "gh", "pr", "view", strconv.Itoa(pr), "--json", "headRefName", "-q", ".headRefName")
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("gh pr view %d: %s", pr, strings.TrimSpace(string(out)))
		}
		branch = strings.TrimSpace(string(out))
	}
	if branch == "" {
		return "", "", fmt.Errorf("say which branch, or a PR number like #1330")
	}
	if wt := WorktreeOf(repoPath, branch); wt != "" {
		return wt, branch, nil
	}
	git(repoPath, "fetch", "origin", branch) // newest from origin, if it's there
	path = filepath.Join(repoPath+".worktrees", strings.ReplaceAll(branch, "/", "-"))
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("%s already exists and isn't on %s", path, branch)
	}
	// A branch only on origin is checked out as a local branch tracking it (git's own guess).
	out, err := git(repoPath, "worktree", "add", path, branch)
	if err != nil && pr > 0 { // a PR from a fork: its head isn't a branch on origin
		if o, e := git(repoPath, "fetch", "origin", fmt.Sprintf("+pull/%d/head:%s", pr, branch)); e == nil {
			out, err = git(repoPath, "worktree", "add", path, branch)
		} else {
			out = o
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("no branch %s here or on origin: %s", branch, out)
	}
	return path, branch, nil
}

// RemoveIfUnused removes the worktree crew made at path, and its branch, when nothing happened in
// it: no changes, and no commits that origin/HEAD doesn't have. It reports what it did, or why not.
func RemoveIfUnused(repoPath, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s isn't there any more", path)
	}
	if out, _ := git(path, "status", "--porcelain"); out != "" {
		return "", fmt.Errorf("%s has changes, so crew leaves it; remove it yourself if you don't need them", path)
	}
	branch := Branch(path)
	if branch != "" {
		if n, _ := git(path, "rev-list", "--count", "origin/HEAD..HEAD"); n != "" && n != "0" {
			return "", fmt.Errorf("%s has %s commit(s) of its own on %s, so crew leaves it", path, n, branch)
		}
	}
	if out, err := git(repoPath, "worktree", "remove", path); err != nil {
		return "", fmt.Errorf("git worktree remove: %s", out)
	}
	if branch != "" {
		git(repoPath, "branch", "-D", branch) // it had nothing of its own
	}
	return "removed " + path + " and its branch " + branch, nil
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
			out, _ := proc.Command(context.Background(), "git", "-C", wt, "diff", "--color=always", "--no-index", "--", "/dev/null", f).Output()
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

// Merge merges branch into the branch checked out in worktree wt (a subtask's into its parent's),
// always as a merge commit. On a conflict it aborts the merge, leaving wt as it was, and returns the
// files that clashed. Uncommitted changes in branch's own worktree aren't part of it.
func Merge(wt, branch string) (conflicts []string, err error) {
	out, err := git(wt, "merge", "--no-ff", "--no-edit", branch)
	if err == nil {
		return nil, nil
	}
	if u, _ := git(wt, "diff", "--name-only", "--diff-filter=U"); u != "" {
		conflicts = strings.Split(u, "\n")
		git(wt, "merge", "--abort")
	}
	return conflicts, fmt.Errorf("git merge %s: %s", branch, out)
}

// Dirty reports whether worktree wt has changes that aren't committed.
func Dirty(wt string) bool {
	out, err := git(wt, "status", "--porcelain")
	return err == nil && out != ""
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

// Find is the attached repo whose origin is owner/name (e.g. oolio-group/accounts), or "".
func (s Store) Find(nameWithOwner string) string {
	want := strings.ToLower(nameWithOwner)
	for _, r := range s.List() {
		u, err := git(r, "remote", "get-url", "origin") // https://github.com/o/n.git or git@github.com:o/n.git
		u = strings.TrimSuffix(strings.ToLower(u), ".git")
		if err == nil && (strings.HasSuffix(u, "/"+want) || strings.HasSuffix(u, ":"+want)) {
			return r
		}
	}
	return ""
}

// GitHub is the owner/name of each attached repo whose origin is on github.com.
func (s Store) GitHub() []string {
	var out []string
	for _, r := range s.List() {
		u, err := git(r, "remote", "get-url", "origin")
		if err != nil || !strings.Contains(u, "github.com") {
			continue
		}
		u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
		u = u[max(strings.LastIndex(u, "github.com")+len("github.com")+1, 0):] // after github.com/ or github.com:
		if strings.Count(u, "/") == 1 && !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out
}

// PRWorktree checks PR number out of repoPath's origin into <repo>.worktrees/<id>, on branch <id>,
// for chatting about the PR; it's reused if it exists.
// ponytail: the checkout stays at the commit it was made at; Claude can `git pull` the PR's head
// (origin pull/<n>/head) when it needs newer commits.
func PRWorktree(repoPath, id string, number int) (path string, err error) {
	path = filepath.Join(repoPath+".worktrees", id)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if out, err := git(repoPath, "fetch", "origin", fmt.Sprintf("+pull/%d/head:%s", number, id)); err != nil {
		return "", fmt.Errorf("git fetch: %s", out)
	}
	if out, err := git(repoPath, "worktree", "add", path, id); err != nil {
		return "", fmt.Errorf("git worktree add: %s", out)
	}
	return path, nil
}

// ReviewWorktree checks PR number out of repoPath's origin into <repo>.worktrees/review-<n>, for
// reading the code around a PR's diff. remove deletes it again.
func ReviewWorktree(repoPath string, number int) (path string, remove func(), err error) {
	ref := fmt.Sprintf("crew-review-%d", number)
	path = filepath.Join(repoPath+".worktrees", fmt.Sprintf("review-%d", number))
	if out, err := git(repoPath, "fetch", "origin", fmt.Sprintf("+pull/%d/head:%s", number, ref)); err != nil {
		return "", nil, fmt.Errorf("git fetch: %s", out)
	}
	git(repoPath, "worktree", "remove", "--force", path) // left over from an earlier review
	if out, err := git(repoPath, "worktree", "add", "--detach", path, ref); err != nil {
		return "", nil, fmt.Errorf("git worktree add: %s", out)
	}
	return path, func() {
		git(repoPath, "worktree", "remove", "--force", path)
		git(repoPath, "branch", "-D", ref)
	}, nil
}
