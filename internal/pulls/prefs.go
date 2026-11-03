package pulls

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"crew/internal/gh"
)

// Prefs are your choices about the list: repos you starred (pinned on top) and repos or whole orgs
// you hid. Kept in <home>/prs.json, so both UIs share them.
type Prefs struct {
	Starred []string `json:"starred"` // owner/name
	Hidden  []string `json:"hidden"`  // owner/name, or owner for a whole org
}

func prefsPath(home string) string { return filepath.Join(home, "prs.json") }

// LoadPrefs reads your choices; none yet is empty.
func LoadPrefs(home string) Prefs {
	var p Prefs
	b, _ := os.ReadFile(prefsPath(home))
	json.Unmarshal(b, &p)
	return p
}

// Save keeps them.
func (p Prefs) Save(home string) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(prefsPath(home), b, 0o644)
}

// set adds name to list, or takes it out.
func set(list []string, name string, on bool) []string {
	list = slices.DeleteFunc(list, func(s string) bool { return s == name })
	if on {
		list = append(list, name)
	}
	slices.Sort(list)
	return list
}

// Star stars a repo (owner/name), or unstars it.
func (p *Prefs) Star(repo string, on bool) { p.Starred = set(p.Starred, repo, on) }

// Hide hides a repo (owner/name) or an org (owner), or shows it again.
func (p *Prefs) Hide(name string, on bool) { p.Hidden = set(p.Hidden, name, on) }

// IsStarred reports whether you starred the repo.
func (p Prefs) IsStarred(repo string) bool { return slices.Contains(p.Starred, repo) }

// IsHidden reports whether a PR stays out of the list: its repo or its org is hidden. A review
// request never does: someone is waiting on you.
func (p Prefs) IsHidden(it gh.Item) bool {
	if it.Review {
		return false
	}
	org, _, _ := strings.Cut(it.Repo, "/")
	return slices.Contains(p.Hidden, it.Repo) || slices.Contains(p.Hidden, org)
}

// Visible is the list without what you hid, and how many PRs that left out.
func (p Prefs) Visible(items []gh.Item) (shown []gh.Item, hidden int) {
	for _, it := range items {
		if p.IsHidden(it) {
			hidden++
		} else {
			shown = append(shown, it)
		}
	}
	return shown, hidden
}
