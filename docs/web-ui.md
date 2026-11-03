# crew: a web UI beside the terminal UI

**Status:** every section is in the web UI (the table says what each leaves out). The app opens the web UI by default. The page's "Use the terminal UI" button, and in the terminal `/ui web` or the ⌘⇧P palette's "Switch to the web UI", switch at once.

## Goal

A Slack-like UI in the same Mac window, while the terminal UI keeps working. Every feature ends up in
both, without writing its rules twice.

## Decisions

- **Same window, no browser.** The Wails window loads a page instead of xterm.js. The shell still holds
  no crew features.
- **One mode at a time, the web on every launch.** The app clears `~/.crew/ui` when it opens; while it's
  open the file says `web` or `terminal`; the app starts crew
  the matching way. To switch, the UI that's running writes the other one and quits (the terminal asks
  first, the page asks if agents are working), and the app, seeing crew quit with the file changed,
  starts the other one in the same window. Both modes take `~/.crew/lock`, so one crew runs per folder. Everything
  durable is already on disk (`tasks/`, `chats/`), so switching loses nothing but a running turn.
- **`crew serve`** is the web mode: the crew binary with no Bubble Tea, serving a JSON API, an event
  stream (SSE) and the frontend (`internal/ui/web/static`, embedded). It listens on `127.0.0.1` on a random port; every
  request needs a token the app was given at launch, and the Host header is checked. Nothing else on the
  Mac can drive agents.
- **Frontend with no build step:** plain ES modules with Preact and htm, vendored like xterm.js
  (`internal/ui/web/static/vendor`). No node, no bundler, so `make setup` stays as it is.
- **Layout:** `internal/core` (behavior, no screen), `internal/ui/terminal` and `internal/ui/web`, the
  two UIs on top of it. `core` imports neither.
- **Behavior lives in `internal/core`** (new, no drawing, no `tea`): the rules a UI must not decide,
  such as the budget check, the queue, auth failures and retry, auto-compact, and the worktree for a new
  task. It calls the services. The terminal UI and the web API are two views of it.

## Parity

The cost of two UIs is drift. These keep it small:

1. **Rules in `core` or a service, drawing in the UI.** If a rule is in `app/` or in JS, the other UI
   can't have it.
2. **One list of what exists:** the table below. A feature isn't done until its row is.
3. **Tests at the right level:** `core` and the API are tested in Go (`httptest`, no browser); the
   terminal UI keeps its smoke tests (`app/smoke_test.go`).
4. **Commands come from one place** (not done yet: the web chat has no slash commands). When it gets
   them, the page asks the API for the list (built from the same registries as the help line) and keeps
   none of its own.

| Section | Terminal | Web |
|---|---|---|
| Home (setup, sections on/off) | yes | yes; fixes show their command to copy |
| TASKS (list, chat, status, plan, changes) | yes | yes; no files or PR tab; one shell per task (⌘T); a board (a lane per status, drag to change it) only on the web |
| PULL REQUESTS | yes | yes; no merge, thread replies/resolve, Claude's review |
| REQUESTS | yes | yes; no tags, app rename/delete |
| REMINDERS | yes | yes; pause is indefinite |
| FOCUS | yes | yes |
| JIRA | yes | yes; sign-in shows the command |
| DOCKER, KUBERNETES | yes | yes |

## API (pilot)

| Call | Does |
|---|---|
| `GET /api/tasks` | the list, grouped by status: id, title, status (and how it reads), repo, branch, agent, model, working |
| `POST /api/tasks` | a new task (title, repo, description, agent, model, branch) |
| `GET /api/tasks/{id}/chat` | the chat's messages (`chat.Message`) |
| `POST /api/tasks/{id}/messages` | send; the budget check and queue-while-busy are `core`'s |
| `POST /api/tasks/{id}/stop` | stop the turn |
| `POST /api/tasks/{id}/shell`, `GET …/shell/out`, `POST …/shell/in`, `POST …/shell/size`, `DELETE …/shell` | the task's shell (`sections/tasks/shell.go`): start it in `core.Dir`, its output as SSE (base64), keys, size, hang up |
| `GET /api/events` | SSE: `task`, `message`, `turn`, `notice` |
| `GET/POST /api/ui` | which UI the app opens next time |

