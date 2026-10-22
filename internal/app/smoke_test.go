package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/usage"
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

func TestCommentOnTheAgentsDiff(t *testing.T) {
	diff := strings.Join([]string{
		" main.go | 3 ++-",
		"diff --git a/main.go b/main.go",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -10,3 +10,4 @@ func main() {",
		" \tfmt.Println(\"hi\")",
		"-\tos.Exit(1)",
		"+\tos.Exit(0)",
		"+\treturn",
	}, "\n")
	lines := parseDiff(diff)
	if d := lines[7]; d.path != "main.go" || d.line != 11 {
		t.Fatalf("an added line should know its file and new line; got %+v", d)
	}
	if d := lines[6]; d.old != 11 || d.line != 0 {
		t.Fatalf("a removed line should know its old line; got %+v", d)
	}
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.InProgress, Created: time.Now()})
	m.selectID("fix-login")
	m.setTab(tabChanges)
	m, _ = press(t, m, diffMsg{id: "fix-login", diff: diff, summary: "1 file changed"})
	m.changesOf("fix-login").sel = 7
	m, _ = press(t, m, keyMsg("c"))
	for _, r := range "exit 1 was right" {
		m, _ = press(t, m, keyMsg(string(r)))
	}
	m, _ = press(t, m, keyMsg("enter"))
	if n := m.changesOf("fix-login").notes; len(n) != 1 || n[0].at() != "main.go:11" {
		t.Fatalf("c then ⏎ should keep a comment on main.go:11; got %+v", n)
	}
	m, _ = press(t, m, keyMsg("s"))
	sent := false
	for _, msg := range m.chat.Conv("fix-login").Msgs {
		sent = sent || strings.Contains(msg.Text, "main.go:11") && strings.Contains(msg.Text, "exit 1 was right")
	}
	if !sent {
		t.Fatal("s should send the comments to the task's agent")
	}
	if len(m.changesOf("fix-login").notes) != 0 {
		t.Fatal("sent comments should be cleared")
	}
}

// runChecks runs what CheckFirst started and hands crew the result.
func runChecks(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	first := cmd()
	if msg, ok := first.(verifyMsg); ok {
		m, _ = press(t, m, msg)
		return m
	}
	batch, _ := first.(tea.BatchMsg)
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(verifyMsg); ok {
			m, _ = press(t, m, msg)
			return m
		}
	}
	t.Fatal("no checks ran")
	return m
}

func TestChecksGateP(t *testing.T) {
	wt := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", wt).CombinedOutput(); err != nil {
		t.Skip("needs git:", string(out))
	}
	os.WriteFile(filepath.Join(wt, "Makefile"), []byte("check:\n\tfalse\n"), 0o644)
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.InProgress, Worktree: wt, Created: time.Now()})
	ready, cmd := m.CheckFirst("fix-login")
	if ready || cmd == nil {
		t.Fatal("P should run the checks first")
	}
	m = runChecks(t, m, cmd)
	if vs := m.verify["fix-login"]; vs == nil || vs.ok || vs.command != "make check" {
		t.Fatalf("make check fails here; got %+v", vs)
	}
	os.WriteFile(filepath.Join(wt, "Makefile"), []byte("check:\n\ttrue\n"), 0o644)
	_, cmd = m.CheckFirst("fix-login")
	m = runChecks(t, m, cmd)
	if ready, _ := m.CheckFirst("fix-login"); !ready {
		t.Fatal("passing checks should let P through")
	}
	m, _ = press(t, m, chat.TurnEndMsg{ID: "fix-login"})
	if ready, _ := m.CheckFirst("fix-login"); ready {
		t.Fatal("after the agent's next turn, the checks should run again before P")
	}
}

func TestBudgetPausesATask(t *testing.T) {
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.InProgress, Budget: 1, Created: time.Now()})
	m.usage["fix-login"] = usage.Usage{Cost: 1.25}
	m.selectID("fix-login")
	m.listFocus = false
	m.input.SetValue("keep going")
	m, _ = press(t, m, keyMsg("enter"))
	if m.input.Value() != "keep going" || !strings.Contains(m.notice, "budget reached") {
		t.Fatalf("a task over its budget shouldn't send, and should keep your message; notice %q", m.notice)
	}
	runBudget(&m, m.current(), "5")
	m, _ = press(t, m, keyMsg("enter"))
	if m.input.Value() != "" {
		t.Fatal("after /budget 5 the message should go")
	}
	if got, _ := tasks.Parse("x", m.current().String()); got.Budget != 5 {
		t.Fatalf("the budget should be kept in the task file; got %v", got.Budget)
	}
}

// run runs a command crew started and hands crew what it says.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected crew to start something")
	}
	m, _ = press(t, m, cmd())
	return m
}

func TestDeleteRemovesTheWorktree(t *testing.T) {
	rp := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=a"}, args...)...).CombinedOutput(); err != nil {
			t.Skip("needs git:", string(out))
		}
	}
	git(rp, "init", "-q")
	git(rp, "commit", "-q", "--allow-empty", "-m", "start")
	wt, _, err := repo.AddWorktree(rp, "fix-login", "")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("wip"), 0o644)
	m := newModel(t, tasks.Task{ID: "fix-login", Title: "Fix login", Status: tasks.InProgress, Repo: rp, Worktree: wt, Created: time.Now()})
	m.openDelete("fix-login")
	m = run(t, m, m.checkDelete())
	if !strings.Contains(m.View(), "uncommitted change") {
		t.Fatal("the delete prompt should warn about uncommitted work in the worktree")
	}
	m, cmd := press(t, m, keyMsg("y"))
	m = run(t, m, cmd)
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("deleting the task should remove its worktree")
	}
	if out, _ := exec.Command("git", "-C", rp, "branch", "--list", "fix-login").Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatal("deleting the task should delete its branch")
	}
	if !strings.Contains(m.notice, "and its worktree") {
		t.Fatalf("crew should say the worktree went; notice %q", m.notice)
	}
}
