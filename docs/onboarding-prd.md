# PRD: Onboarding to crew

**Status:** R1–R5 are built (see `internal/setup` and `internal/ui/setup.go`). Two parts were left out:
repo discovery for the first task, and "needs sign-in" in the TASKS overview. Both are in `next-tasks.md`.

## Problem

Getting from "I heard about crew" to "an agent is working on my task" takes too many steps, and
the app does little to help. You clone the repo, run `make setup` and then `make install-app`, and
you have to know beforehand that `claude` and `gh` must be installed and signed in. When something
is missing, crew doesn't say so until it fails:

- **`claude` missing or signed out.** You see it only after sending your first message, as a red
  line in the chat holding the CLI's raw error (`agent.Run` passes stderr through as `KindError`).
  Nothing says what to do next.
- **Claude login expired.** OAuth tokens expire or get revoked. Every turn in every task then fails
  with the same raw error, and the only fix is to know you should leave crew, open a terminal, run
  `claude`, then `/login`.
- **`gh` missing or signed out.** The PR tab shows a hint, but the sidebar's PR badges, PULL
  REQUESTS and notifications fail quietly.
- **The app can't find the tools.** The app reads your login shell's environment (`loginEnv` in
  `desktop/main.go`). If `claude` is set up in a way that doesn't load there, crew says nothing.
- **Hard to start the first task.** The Welcome screen is a list of keys. With no tasks and no
  repos, it doesn't lead you to your first task.

## Goal

A new user gets from opening crew to a first agent reply in **under 2 minutes**, without reading
the README. When a login expires, they sign in again **without leaving crew**, in **one key**, and
nothing is lost.

## Non-goals

- Installing crew without building it (a signed, notarised `.dmg`, Homebrew cask). Worth doing,
  but separate. This PRD makes the current build path smoother.
- Supporting agents other than Claude (see "More agents" in `next-tasks.md`). The design below
  keeps a per-agent check so another agent can add its own later.
- An account system or telemetry for crew itself.

## Users

1. **First-time user.** Has a Mac and git, may not have Claude Code or `gh`, and has never seen
   crew.
2. **Returning user whose login expired.** Was using crew happily. Today, tasks start failing.
3. **User on a new machine.** Knows crew, but this Mac is missing pieces.

## Principles

- **Check, don't wait for failure.** crew checks what it needs at start-up and when you come back
  to the window. The checks run CLIs and cost no tokens.
- **Every problem comes with a fix and the key for it.** Never just a raw error. Follow CLAUDE.md's
  UI text rule: "Claude isn't signed in · ⏎ sign in · esc later".
- **Fix it inside crew.** Signing in opens in crew's own shell panel (the pty shells already
  exist), so the browser flow and any prompts work as in a terminal.
- **Never block.** crew opens even when something is missing. Only the actions that need the
  missing tool are disabled, and they say why.
- **Nothing is lost.** A message that failed because of auth is kept and can be sent again with
  one key after you sign in.

## Requirements

### R1. Setup check (start-up and on demand)

When crew starts, and when the window regains focus (at most once a minute), run these in a
`tea.Cmd`:

| Check | How (no tokens) | Needed for |
|---|---|---|
| git installed | `exec.LookPath("git")` | everything |
| Claude Code installed | `exec.LookPath("claude")`, `claude --version` | tasks, chats, reviews |
| Claude signed in | `claude auth status --json` → `loggedIn` | tasks, chats, reviews |
| GitHub CLI installed | `exec.LookPath("gh")` | PR tab, PULL REQUESTS |
| GitHub signed in | `gh auth status` | PR tab, PULL REQUESTS |

The result lives on `Model` as a small `setup` struct. `/doctor` (and an entry in the ⌘⇧P palette)
opens a **Setup** panel that lists each check as ✓ or ✗, with one line on what it's for and a key
to fix it. It's the same panel that onboarding shows (R2).

### R2. First run

If `~/.crew` has no tasks and no repos, crew opens to a **Get started** panel instead of the
Welcome screen. It has three steps; each shows ✓ once done, and you can skip any of them.

1. **Connect Claude.** ✓ if installed and signed in. If not installed: show the install command
   (`npm install -g @anthropic-ai/claude-code`, or the official installer) with `⏎` to run it in a
   shell panel. If signed out: `⏎` signs in (R3).
2. **Connect GitHub (optional).** Same pattern, with `brew install gh` and `gh auth login`. Say
   that crew works without it and only the PR features need it.
3. **Start your first task.** Pick a repo: recent git repos found in common folders (`~/code`,
   `~/src`, `~/dev`, `~/projects`, `~/Developer`, at most 2 levels deep, cached), or type a path.
   Then name the task and say what to do. This uses the existing new-task form, with the repo
   already filled in.

After step 3, the chat opens with a one-line tip: "⏎ sends · ⌘⇧P every command · /plan for
bigger tasks".

### R3. Sign in again when Claude's login expires

**Detecting it.**
- *Before a turn:* the R1 check reports `loggedIn: false`.
- *During a turn:* a `KindError` (or an `is_error` result) whose text matches the CLI's auth
  failures (e.g. "Invalid API key", "/login", "OAuth token has expired", "401",
  "authentication_error") is classed as **auth expired**, not a generic error. Keep the patterns
  in `agent` beside `parseClaude`, as an `Agent.AuthError func(text string) bool`, so each agent
  defines its own.
