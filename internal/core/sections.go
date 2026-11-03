package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Sections are crew's parts you turn on or off on home (TASKS, PULL REQUESTS…). Both UIs show them in
// this order; ~/.crew/sections lists the ones that are off, one key per line.

// SectionOrder is every section's key, in home's order.
var SectionOrder = []string{"tasks", "prs", "reqs", "reminders", "focus", "jira", "docker", "kube"}

// SectionsOff reads which sections are turned off, from the file at path.
func SectionsOff(path string) map[string]bool {
	off := map[string]bool{}
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if line, _, _ = strings.Cut(line, "#"); strings.TrimSpace(line) != "" {
			off[strings.TrimSpace(line)] = true
		}
	}
	return off
}

// SectionOn reports whether a section is turned on.
func (c *Core) SectionOn(key string) bool {
	return !SectionsOff(filepath.Join(c.cfg.Home, "sections"))[key]
}

// Home is crew's folder (~/.crew), where each section keeps its things.
func (c *Core) Home() string { return c.cfg.Home }
