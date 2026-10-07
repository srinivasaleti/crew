# crew

crew is a Mac app for running coding agents on tasks in parallel: a terminal UI (Go, Bubble Tea)
shown in its own window (`desktop/`). Each task has its own git worktree and branch, a chat with
Claude Code (run headless, one `claude -p` per message), an editor, its pull request and its usage. crew is a harness: the agent work happens in the `claude` CLI.

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

- `~/.crew/tasks/<id>.md`: a task, as a small `key: value` front matter plus its description (`task`).
- `~/.crew/chats/<id>.jsonl`: the task's chat (`chat`).
- `~/.crew/repos`: attached repos, one path per line (`repo`).
- `~/.crew/plans/<id>.md`: the task's plan (`plan`), written by the `crew:plan` skill.
- `<repo>.worktrees/<id>`: the task's worktree, on branch `<id>` (you can switch it, e.g. to `feat/…`).
- Claude Code's own session logs (`~/.claude/projects/…`) are read for usage (`usage`).
- `CREW_HOME` moves `~/.crew`.
- `CREW_AUTO_COMPACT=<tokens>` runs `/compact` before your next message once a task's context reaches
  that size (off by default). `/new` starts a fresh session; the task file keeps the earlier ones in
  `previous:` so usage still counts them.

## Layout

`internal/` has one small package per job, and `ui` uses them:

| Package | Job |
|---|---|
| `agent` | Runs an agent CLI for one turn and turns its stream-json into events |
| `chat`, `task`, `repo` | Chat logs, task files, attached repos, worktrees, diffs, branches |
| `edit` | The editor's text buffer: cursor, selection, undo, find. No UI |
| `gh` | Pull requests through the `gh` CLI: view, line comments (GraphQL), reply, resolve, create, failed logs |
| `catalog`, `usage` | Claude Code's commands, skills and agents; token and cost usage |
| `plan` | A task's plan: parse the checklist, change a step's mark |

`internal/ui` is one package (everything is a method on `Model`), split into files by area:

| Files | Area |
|---|---|
| `model.go`, `update.go`, `view.go` | The model, message handling and drawing |
| `layout.go` | Sizes, frames, the screen geometry and click hit-testing |
| `keys.go` | Shortcuts and slash commands, as registries (the help line is built from them) |
| `modal.go` | Modals: new task, delete, quit, status and model menus |
| `sidebar.go`, `tabs.go` | The task list; the Changes and Usage tabs |
| `overview*.go` | The main pane for a sidebar group row: a generic table (`overview.go`), built per kind of group (`overview_tasks.go`) |
| `chat_*.go` | The chat: drawing, `@`/`/` suggestions, sending messages (a message sent mid-turn stops the turn and goes next) |
| `files_*.go` | Files tab: the tree, the editor (keys and actions), the editor's drawing and mouse |
| `plan_tab.go` | Plan tab, `/plan`, and what every agent run gets (`--plugin-dir`, `--add-dir`) |
| `pr*.go` | PR tab: state and loading, the list, the popup, the actions (ask Claude, reply, resolve) |
| `shell*.go` | The embedded shells (pty + vt10x) and their mouse handling |
| `select.go`, `shortcuts.go` | Mouse selection per panel, links; the app's ⌘ shortcuts and `/keys` |
| `markdown.go` | Markdown for agent replies and GitHub comments |

Put new code in the file for its area; start a new `area_*.go` file when an area grows past ~600 lines.

## The app

crew ships only as the app; running it in a terminal isn't supported any more. `desktop/` is the Mac
window (Wails: the system web view with xterm.js) around the crew binary that `make app` builds and
bundles. It holds **no crew features**: it passes keys on (⌘ keys and Shift+Enter as escape
sequences), pastes, zooms and reads the login shell's environment. Everything else, ⌘ shortcuts
included, is decided in crew (`shortcuts.go`, bindable in `~/.crew/keys` with `/keys`), so a new
feature never needs the app to change.

## Skills

`plugin/` is a Claude Code plugin with crew's skills (`plugin/skills/<name>/SKILL.md`, named
`crew:<name>` in Claude). `make` bakes this folder's path into the binary and crew passes it to every
agent run with `--plugin-dir`, so skills stay in this repo, never in `~/.claude` or the user's
projects, and edits apply on the next message. Keep skills short and principle-led, like
`plugin/skills/plan`.

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
- **Mouse:** screen positions come from `layout.go` (`sideOuter`, `contentY`, `chatWidth`, …). Use them
  instead of new numbers.
- **The PR tab costs no tokens.** It reads GitHub with `gh`. Only sending something to the agent
  (ask, fix, fix all) does; keep it that way.
- **Outward actions** (push, create a PR, post a reply, resolve a thread) only happen on an explicit key
  from the user, and a PR is created only on a second `P`.
- **UI text** is short and plain: say what happens and the key for it, e.g. "⏎ posts it · esc cancel".

## Backlog

`docs/next-tasks.md` lists what's next. Update it when you finish or add an item.
