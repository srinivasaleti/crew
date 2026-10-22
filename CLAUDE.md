# crew

crew is a developer workspace for the Mac: a terminal UI (Go, Bubble Tea) shown in its own window
(`desktop/`), with sections you turn on or off on home:

- **INBOX**: everything waiting on you, newest first: replies you haven't read, questions an agent asks
  (in a chat, a plan, a proposed split), sign-ins that failed, feedback on your PRs, reviews asked of you.
  ⏎ goes to it. It keeps nothing: crew and the sections say what's waiting.
- **TASKS**: coding agents on tasks in parallel. Each task has its own git worktree and branch, a chat
  with its agent (Claude Code or Gemini CLI, run headless, one run per message), a plan, an editor,
  its pull request and its usage. The agent work happens in the `claude` (or `gemini`) CLI.
- **PULL REQUESTS**: PRs waiting for your review and your own, read with `gh`, with a chat and a
  Claude review you post.
- **REQUESTS**: an HTTP client (applications, environments, keys in the Keychain) with a locked-down
  Claude chat.
- **REMINDERS**: reminders, once or repeating, with a List tab and a locked-down Claude chat that
  manages them with `crew remind`.
- **FOCUS**: a focus timer (pomodoro or flow), move and eye breaks with a break screen, and the whole
  Mac's screen time with a daily limit (nothing to do with tasks; no agent, no tokens).
- **DOCKER** and **KUBERNETES**: containers, images, volumes and networks; pods and the rest, read with
  `docker` and `kubectl`. Logs, inspect/describe, a shell, cleanup, and a chat locked down to `crew ctr`.

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

and that the tests pass (CI runs the same, plus a macOS build):

```bash
go test ./...
```

The user tests crew by hand. Don't launch crew to try a change. The tests are smoke tests that drive
`Update` and `View` the way keys do (`app/smoke_test.go`), the guard hook (`guard_test.go`) and `proc`;
when you fix a key, popup or screen that broke, add a case there. Make a commit only when the user asks.

## Data on disk

- `~/.crew/tasks/<id>.md`: a task, as a small `key: value` front matter plus its description (`tasks`).
- `~/.crew/chats/<id>.jsonl`: the task's chat (`chat`).
- `~/.crew/repos`: attached repos, one path per line (`repo`).
- `~/.crew/plans/<id>.md`: the task's plan (`plan`), written by the `crew:plan` skill.
- `~/.crew/splits/<id>.md`: a proposed split into subtasks (plan format, one step per subtask), written by
  `crew:split` and applied only by you (`app/split.go`). A subtask is a task file with `parent: <id>` and
  `after: <ids>`, its worktree branched from its parent's branch; `m`/`/merge` merges it back (local).
- A chat about a listed PR is a task file with `pr: <url>` (kept out of the task list), run in
  `<repo>.worktrees/<id>` (a checkout of the PR) or, when the repo isn't attached, `~/.crew/prchats/<id>`.
- `~/.crew/requests/<app>/`: REQUESTS' applications: `env/<env>.env` (base URL, auth, plain values such as a request's path variables, `insecure = true` to skip certificate checks;
  keys by name only), `<folder>/*.http` requests (`*.http` at the top is the default folder; a header
  turned off is a `# Name: value` line),
  `.current-env`, `.folded`. Key values are in the macOS Keychain
  (service `crew-requests`, account `<app>/<env>/<key>`), never in a file.
  Its chat (a task file with `kind: requests`) runs Claude locked down (`requests.ChatArgs`); keep it that way.
- `<repo>.worktrees/<id>`: the task's worktree, on branch `<id>` (made from `origin/HEAD` after a `git fetch`); or, with `branch:` set (the New task
  form's Branch, `/branch`), the worktree that already has that branch, else `<repo>.worktrees/<branch>`
  (`repo.UseBranch`). Git won't check a branch out twice, so never `git checkout` it in another worktree.
- Claude Code's own session logs (`~/.claude/projects/…`) are read for usage (`usage`).
- JIRA keeps nothing in `~/.crew` but its chats: Atlassian's CLI (`acli`) holds the sign-in. A task made
  from an issue has `issue: <KEY>`. A chat about an issue is a task file with `kind: jira` and
  `issue: <KEY>` (kept out of the task list), run in `~/.crew/jirachats/<id>`.
