# crew

crew is a developer workspace for the Mac: a terminal UI (Go, Bubble Tea) shown in its own window
(`desktop/`), with sections you turn on or off on home:

- **TASKS**: coding agents on tasks in parallel. Each task has its own git worktree and branch, a chat
  with its agent (Claude Code or Gemini CLI, run headless, one run per message), a plan, an editor,
  its pull request and its usage. The agent work happens in the `claude` (or `gemini`) CLI.
- **PULL REQUESTS**: PRs waiting for your review and your own, read with `gh`, with a chat and a
  Claude review you post.
- **REQUESTS**: an HTTP client (applications, environments, keys in the Keychain) with a locked-down
  Claude chat.
- **REMINDERS**: reminders, once or repeating, with a List tab and a locked-down Claude chat that
  manages them with `crew remind`.
- **FOCUS**: a focus timer, eye and stand-up breaks and screen time (no agent, no tokens).

## Build and run

```bash
make setup        # Go modules, Apple's command line tools, and the GitHub CLI (gh) for the PR tab
make run          # build crew.app and open it
make install-app  # build it and put it in /Applications (make uninstall-app removes it)
```

Before you hand back a change, check that it compiles and passes vet:

```bash
gofmt -l internal && go build ./... && go vet ./...
```

The user tests crew by hand. Don't launch crew or drive it to try a change, and don't add tests unless
asked. Make a commit only when the user asks.

## Data on disk

- `~/.crew/tasks/<id>.md`: a task, as a small `key: value` front matter plus its description (`tasks`).
- `~/.crew/chats/<id>.jsonl`: the task's chat (`chat`).
- `~/.crew/repos`: attached repos, one path per line (`repo`).
- `~/.crew/plans/<id>.md`: the task's plan (`plan`), written by the `crew:plan` skill.
- A chat about a listed PR is a task file with `pr: <url>` (kept out of the task list), run in
  `<repo>.worktrees/<id>` (a checkout of the PR) or, when the repo isn't attached, `~/.crew/prchats/<id>`.
- `~/.crew/requests/<app>/`: REQUESTS' applications: `env/<env>.env` (base URL, auth, plain values;
  keys by name only), `<folder>/*.http` requests (`*.http` at the top is the default folder),
  `.current-env`, `.folded`. Key values are in the macOS Keychain
  (service `crew-requests`, account `<app>/<env>/<key>`), never in a file.
  Its chat (a task file with `kind: requests`) runs Claude locked down (`reqChatArgs`); keep it that way.
- `<repo>.worktrees/<id>`: the task's worktree, on branch `<id>`; or, with `branch:` set (the New task
  form's Branch, `/branch`), the worktree that already has that branch, else `<repo>.worktrees/<branch>`
  (`repo.UseBranch`). Git won't check a branch out twice, so never `git checkout` it in another worktree.
- Claude Code's own session logs (`~/.claude/projects/…`) are read for usage (`usage`).
- JIRA keeps nothing in `~/.crew` but its chats: Atlassian's CLI (`acli`) holds the sign-in. A task made
  from an issue has `issue: <KEY>`. A chat about an issue is a task file with `kind: jira` and
  `issue: <KEY>` (kept out of the task list), run in `~/.crew/jirachats/<id>`.
- `~/.crew/sections`: the sections turned off on home (`tasks`, `prs`, `reqs`, `reminders`, `focus`, `jira`), one per line; all on
  by default (`sections.go`).
- `~/.crew/focus/`: FOCUS's timer (`clock.json`) and `days/<date>.json` (`focus`).
- `~/.crew/reminders/reminders.json`: REMINDERS (`reminders`). Its chat (a task file with
  `kind: reminders`) runs Claude locked down to `crew remind` (`reminders.ChatArgs`); keep it that way.
