package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/agent"
	"crew/internal/chat"
	"crew/internal/home"
	"crew/internal/task"
)

// apply carries out the home assistant's actions, reporting each outcome in the home chat.
func (m *Model) apply(actions []home.Action) []tea.Cmd {
	var cmds []tea.Cmd
	var created string // id of a task created earlier in this reply, for "create then start"
	ok := func(s string) { m.add(homeID, chat.Tool, "✓ "+s) }
	fail := func(s string) { m.add(homeID, chat.Error, s) }

	for _, a := range actions {
		if a.ID == "" && (a.Op == "start" || a.Op == "open") {
			a.ID = created
		}
		t := m.find(a.ID)
		if t == nil && a.Op != "attach" && a.Op != "create" {
			fail("no task with id " + a.ID)
			continue
		}
		switch a.Op {
		case "attach":
			p, err := m.cfg.Repos.Attach(a.Repo)
			if err != nil {
				fail(err.Error())
				continue
			}
			ok("attached " + p)

		case "create":
			rp, err := m.cfg.Repos.Resolve(a.Repo)
			if err != nil {
				fail(err.Error())
				continue
			}
			if a.Agent == "" {
				a.Agent = agent.Claude.Name
			}
			if _, found := agent.Get(a.Agent); !found {
				fail("unknown agent " + a.Agent)
				continue
			}
			if a.Title == "" {
				fail("a task needs a title")
				continue
			}
			nt := task.Task{ID: m.cfg.Tasks.NewID(a.Title), Title: a.Title, Repo: rp, Agent: a.Agent,
				Status: task.Todo, Created: time.Now(), Body: a.Description}
			if err := m.cfg.Tasks.Save(nt); err != nil {
				fail(err.Error())
				continue
			}
			m.reload()
			created = nt.ID
			ok("created " + nt.ID)

		case "update":
			if a.Title != "" {
				t.Title = a.Title
			}
			if a.Description != "" {
				t.Body = a.Description
			}
			if a.Repo != "" || a.Agent != "" {
				if t.Session != "" {
					fail(t.ID + " already started, its repo and agent can't change")
					continue
				}
				if a.Repo != "" {
					rp, err := m.cfg.Repos.Resolve(a.Repo)
					if err != nil {
						fail(err.Error())
						continue
					}
					t.Repo = rp
				}
				if a.Agent != "" {
					if _, found := agent.Get(a.Agent); !found {
						fail("unknown agent " + a.Agent)
						continue
					}
					t.Agent = a.Agent
				}
			}
			if err := m.cfg.Tasks.Save(*t); err != nil {
				fail(err.Error())
				continue
			}
			ok("updated " + t.ID)

		case "start":
			if t.Session != "" {
				ok(t.ID + " is already running, open its chat to talk to it")
				continue
			}
			if m.conv(t.ID).busy {
				continue
			}
			ok("started " + t.ID)
			m.add(t.ID, chat.You, brief(*t))
			cmds = append(cmds, m.start(*t, brief(*t)))
			m.selectID(t.ID)

		case "open":
			m.selectID(t.ID)

		case "delete":
			if m.conv(t.ID).busy {
				fail(t.ID + " is busy, wait for its agent to finish")
				continue
			}
			id, wt := t.ID, t.Worktree
			if err := m.cfg.Tasks.Delete(id); err != nil {
				fail(err.Error())
				continue
			}
			m.cfg.Chats.Delete(id)
			delete(m.convs, id)
			m.reload()
			if wt != "" {
				ok("deleted " + id + " (worktree kept at " + wt + ")")
			} else {
				ok("deleted " + id)
			}

		default:
			fail("unknown action " + a.Op)
		}
	}
	return cmds
}
