package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"crew/internal/agent"
	"crew/internal/catalog"
	"crew/internal/tasks"
)

// Command is a slash command offered in a task's message box: one of crew's own or a Claude Code
// command, skill or agent from the repo's .claude/ or your ~/.claude.
type Command struct {
	Name   string `json:"name"`   // "/plan", or an agent's name
	Desc   string `json:"desc"`   // what it does
	Tag    string `json:"tag"`    // where it comes from: "crew", "skill · repo", "agent · global"…
	Insert string `json:"insert"` // what picking it puts in the box
	Run    bool   `json:"run"`    // picking it also sends it (it takes no arguments)
	Hint   string `json:"hint"`   // the arguments it expects
}

// crewCommand is one of crew's commands that core runs: it turns into the message the agent gets.
type crewCommand struct {
	name, help, hint string
	msg              func(c *Core, t tasks.Task, arg string) (string, error)
}

// ponytail: only the commands that are a message to the agent; /model, /budget, /new, /split… are
// still the terminal's (app/keys.go). Move one here when the web UI needs it.
var crewCommands = []crewCommand{
	{"/plan", "have Claude read the code and write the task's plan (Plan tab); /plan <goal>", "", func(c *Core, t tasks.Task, arg string) (string, error) {
		if err := c.skillOK(t, "plan"); err != nil {
			return "", err
		}
		os.MkdirAll(c.cfg.Plans, 0o755)
		return PlanAsk(filepath.Join(c.cfg.Plans, t.ID+".md"), c.Plan(t.ID) != "", arg, Brief(t)), nil
	}},
	{"/compact", "summarise the conversation to save tokens", "", func(c *Core, t tasks.Task, _ string) (string, error) {
		switch {
		case AgentName(t) == agent.Gemini.Name:
			return "", errors.New("Gemini compresses its context on its own when it gets long")
		case t.Session == "" || !HasSession(t):
			return "", errors.New("nothing to compact yet, this session hasn't had a turn")
		}
		return "/compact", nil
	}},
	{"/debug", "have Claude find a bug's root cause, fix it and add a test that catches it", "<error or symptom>", func(c *Core, t tasks.Task, arg string) (string, error) {
		if err := c.skillOK(t, "debug"); err != nil {
			return "", err
		}
		if arg == "" {
			return "", errors.New("say what's wrong: /debug <the error, a log line, or what it does wrong>")
		}
		return "/crew:debug Symptom " + arg, nil
	}},
	{"/test", "have Claude write tests for this task's change, in the repo's style; /test <area> to focus", "", func(c *Core, t tasks.Task, arg string) (string, error) {
		if err := c.changeSkill(t, "test"); err != nil {
			return "", err
		}
		if arg != "" {
			return "/crew:test Base " + t.Base + ". Area " + arg, nil
		}
		return "/crew:test Base " + t.Base, nil
	}},
	{"/pr", "have Claude write this task's PR title and description", "", func(c *Core, t tasks.Task, _ string) (string, error) {
		if err := c.changeSkill(t, "pr"); err != nil {
			return "", err
		}
		os.MkdirAll(c.cfg.Plans, 0o755)
		return "/crew:pr Description file " + filepath.Join(c.cfg.Plans, t.ID+".pr.md") + ". Base " + t.Base, nil
	}},
	{"/self-review", "have Claude review this task's change before a PR: numbered findings, no code changed", "", func(c *Core, t tasks.Task, _ string) (string, error) {
		if err := c.changeSkill(t, "self-review"); err != nil {
			return "", err
		}
		return "/crew:self-review Review. Base " + t.Base, nil
	}},
}

// changeSkill is skillOK for a skill that reads the task's change, which needs its worktree.
func (c *Core) changeSkill(t tasks.Task, skill string) error {
	if err := c.skillOK(t, skill); err != nil {
		return err
	}
	if t.Base == "" || t.Worktree == "" {
		return errors.New("no change yet · the task's worktree is made with your first message")
	}
	return nil
}

// crewMessage turns one of crew's commands (text as typed) into the agent's message; ok is false
// when text isn't one of them.
func (c *Core) crewMessage(t tasks.Task, text string) (msg string, ok bool, err error) {
	name, arg, _ := strings.Cut(text, " ")
	for _, cc := range crewCommands {
		if cc.name == name {
			msg, err = cc.msg(c, t, strings.TrimSpace(arg))
			return msg, true, err
		}
	}
	return "", false, nil
}

// Commands are the slash commands a task's message box offers: crew's, then the repo's Claude Code
// commands, skills and agents, then your global ones (only Claude has those).
func (c *Core) Commands(id string) ([]Command, error) {
	t, err := c.task(id)
	if err != nil {
		return nil, err
	}
	var out []Command
	for _, cc := range crewCommands {
		cmd := Command{Name: cc.name, Desc: cc.help, Tag: "crew", Insert: cc.name, Run: cc.hint == "", Hint: cc.hint}
		if !cmd.Run {
			cmd.Insert += " "
		}
		out = append(out, cmd)
	}
	if AgentName(t) != agent.Claude.Name {
		return out, nil
	}
	dir := t.Worktree
	if dir == "" {
		dir, _ = c.cfg.Repos.Resolve(t.Repo)
	}
	items := catalog.Load(dir)
	for _, scope := range []string{"repo", "global"} {
		for _, it := range items {
			if it.Scope != scope {
				continue
			}
			cmd := Command{Name: "/" + it.Name, Desc: it.Description, Tag: it.Kind + " · " + it.Scope}
			switch {
			case it.Kind == catalog.Agent: // an agent needs a request, so it starts one
				cmd.Name, cmd.Insert = it.Name, "Use the "+it.Name+" agent to "
			case it.NeedsArgs:
				cmd.Insert, cmd.Hint = cmd.Name+" ", it.ArgHint
				if cmd.Hint == "" {
					cmd.Hint = "arguments"
				}
			default:
				cmd.Insert, cmd.Run = cmd.Name, true
			}
			out = append(out, cmd)
		}
	}
	return out, nil
}
