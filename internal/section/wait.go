package section

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Wait is something waiting on you, for the INBOX: a reply to read, a question to answer, feedback
// on a PR, a review someone asked you for. crew adds the tasks' own; a section adds its with Waits.
type Wait struct {
	Key   string // unique and the same each time: "reply:fix-login"
	Kind  string // one of the Wait… kinds, which the INBOX filters by
	Title string // what it's about: the task, the PR
	Where string // where it is: a repo, a task's parent
	Note  string // what to do, in a few words: "Claude asks 2 questions"
	At    time.Time
	// Open goes to it: the task's tab, the PR.
	Open func(h Host) tea.Cmd
}

// The kinds of Wait.
const (
	WaitReply    = "reply"    // an agent replied and you haven't read it
	WaitQuestion = "question" // an agent asks you something: in a chat, a plan, a proposed split
	WaitPR       = "pr"       // your PR has feedback: failing checks, open comments, changes requested
	WaitReview   = "review"   // someone asked you to review their PR
	WaitSignIn   = "sign-in"  // an agent's turn failed because it's signed out
)

// Waits is a section that adds to the INBOX (PULL REQUESTS: the reviews asked of you).
type Waits interface {
	Waiting(h Host) []Wait
}
