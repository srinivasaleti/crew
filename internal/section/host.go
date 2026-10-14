package section

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/chat"
	"crew/internal/widgets/look"
)

// Host is what crew gives a section.
type Host interface {
	Ctx() context.Context // cancelled on exit
	SectionOn(key string) bool
	Blurred() bool    // crew's window isn't in front
	WhereNow() string // what you're in: the task's title, or the section's label

	// The window: the main pane's viewport, the message box, the help line's notice.
	Viewport() *viewport.Model
	Input() *textarea.Model
	Notice(string)
	Note() string // the notice on the help line now ("" when there's none)
	Layout()      // sizes the window's parts again
	Refresh()     // redraws the main pane
	FocusMain()
	GoUp()             // esc: back to the list the selection came from
	Select(key string) // selects a sidebar row by its key and shows it in the main pane
	SelKey() string    // the selected row's key
	SelectedRow() Row  // the selected row
	Reselect()         // shows the selection again (after a section changed what it shows)
	// SectionModal makes the section's popup the open modal (crew draws it and hands it the keys
	// while the section's ModalOpen says it's open), or closes it.
	SectionModal(open bool)
	ListFocused() bool // the sidebar has the keys
	// ItemLines draws an item's two sidebar lines: mark and title, then what's going on with it.
	ItemLines(r Row, mark, title, second string, selected bool) string
	SpinFrame() int // the working spinner's frame
	Spin() tea.Cmd  // starts the working spinner

	// Chats: crew's chats, and the message box's keys for a section's chat.
	Chats() *chat.Chats
	ChatKey(msg tea.KeyMsg) tea.Cmd // a key for the message box, as in a task's chat
	ChatTyping() bool               // something is typed in the message box, or its suggestions are open
	ClearSuggestions()
	Suggesting() bool              // the message box's suggestions are open
	ChatBox() string               // the message box, with its suggestions above it
	ChatMeta(t *tasks.Task) string // what a section's chat cost, its context, whether it's working
	FindChat(match func(tasks.Task) bool) *tasks.Task
	AddChat(t tasks.Task) *tasks.Task // a section's chat, in memory until its first message saves it
	MarkRead(id string)               // the chat's reply has been seen
	Say(id, role, text string)        // adds a line to a chat (a command's own steps)

	// Tasks.
	Tasks() []tasks.Task
	Task(id string) *tasks.Task // a task or a section's chat, by id
	Current() *tasks.Task       // the task that's open, or nil
	// WorkOn opens the task working on an issue, or the new-task form filled in from it.
	WorkOn(title, issue, brief string) tea.Cmd

	// Setup: what's installed and signed in, and fixing it in a shell.
	Setup() setup.Status
	SignIn(t setup.Tool) tea.Cmd
	RunFix(command, hint string) tea.Cmd
}

// CountTitle is a section's own row as crew draws it by default: its label and count, in bold.
func CountTitle(r Row) string { return look.Bold.Render(fmt.Sprintf("%s (%d)", r.Label, r.Count)) }

// CrewExe is this crew binary, which a locked-down chat's Claude runs (crew req, crew remind).
func CrewExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "crew"
	}
	return exe
}
