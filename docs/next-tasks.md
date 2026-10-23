# crew: next tasks

A backlog for improving crew. Items are grouped by area and roughly ordered by value within each group.
Pick from **Now** first. Each item says what it is and why it matters.

## Now (small, high value)

- [x] **Open a PR from a task.** A key in the task (e.g. `ctrl+p`) runs `git push -u` and `gh pr create` in the worktree, then sets the status to `in-pr` and stores the PR URL on the task. This closes the loop from task to review.
- [x] **Show PR state on the task.** Read `gh pr view --json state,statusCheckRollup,reviewDecision` for tasks with a PR. Show CI ✓/✗/● and the review state in the header and sidebar. Refresh on a timer or with `r`.
- [x] **Stop a running agent.** A key (e.g. `ctrl+x`) cancels the current turn. Today the only way is quitting crew. Needs a per-turn context in `agent.Run`.
- [x] **Send while busy = queue.** Typing a message while the agent is working is ignored. Queue it and send it when the turn ends, and show "queued" in the chat.
- [x] **Fetch before branching.** `repo.AddWorktree` branches from `origin/HEAD` as last fetched, so new tasks can start from stale code. Run `git fetch origin` first, with a short timeout. *(see `ponytail:` in repo.go)*
- [ ] **Rename a task and edit its description** from the status menu or a small form. Today the title is fixed after creation and the description can only be set by hand in the task file.

## Onboarding

See [onboarding-prd.md](onboarding-prd.md) for the full plan.