- `~/.crew/sections`: the sections turned off on home (`inbox`, `tasks`, `prs`, `reqs`, `reminders`, `focus`, `jira`, `docker`, `kube`), one per line; all on
  by default (`sections.go`).
- `~/.crew/focus/`: FOCUS's settings (`settings.json`, edited with `e` on its page; the `CREW_FOCUS_*`
  variables are only defaults until it exists), its timer (`clock.json`) and `days/<date>.json` (`focus`).
- `~/.crew/reminders/reminders.json`: REMINDERS (`reminders`). Its chat (a task file with
  `kind: reminders`) runs Claude locked down to `crew remind` (`reminders.ChatArgs`); keep it that way.
- `~/.crew/containers/<id>/`: where a chat about a container or Kubernetes object runs (a task file with
  `kind: containers`). DOCKER and KUBERNETES keep nothing else; KUBERNETES' context is crew's own (`--context`
  on every call), never written to your kubeconfig. Its chat runs Claude locked down to `crew ctr`
  (`containers.ChatArgs`), which only reads and hides secret values; keep it that way.
- `CREW_PROD_CONTEXTS` lists Kubernetes contexts to treat as production besides names with `prod`: every
  change asks first there, and Cleanup is off.
- A task's `budget:` (USD, `/budget`; `off`; empty uses `CREW_TASK_BUDGET`) pauses it before its next message once its
  agent has spent that (`app/budget.go`; `send` checks it, so every message to an agent goes through it).
- `git config crew.verify "<command>"` (in a repo) is the checks a task must pass before `P` pushes it (`repo.Checks`
  guesses them otherwise). A pass lasts until the agent's next turn ends.
- `CREW_HOME` moves `~/.crew`.
- `~/.crew/lock`: held by the crew that's running (`main.go`), so only one runs per folder; a second
  one says so and exits. `crew req`, `remind`, `ctr` and `doctor` don't take it.
- `CREW_JIRA_EVERY=<duration>` (`2m`, `90s`) is how often JIRA reads your issues (3 minutes by default).
- `CREW_AUTO_COMPACT=<tokens>` runs `/compact` before your next message once a task's context reaches
  that size (off by default). `/new` starts a fresh session; the task file keeps the earlier ones in
  `previous:` so usage still counts them.

## Layout

`internal/` is layered, and each layer only uses the ones below it: **`app`** → **`sections`** →
**`widgets`** → **services**. Sections never import each other, and widgets never import a section.

**Services** (flat in `internal/`, no drawing):

| Package | Job |
|---|---|
| `agent` | Runs an agent CLI (Claude Code or Gemini CLI, `agent.All`) for one turn and turns its stream-json into events; how each one resumes and fails on sign-in |
| `tasks` | Task files (`Task`, `Store`), how a status reads and looks (`Labels`, `Icons`, `Tint`, `Chip`), archiving |
| `repo` | Attached repos, worktrees, diffs, branches |
| `gh` | Pull requests through the `gh` CLI: view, search, diff, line comments (GraphQL), reply, resolve, review, create, failed logs |
| `catalog`, `usage` | Claude Code's commands, skills and agents; token and cost usage |
| `plan` | A task's plan: parse the checklist, change a step's mark |
| `setup` | Checks that git, Claude Code, Gemini CLI and gh are installed and signed in, and whether notifications are allowed (their own status commands, no tokens); `crew doctor` |
| `notify`, `browser` | macOS notifications (through the app); opening links |
| `proc` | Starting a background command (git, gh, acli, the agents, docker, kubectl): its own session with no terminal, the whole group stopped with it |

**Widgets** (`internal/widgets/…`): parts several screens draw, each with a small `Host` interface
for what it needs from crew.

| Package | Job |
|---|---|
| `chat` | Every chat: logs on disk, running a turn (a message sent mid-turn stops the turn and goes next), drawing, `@`/`/` suggestions, Claude's questions as buttons |
| `prview` | A PR read with `gh`: checks and feedback, the popup, asking Claude, replying, resolving (a task's PR tab and a listed PR's conversation) |
| `files` | The Files tab: the tree and the editor; `files/edit` is its text buffer (cursor, selection, undo, find; no UI) |
| `term` | An embedded shell (pty + vt10x) |
| `table` | The filtered table a section shows: buttons, chips, a facet, search, pages, keys and clicks |
| `keys` | Key names (⌘, never ctrl), the ⌘ shortcuts file, decoding the app's escape sequences |
| `look`, `markdown` | The shared styles and small text helpers; Markdown for agent replies and GitHub comments |

