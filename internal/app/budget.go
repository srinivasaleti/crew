package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"crew/internal/tasks"
	"crew/internal/widgets/chat"
)

// Budgets: what a task's agent may spend (USD, from Claude Code's session logs) before crew pauses
// it. A task's own (/budget, budget: in its file), else CREW_TASK_BUDGET for every task; none by
// default. Reaching it stops the next message, not a turn under way: crew reads the cost when a turn
// ends. Section chats (a PR's, REQUESTS') have none. Gemini reports no cost, so its tasks never stop.

// budgetOf is a task's budget in USD; 0 for none.
func budgetOf(t tasks.Task) float64 {
	switch {
	case t.Section() || t.Budget < 0:
		return 0
	case t.Budget > 0:
		return t.Budget
	}
	v, _ := strconv.ParseFloat(os.Getenv("CREW_TASK_BUDGET"), 64)
	return max(v, 0)
}

// overBudget reports whether a task's agent has spent its budget, and how much of what.
func (m Model) overBudget(t tasks.Task) (spent, budget float64, over bool) {
	budget, spent = budgetOf(t), m.usage[t.ID].Cost
	return spent, budget, budget > 0 && spent >= budget
}

// budgetNote is what crew says when a task is paused on its budget.
func budgetNote(spent, budget float64) string {
	return fmt.Sprintf("budget reached: %s of %s · /budget <usd> raises it, /budget off removes it", cost(spent), cost(budget))
}

// spentLabel is the header's cost: "$3.10", or "$3.10 of $5.00" with a budget (orange from 80%).
func (m Model) spentLabel(t tasks.Task) string {
	spent, budget, over := m.overBudget(t)
	switch {
	case budget == 0:
		return cost(spent)
	case over:
		return warn.Render(cost(spent) + " of " + cost(budget) + " · paused")
	case spent >= budget*0.8:
		return notice.Render(cost(spent) + " of " + cost(budget))
	}
	return cost(spent) + " of " + cost(budget)
}

// runBudget is /budget [usd|off]: set the task's budget, or say what it is.
func runBudget(m *Model, t *tasks.Task, arg string) {
	m.add(t.ID, chat.You, strings.TrimSpace("/budget "+arg))
	switch arg = strings.TrimPrefix(strings.TrimSpace(arg), "$"); {
	case arg == "":
		spent, budget, _ := m.overBudget(*t)
		if budget == 0 {
			m.add(t.ID, chat.Tool, "no budget · spent "+cost(spent)+" · /budget 5 sets one")
		} else {
			m.add(t.ID, chat.Tool, "spent "+cost(spent)+" of "+cost(budget))
		}
		return
	case arg == "off":
		t.Budget = -1
	default:
		v, err := strconv.ParseFloat(arg, 64)
		if err != nil || v <= 0 {
			m.add(t.ID, chat.Error, "say an amount in dollars, like /budget 5, or /budget off")
			return
		}
		t.Budget = v
	}
	if err := m.cfg.Tasks.Save(*t); err != nil {
		m.add(t.ID, chat.Error, err.Error())
		return
	}
	if budget := budgetOf(*t); budget > 0 {
		m.add(t.ID, chat.Tool, "budget set to "+cost(budget))
	} else {
		m.add(t.ID, chat.Tool, "no budget for this task")
	}
}
