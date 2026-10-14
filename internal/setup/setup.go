// Package setup checks what crew needs on this Mac: git, Claude Code, the GitHub CLI and Atlassian's
// CLI (for JIRA), installed and signed in. It runs the CLIs' own status commands, so a check costs no tokens. No UI.
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	Hint      string // what to do once SignIn runs, when it isn't obvious
	Agent     string // the agent it runs (agent.Agent's Name), for an agent's CLI
	Optional  bool   // crew works without it; only some features need it
	Off       string // what not signed in is called, when it isn't "signed out" (Notifications: "off")
}

// Notifications is the Notifications row's name; ⏎ on it asks macOS instead of running a command.
const Notifications = "Notifications"

// OK reports whether the tool is installed and signed in.
func (t Tool) OK() bool { return t.Installed && t.SignedIn }

// Status is the result of one check of everything.
type Status struct {
	Git, Claude, Gemini, GH, Jira, Notify Tool
	Checked                               bool // a check has finished; before that, nothing is known
}

// Tools lists them in the order they're shown.
func (s Status) Tools() []Tool { return []Tool{s.Claude, s.Gemini, s.GH, s.Jira, s.Notify, s.Git} }

// Agent is the check of an agent's CLI, by the agent's name (Claude Code for anything unknown).
func (s *Status) Agent(name string) *Tool {
	if name == s.Gemini.Agent {
		return &s.Gemini
	}
	return &s.Claude
}

// Check runs every check; notifyDir is where crew's app says whether notifications are allowed. It
// takes a few seconds at most, so run it off the UI's goroutine.
func Check(ctx context.Context, notifyDir string) Status {
	return Status{Git: git(), Claude: claude(ctx), Gemini: gemini(), GH: gh(ctx), Jira: jira(ctx), Notify: notifications(notifyDir), Checked: true}
}

// InApp reports whether crew runs in its Mac app, which sends notifications in crew's name.
func InApp() bool { return os.Getenv("CREW_APP") == "1" }

// NotifyStatus is what crew's app last said about notifications: "allowed", "off", "unavailable:
// <why>", or "" when it hasn't (crew isn't in the app).
func NotifyStatus(dir string) string {
	data, _ := os.ReadFile(filepath.Join(dir, "status"))
	return strings.TrimSpace(string(data))
}

// notifications checks whether macOS lets crew notify you. In the app they're crew's own, allowed
// or not in System Settings; elsewhere (a terminal, crew doctor) osascript sends them, under Script
// Editor's setting, which crew can't read.
func notifications(dir string) Tool {
	t := Tool{Name: Notifications, For: "reminders, finished agents and PR changes reach you", Installed: true, SignedIn: true,
		Optional: true, Off: "off", Account: "sent with osascript: allow Script Editor in System Settings → Notifications"}
	if !InApp() {
		return t
	}
	switch st := NotifyStatus(dir); {
	case st == "allowed":
		t.Account, t.SignIn = "allowed", Notifications
		if e, err := os.ReadFile(filepath.Join(dir, "error")); err == nil { // allowed, but macOS refused the last one
			t.SignedIn, t.Off = false, "allowed, but the last one failed: "+strings.TrimSpace(string(e))
		}
	case st == "off":
		t.SignedIn, t.SignIn = false, Notifications
	case st != "":
		t.Account = "sent with osascript (" + st + ")"
	}
	return t
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
	t := Tool{Name: "Claude Code", For: "the default agent: tasks, PR chats and reviews",
		Install: "curl -fsSL https://claude.ai/install.sh | bash", SignIn: ClaudeSignIn, Agent: "claude"}
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

// gemini checks Gemini CLI. It has no status command, so it counts as signed in with an API key in
// the environment or the credentials "Login with Google" saves. Signing in runs Gemini itself.
func gemini() Tool {
	t := Tool{Name: "Gemini CLI", For: "tasks you give to Gemini (crew works without it)",
		Install: "npm install -g @google/gemini-cli", SignIn: "gemini",
		Hint: "pick Login with Google (or paste an API key), then /quit", Agent: "gemini", Optional: true}
	if _, err := exec.LookPath("gemini"); err != nil {
		return t
	}
	t.Installed = true
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_GCA"} {
		if os.Getenv(k) != "" {
			t.SignedIn, t.Account = true, k
			return t
		}
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".gemini", "oauth_creds.json")); err == nil {
		t.SignedIn, t.Account = true, "Google login"
	}
	return t
}

// ghAccount finds the account in `gh auth status`'s "Logged in to github.com account NAME".
var ghAccount = regexp.MustCompile(`account (\S+)`)

func gh(ctx context.Context) Tool {
	t := Tool{Name: "GitHub CLI", For: "the PR tab and PULL REQUESTS (crew works without it)",
		Install: "brew install gh", SignIn: GHSignIn, Optional: true}
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

// AcliInstall installs Atlassian's CLI the way Atlassian documents with curl, into ~/.local/bin
// (where Claude Code installs too), so it needs no sudo. The Makefile's acli target does the same.
const AcliInstall = `mkdir -p ~/.local/bin && curl -fsSL -o ~/.local/bin/acli "https://acli.atlassian.com/darwin/latest/acli_darwin_$(uname -m | sed s/x86_64/amd64/)/acli" && chmod +x ~/.local/bin/acli`

// jira checks Atlassian's CLI, which JIRA reads your issues with; signing in opens the browser.
func jira(ctx context.Context) Tool {
	t := Tool{Name: "Atlassian CLI", For: "JIRA: your Jira issues (crew works without it)",
		Install: AcliInstall, SignIn: "acli jira auth login --web", Optional: true}
	if _, err := exec.LookPath("acli"); err != nil {
		return t
	}
	t.Installed = true
	_, err := run(ctx, "acli", "jira", "auth", "status")
	t.SignedIn = err == nil
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
		case !t.Installed && t.Optional:
			mark, state = "○", "not installed (optional) · install: "+t.Install
		case !t.Installed:
			mark, state = "✗", "not installed · install: "+t.Install
		case !t.SignedIn && t.Off != "":
			mark, state = "✗", t.Off
		case !t.SignedIn:
			mark, state = "✗", "signed out · sign in: "+t.SignIn
		case t.Off != "": // not a sign-in: say it as it is
			state = t.Account
		case t.Account != "":
			state = "signed in (" + t.Account + ")"
		case t.SignIn != "":
			state = "signed in"
		}
		fmt.Fprintf(w, "%s %-13s %s\n  %s\n", mark, t.Name, state, t.For)
	}
	return s.Git.OK() && s.Claude.OK()
}
