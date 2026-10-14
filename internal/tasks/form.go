package tasks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/agent"
	"crew/internal/look"
	"crew/internal/repo"
)

// Form is the new-task form: a name, a repo folder (your recent repos to pick from), the agent and
// what to do.
type Form struct {
	tasks   Store
	repos   repo.Store
	field   int // focused field: 0 name, 1 folder, 2 agent, 3 branch, 4 description
	agent   int // the agent picked, an index into agent.All
	name    textinput.Model
	folder  textinput.Model
	branch  textinput.Model // an existing branch or PR to work on; empty makes a new branch
	desc    textinput.Model // what the task is about, told to the agent
	repoSel int             // the recent repo picked in the list, or -1
	err     string
	issue   string // the Jira issue the task works on (From)
	brief   string // what the task's description gets after what's typed (From)
}

// From fills the form in for a task made from a Jira issue: its name, the issue it's linked to,
// and the issue's text, which goes in the task's description after anything you type.
func (f *Form) From(name, issue, brief string) {
	f.name.SetValue(name)
	f.issue, f.brief = issue, brief
}

// NewForm opens the form, with the folder prefilled from the last attached repo.
func NewForm(tasks Store, repos repo.Store) (*Form, tea.Cmd) {
	f := &Form{tasks: tasks, repos: repos, repoSel: -1, name: textinput.New(), folder: textinput.New(), branch: textinput.New(), desc: textinput.New()}
	f.name.Placeholder, f.name.CharLimit, f.name.Width = "Sort orgs alphabetically", 100, 44
	f.folder.Placeholder, f.folder.ShowSuggestions, f.folder.Width = "~/code/my-app", true, 44
	f.branch.Placeholder, f.branch.Width = "empty: a new branch · or an existing one, or a PR: #1330", 54
	f.desc.Placeholder, f.desc.CharLimit = "optional: what to do, for the agent (you can also say it in your first message)", 2000
	if recent := f.recent(); len(recent) > 0 {
		f.repoSel = 0
		f.folder.SetValue(look.Tildify(recent[0]))
		f.folder.CursorEnd()
	}
	return f, f.focus(0)
}

func (f *Form) focus(i int) tea.Cmd {
	f.field = i
	f.name.Blur()
	f.folder.Blur()
	f.branch.Blur()
	f.desc.Blur()
	switch i {
	case 0:
		return f.name.Focus()
	case 1:
		f.suggestFolders()
		return f.folder.Focus()
	case 2:
		return nil // the agent row takes ←/→
	case 3:
		return f.branch.Focus()
	}
	return f.desc.Focus()
}

// recent are the repos you've used, the most recent first.
func (f *Form) recent() []string {
	repos := f.repos.List()
	slices.Reverse(repos)
	return repos
}

// shown are the recent repos matching what's typed in the folder field (all of them while it still
// holds a picked one).
func (f *Form) shown() []string {
	typed := strings.ToLower(strings.TrimSpace(f.folder.Value()))
	var out []string
	for i, r := range f.recent() {
		if i == f.repoSel || typed == "" || strings.Contains(strings.ToLower(look.Tildify(r)), typed) || f.repoSel >= 0 {
			out = append(out, r)
		}
	}
	return out[:min(len(out), 6)]
}

// Key handles a key. created is the task it made (⏎ on the description); closed is set when the
// form is done, made or cancelled.
func (f *Form) Key(msg tea.KeyMsg) (cmd tea.Cmd, created *Task, closed bool) {
	switch msg.String() {
	case "esc":
		return nil, nil, true
	case "up", "down":
		if shown := f.shown(); f.field == 1 && len(shown) > 0 { // pick one of your recent repos
			recent, i := f.recent(), -1 // typed: ↓ picks the first match
			if f.repoSel >= 0 {
				i = slices.Index(shown, recent[f.repoSel])
			}
			if msg.String() == "up" {
				i = max(i-1, 0)
			} else {
				i = min(i+1, len(shown)-1)
			}
			f.repoSel = slices.Index(recent, shown[i])
			f.folder.SetValue(look.Tildify(shown[i]))
			f.folder.CursorEnd()
			return nil, nil, false
		}
		if msg.String() == "up" {
			return f.focus(max(f.field-1, 0)), nil, false
		}
		return f.focus(min(f.field+1, 4)), nil, false
	case "left", "right":
		if f.field == 2 { // pick the agent
			d := map[string]int{"left": -1, "right": 1}[msg.String()]
			f.agent = (f.agent + d + len(agent.All)) % len(agent.All)
			return nil, nil, false
		}
	case "shift+tab":
		return f.focus(max(f.field-1, 0)), nil, false
	case "tab":
		if f.field != 1 { // in the folder, tab completes a folder name
			return f.focus(min(f.field+1, 4)), nil, false
		}
	case "enter":
		if f.field == 0 && strings.TrimSpace(f.name.Value()) == "" {
			f.err = "Give the task a name"
			return nil, nil, false
		}
		if f.field < 4 {
			f.err = ""
			return f.focus(f.field + 1), nil, false
		}
		if t := f.create(); t != nil {
			return nil, t, true
		}
		return nil, nil, false
	}
	switch f.field {
	case 0:
		f.name, cmd = f.name.Update(msg)
	case 1:
		f.folder, cmd = f.folder.Update(msg) // tab accepts the folder suggestion
		f.repoSel = -1                       // typing: the list filters to what matches
		f.suggestFolders()
	case 3:
		f.branch, cmd = f.branch.Update(msg)
	case 4:
		f.desc, cmd = f.desc.Update(msg)
	}
	return cmd, nil, false
}

