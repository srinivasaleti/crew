// Package keys is crew's keys: the ⌘ shortcuts' bindings (the defaults and ~/.crew/keys), decoding
// the ⌘ and modified keys the app sends, and how hints name keys (ctrl+f is shown as ⌘F).
package keys

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// shortcut binds ⌘ keys to an action.
type shortcut struct{ keys, action string }

// defaultShortcuts are the bindings before ~/.crew/keys changes them.
var defaultShortcuts = []shortcut{
	{"cmd+alt+left", "panel-left"}, {"cmd+alt+right", "panel-right"},
	{"cmd+alt+up", "panel-up"}, {"cmd+alt+down", "panel-down"},
	{"cmd+[", "panel-prev"}, {"cmd+]", "panel-next"}, {"cmd+alt+t", "panel-next"},
	{"cmd+d", "shell-new"}, {"cmd+shift+d", "shell-new"},
	{"cmd+w", "close"},
	{"cmd+k", "clear"},
	{"cmd+1", "tab-1"}, {"cmd+2", "tab-2"}, {"cmd+3", "tab-3"}, {"cmd+4", "tab-4"}, {"cmd+5", "tab-5"}, {"cmd+6", "tab-6"},
	{"cmd+shift+[", "tab-prev"}, {"cmd+shift+]", "tab-next"},
	{"cmd+f", "find"},
	{"cmd+c", "copy"},
	{"cmd+shift+z", "redo"},
	{"cmd+shift+p", "palette"}, {"cmd+/", "comment"},
	{"cmd+shift+f", "focus"}, {"cmd+shift+e", "extend"},
	{"cmd+backspace", "task-delete"},
	{"cmd+shift+h", "home"},
	{"cmd+q", "quit"},
	{"cmd+shift+enter", "maximize"},
	{"cmd+up", "list"}, {"cmd+shift+up", "item-prev"}, {"cmd+shift+down", "item-next"},
	{"cmd+alt+shift+left", "shells-wider"}, {"cmd+alt+shift+right", "shells-narrower"},
}

// Map is the bindings in use, re-read when the keys file changes.
type Map struct {
	Path  string // the keys file (/keys opens it)
	mod   time.Time
	binds map[string]string
}

// Bindings are the defaults with the keys file's lines on top.
func (k *Map) Bindings() map[string]string {
	st, err := os.Stat(k.Path)
	if k.binds != nil && (err != nil && k.mod.IsZero() || err == nil && st.ModTime().Equal(k.mod)) {
		return k.binds
	}
	k.binds = map[string]string{}
	for _, s := range defaultShortcuts {
		k.binds[s.keys] = s.action
	}
	if err != nil {
		return k.binds
	}
	k.mod = st.ModTime()
	data, _ := os.ReadFile(k.Path)
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		keys, action, ok := strings.Cut(line, "=")
		if ok {
			k.binds[normKeys(keys)] = strings.TrimSpace(action)
		}
	}
	return k.binds
}

// normKeys writes key names one way: lower case, no spaces, "option" and "opt" as alt.
func normKeys(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	return strings.NewReplacer("option+", "alt+", "opt+", "alt+", "⌘", "cmd+", "command+", "cmd+").Replace(s)
}

// File is the keys file with every binding in use, and the actions there are (by name, with what
// each does).
func (k *Map) File(actions map[string]string) string {
	var b strings.Builder
	b.WriteString("# crew's ⌘ shortcuts (in the app). One per line: keys = action. Save to apply.\n")
	b.WriteString("# Keys: cmd plus any of ctrl, alt (option), shift, then a letter, digit, [ ], enter, backspace or an arrow\n")
	b.WriteString("# (left, right, up, down). A ⌘ letter that isn't bound does what ctrl and the letter do.\n#\n# Actions:\n")
	var names []string
	for n := range actions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "#   %-12s %s\n", n, actions[n])
	}
	b.WriteString("\n")
	binds := k.Bindings()
	var keys []string
	for key := range binds {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "%s = %s\n", key, binds[key])
	}
	return b.String()
}

// DecodeCmd names a ⌘ key the app sends: ESC [ code ; mods u, or ESC [ 1 ; mods A–D for the
// arrows, with the super bit (8) in mods. Terminals keep ⌘ keys to themselves; only the app sends them.
func DecodeCmd(s string) (string, bool) {
	seq, ok := csiBytes(s)
	if !ok || len(seq) < 3 {
		return "", false
	}
	end := seq[len(seq)-1]
	code, mod, ok := strings.Cut(seq[:len(seq)-1], ";")
	c, err1 := strconv.Atoi(code)
	md, err2 := strconv.Atoi(mod)
	if !ok || err1 != nil || err2 != nil || (md-1)&8 == 0 {
		return "", false
	}
	var key string
	switch {
	case end >= 'A' && end <= 'D' && c == 1:
		key = map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left"}[end]
	case end == 'u' && c == 13:
		key = "enter"
	case end == 'u' && c > 32 && c < 127:
		key = string(rune(c))
	case end == 'u' && c == 127:
		key = "backspace"
	default:
		return "", false
	}
	bits, name := md-1, "cmd+"
	for _, mm := range []struct {
		bit  int
		name string
	}{{4, "ctrl+"}, {2, "alt+"}, {1, "shift+"}} {
		if bits&mm.bit != 0 {
			name += mm.name
		}
	}
	return name + key, true
}

