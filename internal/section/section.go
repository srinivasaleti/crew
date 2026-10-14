package section

import (
	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/setup"
	"crew/internal/tasks"
	"crew/internal/widgets/prview"
	"crew/internal/widgets/table"
)

// Def describes a section on home: its key (in ~/.crew/sections and row keys), its label, the
// letter that turns it on or off, and what it's for.
type Def struct{ Key, Label, Letter, About string }

// Command is a slash command a section adds to the message box (/focus).
type Command struct {
	Name, Help string
	Run        func(h Host, t *tasks.Task, arg string) tea.Cmd
}

// Action is something a section adds to what a ⌘ shortcut can be bound to (~/.crew/keys).
type Action struct {
	Name, Help string
	Run        func(h Host) tea.Cmd
}

// Section is one of crew's sections. Every method gets the Host; those about the main pane get
// the selected row, which is always one of the section's own. Embed Base for the defaults.
type Section interface {
	Def() Def

	// Init starts what the section runs in the background when crew starts (reads, clocks).
	Init(h Host) tea.Cmd
	// On starts it again when it's turned on from home.
	On(h Host) tea.Cmd
	// Reload re-reads what the section keeps on disk, when crew re-reads its tasks.
	Reload(h Host)
	// Update takes the section's own messages; ok is false for the rest.
	Update(h Host, msg tea.Msg) (cmd tea.Cmd, ok bool)
	// AutoLoad starts what the screen waits for (an opened item's details); it runs after every
	// message, so an item loads however it got selected.
	AutoLoad(h Host) tea.Cmd
	// SetupChanged is a Setup check that finished; was is what crew knew before it.
	SetupChanged(h Host, was setup.Status) tea.Cmd
	// TurnEnded is a chat's turn that ended (the section's chat may have changed its things).
	TurnEnded(h Host, id string)
	// Spin is the working spinner's tick: the section redraws what spins and reports whether it
	// still needs the spinner.
	Spin(h Host) bool
	// Status is the start of the help line, for what must stay in sight wherever you are.
	Status(h Host) string
	// Commands are slash commands the section adds to every chat's message box.
	Commands() []Command
	// Actions are what the section adds to the ⌘ shortcuts' actions.
	Actions() []Action

	// Rows are the section's rows in the sidebar: its own row, then what's open under it.
	Rows(h Host) []Row
	// Title is the section's own row: its title and a badge after it (a count, a timer).
	Title(h Host, r Row) (title, badge string)
	// Lines draws one of its items' two lines in the sidebar.
	Lines(h Host, r Row, selected bool) string
	// Selected is one of its rows becoming the selection (from is the key of what was selected).
	Selected(h Host, r Row, from string)
	// ListKey is a key while the sidebar has the keys on one of its rows; ok is false to let crew
	// handle it (↑↓, ⏎ into the main pane, esc).
	ListKey(h Host, r Row, msg tea.KeyMsg) (cmd tea.Cmd, ok bool)

	// ShowsTable reports whether a row of the section shows a table (crew draws it, with its filters
	// and search) rather than a page the section draws itself.
	ShowsTable(h Host, r Row) bool
	// Table is the table a row shows, when ShowsTable says it does.
	Table(h Host, r Row) table.Table
	// Open does what the table asks on a row (its id; "" for a Global key) with an action.
	Open(h Host, r Row, id, action string) tea.Cmd

	// Frame is the main pane's frame title.
	Frame(h Host, r Row) string
	// Header is the main pane's first line, w wide.
	Header(h Host, r Row, w int) string
	// Tabs is the main pane's second line: the tab bar, or "" for a plain rule.
	Tabs(h Host, r Row, w int) string
	// TabsHere is the open tab and how many there are (n is 0 for no tabs).
	TabsHere(h Host, r Row) (cur, n int)
	// GotoTab opens tab i.
	GotoTab(h Host, r Row, i int) tea.Cmd
	// Render fills the main pane's viewport.
	Render(h Host, r Row)
	// Hint is the bottom of the main pane: a hint line, or the message box when a chat is open.
	Hint(h Host, r Row) string
	// EditBox reports whether Hint is the message box used to edit something, under a line that
	// says what (the box's height plus that line).
	EditBox(h Host, r Row) bool
	// Key is a key while the main pane has the keys; ok is false to let crew handle it.
	Key(h Host, r Row, msg tea.KeyMsg) (cmd tea.Cmd, ok bool)
	// Click is a left click in the main pane at (x, y), x inside its frame, y on the screen.
	Click(h Host, r Row, x, y int) (cmd tea.Cmd, ok bool)
	// Wheel is the mouse wheel over the main pane; ok is false to let the viewport scroll.
	Wheel(h Host, r Row, down bool) (ok bool)
	// Focus names who has the keys in the main pane, for the badge: "overview", "chat" or "pr".
	Focus(h Host, r Row) string
	// ChatOpen reports whether the page is a chat (it gets the message box).
	ChatOpen(h Host, r Row) bool
	// Chat is the chat on screen, when the page is one and it's been made, or nil.
	Chat(h Host, r Row) *tasks.Task
	// EnsureChat makes the page's chat when it isn't there yet (a reload dropped it).
	EnsureChat(h Host, r Row) *tasks.Task

	// Where sets up where one of the section's chats runs (its kind), when it's one of them.
	Where(h Host, t tasks.Task, prompt string) (cmd tea.Cmd, ok bool)
	// Args is what its chats' agent gets on every run, when the section decides.
	Args(h Host, t tasks.Task) (args []string, ok bool)

	// Modal is the section's popup, when one is open (Host.SectionModal): its view, keys and clicks.
	ModalOpen(h Host) bool
	ModalView(h Host) string
	ModalKey(h Host, msg tea.KeyMsg) tea.Cmd
	ModalClick(h Host, x, y int) tea.Cmd
	ModalWheel(h Host, down bool)
}