// create saves the task, or says what's wrong and returns nil.
func (f *Form) create() *Task {
	name := strings.TrimSpace(f.name.Value())
	if name == "" {
		f.err = "Give the task a name"
		f.focus(0)
		return nil
	}
	rp, err := f.repos.Attach(strings.TrimSpace(f.folder.Value()))
	if err != nil {
		f.err = err.Error()
		return nil
	}
	t := Task{ID: f.tasks.NewID(name), Title: name, Repo: rp, Agent: agent.All[f.agent].Name, Branch: strings.TrimSpace(f.branch.Value()),
		Issue: f.issue, Status: Todo, Created: time.Now(), Body: strings.TrimSpace(f.desc.Value() + "\n\n" + f.brief)}
	if err := f.tasks.Save(t); err != nil {
		f.err = err.Error()
		return nil
	}
	return &t
}

// suggestFolders offers sub-folders matching what has been typed so far; tab accepts one.
func (f *Form) suggestFolders() {
	typed := f.folder.Value()
	// Split what was typed (not the ~-expanded path) so dir+prefix always rebuilds typed exactly.
	dir, prefix := filepath.Split(typed)
	readDir := expand(dir)
	if readDir == "" {
		readDir = "."
	}
	entries, _ := os.ReadDir(readDir)
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && strings.HasPrefix(n, prefix) && (!strings.HasPrefix(n, ".") || strings.HasPrefix(prefix, ".")) {
			out = append(out, dir+n+"/")
		}
	}
	f.folder.SetSuggestions(out)
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

// View draws the form.
func (f *Form) View() string {
	label := func(i int, s string) string {
		if f.field == i {
			return look.Sel.Render(s)
		}
		return look.Bold.Render(s)
	}
	errLine := ""
	if f.err != "" {
		errLine = "\n" + look.Warn.Render(f.err)
	}
	repos, recent := "", f.recent()
	for _, r := range f.shown() {
		mark, st := "  ", look.Dim
		if f.repoSel >= 0 && f.repoSel < len(recent) && r == recent[f.repoSel] {
			mark, st = look.Sel.Render("▶ "), lipgloss.NewStyle()
		}
		repos += "\n" + mark + st.Render(ansi.Truncate(look.Tildify(r), 54, "…"))
	}
	if repos != "" {
		repos = "\n" + look.Dim.Render("recent repos · ↑↓ to pick") + repos
	}
	return look.MenuBox.Width(64).Render(look.Bold.Render("New task") + "\n\n" +
		label(0, "Name") + "\n" + f.name.View() + "\n\n" +
		label(1, "Repo") + look.Dim.Render("  any git repo on this machine") + "\n" + f.folder.View() + repos + errLine + "\n\n" +
		label(2, "Agent") + look.Dim.Render("  ←/→ to pick") + "\n" + f.agentRow() + "\n\n" +
		label(3, "Branch") + look.Dim.Render("  optional") + "\n" + f.branch.View() + "\n\n" +
		label(4, "Description") + "\n" + f.desc.View() + "\n\n" +
		look.Dim.Render("⏎ next · tab completes a folder · ⏎ on the description creates · esc cancel"))
}

// agentRow lists the agents a task can use, the picked one marked.
func (f *Form) agentRow() string {
	var parts []string
	for i, a := range agent.All {
		if i == f.agent {
			parts = append(parts, look.Sel.Render("▶ "+a.Title))
		} else {
			parts = append(parts, look.Dim.Render("  "+a.Title))
		}
	}
	return strings.Join(parts, "   ")
}