// active is the keymap in use, for Names (the editor draws its own hints without crew's model).
var active *Map

// Use makes a keymap the one hints name keys by.
func Use(k *Map) { active = k }

var ctrlKey = regexp.MustCompile(`\bctrl\+([a-z])\b`)

// Names writes crew's ctrl keys in hints as the ⌘ keys you press: ctrl+f as ⌘F. Hints only ever
// show ⌘ keys; a ⌘ letter bound to an action that isn't its ctrl key (a /keys rebinding) keeps ctrl.
func Names(s string) string {
	if active == nil {
		return s
	}
	binds := active.Bindings()
	return ctrlKey.ReplaceAllStringFunc(s, func(k string) string {
		letter := k[len("ctrl+"):]
		if action, bound := binds["cmd+"+letter]; bound && ctrlFor[action] != k {
			return k
		}
		return "⌘" + strings.ToUpper(letter)
	})
}

// ctrlFor is the ctrl key an action does the same as, so its ⌘ key can stand for it in hints.
var ctrlFor = map[string]string{"find": "ctrl+f", "redo": "ctrl+y", "close": "ctrl+w", "copy": "ctrl+c", "clear": "ctrl+k", "shell-new": "ctrl+d"}

// ShiftEnter holds the ways terminals report Shift+Enter once main turns on xterm's
// modifyOtherKeys (ESC[>4;1m): xterm style and CSI-u style. bubbletea v1 doesn't decode
// them and delivers them as an unknown-CSI message whose String() has this form.
var ShiftEnter = map[string]bool{
	fmt.Sprintf("?CSI%+v?", []byte("27;2;13~")): true,
	fmt.Sprintf("?CSI%+v?", []byte("13;2u")):    true,
}

// csiBytes is the sequence inside an escape bubbletea didn't recognise, which it reports as
// "?CSI[49 51 59 50 117]?" (the bytes after ESC [).
func csiBytes(s string) (string, bool) {
	inner, ok := strings.CutPrefix(s, "?CSI[")
	if inner, ok = strings.CutSuffix(inner, "]?"); !ok {
		return "", false
	}
	var raw []byte
	for _, f := range strings.Fields(inner) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return "", false
		}
		raw = append(raw, byte(n))
	}
	return string(raw), true
}

// DecodeModified turns a key reported in xterm's modifyOtherKeys form (ESC[27;<mod>;<code>~)
// or CSI-u form (ESC[<code>;<mod>u) back into a normal key. main turns modifyOtherKeys on for
// Shift+Enter, and the terminal then reports some other combinations (like ctrl+]) this way too;
// bubbletea v1 hands those over undecoded, as a Stringer of the form "?CSI[<bytes>]?".
func DecodeModified(s string) (tea.KeyMsg, bool) {
	seq, ok := csiBytes(s)
	if !ok {
		return tea.KeyMsg{}, false
	}
	var code, mod int
	switch {
	case strings.HasPrefix(seq, "27;") && strings.HasSuffix(seq, "~"):
		if _, err := fmt.Sscanf(seq, "27;%d;%d~", &mod, &code); err != nil {
			return tea.KeyMsg{}, false
		}
	case strings.HasSuffix(seq, "u"):
		if _, err := fmt.Sscanf(seq, "%d;%du", &code, &mod); err != nil {
			return tea.KeyMsg{}, false
		}
	default:
		return tea.KeyMsg{}, false
	}
	bits := mod - 1 // 1 shift, 2 alt, 4 ctrl
	alt, ctrl := bits&2 != 0, bits&4 != 0
	switch {
	case ctrl && (code >= '@' && code <= '_' || code >= 'a' && code <= 'z'):
		return tea.KeyMsg{Type: tea.KeyType(code & 0x1f), Alt: alt}, true // control code, e.g. ctrl+] = 29
	case code == 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}, true
	case code == 9:
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case code == 27:
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}, true
	case code == 127:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	case code >= 32 && code < 0x110000:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(code)}, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}

// Pretty writes "cmd+alt+left" as "⌘⌥←".
func Pretty(k string) string {
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

// cmdOf is the ⌘ key for a command whose ctrl key's ⌘ letter does something else: ⌘D opens a
// shell, so deleting a task is ⌘⌫; the app quits on ⌘Q.
var cmdOf = map[string]string{"ctrl+d": "cmd+backspace", "ctrl+c": "cmd+q"}

// CmdName is how a command's key is shown: its ⌘ key, e.g. ctrl+s as ⌘S.
func CmdName(k string) string {
	if c, ok := cmdOf[k]; ok {
		return Pretty(c)
	}
	return Names(k)
}
