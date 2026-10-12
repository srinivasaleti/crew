# crew: next tasks

A backlog for improving crew. Items are grouped by area and roughly ordered by value within each group.
Pick from **Now** first. Each item says what it is and why it matters.

## Now (small, high value)

- [x] **Open a PR from a task.** A key in the task (e.g. `ctrl+p`) runs `git push -u` and `gh pr create` in the worktree, then sets the status to `in-pr` and stores the PR URL on the task. This closes the loop from task to review.
- [x] **Show PR state on the task.** Read `gh pr view --json state,statusCheckRollup,reviewDecision` for tasks with a PR. Show CI ✓/✗/● and the review state in the header and sidebar. Refresh on a timer or with `r`.
- [x] **Stop a running agent.** A key (e.g. `ctrl+x`) cancels the current turn. Today the only way is quitting crew. Needs a per-turn context in `agent.Run`.
- [x] **Send while busy = queue.** Typing a message while the agent is working is ignored. Queue it and send it when the turn ends, and show "queued" in the chat.
- [ ] **Fetch before branching.** `repo.AddWorktree` branches from `origin/HEAD` as last fetched, so new tasks can start from stale code. Run `git fetch origin` first, with a short timeout. *(see `ponytail:` in repo.go)*
- [ ] **Rename a task and edit its description** from the status menu or a small form. Today the title is fixed after creation and the description can only be set by hand in the task file.

## Onboarding

See [onboarding-prd.md](onboarding-prd.md) for the full plan.

