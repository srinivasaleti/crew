// Package task stores tasks as markdown files with a small front matter header.
package task

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status values you set on a task. Agent activity (running, reply ready) is shown separately.
const (
	Todo       = "todo"
	InProgress = "in-progress"
	InPR       = "in-pr"
	Blocked    = "blocked"
	Done       = "done"
)

// Statuses lists them in menu order.
var Statuses = []string{Todo, InProgress, InPR, Blocked, Done}

// legacy maps statuses written by earlier crew versions.
var legacy = map[string]string{"working": InProgress, "waiting": InProgress, "review": InPR}

// Task is one unit of work. Stored as <dir>/<ID>.md:
//
//	---
//	title: Fix login redirect loop
//	repo: accounts
//	agent: claude
//	status: todo
//	session: <agent session id, set on start>
//	worktree: <path, set on start>
//	created: 2026-10-05T10:00:00+10:00
//	---
//
//	Free-form description, handed to the agent as its prompt.
type Task struct {
	ID       string // filename without .md; also the branch and worktree name
	Title    string
	Repo     string
	Agent    string
	Model    string // model alias or id passed to the agent; empty uses the agent's default
	Status   string
	Session  string
	Worktree string
	Base     string // commit the task branch started from, for the changes view
	Archived bool   // hidden from the task list until shown with ctrl+o
	Created  time.Time
	Updated  time.Time // set on every save; used to archive old done tasks
	Body     string
}

// ponytail: hand-rolled key: value front matter, swap for a YAML lib if tasks grow nested fields.
func Parse(id, s string) (Task, error) {
	t := Task{ID: id, Status: Todo}
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return t, fmt.Errorf("%s.md: missing front matter", id)
	}
	head, body, ok := strings.Cut(rest+"\n", "\n---\n")
	if !ok {
		return t, fmt.Errorf("%s.md: unterminated front matter", id)
	}
	for _, line := range strings.Split(head, "\n") {
		k, v, _ := strings.Cut(line, ":")
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "title":
			t.Title = v
		case "repo":
			t.Repo = v
		case "agent":
			t.Agent = v
		case "model":
			t.Model = v
		case "status":
			t.Status = v
			if s, ok := legacy[v]; ok {
				t.Status = s
			}
		case "archived":
			t.Archived = v == "true"
		case "session":
			t.Session = v
		case "worktree":
			t.Worktree = v
		case "base":
			t.Base = v
		case "created":
			t.Created, _ = time.Parse(time.RFC3339, v)
		case "updated":
			t.Updated, _ = time.Parse(time.RFC3339, v)
		}
	}
	t.Body = strings.TrimSpace(body)
	return t, nil
}

func (t Task) String() string {
	return fmt.Sprintf("---\ntitle: %s\nrepo: %s\nagent: %s\nmodel: %s\nstatus: %s\nsession: %s\nworktree: %s\nbase: %s\narchived: %t\ncreated: %s\nupdated: %s\n---\n\n%s\n",
		t.Title, t.Repo, t.Agent, t.Model, t.Status, t.Session, t.Worktree, t.Base, t.Archived, t.Created.Format(time.RFC3339), t.Updated.Format(time.RFC3339), t.Body)
}

// Slugify makes a git-branch-safe id: lowercase ASCII letters, digits and dashes.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := b.String()
	if len(out) > 50 {
		out = out[:50]
	}
	out = strings.TrimSuffix(out, "-")
	if out == "" {
		out = "task"
	}
	return out
}

// Store reads and writes task files in one directory.
type Store struct{ Dir string }

func (s Store) Path(id string) string { return filepath.Join(s.Dir, id+".md") }

func (s Store) Save(t Task) error {
	t.Updated = time.Now()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path(t.ID), []byte(t.String()), 0o644)
}

func (s Store) Delete(id string) error { return os.Remove(s.Path(id)) }

// Load returns all tasks, newest first. Broken files are skipped and reported.
func (s Store) Load() ([]Task, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var tasks []Task
	var errs []error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t, err := Parse(strings.TrimSuffix(filepath.Base(p), ".md"), string(data))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tasks = append(tasks, t)
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Created.After(tasks[j].Created) })
	return tasks, errors.Join(errs...)
}

// NewID returns an unused id derived from title.
func (s Store) NewID(title string) string {
	base := Slugify(title)
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(s.Path(id)); errors.Is(err, os.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}
