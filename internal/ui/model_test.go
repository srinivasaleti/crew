package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/task"
)

// Drives the real key flow: n → type title → tab (switch repo) → enter → d → y.
func TestCreateAndDeleteTask(t *testing.T) {
	store := task.Store{Dir: t.TempDir()}
	var m tea.Model = New(store, "/repos", []string{"accounts", "web"})
	send := func(msgs ...tea.Msg) {
		for _, msg := range msgs {
			m, _ = m.Update(msg)
		}
	}
	send(tea.WindowSizeMsg{Width: 100, Height: 30},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Fix login loop")},
		tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyEnter})

	tasks, err := store.Load()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d (%v)", len(tasks), err)
	}
	if got := tasks[0]; got.ID != "fix-login-loop" || got.Repo != "web" || got.Status != task.Todo {
		t.Fatalf("unexpected task %+v", got)
	}
	if v := m.View(); !strings.Contains(v, "Fix login loop") {
		t.Fatalf("task missing from view:\n%s", v)
	}

	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if tasks, _ := store.Load(); len(tasks) != 0 {
		t.Fatalf("task not deleted: %+v", tasks)
	}
}
