package todos

import (
	"path/filepath"
	"testing"
	"time"
)

// TestParse checks a quick-add line's due day, priority and list come out of its text.
func TestParse(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local) // a Wednesday
	for _, tc := range []struct {
		line string
		want Item
	}{
		{"call the bank tomorrow !high #Home", Item{Text: "call the bank", Due: "2026-11-05", Priority: 3, List: "Home"}},
		{"ship it fri !!", Item{Text: "ship it", Due: "2026-11-06", Priority: 2}},
		{"plan wed", Item{Text: "plan", Due: "2026-11-11"}}, // today's weekday is next week
		{"review next week", Item{Text: "review", Due: "2026-11-09"}},
		{"taxes 2026-12-01 !", Item{Text: "taxes", Due: "2026-12-01", Priority: 1}},
		{"buy milk", Item{Text: "buy milk"}},
	} {
		if got := Parse(tc.line, now); got != tc.want {
			t.Errorf("%q: %+v, want %+v", tc.line, got, tc.want)
		}
	}
}

// TestStore checks adding, moving, renaming a list and clearing done items, saved and read back.
func TestStore(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "todos.json")}
	err := s.Change(func(td *Todos) error {
		a := td.Add(Item{Text: "a"})
		b := td.Add(Item{Text: "b", List: "Work"})
		if err := td.Move(b.ID, a.ID, Inbox); err != nil {
			return err
		}
		td.Find(a.ID).SetDone(true, time.Now())
		td.ClearDone(Inbox)
		return td.RenameList("Work", "Job")
	})
	if err != nil {
		t.Fatal(err)
	}
	td := s.Load()
	if len(td.Items) != 1 || td.Items[0].Text != "b" || td.Items[0].List != Inbox || td.Lists[0] != Inbox || td.Lists[1] != "Job" {
		t.Errorf("got %+v", td)
	}
	if td.RenameList(Inbox, "x") == nil || td.DeleteList(Inbox) == nil {
		t.Error("Inbox was renamed or deleted")
	}
}