Outward actions (push, PR, post) stay on an explicit user action in the UI, as in the terminal UI;
the guard hook is unchanged.

## Adding a section to the web UI

Each section has a service, a Go package and a folder of its own, so sections are built without
touching each other:

1. **Its logic in a service** (`internal/<name>`): what the terminal section did that isn't drawing
   (reading, parsing, firing, its chat's lock-down), moved out of `ui/terminal/sections/<name>` so both
   UIs call it. The terminal section imports it.
2. **`internal/ui/web/sections/<key>/`:** a package whose `init` calls
   `api.Register(api.Module{Key, Routes, Run})`, plus a blank import of it in `web.go`. Routes go under
   `/api/<key>/` and answer with `api.Reply`, `api.Fail` and `api.Body`; `Run` is its background work
   while the web UI is open (it checks `c.SectionOn(key)`), and `c.Publish(core.Event{Kind: key, Data: …})`
   tells the page. Name the package after the section and import a service of the same name under a
   short alias (`task "crew/internal/tasks"`), as the terminal's sections do. Split a file past ~300
   lines (TASKS: `tasks.go`, `shell.go`). A section's chat: `c.RegisterChat(kind, core.ChatKind{Dir, Args})`
   and `c.FindChat(…)`; the page then uses the one chat (`Chat` in `lib/chat.js`, over
   `/api/tasks/<chat id>/…`).
3. **`static/sections/<key>/`:** `index.js` is the page (default export) and, optionally,
   `watch(setBadge)` for the rail's badge; add the key to `sections/index.js`. Its own styles are
   `style.css` beside it, loaded with `css(import.meta.url)`; bigger pages split into parts beside it
   (TASKS: `board.js`, `row.js`, `task.js`, `shell.js`, `common.js`). Build it from `lib.js`, which
   re-exports `lib/` (`api.js`: `api`, `on`, `useLoad`…; `markdown.js`; `chat.js`; `ui.js`: `Toolbar`,
   `Search`, `useListKeys`, `Form`, `Modal`, `ask`, `css`), and the classes in `styles/` (`layout.css`:
   `split`, `col`, `item`, `page`, `tabs`, `table.grid`), so every section moves the same way: ↑↓/jk
   move, ⏎ opens, esc closes, / filters, a letter for each action shown on its button. Colours come only
   from the tokens in `styles/theme.css` (Slack's palette, dark and light); a tint is `color-mix()` of a
   token, never a new hex. Shared by two sections only: a folder of its own with no `index.js`
   (`sections/containers/` for DOCKER and KUBERNETES).

Check a page renders with headless Chrome against `crew serve` with `?noevents` in the URL (the event
stream is off, so the page settles).

## Steps

1. [x] **Spike.** The window loads `http://127.0.0.1:<port>` and an SSE stream arrives live
   (`NSAllowsLocalNetworking` in `Info.plist`).
2. [x] **`internal/core`:** send, stop, events, the queue and the budget. The budget, plan note, agent
   arguments and worktree setup are shared with the terminal UI (`core/rules.go`).
3. [x] **`crew serve`:** the API, the token, the lock, `httptest` tests.
4. [x] **`internal/ui/web/static`:** the sidebar (tasks by status, a dot for a reply, one while working),
   the chat (a name and time per message, runs by one sender grouped, tool calls folded), the composer.
5. [x] **Switching both ways:** `/ui web` and a palette entry in the terminal, a button in the page.
6. [x] **Side pane** (like a Slack thread) for a run of tool calls and their results; light and dark
   follow the system.

## Left out of the pilot

- The agent's questions as buttons, slash commands (`/model`, `/plan`, `/budget`…), `@` file
  suggestions, auto-compact and retry after a sign-in. Each is a rule in the terminal UI's chat
  (`widgets/chat`) that `core` doesn't have yet.
- Cost, budget and plan progress in the list; diffs and the plan in the pane.
- The terminal's chat still runs its own turns. Moving it onto `core` is what ends the double rules.

## Open questions

- Should the terminal chat move fully onto `core` after the pilot? Recommended: yes, once step 4 works;
  it's the only way the two chats can't drift.
- Is the terminal UI kept for good, or retired after the web UI covers every section? Decide after the
  pilot; nothing here depends on the answer.
