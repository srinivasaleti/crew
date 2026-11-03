package app

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/core"
	"crew/internal/ui/terminal/section"
)

// Sections are crew's parts you can turn on or off from home: TASKS, PULL REQUESTS, REQUESTS… One
// that's off has no sidebar row and does no work in the background (PULL REQUESTS stops polling
// GitHub). Which ones are off is ~/.crew/sections, one key per line; all are on by default. Each is
// a plugin (the section package, sections/<name>) with its own Def, in m.secs.

// sectionOrder is the sections' order on home (sidebarOrder is the sidebar's).
var sectionOrder = core.SectionOrder

// defs are every section, in home's order.
func (m Model) defs() []section.Def {
	var defs []section.Def
	for _, s := range m.secs {
		defs = append(defs, s.Def())
	}
	slices.SortStableFunc(defs, func(a, b section.Def) int {
		return slices.Index(sectionOrder, a.Key) - slices.Index(sectionOrder, b.Key)
	})
	return defs
}

// sectionOn reports whether a section is turned on.
func (m Model) sectionOn(key string) bool { return !m.sectionsOff[key] }

// toggleSection turns a section on or off and saves it. Turning one off while you're in it takes
// you home; turning one on starts it again (PULL REQUESTS reads its list).
func (m *Model) toggleSection(key string) tea.Cmd {
	m.sectionsOff[key] = !m.sectionsOff[key]
	if err := core.SaveSectionsOff(m.cfg.Sections, m.sectionsOff); err != nil {
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