**`internal/section`** is the contract between crew and a section: `Section` (embed `Base` for the
defaults), `Row` (a sidebar row), and `Host` (what crew gives a section).

**Sections** (`internal/sections/<name>`): each is all of one section, with a `section.go` that
implements `section.Section`. Its rows, its table or its own page (`ShowsTable`, `Render`, `Key`,
`Click`, `Tabs`…), its popup, its background work (`Init`, `On`, `Update`, `Spin`), its chat (a
hidden task: `Where`, `Args`) and the commands and ⌘ actions it adds.

| Package | Section |
|---|---|
| `inbox` | INBOX: the list of what's waiting (`Host.Waiting`: crew's own from `app/inbox.go`, plus each section that implements `section.Waits`, as PULL REQUESTS does for reviews asked of you) |
| `tasks` | TASKS: its row with the tasks in motion, the table (`Item`, `Table`), the new-task form; the status menu and delete prompt that crew shows (`Menu`, `DeleteView`) |
| `prs` | PULL REQUESTS: the list on its own clock (to review, yours, and the rest in your attached repos, grouped by repo; `r` reads it now), a listed PR's panes (conversation, chat, diff, review), its chat (`pr:` in the task file), Claude's review you post (`review.go`) |
| `requests` | REQUESTS: applications, environments (base URL, auth, keys in the Keychain), folders and `.http` requests, sending (keys only to the env's hosts), redaction, `crew req`, its screens and forms, and its locked-down chat |
| `reminders` | REMINDERS: reminders and how they repeat, parsing "4pm"/"every weekday", firing what's due, `crew remind`, the List table, the form, and its locked-down chat |
| `focus` | FOCUS: settings, the timer and days (`focus.go`), the settings form (`form.go`), the Mac's idle time from `ioreg` (`idle.go`), the ticker, breaks and the break screen, `/focus` (`section.go`), the page and the help line's status (`page.go`) |
| `containers` | DOCKER and KUBERNETES, two sections from one package (`NewDocker`, `NewKube`): the CLI reads (`docker.go`, `kube.go`), the tables per tab (`tables.go`, with `redact`), the keys' actions (`actions.go`), an item's page with Logs, Inspect/Describe and Chat (`thing.go`), the confirm, context and Cleanup popups (`popups.go`), and `crew ctr` for the chat (`cli.go`) |
| `jira` | JIRA: your open issues read with Atlassian's CLI (`acli`), grouped by space, an issue's card (Issue, Comments and Chat tabs; editing its summary, description, labels and status; posting a comment you write or Claude drafts, each only on your key), on its own clock (`CREW_JIRA_EVERY`) |

**`internal/app`** is crew itself: the window, the sidebar and the main pane, home (Setup and the
Sections block), the open task's workspace (its chat, tabs, shells and editor, which every section's
chat reuses), and the Host each section and widget gets. One package (everything is a method on
`Model`), split into files by area:

