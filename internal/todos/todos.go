// Package todos is TODO: checklists of things to do (Inbox, Work…), each item with an optional due
// day and priority. A plain list you tick off; REMINDERS is for alerts and TASKS for agents.
package todos

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Inbox is the list an item goes to when none is named, and the one that's always there.
const Inbox = "Inbox"

// Item is one thing to do.
type Item struct {
	ID       int       `json:"id"`
	Text     string    `json:"text"`
	List     string    `json:"list"`
	Done     bool      `json:"done"`
	DoneAt   time.Time `json:"doneAt,omitzero"`
	Due      string    `json:"due,omitempty"`      // a day, YYYY-MM-DD
	Priority int       `json:"priority,omitempty"` // 0 none, 1 low, 2 medium, 3 high
	Note     string    `json:"note,omitempty"`
	Task     string    `json:"task,omitempty"` // the crew task made from it
}

// Todos are the lists (in your order, empty ones kept) and the items (in your order).
type Todos struct {
	Lists []string `json:"lists"`
	Items []Item   `json:"items"`
}

// Store is TODO's file (~/.crew/todos/todos.json).
type Store struct{ Path string }

// Load reads the lists and items; Inbox is always first.
func (s Store) Load() Todos {
	var t Todos
	if data, err := os.ReadFile(s.Path); err == nil {
		json.Unmarshal(data, &t)
	}
	if !slices.Contains(t.Lists, Inbox) {
		t.Lists = append([]string{Inbox}, t.Lists...)
	}
	return t
}

// Change loads them, lets f change them, and saves them.
// ponytail: load-change-save with no lock; only the page writes this file.
func (s Store) Change(f func(t *Todos) error) error {
	t := s.Load()
	if err := f(&t); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(t, "", "  ")
	return os.WriteFile(s.Path, data, 0o644)
}

// Find is the item with this id, or nil.
func (t *Todos) Find(id int) *Item {
	for i := range t.Items {
		if t.Items[i].ID == id {
			return &t.Items[i]
		}
	}
	return nil
}

// Add puts a new item at the end of its list (Inbox when it names none), adding the list if it's new.
func (t *Todos) Add(it Item) Item {
	for _, x := range t.Items {
		it.ID = max(it.ID, x.ID)
	}
	it.ID++
	if it.List = strings.TrimSpace(it.List); it.List == "" {
		it.List = Inbox
	}
	t.AddList(it.List)
	t.Items = append(t.Items, it)
	return it
}

// AddList adds a list if there isn't one by that name.
func (t *Todos) AddList(name string) {
	if !slices.Contains(t.Lists, name) {
		t.Lists = append(t.Lists, name)
	}
}

// RenameList renames a list and moves its items with it.
func (t *Todos) RenameList(from, to string) error {
	to = strings.TrimSpace(to)
	switch {
	case to == "":
		return errors.New("give the list a name")
	case from == Inbox:
		return errors.New("Inbox can't be renamed")
	case slices.Contains(t.Lists, to):
		return errors.New("there's already a list called " + to)
	}
	i := slices.Index(t.Lists, from)
	if i < 0 {
		return errors.New("no list " + from)
	}
	t.Lists[i] = to
	for j := range t.Items {
		if t.Items[j].List == from {
			t.Items[j].List = to
		}
	}
	return nil
}

// DeleteList removes a list and its items.
func (t *Todos) DeleteList(name string) error {
	if name == Inbox {
		return errors.New("Inbox can't be deleted")
	}
	t.Lists = slices.DeleteFunc(t.Lists, func(l string) bool { return l == name })
	t.Items = slices.DeleteFunc(t.Items, func(it Item) bool { return it.List == name })
	return nil
}

// Move puts item id just before item before (0: at the end of its list), into list ("" keeps it).
func (t *Todos) Move(id, before int, list string) error {
	i := slices.IndexFunc(t.Items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		return errors.New("no such item")
	}
	it := t.Items[i]
	if list != "" {
		it.List = list
		t.AddList(list)
	}
	t.Items = slices.Delete(t.Items, i, i+1)
	j := slices.IndexFunc(t.Items, func(x Item) bool { return x.ID == before })
	if j < 0 {
		j = len(t.Items)
	}
	t.Items = slices.Insert(t.Items, j, it)
	return nil
}

// SetDone ticks an item off, or back on.
func (it *Item) SetDone(done bool, now time.Time) {
	it.Done, it.DoneAt = done, time.Time{}
	if done {
		it.DoneAt = now
	}
}

// ClearDone removes the done items of a list ("" for every list).
func (t *Todos) ClearDone(list string) int {
	n := len(t.Items)
	t.Items = slices.DeleteFunc(t.Items, func(it Item) bool { return it.Done && (list == "" || it.List == list) })
	return n - len(t.Items)
}

var priorities = map[string]int{"!": 1, "!low": 1, "!!": 2, "!med": 2, "!medium": 2, "!!!": 3, "!high": 3}

// Parse reads a quick-add line: "call the bank tomorrow !high #Home" is the text, a due day
// (today, tomorrow, a weekday, next week, 2026-11-20), a priority (!, !!, !!!, !low/!med/!high)
// and a list (#Name). Words it doesn't know stay in the text.
func Parse(line string, now time.Time) Item {
	var it Item
	var text []string
	words := strings.Fields(line)
	for i := 0; i < len(words); i++ {
		w, lw := words[i], strings.ToLower(words[i])
		if p, ok := priorities[lw]; ok {
			it.Priority = p
			continue
		}
		if strings.HasPrefix(w, "#") && len(w) > 1 {
			it.List = w[1:]
			continue
		}
		if lw == "next" && i+1 < len(words) && strings.ToLower(words[i+1]) == "week" {
			it.Due = day(now, daysTo(now, time.Monday))
			i++
			continue
		}
		if d, ok := dueWord(lw, now); ok {
			it.Due = d
			continue
		}
		text = append(text, w)
	}
	it.Text = strings.Join(text, " ")
	return it
}

// dueWord is the day a single word means, if it means one.
func dueWord(w string, now time.Time) (string, bool) {
	switch w {
	case "today":
		return day(now, 0), true
	case "tomorrow", "tmr":
		return day(now, 1), true
	}
	if t, err := time.Parse("2006-01-02", w); err == nil {
		return t.Format("2006-01-02"), true
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		name := strings.ToLower(d.String())
		if w == name || w == name[:3] {
			return day(now, daysTo(now, d)), true
		}
	}
	return "", false
}

// daysTo is how many days until the next weekday d (a week away when it's today).
func daysTo(now time.Time, d time.Weekday) int {
	if n := (int(d) - int(now.Weekday()) + 7) % 7; n > 0 {
		return n
	}
	return 7
}

// day is now plus n days, as YYYY-MM-DD.
func day(now time.Time, n int) string { return now.AddDate(0, 0, n).Format("2006-01-02") }
