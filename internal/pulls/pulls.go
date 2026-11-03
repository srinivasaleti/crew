// Package pulls is PULL REQUESTS without a screen: the list (PRs waiting for your review, yours, and
// the rest in your attached repos), how often it's read, and a PR's chat (its task and its folder).
// Both UIs use it; GitHub is read with the gh service.
package pulls

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/tasks"
)

// Every is how often the list is read again.
const Every = 2 * time.Minute

// List reads the PRs waiting for your review, yours, then the rest in your attached repos: each PR
// once (waiting for you wins, then yours), the most recently updated first in each.
func List(repos repo.Store) ([]gh.Item, error) {
	review, err1 := gh.ToReview()
	mine, err2 := gh.Mine()
	others, err3 := gh.InRepos(repos.GitHub())
	var items []gh.Item
	seen := map[string]bool{}
	for _, it := range append(append(review, mine...), others...) {
		if !seen[it.URL] {
			seen[it.URL] = true
			items = append(items, it)
		}
	}
	slices.SortStableFunc(items, func(a, b gh.Item) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return b.Updated.Compare(a.Updated)
	})
	return items, errors.Join(err1, err2, err3)
}

// rank orders the lists: waiting for your review, yours, the rest in your repos.
func rank(it gh.Item) int {
	switch {
	case it.Review:
		return 0
	case it.Mine:
		return 1
	}
	return 2
}

// ChatTask is a new chat about a PR: a task file with pr: set, kept out of the task list. Its repo is
// the attached checkout of the PR's repo, or "" when it isn't on this machine.
func ChatTask(it gh.Item, repos repo.Store) tasks.Task {
	return tasks.Task{
		ID:    tasks.Slugify(fmt.Sprintf("pr %s %d", it.Repo, it.Number)),
		Title: fmt.Sprintf("#%d %s", it.Number, it.Title),
		Repo:  repos.Find(it.Repo),
		PR:    it.URL, Status: tasks.InProgress, Created: time.Now(),
		Body: fmt.Sprintf("You're helping me with pull request %s (%s #%d, %q by @%s). Read it with gh "+
			"(gh pr view %[1]s --comments, gh pr diff %[1]s). Don't push, comment, review or change it on GitHub unless I ask.",
			it.URL, it.Repo, it.Number, it.Title, it.Author),
	}
}

// ChatDir makes the folder a PR's chat runs in: a checkout of the PR when its repo is attached, else
// <chatDir>/<id> (stable, so the agent can resume its session there).
func ChatDir(t tasks.Task, chatDir string) (dir string, err error) {
	if t.Repo == "" {
		dir = filepath.Join(chatDir, t.ID)
		return dir, os.MkdirAll(dir, 0o755)
	}
	n, _ := strconv.Atoi(t.PR[strings.LastIndex(t.PR, "/")+1:]) // …/pull/<n>
	return repo.PRWorktree(t.Repo, t.ID, n)
}
