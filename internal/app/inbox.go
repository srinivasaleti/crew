package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/section"
	taskssec "crew/internal/sections/tasks"
	"crew/internal/tasks"
)

// The INBOX's items from crew's own tasks (the sections add theirs, section.Waits): a reply you
// haven't read, a question in a chat or a plan, a split to apply or discard, a sign-in that failed,
// and feedback on a task's PR. A task whose agent is working waits on nobody, except its PR.

// waiting is everything waiting on you, newest first: the tasks', then each section's.
// ponytail: built on every call (each draw); cache it per message if a task list ever makes it slow.
func (m *Model) waiting() []section.Wait {
	var ws []section.Wait
	v, _ := m.PR()
	for _, t := range m.tasks {
		if t.Archived || t.Status == tasks.Done {
			continue
		}
		id, title := t.ID, t.Title
		open := func(tab string) func(section.Host) tea.Cmd {
			return func(h section.Host) tea.Cmd { return h.OpenTask(id, tab) }
		}
		where := m.crumb(t) + filepath.Base(t.Repo)
		at := m.lastAt(id, t.Updated)
		conv := m.chat.Conv(id)
		switch qs := m.chat.Questions(id); {
		case conv.Busy:
		case conv.Retry() != nil:
			ws = append(ws, section.Wait{Key: "sign-in:" + id, Kind: section.WaitSignIn, Title: title, Where: where, At: at,
				Note: "its agent is signed out · ⌘L signs in, then it sends again", Open: open("")})
		case len(qs) > 0:
			ws = append(ws, section.Wait{Key: "ask:" + id, Kind: section.WaitQuestion, Title: title, Where: where, At: at,
				Note: fmt.Sprintf("asks you %d question%s", len(qs), plural(len(qs))), Open: open("")})
		case m.unread[id]:
			ws = append(ws, section.Wait{Key: "reply:" + id, Kind: section.WaitReply, Title: title, Where: where, At: at,
				Note: "replied", Open: open("")})
		}
		if pv := m.plans[id]; pv != nil && !conv.Busy {
			switch qs, _ := pv.questions(); {
			case pv.splitOK:
				ws = append(ws, section.Wait{Key: "split:" + id, Kind: section.WaitQuestion, Title: title, Where: where, At: at,
					Note: "a split into subtasks to apply or discard", Open: open("plan")})
			case pv.ok && len(qs) > 0:
				ws = append(ws, section.Wait{Key: "plan:" + id, Kind: section.WaitQuestion, Title: title, Where: where, At: at,
					Note: fmt.Sprintf("its plan asks you %d question%s", len(qs), plural(len(qs))), Open: open("plan")})
			}
		}
		if _, pr := v.Branch(id); pr != nil {
			if n := taskssec.PRTodo(pr); n > 0 {
				ws = append(ws, section.Wait{Key: "pr:" + id, Kind: section.WaitPR, Title: title, Where: where, At: at,
					Note: fmt.Sprintf("PR #%d: %d to do (checks, comments, changes asked)", pr.Number, n), Open: open("pr")})
			}
		}
	}
	for _, s := range m.secs {
		if w, ok := s.(section.Waits); ok && m.sectionOn(s.Def().Key) {
			ws = append(ws, w.Waiting(m)...)
		}
	}
	slices.SortStableFunc(ws, func(a, b section.Wait) int { return b.At.Compare(a.At) })
	return ws
}

// lastAt is when a chat last had a message, else fallback.
func (m Model) lastAt(id string, fallback time.Time) time.Time {
	if msgs := m.chat.Conv(id).Msgs; len(msgs) > 0 {
		if at, err := time.Parse(time.RFC3339, msgs[len(msgs)-1].At); err == nil {
			return at
		}
	}
	return fallback
}

// Waiting is everything waiting on you (section.Host).
func (m *Model) Waiting() []section.Wait { return m.waiting() }
