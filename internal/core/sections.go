package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Sections are crew's parts you turn on or off on home (TASKS, PULL REQUESTS…). Both UIs show them in
// this order; ~/.crew/sections lists the ones that are off, one key per line.

// SectionOrder is every section's key, in home's order.
var SectionOrder = []string{"tasks", "prs", "jira", "mcp", "reqs", "reminders", "todos", "notes", "focus", "docker", "kube"}

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

// SaveSectionsOff writes which sections are off to the file at path.
func SaveSectionsOff(path string, off map[string]bool) error {
	var keys []string
	for _, k := range SectionOrder {
		if off[k] {
			keys = append(keys, k)
		}
	}
	data := "# crew's sections that are turned off, one per line (home: each one's letter turns it on and off)\n" + strings.Join(keys, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0o644)
}

// SetSectionOn turns a section on or off, and tells the UIs.
func (c *Core) SetSectionOn(key string, on bool) error {
	path := filepath.Join(c.cfg.Home, "sections")
	off := SectionsOff(path)
	off[key] = !on
	if err := SaveSectionsOff(path, off); err != nil {
		return err
	}
	c.publish(Event{Kind: "sections", ID: key})
	return nil
}

// SectionOn reports whether a section is turned on.
func (c *Core) SectionOn(key string) bool {
	return !SectionsOff(filepath.Join(c.cfg.Home, "sections"))[key]
}

// Home is crew's folder (~/.crew), where each section keeps its things.
func (c *Core) Home() string { return c.cfg.Home }