| Files | Area |
|---|---|
| `model.go`, `update.go`, `view.go` | The model, message handling and drawing |
| `host.go` | The sections: finding the one a row belongs to (`owner`, `page`), and `section.Host` |
| `sections.go`, `sidebar.go`, `overview.go` | Turning sections on or off on home; the sidebar (home, then each section's rows); a section's table in the main pane |
| `layout.go` | Sizes, frames, the screen geometry and click hit-testing |
| `keys.go`, `shortcuts.go`, `palette.go` | Shortcuts and slash commands as registries (the help line is built from them); the ⌘ shortcuts and `/keys`; the ⌘⇧P palette |
| `modal.go`, `focus.go`, `select.go` | Modals (quit, the model menu, a section's popup); who has the keys (`focusOn`); mouse selection and links |
| `chat*.go` | crew's side of the chat widget: which chat is on screen (`chat_here.go`), sending, questions as buttons |
| `tasks.go`, `tabs.go`, `changes.go`, `plan_tab.go`, `split.go`, `files.go`, `pr.go`, `shell*.go` | The task's workspace: its buttons, status and delete, `/branch`; the Changes and Usage tabs; your comments on lines of the diff, sent to the agent as a review (`changes.go`); the verify gate before `P` and `/verify` (`verify.go`, the command from `repo.Checks`); the Plan tab and what every agent run gets (`--plugin-dir`, `--add-dir`); `/split` (its proposal on the Plan tab, applying it, starting subtasks) and `/merge`; the Files tab; the PR tab and polling tasks' PRs; the shells |
| `inbox.go` | What the tasks have waiting on you for the INBOX (replies, questions, plans, splits, sign-ins, PR feedback), and `Host.Waiting` |
| `setup.go`, `notify.go` | Setup checks, the help line's banner, ⌘L and `/login`, the Setup panel (`/doctor`), Get started on home; notifications for replies and PRs |

**A new section** is a package in `internal/sections/<name>` with a `Section` (embed `section.Base`),
added to `m.secs` in `app.New`, to `sectionOrder` and `sidebarOrder` (`app/sections.go`,
`app/sidebar.go`); it checks `h.SectionOn` before background work. If it needs something from crew
that `section.Host` doesn't give, add it to `Host` rather than reaching into `app`. If it has things that
wait on you, implement `section.Waits` so they show in the INBOX.

Put new code in the file for its area; start a new file when an area grows past ~600 lines.

## The app

crew ships only as the app; running it in a terminal isn't supported any more. `desktop/` is the Mac
window (Wails: the system web view with xterm.js) around the crew binary that `make app` builds and
bundles. It holds **no crew features**: it passes keys on (⌘ keys and Shift+Enter as escape
sequences), pastes, zooms, reads the login shell's environment, keeps to one window per crew folder,
brings its window over every app for FOCUS's breaks when crew asks (`~/.crew/notify/window`: `front`, `normal`),
hands ⌘Q, the Dock's Quit and the close button to crew as ⌘Q (crew asks first), and relays notifications: crew
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

**Agents propose, you apply.** `plugin/hooks/guard.sh` (a `PreToolUse` hook in the plugin) blocks an
agent from writing crew's task files, so a split (or anything else that makes tasks) only happens on
your key in crew, and from pushing or posting to GitHub (`git push`, `gh pr create/merge/comment…`,
`gh api` writes): agents commit, and you push (`P` on the PR tab, which also pushes to an open PR) and
post from crew. Keep it that way, and never tell an agent to push or post in a prompt.

`make app` copies `plugin/` into `crew.app` (`Contents/Resources/plugin`); crew uses the checkout's
when it's there (edits apply on the next message) and the bundled copy otherwise (`main.plugin`).

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
- **Background commands never get crew's terminal.** It's the app's window: a child that reads `/dev/tty`
  (a kubectl sign-in plugin, an ssh or git prompt) takes your keys and keeps the window open after crew
  quits. Start them with `proc.Command`, never `exec.Command`; only GUI launchers (`open`, `osascript`,
  `security`) and the shell panel (its own pty) are exempt.
- **Bubble Tea style:** `Update` and `View` take `Model` by value; helpers that change state take `*Model`.
  Per-task state lives in maps keyed by task id (`fileTabs`, `shells`, `plans` on `Model`; chats in the
  chat widget, PRs in prview). A widget or section gets crew as a `Host` on each call (`&m`) and never
  keeps it: `Model` is a value, so a kept pointer goes stale.
- **Keys:** a shortcut that works everywhere goes in `keyCommands` (`keys.go`). A tab's own keys go in its
  `update…` function. Who gets a key is decided in `Update` (modals, the filter, ⇧+arrows, the editor,
  shells, crew's shortcuts, then the tab), so check that order when adding a focus state.
- **One way to move, everywhere** (REQUESTS is the model). Each key has one job:
  - `tab` / `⇧tab` switch tabs: a task's tabs, a listed PR's panes, REQUESTS' tabs, a table's filter
    chips. Nothing else switches tabs, and tab does nothing where there are none (the sidebar).
  - Every tab bar numbers its tabs ("Plan ⌘2", `tabLabel`) and `⌘1`… jump to them, even while typing
    (`tabsHere`, `jumpTab`). A section's tab bar reports itself with `TabsHere` and `GotoTab`. A table on a
    screen with no tab bar numbers its chips instead, and `⌘1`… pick them (`chipTabs`, `Table.ChipKeys`).
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