- `CREW_HOME` moves `~/.crew`.
- `CREW_JIRA_EVERY=<duration>` (`2m`, `90s`) is how often JIRA reads your issues (3 minutes by default).
- `CREW_AUTO_COMPACT=<tokens>` runs `/compact` before your next message once a task's context reaches
  that size (off by default). `/new` starts a fresh session; the task file keeps the earlier ones in
  `previous:` so usage still counts them.

## Layout

`internal/` has one small package per job, and `ui` uses them:

| Package | Job |
|---|---|
| `agent` | Runs an agent CLI (Claude Code or Gemini CLI, `agent.All`) for one turn and turns its stream-json into events; how each one resumes and fails on sign-in |
| `chat`, `repo` | Chat logs, attached repos, worktrees, diffs, branches |
| `tasks` | TASKS, all of it: task files (`Store`), how a status looks, the order tasks are listed in, the TASKS table, the new-task form and the status menu. ui hands it what it knows about each task (`Item`) |
| `table` | The filtered table a section shows (TASKS, PULL REQUESTS): buttons, chips, a facet, search, pages, keys and clicks |
| `edit` | The editor's text buffer: cursor, selection, undo, find. No UI |
| `gh` | Pull requests through the `gh` CLI: view, search, diff, line comments (GraphQL), reply, resolve, review, create, failed logs |
| `catalog`, `usage` | Claude Code's commands, skills and agents; token and cost usage |
| `plan` | A task's plan: parse the checklist, change a step's mark |
| `jira` | JIRA, all of it: your open issues, read with Atlassian's CLI (`acli`, signed in through the browser), grouped by space (`Screen`, the table), and an issue's card (`Card`): its Issue tab (details, description, attachments, linked issues, subtasks) and Comments tab, editing its summary, description and labels, changing its status, and posting a comment you write (or Claude drafts), each only on your key |
| `requests` | REQUESTS, all of it: applications, environments (base URL, auth, keys in the Keychain), folders and `.http` requests, sending (keys only to the env's hosts), redaction, `crew req`, and its screens and forms (`Screen`, `Form`) |
| `focus` | FOCUS's data: settings, the timer and days of screen time, in `~/.crew/focus` |
| `reminders` | REMINDERS, all of it but the glue: reminders and how they repeat, parsing "4pm"/"every weekday", firing what's due, `crew remind`, the List table, the form, and its chat's brief and lock-down |
| `look` | The shared styles and small text helpers, so screens in their own packages look like the rest |
| `setup` | Checks that git, Claude Code, Gemini CLI and gh are installed and signed in, and whether notifications are allowed (their own status commands, no tokens); `crew doctor` |

`internal/ui` is one package (everything is a method on `Model`), split into files by area:

| Files | Area |
|---|---|
| `model.go`, `update.go`, `view.go` | The model, message handling and drawing |
| `layout.go` | Sizes, frames, the screen geometry and click hit-testing |
| `keys.go` | Shortcuts and slash commands, as registries (the help line is built from them) |
| `modal.go` | Modals: which one is open, quit, the model menu |
| `sidebar.go`, `tabs.go` | The task list; the Changes and Usage tabs |
| `overview.go` | The main pane for a section's row: its table (the `table` package), handed keys and clicks |
| `tasks.go` | The glue for TASKS (the `tasks` package): each task's `Item`, opening a row, the new-task form and status menu as modals, deleting a task |
| `chat_*.go` | The chat: drawing, `@`/`/` suggestions, sending messages (a message sent mid-turn stops the turn and goes next), Claude's questions as buttons (`chat_ask.go`) |
| `files_*.go` | Files tab: the tree, the editor (keys and actions), the editor's drawing and mouse |
| `plan_tab.go` | Plan tab, `/plan`, and what every agent run gets (`--plugin-dir`, `--add-dir`) |
| `prs.go`, `review.go` | The PULL REQUESTS section (PRs to review and yours), a listed PR's panes, and Claude's review you post |
| `pr*.go` | PR tab: state and loading, the list, the popup, the actions (ask Claude, reply, resolve) |
| `jira.go` | The glue for JIRA (the `jira` package): its sidebar row, reading the list at start and on its own clock (`CREW_JIRA_EVERY`) while `acli` is installed, installing and signing in through Setup's shell, and working on an issue as a crew task (`issue:` in the task file) |
| `jira_issue.go` | A Jira issue opened in the main pane, like a listed PR: its sidebar row, the Issue / Comments / Chat tabs, and its Claude chat (a hidden task, `kind: jira`) |
| `reqs.go` | The glue for REQUESTS (the `requests` package): sidebar rows, handing it keys, clicks, messages and its popup, and its Claude chat |
| `pr_chat.go` | A listed PR's Chat pane: a hidden task per PR, so it reuses the task chat (`chatTask`, `prAgent`) |
| `shell*.go` | The embedded shells (pty + vt10x) and their mouse handling |
| `select.go`, `shortcuts.go` | Mouse selection per panel, links; the app's ⌘ shortcuts and `/keys` |
| `focus.go`, `palette.go`, `notify.go` | Who has the keys (`focusOn`); the ⌘⇧P palette; macOS notifications |
| `focus_section.go` | FOCUS: its row and page, the tick (counting time, ending sessions, breaks), the help line's timer, `⌘⇧F`, `/focus` |
| `reminders.go` | The glue for REMINDERS: its row, List and Chat tabs, the form as a popup, the tick that fires reminders, its Claude chat |
| `markdown.go` | Markdown for agent replies and GitHub comments |
| `sections.go` | Sections (TASKS, PULL REQUESTS, REQUESTS) you turn on or off on home: the list, the saved setting, the Sections block. A new section adds itself to `sectionDefs` and checks `sectionOn` where it adds sidebar rows or starts background work |
| `setup.go` | Setup checks on start and focus, the help line's banner, ⌘L and `/login` (sign in in a shell), the Setup panel (`/doctor`), Get started on home, resending a message that failed on auth |

Put new code in the file for its area; start a new `area_*.go` file when an area grows past ~600 lines.
A section that's mostly its own world (like REQUESTS) gets its own package with its screen, and `ui`
keeps only the glue.

## The app

crew ships only as the app; running it in a terminal isn't supported any more. `desktop/` is the Mac
window (Wails: the system web view with xterm.js) around the crew binary that `make app` builds and
bundles. It holds **no crew features**: it passes keys on (⌘ keys and Shift+Enter as escape
sequences), pastes, zooms, reads the login shell's environment, and relays notifications: crew
(`notify.go`, with `CREW_APP=1` set by the app) writes them to `~/.crew/notify/out/`, and the app
sends them as crew's own with Wails' notifications, asks or checks permission on `ask`/`check`
files, and writes `~/.crew/notify/status` (allowed, off, unavailable) for Setup. Everything else, ⌘ shortcuts
included, is decided in crew (`shortcuts.go`, bindable in `~/.crew/keys` with `/keys`), so a new
feature never needs the app to change.

## Skills

`plugin/` is a Claude Code plugin with crew's skills (`plugin/skills/<name>/SKILL.md`, named
`crew:<name>` in Claude). `make` bakes this folder's path into the binary and crew passes it to every
agent run with `--plugin-dir`, so skills stay in this repo, never in `~/.claude` or the user's
projects, and edits apply on the next message. Keep skills short and principle-led, like
`plugin/skills/plan`. A crew command that runs a skill checks `canUseSkill` (Claude only, plugin
found) and sends `/crew:<skill> <args>` with `raw: true`, as `/plan` and `/self-review` do.

