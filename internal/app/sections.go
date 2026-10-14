package app

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/section"
)

// Sections are crew's parts you can turn on or off from home: TASKS, PULL REQUESTS, REQUESTS. One
// that's off has no sidebar row and does no work in the background (PULL REQUESTS stops polling
// GitHub). Which ones are off is ~/.crew/sections, one key per line; all are on by default. A new
// section is a plugin (the section package) with its own Def, in m.secs.

// builtinDefs are the sections crew still draws itself, in home's order; the plugin sections
// (m.secs) come after them, each with its own Def.
var builtinDefs = []section.Def{
	{Key: "tasks", Label: "TASKS", Letter: "t", About: "tasks, each with its own worktree, branch and agent chat"},
	{Key: "prs", Label: "PULL REQUESTS", Letter: "p", About: "PRs waiting for your review and yours, from GitHub (gh)"},
	{Key: "reqs", Label: "REQUESTS", Letter: "r", About: "an HTTP client for your APIs, with a locked-down Claude chat"},
	{Key: "reminders", Label: "REMINDERS", Letter: "m", About: "reminders, once or repeating, and a Claude chat that manages them"},
	{Key: "focus", Label: "FOCUS", Letter: "f", About: "a focus timer, eye and stand-up breaks, reminders and your screen time"},
}

// defs are every section, in home's order.
func (m Model) defs() []section.Def {
	defs := slices.Clone(builtinDefs)
	for _, s := range m.secs {
		defs = append(defs, s.Def())
	}
	return defs
}

// loadSectionsOff reads which sections are turned off.
func loadSectionsOff(path string) map[string]bool {
	off := map[string]bool{}
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if line, _, _ = strings.Cut(line, "#"); strings.TrimSpace(line) != "" {
			off[strings.TrimSpace(line)] = true
		}
	}
	return off
}

// sectionOn reports whether a section is turned on.
func (m Model) sectionOn(key string) bool { return !m.sectionsOff[key] }

// toggleSection turns a section on or off and saves it. Turning one off while you're in it takes
// you home; turning PULL REQUESTS on reads the list.
func (m *Model) toggleSection(key string) tea.Cmd {
	m.sectionsOff[key] = !m.sectionsOff[key]
	var off []string
	for _, s := range m.defs() {
		if m.sectionsOff[s.Key] {
			off = append(off, s.Key)
		}
	}
	data := "# crew's sections that are turned off, one per line (home: each one's letter turns it on and off)\n" + strings.Join(off, "\n") + "\n"
	if err := os.WriteFile(m.cfg.Sections, []byte(data), 0o644); err != nil {
		m.notice = err.Error()
	}
	defs := m.defs()
	i := slices.IndexFunc(defs, func(s section.Def) bool { return s.Key == key })
	state := "on"
	if m.sectionsOff[key] {
		state = "off"
	}
	m.notice = defs[i].Label + " is " + state
	if m.selRow().Kind == rowHome || slices.ContainsFunc(m.rows(), func(r row) bool { return m.rowKey(r) == m.selKey }) {
		m.refresh()
	} else {
		m.goHome() // what you were in went away with its section
	}
	if key == "prs" && !m.sectionsOff[key] {
		return m.pollPRs()
	}
	if key == "focus" && !m.sectionsOff[key] {
		return m.restartFocusTick()
	}
	if key == "reminders" && !m.sectionsOff[key] {
		return m.restartRemTick()
	}
	if s := m.secOf(key); s != nil && !m.sectionsOff[key] {
		return s.On(m)
	}
	return nil
}

// sectionsHome is home's Sections block, and which of its lines is which section (for clicks).
func (m Model) sectionsHome() (string, map[int]string) {
	at := map[int]string{}
	lines := []string{bold.Render("Sections") + dim.Render("  turn on what you use; the letter turns one on or off"), ""}
	for _, s := range m.defs() {
		mark, label := green.Render("[✓]"), bold.Render(fmt.Sprintf("%-14s", s.Label))
		if !m.sectionOn(s.Key) {
			mark, label = dim.Render("[ ]"), dim.Render(fmt.Sprintf("%-14s", s.Label))
		}
		at[len(lines)] = s.Key
		lines = append(lines, "  "+mark+" "+label+" "+sel.Render(s.Letter)+"  "+dim.Render(s.About))
	}
	return strings.Join(lines, "\n") + "\n\n", at
}

// sectionKey turns a section on or off by its letter on home.
func (m *Model) sectionKey(k string) (tea.Cmd, bool) {
	for _, s := range m.defs() {
		if s.Letter == k {
			return m.toggleSection(s.Key), true
		}
	}
	return nil, false
}

// clickHome turns a section on or off when its line on home is clicked (y inside the content).
func (m *Model) clickHome(y int) tea.Cmd {
	setup := strings.Count(m.getStarted(), "\n") // the Sections block comes right after Setup
	_, at := m.sectionsHome()
	if key, ok := at[m.vp.YOffset+y-setup]; ok {
		return m.toggleSection(key)
	}
	return nil
}