- [x] **Sign in again when Claude's login expires.** Tell an auth failure apart from other errors, show "Claude is signed out · ⌘L sign in", run `claude auth login` in a shell panel, then resend the failed message on the same session.
- [x] **Setup check and `/doctor`.** At start-up and on focus, check that git, `claude` and `gh` are installed and signed in (`claude auth status --json`, `gh auth status`; no tokens). Show a Setup panel with a fix key for each. GitHub sign-in gets the same banner and fix.
- [x] **Get started on first run.** Until there's a task (or while something's missing), home shows the checks and the first task, with `d` for Setup and `n` for a task.
- [ ] **Find repos for the first task.** Suggest git repos from `~/code`, `~/src`, `~/Developer`… in the new-task form, not just recent ones. Left out of v1 (see the PRD's open questions).
- [ ] **"Needs sign-in" in TASKS.** Show a task whose turn failed on auth as needing you in the overview, not just in its chat.
- [x] **Install path.** `make setup` checks for `claude` too; `crew doctor` / `make doctor` run the checks from a terminal.

## Agent and chat

- [ ] **Keep one Claude process per active task (for speed, not tokens).** Today every message starts `claude -p --resume`, which costs 1–3 s of start-up plus hooks and MCP servers each time. A long-running `claude -p --input-format stream-json` process per task, stopped after ~1 h idle or when the task is done or archived, would make replies start instantly and keep MCP servers connected. A probe showed it does **not** meaningfully save tokens: each turn still writes ~2.5k tokens to the cache either way. It also makes "Stop agent" and "queue while busy" easier.

- [ ] **Permission mode per task.** Agents run with `--permission-mode auto`. Let a task pick `plan`, `acceptEdits` or `auto` in the New task form, and show it in the header. *(see `ponytail:` in agent.go)*
- [x] **Model per task.** Choose Opus/Sonnet/Haiku when creating a task (`--model`), and show it in the header.
- [x] **Render agent replies as Markdown.** Replies contain code blocks, lists and links. Use `glamour` (charmbracelet) to render them. Keep tool lines as they are.
- [ ] **Expand an action.** Select a tool line to see its full input and output (the full command, the edit's old/new text). The data is in Claude's session log.
- [ ] **Inline diffs for edits.** Show a small red/green diff under `✎ Edit` lines, built from the tool input.
- [ ] **Live progress for long turns.** Show elapsed time and a spinner in the header while working, and use `--include-partial-messages` to stream text as it's written.
- [x] **Gemini.** Pick Claude or Gemini per task in the New task form (`agent.Gemini`: Gemini CLI headless, `--approval-mode yolo`). Gemini tasks have chat, tools, resume, `/model`, sign-in (⌘L runs `gemini`) and plans by hand; no usage, `/compact`, `/plan` or Claude Code commands.
- [ ] **More agents.** Codex (or others) with their own `Args`/`Parse`/`AuthError` in `agent.All` and a check in `setup`.
- [ ] **Usage for Gemini.** Read token counts from Gemini's session files (`~/.gemini/tmp/*/chats/`) or its `result` stats, so the Usage tab and costs cover Gemini tasks.
- [ ] **Copy a message.** A key that copies the selected reply to the clipboard (mouse mode blocks normal selection; ⌥-drag works but isn't obvious).

## Tasks and workflow

- [ ] **Clean up when done.** Optionally remove a task's worktree (and the branch, if merged) when it's marked done or archived. Ask first, and never remove uncommitted work.
- [x] **Search and filter tasks.** The TASKS and PULL REQUESTS overviews filter and search (`/`, `ctrl+f`); the sidebar lists only the sections and what's open, and `esc`/`⌘↑` goes back to the list, `⌘⇧↑/↓` to the previous or next item.
- [x] **Chat about a listed PR.** A PR's Chat pane: its own Claude session, in a checkout of the PR when the repo is attached; asks from the PR's comments go there.
- [x] **REQUESTS.** An HTTP client section: applications with environments (base URL, auth, Keychain keys that only go to the env's hosts and come back hidden), requests grouped in folders and edited in a popup (send, response, save, delete) with tags to filter by, environment switching, and a locked-down Claude chat that writes and sends requests (`crew:requests`).
- [ ] **Requests: capture values.** Save a value from a response (a login's token) into a secret for the next request, without it passing through the chat.
- [ ] **Group tasks by repo** in the sidebar, as an alternative to grouping by status.
- [ ] **Task templates.** "Fix bug", "Add endpoint", "Write tests": a starting description and status in the New task form.
- [ ] **Import from GitHub issues / Jira.** Create a task from an issue URL with its title and body.
- [ ] **Notifications.** A macOS notification (`osascript`) when an agent finishes or fails while crew isn't the focused window.

## Changes tab

- [ ] **File list navigation.** Jump between files in the diff (`n`/`p`) and collapse files.
- [ ] **Commit from the Changes tab.** Stage all and commit with a message, without asking the agent.
- [ ] **Page large diffs.** Untracked files are shown in full only for the first 50. *(see `ponytail:` in repo.go)*
- [ ] **Side-by-side view** for wide terminals.

## Usage tab

- [ ] **Total across tasks** on the home screen, and per repo.
- [ ] **Per-turn cost.** Use the difference between consecutive `cost-state` entries in the session log.
- [ ] **Load usage in the background.** It's read synchronously and will lag with very large session logs. *(see `ponytail:` in tabs.go)*

## Run (dev servers)

- [ ] **Setup step per repo.** New worktrees lack `node_modules`, `.env` files etc. A per-repo setup command (stored with the attached repo) runs after the worktree is created.
- [ ] **Run tab.** Start the repo's dev server in the worktree with an automatic free port, and show its logs. Stop it when the task is done.

## Robustness

- [ ] **Report failed writes.** Chat history writes ignore errors. Surface them once in the status line. *(see `ponytail:` in model.go)*
- [ ] **Validate task files.** A broken task file is skipped with a red line in the sidebar. Offer to fix or show which line is wrong.
- [ ] **YAML front matter.** The task file parser is a hand-rolled `key: value` reader. Switch to a YAML library if tasks grow nested fields. *(see `ponytail:` in task.go)*
- [ ] **Tests for the core packages.** Tests were removed on purpose. If the tool grows, add a few: task file round-trip, `repo.Diff`, `usage.Load` on a sample log, agent stream parsing.
- [ ] **Resize handling for small terminals.** Check layouts below ~80 columns and 20 rows.

## Distribution

- [ ] **Homebrew tap or `go install`** so other developers can install crew without cloning.
- [ ] **Config file** (`~/.crew/config`) for defaults: model, permission mode, sidebar width, colours.
- [ ] **README** with screenshots, keys and the data layout (`~/.crew/tasks`, `chats`, `repos`, and Claude's session logs).
