package prs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"crew/internal/gh"
	"crew/internal/repo"
	"crew/internal/section"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
)

// A chat about a listed PR: the PR's Chat pane. It's a task underneath (a task file with pr: set,
// kept in crew's section chats, never in the task list), so it gets everything a task's chat has:
// its own Claude session, the chat log, /compact, /new, /model, CREW_AUTO_COMPACT, usage, stopping
// and steering mid-turn. Claude works in a checkout of the PR when its repo is attached here, else
// in an empty folder with gh. The file is written with your first message.

// chat is the chat about a PR, or nil before you open its Chat pane.
func (s *Section) chat(h section.Host, url string) *tasks.Task {
	return h.FindChat(func(t tasks.Task) bool { return t.PR == url })
}

// ensureChat makes the chat about a PR if there isn't one yet (in memory; its first message saves it).
func (s *Section) ensureChat(h section.Host, it gh.Item) *tasks.Task {
	if t := s.chat(h, it.URL); t != nil {
		return t
	}
	return h.AddChat(tasks.Task{
		ID:    tasks.Slugify(fmt.Sprintf("pr %s %d", it.Repo, it.Number)),
		Title: fmt.Sprintf("#%d %s", it.Number, it.Title),
		Repo:  s.repos.Find(it.Repo), // "" when the repo isn't on this machine
		PR:    it.URL, Status: tasks.InProgress, Created: time.Now(),
		Body: fmt.Sprintf("You're helping me with pull request %s (%s #%d, %q by @%s). Read it with gh "+
			"(gh pr view %[1]s --comments, gh pr diff %[1]s). Don't push, comment, review or change it on GitHub unless I ask.",
			it.URL, it.Repo, it.Number, it.Title, it.Author),
	})
}

// chatView is the Chat pane: the conversation, or what it's for before the first message.
func (s *Section) chatView(h section.Host, it gh.Item) string {
	t, w := s.chat(h, it.URL), max(h.Viewport().Width, 20)
	if t == nil || len(h.Chats().Conv(t.ID).Msgs) == 0 {
		where := "Claude reads it with gh (the repo isn't attached to crew, so there's no checkout)."
		if r := s.repos.Find(it.Repo); r != "" {
			where = "Claude works in a checkout of it, next to " + look.Tildify(r) + "."
		}
		return look.Dim.Render(lipgloss.NewStyle().Width(w).Render("Ask Claude anything about this PR: what it does, whether it's safe, " +
			"how to test it. " + where + " Nothing goes to GitHub unless you ask.\n\n/compact, /new and /model work as in a task."))
	}
	return h.Chats().Render(t.ID, w, h.SpinFrame())
}

// ChatOpen reports whether a PR's Chat pane is open.
func (s *Section) ChatOpen(h section.Host, r section.Row) bool {
	return r.Kind == rowPR && s.selected(h) != nil && s.pane == paneChat
}

// Chat is the PR's chat on its Chat pane.
func (s *Section) Chat(h section.Host, r section.Row) *tasks.Task {
	if it := s.selected(h); it != nil && r.Kind == rowPR && s.pane == paneChat {
		return s.chat(h, it.URL)
	}
	return nil
}

// EnsureChat makes the PR's chat when a reload dropped it.
func (s *Section) EnsureChat(h section.Host, r section.Row) *tasks.Task { return s.PRAgent(h, r, true) }

// Where makes the folder a PR's chat runs in, then its first turn: a checkout of the PR when its
// repo is attached, else <chatDir>/<id> (stable, so Claude can resume the session there).
func (s *Section) Where(_ section.Host, t tasks.Task, prompt string) (tea.Cmd, bool) {
	if t.PR == "" {
		return nil, false
	}
	dir := filepath.Join(s.chatDir, t.ID)
	return func() tea.Msg {
		if t.Repo == "" {
			return chat.StartedMsg{ID: t.ID, Worktree: dir, Prompt: prompt, Err: os.MkdirAll(dir, 0o755)}
		}
		n, _ := strconv.Atoi(t.PR[strings.LastIndex(t.PR, "/")+1:]) // …/pull/<n>
		wt, err := repo.PRWorktree(t.Repo, t.ID, n)
		return chat.StartedMsg{ID: t.ID, Repo: t.Repo, Worktree: wt, Prompt: prompt, Err: err}
	}, true
}