- `auth status` can say "signed in" while the server rejects the token, so the during-a-turn
  check is the one we rely on. The start-up check catches the simple cases early.

**What the user sees.**
- In the chat, instead of the raw error: a clear block that says
  "Claude's login expired · ⏎ sign in again · ⌘C copy error". The raw text is folded below it.
- A banner at the top of the window, shown once for all tasks: "Claude is signed out · ⌘L
  sign in". It shows on every screen until signing in works.
- The task's agent state shows "needs sign-in" in the TASKS overview, not "failed".

**Signing in.**
- `⏎` on the block, `⌘L`, or `/login` opens a shell panel running `claude auth login`. That
  opens the browser, as it does in a terminal. If the user's account uses SSO or the Console,
  the panel says so and offers `claude auth login --sso` / `--console`.
- When that shell exits, crew runs the R1 Claude check again. If it passes, the banner goes away,
  the shell closes, and a notice says "Signed in · ⏎ resend your last message".
- **Resend.** The message that failed is kept on the task (not just in the chat log). `⏎` sends
  it again with `--resume` on the same session, so the conversation carries on. Messages queued
  in other tasks while signed out stay queued and are sent in order once sign-in works.
- `/logout` runs `claude auth logout` (asks first). `/doctor` shows which account and method are
  in use (`authMethod` from `auth status`).

**API-key users.** If `ANTHROPIC_API_KEY` is set and fails, the fix is a key, not a browser
login. Say "Your ANTHROPIC_API_KEY was rejected · set a new one in ~/.zshrc and restart crew".
Never show the key.

### R4. GitHub sign-in, the same way

`gh` auth failures (crew already runs `gh` from `internal/gh`) are classed the same way and get the
same banner (a separate one, "GitHub is signed out · ⌘G sign in") and the same shell-panel fix
(`gh auth login`). PR features show "Sign in to GitHub to see pull requests · ⌘G" instead of
empty lists.

### R5. Install path

- `make setup` also checks for `claude` and prints how to install it and `claude auth login` if it's
  missing or signed out, as it already does for `gh`.
- README Setup becomes 3 commands plus "open crew; it walks you through the rest".
- `make doctor` runs the same checks from the terminal (the crew binary with a `doctor`
  argument, next to the existing `crew req`).

## UX copy (draft)

| Moment | Text |
|---|---|
| Banner, Claude signed out | `Claude is signed out · ⌘L sign in` |
| Chat block, auth failed | `Claude's login expired · ⏎ sign in again` |
| After sign-in | `Signed in · ⏎ resend your last message` |
| Claude not installed | `Claude Code isn't installed · ⏎ install it` |
| GitHub signed out (PR views) | `Sign in to GitHub to see pull requests · ⌘G` |
| Get started, step 3 | `Pick a repo for your first task · ↑↓ pick · ⏎ use it · type a path` |

## Where it goes in the code

| Piece | Where |
|---|---|
| Checks (`Check() Status`) | new `internal/setup` package: install and auth for claude, gh, git. No UI |
| Auth error patterns | `internal/agent` (`Agent.AuthError`), `internal/gh` (same for gh) |
| Setup panel, Get started, banner | new `internal/ui/setup.go` |
| `⌘L`, `⌘G`, `/login`, `/logout`, `/doctor` | `keyCommands` and slash commands in `keys.go` |
| Sign-in shell | reuse `shell.go`, starting a command instead of the login shell |
| Keep and resend the failed message | the conversation state in `chat_session.go` |
| `crew doctor` | `main.go`, next to `crew req` |

Every check runs in a `tea.Cmd` and comes back as a `setupMsg`, so it never blocks `Update`.

## Milestones

1. **Sign in again (R3).** The pain is highest here: today an expired login breaks every task
   without explaining why. Includes the auth-error class, the banner, `⌘L` / `/login` in a shell,
   and resend.
2. **Setup check and `/doctor` (R1, R4).** Checks at start-up and on focus, the Setup panel, and
   the GitHub sign-in banner.
3. **Get started (R2).** First-run panel and repo discovery.
4. **Install path (R5).** `make setup` and `make doctor`, README.

## How we'll know it works

The user tests by hand, so success is a short script:

- New Mac account with only git and Go: clone, `make setup`, `make install-app`, open crew, first
  reply in under 2 minutes, no README needed.
- `claude auth logout` while crew is open, then send a message: the banner and chat block appear,
  `⌘L` signs in, `⏎` resends, the reply continues the same conversation.
- `gh auth logout`: PR views say how to sign in; tasks keep working.
- Uninstall `claude`: crew still opens, and the Setup panel says how to install it.

## Open questions

1. **Exact auth error texts.** We need samples of the CLI's output for an expired OAuth token, a
   revoked one, and a bad API key, in stream-json, to pin down the patterns. Gather them before
   building R3.
2. **Shell or not for sign-in?** `claude auth login` might not need a TTY and could run as a plain
   process that opens the browser. A shell panel is the safe default, since it handles any
   prompts. Confirm while building.
3. **Repo discovery.** Is scanning common folders worth it, or is "recent repos + type a path"
   enough? It could be skipped in v1.
4. **Long-lived tokens.** `claude setup-token` makes a long-lived token. Should `/doctor` offer it
   to people who get logged out often? Only with a clear note on where the token is kept.
