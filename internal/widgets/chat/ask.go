package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"crew/internal/plan"
	"crew/internal/widgets/look"
)

// Claude can't ask you anything mid-turn (crew runs it headless, where its question tool doesn't
// exist), so it ends its turn with a "## Questions" section, the format crew:plan uses: numbered
// questions with the options as indented bullets. When the last reply in a chat has one, its
// options show above the message box as buttons. A click answers that question; typing only the
// letters (a, or ab for two questions) and ⏎ answers them all. Anything else you type is sent as is.

// Questions are the questions with options at the end of Claude's last reply in a chat, or nil
// (also while it's working, or once you've replied).
func (cs *Chats) Questions(id string) []plan.Question {
	if cs.Conv(id).Busy {
		return nil
	}
	msgs := cs.Conv(id).Msgs
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case You:
			return nil
		case Agent:
			qs, _ := plan.QuestionsIn(msgs[i].Text)
			var out []plan.Question
			for _, q := range qs {
				if len(q.Options) > 0 {
					out = append(out, q)
				}
			}
			return out
		}
	}
	return nil
}

// AskView draws the questions' buttons above the message box, w wide, and where each button is:
// its line (from 0), start and end columns, question and option.
func AskView(qs []plan.Question, w int) (string, [][5]int) {
	if len(qs) == 0 {
		return "", nil
	}
	var at [][5]int
	lines := []string{look.KeyHints("Claude asks · click an option, or type its letter (one per question) and ⏎")}
	for i, q := range qs {
		line, x := fmt.Sprintf("%d ", i+1), 2
		for j, o := range q.Options {
			b := look.Button(optionLabel(o, j == q.Recommended), string(rune('a'+j)))
			if x+ansi.StringWidth(b) > w {
				break
			}
			at = append(at, [5]int{len(lines), x, x + ansi.StringWidth(b), i, j})
			line += b + " "
			x += ansi.StringWidth(b) + 1
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n", at
}

// optionLabel shortens an option for its button: its text without the "(recommended: …)" reason,
// with a ★ when it's the recommended one.
func optionLabel(o string, recommended bool) string {
	if i := strings.Index(strings.ToLower(o), "(recommended"); i >= 0 {
		o = strings.TrimSpace(o[:i])
	}
	if recommended {
		o += " ★"
	}
	return o
}

// PickedAnswers turns a message of only letters, one per question ("a", "ab", "a c"), into the
// answers; ok is false for anything else, which is sent as you typed it.
func PickedAnswers(qs []plan.Question, text string) (string, bool) {
	letters := strings.ReplaceAll(text, " ", "")
	if len(letters) != len(qs) {
		return "", false
	}
	var b strings.Builder
	for i, q := range qs {
		j := int(letters[i]) - 'a'
		if j < 0 || j >= len(q.Options) {
			return "", false
		}
		fmt.Fprintf(&b, "%s\n", AnswerLine(i, q, j))
	}
	return "My answers:\n" + b.String(), true
}

// AnswerLine is one answer as Claude gets it: the question and the option picked.
func AnswerLine(i int, q plan.Question, j int) string {
	return fmt.Sprintf("%d. %s → %s", i+1, q.Text, optionLabel(q.Options[j], false))
}
