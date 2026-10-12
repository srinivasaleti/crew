// Package setup checks what crew needs on this Mac: git, Claude Code and the GitHub CLI, installed
// and signed in. It runs the CLIs' own status commands, so a check costs no tokens. No UI.
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Tool is one thing crew needs, as last checked.
type Tool struct {
	Name      string // what it's called on screen, e.g. "Claude Code"
	For       string // what crew uses it for
	Installed bool
	SignedIn  bool   // always true for a tool with no sign-in (git)
	Account   string // who it's signed in as, or how (e.g. "oauth_token"), when the CLI says
	Install   string // the command that installs it
	SignIn    string // the command that signs in; "" when there's nothing to sign in to
}

// OK reports whether the tool is installed and signed in.
func (t Tool) OK() bool { return t.Installed && t.SignedIn }

// Status is the result of one check of everything.
type Status struct {
	Git, Claude, GH Tool
	Checked         bool // a check has finished; before that, nothing is known
}

// Tools lists them in the order they're shown.
func (s Status) Tools() []Tool { return []Tool{s.Claude, s.GH, s.Git} }

// Check runs every check. It takes a few seconds at most, so run it off the UI's goroutine.
func Check(ctx context.Context) Status {
	return Status{Git: git(), Claude: claude(ctx), GH: gh(ctx), Checked: true}
}

// ClaudeSignIn is the command that signs Claude Code in. It opens the browser.
const ClaudeSignIn = "claude auth login"

// GHSignIn is the command that signs the GitHub CLI in.
const GHSignIn = "gh auth login"

func git() Tool {
	t := Tool{Name: "git", For: "worktrees and branches for every task", Install: "xcode-select --install", SignedIn: true}
	_, err := exec.LookPath("git")
	t.Installed = err == nil
	return t
}

func claude(ctx context.Context) Tool {
	t := Tool{Name: "Claude Code", For: "the agent in every task, chat and review",
		Install: "curl -fsSL https://claude.ai/install.sh | bash", SignIn: ClaudeSignIn}
	if _, err := exec.LookPath("claude"); err != nil {
		return t
	}
	t.Installed = true
	out, _ := run(ctx, "claude", "auth", "status", "--json")
	var st struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
		Email      string `json:"email"`
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		// ponytail: a Claude Code too old for `auth status` counts as signed in, so crew doesn't
		// nag; a turn that fails on auth still shows the sign-in prompt (agent.AuthError).
		t.SignedIn = true
		return t
	}
	t.SignedIn = st.LoggedIn
	t.Account = st.Email
	if t.Account == "" {
		t.Account = strings.ReplaceAll(st.AuthMethod, "_", " ")
	}
	return t
}

// ghAccount finds the account in `gh auth status`'s "Logged in to github.com account NAME".
var ghAccount = regexp.MustCompile(`account (\S+)`)

func gh(ctx context.Context) Tool {
	t := Tool{Name: "GitHub CLI", For: "the PR tab and PULL REQUESTS (crew works without it)",
		Install: "brew install gh", SignIn: GHSignIn}
	if _, err := exec.LookPath("gh"); err != nil {
		return t
	}
	t.Installed = true
	out, err := run(ctx, "gh", "auth", "status")
	t.SignedIn = err == nil
	if m := ghAccount.FindStringSubmatch(out); t.SignedIn && m != nil {
		t.Account = m[1]
	}
	return t
}

// run runs a status command with a short timeout and returns its stdout and stderr together.
func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil // a status command must never wait for input
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Report writes the check as plain text, for `crew doctor`. It reports whether everything crew
// needs to run tasks (git and Claude Code) is ready.
func Report(w io.Writer, s Status) bool {
	for _, t := range s.Tools() {
		mark, state := "✓", "ready"
		switch {
		case !t.Installed:
			mark, state = "✗", "not installed · install: "+t.Install
		case !t.SignedIn:
			mark, state = "✗", "signed out · sign in: "+t.SignIn
		case t.Account != "":
			state = "signed in (" + t.Account + ")"
		case t.SignIn != "":
			state = "signed in"
		}
		fmt.Fprintf(w, "%s %-12s %s\n  %s\n", mark, t.Name, state, t.For)
	}
	return s.Git.OK() && s.Claude.OK()
}
