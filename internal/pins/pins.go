// Package pins is your choices about a list (PULL REQUESTS' repos, JIRA's projects): names you
// starred, pinned on top, and names you hid. Each section keeps its own file and decides what a name
// is and what hiding spares.
package pins

import (
	"encoding/json"
	"os"
	"slices"
)

// Pins are the starred and hidden names.
type Pins struct {
	Starred []string `json:"starred"`
	Hidden  []string `json:"hidden"`
}

// Load reads them from path; none yet is empty.
func Load(path string) Pins {
	var p Pins
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &p)
	return p
}

// Save keeps them at path.
func (p Pins) Save(path string) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

// set adds name to list, or takes it out, keeping it sorted and each name once.
func set(list []string, name string, on bool) []string {
	list = slices.DeleteFunc(list, func(s string) bool { return s == name })
	if on {
		list = append(list, name)
	}
	slices.Sort(list)
	return list
}

// Star stars a name, or unstars it.
func (p *Pins) Star(name string, on bool) { p.Starred = set(p.Starred, name, on) }

// Hide hides a name, or shows it again.
func (p *Pins) Hide(name string, on bool) { p.Hidden = set(p.Hidden, name, on) }

// IsStarred reports whether you starred the name.
func (p Pins) IsStarred(name string) bool { return slices.Contains(p.Starred, name) }

// IsHidden reports whether any of names is hidden (a repo, or its org).
func (p Pins) IsHidden(names ...string) bool {
	for _, n := range names {
		if slices.Contains(p.Hidden, n) {
			return true
		}
	}
	return false
}
