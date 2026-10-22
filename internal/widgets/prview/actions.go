package prview

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/gh"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/markdown"
)

// askAbout sends your question with the row's feedback to the agent; the answer shows in the popup.
func (v *View) askAbout(h Host, it prItem, question string) tea.Cmd {
	t, st := h.Agent(true), v.Of(h.Ref().Key)
	if t == nil {
		return nil
	}
	if h.Chats().Conv(t.ID).Busy {
		h.Notice("Claude stops its current step and takes your question")
	}
	text := fmt.Sprintf("About %s on PR #%d:\n\n%s\n\n%s", it.about, st.pr.Number, it.context, question)
	cmd := h.Send(*t, chat.Outgoing{Text: text, TalkKey: it.key, Asked: question})
	v.Render(h)
	return cmd
}

// askAll sends every failing check, open line comment and change request to the agent at once,
// and shows the chat.
func (v *View) askAll(h Host) tea.Cmd {
	st := v.Of(h.Ref().Key)
	if st.pr == nil {
		return nil
	}
	asks := v.asks(h, st.pr)
	if len(asks) == 0 {
		h.Notice("Nothing waiting on you: no failing checks, open comments or change requests")
		return nil
	}
	t := h.Agent(true)
	if t == nil {
		return nil
	}
	text := fmt.Sprintf("Please work through this feedback on PR #%d, then summarise what you changed:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
	if t.PR != "" {
		text = fmt.Sprintf("Please go through this feedback on PR #%d: what each asks for and how you'd address it:\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
		cmd := h.Send(*t, chat.Outgoing{Text: text})
		h.ShowChat(*t)
		return cmd
	}
	h.ShowChat(*t)
	return h.Send(*t, chat.Outgoing{Text: text})
}

// asks are a PR's failing checks, open line comments and change requests, numbered, each with
// its feedback quoted.
func (v *View) asks(h Host, pr *gh.PR) []string {
	asks, _ := v.asksKeyed(h, pr)
	return asks
}

// asksKeyed are asks, and the key of each one's row.
func (v *View) asksKeyed(h Host, pr *gh.PR) (asks, keys []string) {
	for _, it := range v.items(h, pr) {
		if !it.head && needsYou(it) && it.context != "" && (it.thread == nil || !it.thread.Outdated) {
			asks = append(asks, fmt.Sprintf("%d. %s", len(asks)+1, it.context))
			keys = append(keys, it.key)
		}
	}
	return asks, keys
}

// FixReview is /fix-review in a task's chat: the feedback on the task's PR (failing checks, open
// line comments, change requests) goes to its agent to fix, as a on the PR tab sends it, with your
// note after it. It reads the PR again first, line comments included (the sidebar's poll doesn't).
func (v *View) FixReview(h Host, t tasks.Task, note string) tea.Cmd {
	switch {
	case !gh.Installed():
		h.Notice("/fix-review reads the PR with the GitHub CLI (gh), which isn't installed · /doctor")
		return nil
	case t.Worktree == "":
		h.Notice("This task has no PR yet · its worktree is made with your first message")
		return nil
	}
	st := v.Of(t.ID)
	st.fix, st.fixNote = true, note
	return v.Load(TaskRef(&t), true) // nil while a read is under way; fixing waits for it (Update)
}

// fixReview sends the feedback /fix-review waited for, once its PR has been read with its threads.
func (v *View) fixReview(h Host, id string) tea.Cmd {
	st := v.Of(id)
	t := h.Task(id)
	st.fix = false
	switch {
	case t == nil:
		return nil
	case st.pr == nil:
		h.Notice(t.Title + " has no PR yet · P on its PR tab opens one")
		return nil
	}
	asks, keys := v.asksKeyed(h, st.pr)
	if len(asks) == 0 {
		h.Notice("Nothing to fix on PR #" + fmt.Sprint(st.pr.Number) + ": no failing checks, open comments or change requests")
		return nil
	}
	st.fixKeys = keys
	text := fmt.Sprintf("Please go through this feedback on PR #%d. Fix what needs fixing, the simplest way that's correct, "+
		"then summarise what you changed. Where a comment needs no code change (you disagree, it's already handled, "+
		"or it's a question), don't change code: draft a short, polite reply to the reviewer that says why. "+
		"Commit your fixes, but don't push or post anything on GitHub yourself: I'll push your commits and post your drafts. End your reply with a ## Replies section, "+
		"one line per draft, numbered as the feedback below: \"3. <reply>\". "+
		"If a fix needs more than a small, local change, don't make it: end your reply with a ## Questions section "+
		"(numbered, options as indented bullets, one marked (recommended: …)) asking me.\n\n%s", st.pr.Number, strings.Join(asks, "\n\n"))
	if st.fixNote != "" {
		text += "\n\n" + st.fixNote
	}
	return h.Send(*t, chat.Outgoing{Text: text})
}

// replyNum is a numbered line of a ## Replies section: "3. <reply>".
var replyNum = regexp.MustCompile(`^\s*(\d+)[.)]\s+(.*)$`)

// TakeReplies reads the replies an agent drafted in its answer to /fix-review (its ## Replies
// section) and keeps them on their rows, to post on your key; n is how many. It waits for the
// turn that has them.
func (v *View) TakeReplies(id, answer string) (n int) {
	st := v.states[id]
	if st == nil || st.fixKeys == nil {
		return 0
	}
	_, section, ok := strings.Cut(answer, "## Replies")
	if !ok {
		return 0
	}
	if st.drafts == nil {
		st.drafts = map[string]string{}
	}
	cur := ""
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			break // the next section
		}
		if m := replyNum.FindStringSubmatch(line); m != nil {
			cur = ""
			if i, _ := strconv.Atoi(m[1]); i >= 1 && i <= len(st.fixKeys) && !strings.HasPrefix(st.fixKeys[i-1], "check") {
				cur = st.fixKeys[i-1]
				st.drafts[cur] = strings.TrimSpace(m[2])
			}
		} else if cur != "" && strings.TrimSpace(line) != "" {
			st.drafts[cur] += "\n" + strings.TrimSpace(line)
		}
	}
	st.fixKeys = nil
	return len(st.drafts)
}

