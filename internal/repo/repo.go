// Package repo finds the git repositories crew can assign tasks to.
package repo

import (
	"os"
	"path/filepath"
)

// List returns the names of direct subfolders of root that are git repos.
func List(root string) []string {
	entries, _ := os.ReadDir(root)
	var repos []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), ".git")); e.IsDir() && err == nil {
			repos = append(repos, e.Name())
		}
	}
	return repos
}
