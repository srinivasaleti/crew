package app

import (
	"crew/internal/plan"
	"crew/internal/widgets/chat"
)

// Claude's questions as buttons above the message box (the chat package has them); this finds the
// chat on screen's and answers one on a click.

// chatQuestions are the questions at the end of Claude's last reply in the chat on screen, or nil.
func (m Model) chatQuestions() []plan.Question {
	t := m.chatTask()
	if t == nil {
		return nil
	}
	return m.chat.Questions(t.ID)
}

// askView draws the chat on screen's questions as buttons, w wide, and where each one is.
func (m Model) askView(w int) (string, [][5]int) { return chat.AskView(m.chatQuestions(), w) }

// clickAsk answers a question when one of its buttons is clicked (x, y on the screen), reporting
// whether the click was on one.
func (m *Model) clickAsk(x, y int) bool {
	t := m.chatTask()
	if t == nil || !m.chatOpen() {
		return false
	}
	_, at := m.askView(m.chatWidth() - 4)
	top, left := m.contentY()+m.vp.Height+1, m.sideOuter()+3 // inside the message box's border and padding
	for _, b := range at {
		if y == top+b[0] && x >= left+b[1] && x < left+b[2] {
			q := m.chatQuestions()[b[3]]
			m.pending = m.send(*t, chat.Outgoing{Text: "My answer:\n" + chat.AnswerLine(b[3], q, b[4])})
			m.layout()
			return true
		}
	}
	return false
}