**Asking you:** crew runs Claude headless, where its question tool doesn't exist (and hooks can't
answer it). So a skill that needs a decision ends its reply with a `## Questions` section, numbered,
options as indented bullets, one `(recommended: …)`; the plan shows them in the Plan tab, a chat
shows them as buttons above the message box (`chat_ask.go`).

## Practices

- **Write the least code that works.** Reuse what's in the repo, then the standard library, then an
  installed dependency. Add a dependency only when a few lines can't do it. No abstraction with one user.
- **Mark deliberate shortcuts** with a `ponytail:` comment that says the limit and the upgrade path,
  e.g. `// ponytail: first 100 threads; page if a PR ever has more.`
- **Comments explain why** in plain English. Every type and function gets a short doc comment; match the
  density of the surrounding code.
- **Imports** come in three groups: standard library, third party, `crew/…`.
- **Never block `Update`.** Anything slow (git, gh, the agent) runs in a `tea.Cmd` and comes back as a
  `…Msg`, declared next to the code that handles it.
- **Bubble Tea style:** `Update` and `View` take `Model` by value; helpers that change state take `*Model`.
  Per-task state lives in maps on `Model` keyed by task id (`convs`, `prs`, `editors`, `trees`, `shells`).
- **Keys:** a shortcut that works everywhere goes in `keyCommands` (`keys.go`). A tab's own keys go in its
  `update…` function. Who gets a key is decided in `Update` (modals, the filter, ⇧+arrows, the editor,
  shells, crew's shortcuts, then the tab), so check that order when adding a focus state.
- **One way to move, everywhere** (REQUESTS is the model). Each key has one job:
  - `tab` / `⇧tab` switch tabs: a task's tabs, a listed PR's panes, REQUESTS' tabs, a table's filter
    chips. Nothing else switches tabs, and tab does nothing where there are none (the sidebar).
  - Every tab bar numbers its tabs ("Plan ⌘2", `tabLabel`) and `⌘1`… jump to them, even while typing
    (`tabsHere`, `jumpTab`). A new tab bar adds its case to `tabsHere`.
  - `↑↓` move between rows: the sidebar, a table, REQUESTS, the Files tree, the PR tab, the plan.
  - `←`/`→` fold and unfold (folders, groups, the Files tree). They never move between panes or open
    things. On a row with nothing to fold, `←` goes up to its folder, group or section.
  - `⏎` goes into the task, PR, request or file (from the sidebar: into the main pane); `esc` comes
    back out. `⇧`+arrows move between panels from anywhere.
  - Kept on purpose: `⏎` on a folder or group folds it (nothing else to open); `↑↓` scroll a chat
    (you're reading, not choosing; ⌘⇧↑/↓ switch tasks); `tab` keeps its usual job in forms (next
    field), the chat box (accept a suggestion) and the editor (indent).
  - A screen's own actions are plain letters (`n` new, `s` status, `x` delete, `/` search…), shown as
    buttons above its list and in the hint under the pane.
  - The help line at the bottom is for keys that work everywhere; a key only one screen uses goes in
    that screen's hint under the pane, not the help line (`inListHint`).
- **Shortcuts are ⌘ keys, never ctrl.** Hints, buttons, help and docs only ever show ⌘ (`keyNames`,
  `cmdName`; the requests and table packages get it as `KeyName`). Internally a ⌘ letter arrives as
  its ctrl key (`cmdKey`), so ctrl keys still exist in code but are never shown. A new shortcut needs
  a ⌘ key that's free: check `defaultShortcuts`, and the editor, which gets ⌘ keys first while editing.
- **Mouse:** screen positions come from `layout.go` (`sideOuter`, `contentY`, `chatWidth`, …). Use them
  instead of new numbers.
- **The PR tab costs no tokens.** It reads GitHub with `gh`. Only sending something to the agent
  (ask, fix, fix all) does; keep it that way.
- **Outward actions** (push, create a PR, post a reply, resolve a thread) only happen on an explicit key
  from the user, and a PR is created only on a second `P`.
- **UI text** is short and plain: say what happens and the key for it, e.g. "⏎ posts it · esc cancel".

## Backlog

`docs/next-tasks.md` lists what's next. Update it when you finish or add an item.
