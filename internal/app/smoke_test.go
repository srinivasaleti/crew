package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// Smoke tests: they drive crew the way keys do (Update, then View) and check what you'd see break
// first: quitting, making a task, every screen drawing, ⌘1… on a list. They don't run the commands
// crew starts (git, gh, the agents); a test that needs one is a sign the code blocks Update.

// newModel is crew on an empty folder, sized like a window, with tasks made first.
func newModel(t *testing.T, ts ...tasks.Task) Model {
	t.Helper()
	home := t.TempDir()
	cfg := Config{Ctx: context.Background(), Tasks: tasks.Store{Dir: filepath.Join(home, "tasks")}, Chats: chat.Log{Dir: filepath.Join(home, "chats")},
		Repos: repo.Store{Path: filepath.Join(home, "repos")}, Plans: filepath.Join(home, "plans"), Splits: filepath.Join(home, "splits"),
		PRChat: filepath.Join(home, "prchats"), JiraChat: filepath.Join(home, "jirachats"), Reqs: filepath.Join(home, "requests"),
		Keys: filepath.Join(home, "keys"), Sections: filepath.Join(home, "sections"), Focus: filepath.Join(home, "focus"),
		Reminders: filepath.Join(home, "reminders"), Notify: filepath.Join(home, "notify"), Containers: filepath.Join(home, "containers")}
	for _, tk := range ts {
		if err := cfg.Tasks.Save(tk); err != nil {
			t.Fatal(err)
		}
	}
	m := New(cfg)
	m, _ = press(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	return m
}

// press hands crew a message and draws the screen, failing if either takes long (Update must
// never block) or panics.
func press(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	type result struct {
		m   Model
		cmd tea.Cmd
	}
	done := make(chan result, 1)
	go func() {
		next, cmd := m.Update(msg)
		nm := next.(Model)
		_ = nm.View()
		done <- result{nm, cmd}
	}()
	select {
	case r := <-done:
		return r.m, r.cmd
	case <-time.After(2 * time.Second):
		t.Fatalf("Update or View blocked on %#v", msg)
	}
	return m, nil
}

// keyMsg is a key as crew gets it: a letter, or a named key like "esc".
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestQuitAsksThenQuits(t *testing.T) {
	m := newModel(t)
	m, _ = press(t, m, keyMsg("ctrl+c"))
	if m.modal != quitModal {
		t.Fatalf("⌘Q should ask first; modal is %d", m.modal)
	}
	_, cmd := press(t, m, keyMsg("y"))
	if cmd == nil {
		t.Fatal("y on Quit crew? did nothing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("y on Quit crew? didn't quit")
	}
	m, _ = press(t, m, keyMsg("n"))
	if m.modal != noModal {
		t.Fatal("any other key should cancel quitting")
	}
}

func TestNewTaskOpensTheForm(t *testing.T) {
	m := newModel(t)
	if m, _ = press(t, m, keyMsg("n")); m.modal != secModal {
		t.Fatal("n on home should open the new-task form")
	}
	m = newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.Todo, Created: time.Now()})
	m.selectSection("g:tasks")
	m.listFocus = false
	if m, _ = press(t, m, keyMsg("n")); m.modal != secModal {
		t.Fatal("n on TASKS should open the new-task form")
	}
	m, _ = press(t, m, keyMsg("esc"))
	if m.modal != noModal {
		t.Fatal("esc should close the form")
	}
}

func TestEveryScreenDraws(t *testing.T) {
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.InProgress, Created: time.Now()})
	for _, w := range []int{80, 200} {
		m, _ = press(t, m, tea.WindowSizeMsg{Width: w, Height: 40})
		for _, r := range m.rows() {
			m.selectRow(r)
			if m.View() == "" {
				t.Fatalf("%s drew nothing at %d wide", m.rowKey(r), w)
			}
			if m.current() != nil { // a task: each of its tabs
				for i := range tabNames {
					m, _ = press(t, m, keyMsg("esc"))
					m.selectRow(r)
					m.setTab(tab(i))
					_ = m.View()
				}
			}
		}
	}
}

func TestCmdNumbersPickAListsChips(t *testing.T) {
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.Todo, Created: time.Now()})
	m.selectSection("g:tasks")
	if cur, n := m.tabsHere(); cur != 0 || n < 2 {
		t.Fatalf("TASKS' chips should be its tabs; got %d of %d", cur, n)
	}
	m.jumpTab(1)
	if cur, _ := m.tabsHere(); cur != 1 {
		t.Fatalf("⌘2 should pick the second chip; on %d", cur)
	}
}