// postDrafts posts every drafted reply (D twice on the PR tab), each where r would.
func (v *View) postDrafts(h Host, ref Ref) tea.Cmd {
	st := v.Of(ref.Key)
	var cmds []tea.Cmd
	for key, text := range st.drafts {
		if it := v.itemByKey(h, key); it != nil {
			cmds = append(cmds, v.reply(h, ref, *it, text))
		}
	}
	st.drafts = nil
	h.Notice(fmt.Sprintf("Posting %d replies…", len(cmds)))
	return tea.Batch(cmds...)
}

// reply posts your reply on GitHub. A line comment gets it in its thread. GitHub has no replies
// for reviews and conversation comments, so those get a new PR comment that quotes them.
func (v *View) reply(h Host, ref Ref, it prItem, text string) tea.Cmd {
	st := v.Of(ref.Key)
	dir, prURL, id := ref.Dir, st.pr.URL, ref.Key
	h.Notice("Posting your reply…")
	var post func() error
	switch {
	case it.thread != nil:
		threadID := it.thread.ID
		post = func() error { return gh.ReplyThread(dir, prURL, threadID, text) }
	case it.review != nil:
		body := quote(it.review.Comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	default:
		body := quote(*it.comment) + "\n\n" + text
		post = func() error { return gh.AddComment(dir, prURL, body) }
	}
	return func() tea.Msg { return PostedMsg{id, "Reply posted", post()} }
}

// toggleResolved resolves or reopens a line-comment thread, showing the change right away.
func (v *View) toggleResolved(h Host, ref Ref, th *gh.Thread) tea.Cmd {
	st := v.Of(ref.Key)
	resolve := !th.Resolved
	for i := range st.pr.Threads {
		if st.pr.Threads[i].ID == th.ID {
			st.pr.Threads[i].Resolved = resolve
		}
	}
	v.Render(h)
	done := "Resolved"
	if !resolve {
		done = "Reopened"
	}
	dir, prURL, id, threadID := ref.Dir, st.pr.URL, ref.Key, th.ID
	return func() tea.Msg { return PostedMsg{id, done, gh.SetResolved(dir, prURL, threadID, resolve)} }
}

func (v *View) readLog(h Host, ref Ref, c *gh.Check) tea.Cmd {
	h.Notice("Reading the failed log…")
	id, dir, name, u := ref.Key, ref.Dir, c.Name, c.URL
	return func() tea.Msg {
		log, err := gh.FailedLog(dir, u)
		return LogMsg{id, name, log, err}
	}
}

// create pushes the branch and opens a PR, on the second P; with the PR open, the second P pushes
// the branch's new commits to it. Agents can't push (crew's guard hook), so this is how they go.
func (v *View) create(h Host, t tasks.Task) tea.Cmd {
	st := v.Of(t.ID)
	branch := taskBranch(t.Worktree, t.ID)
	switch {
	case t.Worktree == "" || !gh.Installed():
		h.Notice("Needs the task's branch and the GitHub CLI (make setup)")
	case st.pr != nil && st.pr.State == "OPEN" && !st.armed:
		st.armed = true
		h.Notice("Press P again to push " + branch + "'s new commits to its PR")
	case st.pr != nil && st.pr.State == "OPEN":
		st.armed, st.loading = false, true
		h.Notice("Pushing " + branch + "…")
		id, dir, url := t.ID, t.Worktree, st.pr.URL
		return func() tea.Msg { return CreatedMsg{id, url, true, gh.Push(dir, branch)} }
	case !st.armed:
		st.armed = true
		h.Notice("Press P again to push " + taskBranch(t.Worktree, t.ID) + " to origin and open a PR")
	default:
		st.armed, st.loading = false, true
		h.Notice("Pushing and opening a PR…")
		id, dir := t.ID, t.Worktree
		return func() tea.Msg {
			u, err := gh.Create(dir, taskBranch(dir, id))
			return CreatedMsg{id, u, false, err}
		}
	}
	return nil
}

// quote is a comment as a Markdown quote, for Claude and for replies that quote it.
func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(markdown.Slim(c.Body), "\n", "\n> ")
}
