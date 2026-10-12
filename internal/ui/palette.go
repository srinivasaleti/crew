package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The palette (⌘⇧P, or ⌘/ as a shortcut sheet) lists everything crew can do: the ⌘ shortcuts'
// actions, crew's own keys and its slash commands, each with its keys. Type to filter, ⏎ runs one.

type paletteItem struct {
	label, keys string
	run         func(m *Model) tea.Cmd
}

type paletteState struct {
	query string
	sel   int
}

// prettyKeys writes "cmd+alt+left" as "⌘⌥←".
func prettyKeys(k string) string {
	names := map[string]string{"cmd": "⌘", "alt": "⌥", "shift": "⇧", "ctrl": "⌃", "left": "←", "right": "→", "up": "↑", "down": "↓", "enter": "↵", "backspace": "⌫"}
	var b strings.Builder
	for _, p := range strings.Split(k, "+") {
		if n, ok := names[p]; ok {
			b.WriteString(n)
		} else {
			b.WriteString(strings.ToUpper(p))
		}
	}
	return b.String()
}

func (m Model) paletteItems() []paletteItem {
	var items []paletteItem
	keysOf := map[string][]string{} // action -> its ⌘ keys
	for k, a := range m.keys.bindings() {
		keysOf[a] = append(keysOf[a], prettyKeys(k))
	}
	var names []string
	for n := range shortcutActions {
		if n != "none" && n != "palette" && n != "comment" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		sort.Strings(keysOf[n])
		a := shortcutActions[n]
		items = append(items, paletteItem{strings.ToUpper(a.help[:1]) + a.help[1:], strings.Join(keysOf[n], " "), func(m *Model) tea.Cmd {
			nm, cmd := a.run(*m)
			*m = nm.(Model)
			return cmd
		}})
	}
	for _, c := range keyCommands {
		if c.help == "" || c.when != nil && !c.when(&m) {
			continue
		}
		run := c.run
		items = append(items, paletteItem{strings.ToUpper(c.help[:1]) + c.help[1:], cmdName(c.keys[0]), run})
	}
	for _, c := range slashCommands {
		name := c.name
		items = append(items, paletteItem{name + " · " + c.help, "", func(m *Model) tea.Cmd {
			if m.current() == nil {
				m.notice = name + " works in a task's chat · pick a task first"
				return nil
			}
			m.listFocus = false
			cmd := m.setTab(tabChat)
			m.input.SetValue(name + " ")
			m.input.CursorEnd()
			m.input.Focus()
			return cmd
		}})
	}
	return items
}

// paletteMatches is the items that have every word typed.
func (m Model) paletteMatches() []paletteItem {
	words := strings.Fields(strings.ToLower(m.pal.query))
	var out []paletteItem
	for _, it := range m.paletteItems() {
		hay, ok := strings.ToLower(it.label+" "+it.keys), true
		for _, w := range words {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) openPalette() {
	m.modal, m.pal = paletteModal, paletteState{}
	m.input.Blur()
}

func (m Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.paletteMatches()
	switch k := msg.String(); {
	case k == "esc":
		m.closeModal()
	case k == "up":
		m.pal.sel = max(m.pal.sel-1, 0)
	case k == "down":
		m.pal.sel = min(m.pal.sel+1, max(len(items)-1, 0))
	case k == "enter":
		if m.pal.sel < len(items) {
			it := items[m.pal.sel]
			m.closeModal()
			cmd := it.run(&m)
			return m, cmd
		}
	case k == "backspace":
		if q := []rune(m.pal.query); len(q) > 0 {
			m.pal.query, m.pal.sel = string(q[:len(q)-1]), 0
		}
	case msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace:
		m.pal.query, m.pal.sel = m.pal.query+string(msg.Runes), 0
		if msg.Type == tea.KeySpace {
			m.pal.query += " "
		}
	}
	return m, nil
}

func (m Model) paletteView() string {
	w := min(max(m.chatWidth()-8, 50), 100)
	items := m.paletteMatches()
	rows := max(m.bodyHeight()-12, 5)
	top := max(m.pal.sel-rows+1, 0)
	lines := []string{bold.Render("Commands and shortcuts") + dim.Render("   type to filter · ⏎ runs · esc closes"), "",
		sel.Render("> ") + m.pal.query + "▏", ""}
	for i := top; i < len(items) && i < top+rows; i++ {
		it := items[i]
		keys := dim.Render(it.keys)
		label := ansi.Truncate(it.label, w-4-ansi.StringWidth(it.keys)-2, "…")
		gap := strings.Repeat(" ", max(w-4-ansi.StringWidth(label)-ansi.StringWidth(it.keys), 1))
		line := "  " + label + gap + keys
		if i == m.pal.sel {
			line = focusBar.Width(w - 2).Render("▶ " + ansi.Strip(label) + gap + it.keys)
		}
		lines = append(lines, line)
	}
	if len(items) == 0 {
		lines = append(lines, dim.Render("  nothing matches"))
	}
	if n := len(items); n > rows {
		lines = append(lines, "", dim.Render(fmt.Sprintf("  %d of %d · ↑↓ for more", min(rows, n-top), n)))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1).Width(w).Render(strings.Join(lines, "\n"))
}
