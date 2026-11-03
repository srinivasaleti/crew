package pulls

import (
	"path/filepath"
	"strings"

	"crew/internal/gh"
	"crew/internal/pins"
)

// Prefs are your stars (repos pinned on top) and hidden repos or orgs, in <home>/prs.json.
func Prefs(home string) pins.Pins { return pins.Load(PrefsPath(home)) }

// PrefsPath is where they're kept.
func PrefsPath(home string) string { return filepath.Join(home, "prs.json") }

// Hidden reports whether a PR stays out of the list: its repo (owner/name) or its org is hidden. A
// review request never does: someone is waiting on you.
func Hidden(p pins.Pins, it gh.Item) bool {
	org, _, _ := strings.Cut(it.Repo, "/")
	return !it.Review && p.IsHidden(it.Repo, org)
}

// Visible is the list without what you hid, and how many PRs that left out.
func Visible(p pins.Pins, items []gh.Item) (shown []gh.Item, hidden int) {
	for _, it := range items {
		if Hidden(p, it) {
			hidden++
		} else {
			shown = append(shown, it)
		}
	}
	return shown, hidden
}