- [x] **Sign in again when Claude's login expires.** Tell an auth failure apart from other errors, show "Claude is signed out · ⌘L sign in", run `claude auth login` in a shell panel, then resend the failed message on the same session.
- [x] **Setup check and `/doctor`.** At start-up and on focus, check that git, `claude` and `gh` are installed and signed in (`claude auth status --json`, `gh auth status`; no tokens). Show a Setup panel with a fix key for each. GitHub sign-in gets the same banner and fix.
- [x] **Get started on first run.** Home always shows the checks (as Get started, with the first task, until there's a task), with `d` for Setup and `n` for a task.
- [ ] **Find repos for the first task.** Suggest git repos from `~/code`, `~/src`, `~/Developer`… in the new-task form, not just recent ones. Left out of v1 (see the PRD's open questions).
- [ ] **"Needs sign-in" in TASKS.** Show a task whose turn failed on auth as needing you in the overview, not just in its chat.
- [x] **Install path.** `make setup` checks for `claude` too; `crew doctor` / `make doctor` run the checks from a terminal.

## FOCUS

See [focus-prd.md](focus-prd.md).

- [x] **Focus timer, breaks and screen time.** ⌘⇧F, /focus, eye and stand-up breaks, Today and This week on the FOCUS page.
- [x] **Screen time and breaks for the whole Mac.** Settings in the app (`e`, `settings.json`), pomodoro or flow, a move break every 30 minutes, quiet eye breaks, a break screen over every app (time's up, the break, break done), more time (⌘⇧E, or 5/15/25 on time's up, twice per session), auto start and starting with crew, the Mac's idle time from `ioreg`, a daily limit and an end of day. Nothing about tasks or agents any more.

## REMINDERS

- [x] **Reminders, once or repeating.** Their own section (moved out of FOCUS): a List tab with a form, and a Chat tab where Claude manages them with `crew remind`.
- [x] **Notifications from crew itself.** Sent by the app in crew's name (Wails 2.16), with a sound; Setup shows whether they're allowed and ⌘L asks macOS or opens System Settings. A fired reminder stays on the help line for 10 minutes.
- [ ] **Snooze.** "10m · 1h · tomorrow" on a reminder that just fired.
- [ ] **Reminders from tasks.** Tie a reminder to a task or PR, so it opens it when it fires.
- [ ] **While you're on break.** When a break starts, offer work to hand to agents (PR comments to fix, the next plan step).
- [ ] **Goals and streaks.** A daily goal (e.g. 4 focus sessions) and a streak on the FOCUS page.
- [ ] **A menu-bar timer.** Show the countdown in the Mac menu bar (needs `desktop/`).

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

- [x] **`/fix-review` fixes the PR's review feedback.** It sends the task's PR feedback (failing checks, open comments, change requests) to the agent, as `a` on the PR tab does; a self-review's findings are fixed by replying "fix all" or "fix 1 3". Comments that need no change get a drafted reply, which you post from the PR tab (`D` twice, or `r` on one).
- [x] **Split a task into parallel subtasks.** `/split` has Claude propose subtasks (`crew:split`) on the Plan tab; only you apply it (`⏎` twice), and a hook keeps agents off crew's task files. Subtasks branch from the parent's branch, start when what they come after is done, show under their task in TASKS and the sidebar, and `m`/`/merge` merges one back into the parent's branch.
- [ ] **Split from TASKS.** An `S` on a row that opens the task's Plan tab and runs `/split`.
- [ ] **Fold subtasks** under their task in the TASKS table (`←`/`→`).
- [x] **Clean up when done.** Deleting a task removes the worktree and branch crew made for it; its prompt warns first when that loses uncommitted or unpushed work. A branch that was there before the task keeps its worktree.
- [x] **Search and filter tasks.** The TASKS and PULL REQUESTS overviews filter and search (`/`, `ctrl+f`); the sidebar lists only the sections and what's open, and `esc`/`⌘↑` goes back to the list, `⌘⇧↑/↓` to the previous or next item.
- [x] **Chat about a listed PR.** A PR's Chat pane: its own Claude session, in a checkout of the PR when the repo is attached; asks from the PR's comments go there.
- [x] **REQUESTS.** An HTTP client section: applications with environments (base URL, auth, Keychain keys that only go to the env's hosts and come back hidden), requests grouped in folders and edited in a popup (send, response, save, delete) with tags to filter by, environment switching, and a locked-down Claude chat that writes and sends requests (`crew:requests`).
- [x] **Requests: insecure environments.** An environment's Certificates choice (`insecure = true` in its file) skips checking its servers' certificates, like curl `--insecure`, for dev servers with self-signed ones.
- [x] **Requests: a Postman-like editor.** Params (query rows in step with the URL, and path variables kept in the environment), Headers (rows you can turn off) and a JSON Body with line numbers and beautify; the response with its Body and Headers.
- [ ] **Requests: capture values.** Save a value from a response (a login's token) into a secret for the next request, without it passing through the chat.
- [x] **PULL REQUESTS: every PR you'd expect, by repo.** Review requests to your teams too, the other open PRs in your attached repos, grouped by repo, `r` to read again, and `⌘1`… on a list's chips.
- [ ] **PULL REQUESTS: more than 100.** Each search reads up to 100 PRs; page when a list has more.
- [ ] **Group tasks by repo** in the sidebar, as an alternative to grouping by status.
- [ ] **Task templates.** "Fix bug", "Add endpoint", "Write tests": a starting description and status in the New task form.
- [x] **JIRA issues in full.** An issue opens in the main pane like a PR: Issue (details, description, attachments, linked issues, subtasks; digits open them), Comments (write one or have Claude draft it), Chat (Claude about the issue, with acli). `e` edits the summary, description and labels, `s` changes the status. The list loads at start and every `CREW_JIRA_EVERY` (3m).
- [ ] **Jira: keep the description's formatting when editing.** It's edited and saved as plain text; send ADF (`--description-file`) instead.
- [ ] **Jira: real transitions.** The status list offers the statuses of your own issues in the space, plus Done; ask Jira for the workflow's transitions.
- [ ] **Import from GitHub issues / Jira.** Create a task from an issue URL with its title and body.
- [ ] **Notifications.** A macOS notification (`osascript`) when an agent finishes or fails while crew isn't the focused window.

## DOCKER and KUBERNETES

- [x] **Containers and clusters.** DOCKER (containers, images, volumes, networks) and KUBERNETES (pods, deployments, services, jobs, config, events) read with their CLIs: logs, inspect/describe with secrets hidden, a shell, port-forward, restart and remove on your key, Cleanup that shows what goes first, production contexts that ask, and a chat locked down to `crew ctr`.
- [ ] **Compose projects as a whole.** Restart or stop every container of a Compose project at once (`docker compose -p`).
- [ ] **Search and pick in logs.** `/` to filter the Logs tab, and pick lines to ask Claude about.
- [ ] **Scale a deployment** from KUBERNETES (asks first; twice on production).
- [ ] **One namespace on big clusters.** KUBERNETES reads every namespace (`-A`); read only the one picked when that gets slow.
- [ ] **Docker and kubectl in Setup.** Show them in Setup and `crew doctor`, with how to install each.

## Branches

- [x] **Work on an existing branch or PR.** The New task form's Branch field and `/branch` use the worktree that has the branch (git won't check one out twice), else check it out; `/branch cleanup` removes the unused one crew made.
- [ ] **Pick from branches.** Suggest local and remote branches, and your open PRs, in the Branch field.

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

## Code structure

- [x] **No more `ui` package.** `internal/` is layered: `app` → `sections` (each section a plugin of the `section` contract) → `widgets` → services. See CLAUDE.md's Layout.
- [ ] **The task workspace in its own package.** `app` still holds an open task's workspace (its tabs, Plan tab, Files tab glue, shells, `/branch`). Move it out (e.g. `internal/workspace`) once its Host is clear, so `app` is only the window, the sidebar, home and the sections.
- [ ] **Home as a section.** Home (Setup and the Sections block) is still drawn by `app`; it could be a section like the others if it grows.

## Running crew

- [x] **One crew per folder.** crew locks `~/.crew` while it runs (a second one says so and exits), and the app brings its window to the front when opened again. Two crews on one folder would run a task's agent twice and fire reminders twice.
- [x] **Ask before quitting the app.** ⌘Q, the Dock's Quit and the window's close button go to crew's "Quit crew?".
- [ ] **Profiles.** "New window with profile…" in the app (its own `CREW_HOME`), with the profile's name in the title bar, for separate setups (work and personal) side by side.

## Distribution

- [ ] **Homebrew tap or `go install`** so other developers can install crew without cloning.
- [ ] **Config file** (`~/.crew/config`) for defaults: model, permission mode, sidebar width, colours.
- [ ] **README** with screenshots, keys and the data layout (`~/.crew/tasks`, `chats`, `repos`, and Claude's session logs).