// PRPage is a section whose page shows a PR in crew's PR view (PULL REQUESTS): crew asks it which
// PR, and which chat works on it, for the view and its popup.
type PRPage interface {
	// PR is the PR on the row's page, and whether its conversation (the PR view) is showing.
	PR(h Host, r Row) (ref prview.Ref, onView bool)
	// PRAgent is the chat that works on it, made with create (else nil until there is one).
	PRAgent(h Host, r Row, create bool) *tasks.Task
	// ShowPRChat shows that chat.
	ShowPRChat(h Host, r Row)
}

// Base is a section's defaults: nothing in the background, its own row with a count, a table it
// doesn't have, and no chats or popups. A section embeds it and overrides what it uses.
type Base struct{}

func (Base) Init(Host) tea.Cmd                              { return nil }
func (Base) On(Host) tea.Cmd                                { return nil }
func (Base) Reload(Host)                                    {}
func (Base) Update(Host, tea.Msg) (tea.Cmd, bool)           { return nil, false }
func (Base) AutoLoad(Host) tea.Cmd                          { return nil }
func (Base) SetupChanged(Host, setup.Status) tea.Cmd        { return nil }
func (Base) TurnEnded(Host, string)                         {}
func (Base) Spin(Host) bool                                 { return false }
func (Base) Status(Host) string                             { return "" }
func (Base) Commands() []Command                            { return nil }
func (Base) Actions() []Action                              { return nil }
func (Base) Rows(Host) []Row                                { return nil }
func (Base) Title(_ Host, r Row) (string, string)           { return CountTitle(r), "" }
func (Base) Lines(Host, Row, bool) string                   { return "" }
func (Base) Selected(Host, Row, string)                     {}
func (Base) ListKey(Host, Row, tea.KeyMsg) (tea.Cmd, bool)  { return nil, false }
func (Base) ShowsTable(Host, Row) bool                      { return false }
func (Base) Table(Host, Row) table.Table                    { return table.Table{} }
func (Base) Open(Host, Row, string, string) tea.Cmd         { return nil }
func (Base) Frame(_ Host, r Row) string                     { return "" }
func (Base) Header(Host, Row, int) string                   { return "" }
func (Base) Tabs(Host, Row, int) string                     { return "" }
func (Base) TabsHere(Host, Row) (int, int)                  { return 0, 0 }
func (Base) GotoTab(Host, Row, int) tea.Cmd                 { return nil }
func (Base) Render(Host, Row)                               {}
func (Base) Hint(Host, Row) string                          { return "" }
func (Base) EditBox(Host, Row) bool                         { return false }
func (Base) Key(Host, Row, tea.KeyMsg) (tea.Cmd, bool)      { return nil, false }
func (Base) Click(Host, Row, int, int) (tea.Cmd, bool)      { return nil, false }
func (Base) Wheel(Host, Row, bool) bool                     { return false }
func (Base) Focus(Host, Row) string                         { return "overview" }
func (Base) ChatOpen(Host, Row) bool                        { return false }
func (Base) Chat(Host, Row) *tasks.Task                     { return nil }
func (Base) EnsureChat(Host, Row) *tasks.Task               { return nil }
func (Base) Where(Host, tasks.Task, string) (tea.Cmd, bool) { return nil, false }
func (Base) Args(Host, tasks.Task) ([]string, bool)         { return nil, false }
func (Base) ModalOpen(Host) bool                            { return false }
func (Base) ModalView(Host) string                          { return "" }
func (Base) ModalKey(Host, tea.KeyMsg) tea.Cmd              { return nil }
func (Base) ModalClick(Host, int, int) tea.Cmd              { return nil }
func (Base) ModalWheel(Host, bool)                          {}
