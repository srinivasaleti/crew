// Package inbox is the INBOX section: everything waiting on you in one list, newest first. A reply
// you haven't read, a question an agent asks (in a chat, a plan or a proposed split), a sign-in that
// failed, feedback on a task's PR, a review someone asked of you. It keeps nothing: crew and the
// sections say what's waiting (section.Host.Waiting), and ⏎ goes to it.
package inbox

import (
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/section"
	"crew/internal/widgets/look"
	"crew/internal/widgets/table"
)

// Section is the INBOX.
type Section struct{ section.Base }

// NewSection makes the INBOX.
func NewSection() *Section { return &Section{} }

// Def is the INBOX on home.
func (s *Section) Def() section.Def {
	return section.Def{Key: "inbox", Label: "INBOX", Letter: "i", About: "everything waiting on you: replies, questions, PR feedback, reviews"}
}

// Rows is the INBOX's row, with how many things wait on you.
func (s *Section) Rows(h section.Host) []section.Row {
	return []section.Row{{Kind: section.Group, Section: "inbox", Label: "INBOX", Count: len(h.Waiting())}}
}

// Title is the row, in orange while something waits.
func (s *Section) Title(_ section.Host, r section.Row) (string, string) {
	if r.Count > 0 {
		return look.Orange.Bold(true).Render(r.Label) + look.Bold.Render(" ("+strconv.Itoa(r.Count)+")"), ""
	}
	return section.CountTitle(r), ""
}

// kinds are how each kind reads, its mark, and the chip it's under.
var kinds = map[string]struct{ label, mark, chip string }{
	section.WaitReply:    {"reply", look.Ready.Render("◐"), "replies"},
	section.WaitQuestion: {"asks you", look.Sel.Render("?"), "asks"},
	section.WaitSignIn:   {"sign in", look.Warn.Render("!"), "asks"},
	section.WaitPR:       {"your PR", look.Orange.Render("●"), "prs"},
	section.WaitReview:   {"review", look.Sel.Render("●"), "prs"},
}

// ShowsTable is the INBOX's row.
func (s *Section) ShowsTable(_ section.Host, r section.Row) bool { return r.Kind == section.Group }

// Table is what's waiting, newest first.
func (s *Section) Table(h section.Host, _ section.Row) table.Table {
	t := table.Table{
		Empty: "Nothing waiting on you.",
		Cols:  []table.Col{{Name: "what", Width: 11}, {Name: "about"}, {Name: "where", Width: 22}, {Name: "", Width: 44}, {Name: "since", Width: 9}},
		Chips: []table.Chip{{ID: "all", Label: "All"}, {ID: "replies", Label: "Replies"}, {ID: "asks", Label: "Asks you"}, {ID: "prs", Label: "PRs"}},
		Acts:  []table.Act{{Label: "Open", Key: "enter"}},
		Hint:  "⏎ goes to it · tab filter · / search",
	}
	ws := h.Waiting()
	for _, w := range ws {
		k := kinds[w.Kind]
		t.Rows = append(t.Rows, table.Row{ID: w.Key, Tags: []string{"all", k.chip},
			Cells:  []string{k.mark + " " + k.label, w.Title, look.Dim.Render(w.Where), w.Note, look.Dim.Render(strings.TrimSuffix(look.Ago(w.At), " ago"))},
			Search: strings.Join([]string{w.Title, w.Where, w.Note, k.label}, " ")})
	}
	t.Summary = strconv.Itoa(len(ws)) + " waiting on you"
	return t
}

// Open goes to what's waiting.
func (s *Section) Open(h section.Host, _ section.Row, key, _ string) tea.Cmd {
	ws := h.Waiting()
	if i := slices.IndexFunc(ws, func(w section.Wait) bool { return w.Key == key }); i >= 0 && ws[i].Open != nil {
		return ws[i].Open(h)
	}
	return nil
}

// Actions add opening the INBOX to what a ⌘ shortcut can do (/keys).
func (s *Section) Actions() []section.Action {
	return []section.Action{{Name: "inbox", Help: "open the INBOX: everything waiting on you", Run: func(h section.Host) tea.Cmd {
		h.Select("g:inbox")
		h.FocusMain()
		return nil
	}}}
}
