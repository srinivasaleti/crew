# crew: a web UI beside the terminal UI

Status: **the pilot works for TASKS (list, chat, new task).** Open it with `/ui web` (in a task's chat) and restart, or `CREW_UI=web make run`. Try it with `CREW_UI=web make run` (or `web` in `~/.crew/ui`): the window should show a counter that goes up once a second. The pilot is TASKS only (the task list and an agent's chat).

## Goal

A Slack-like UI in the same Mac window, while the terminal UI keeps working. Every feature ends up in
both, without writing its rules twice.

## Decisions

- **Same window, no browser.** The Wails window loads a page instead of xterm.js. The shell still holds
  no crew features.
- **One mode per launch.** `~/.crew/ui` says `terminal` (default while piloting) or `web`; the app
  starts crew the matching way. Both modes take `~/.crew/lock`, so one crew runs per folder. Everything
  durable is already on disk (`tasks/`, `chats/`), so switching loses nothing but a running turn.
- **`crew serve`** is the web mode: the crew binary with no Bubble Tea, serving a JSON API, an event
  stream (SSE) and the frontend (`internal/web/static`, embedded). It listens on `127.0.0.1` on a random port; every
  request needs a token the app was given at launch, and the Host header is checked. Nothing else on the
  Mac can drive agents.
- **Frontend with no build step:** plain ES modules with Preact and htm, vendored like xterm.js
  (`internal/web/static/vendor`). No node, no bundler, so `make setup` stays as it is.
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
| TASKS (list, chat, new task) | yes | yes, except what's under Left out |
| TASKS (plan, changes, files, PR tabs, shells) | yes | no |
| PULL REQUESTS, REQUESTS, REMINDERS, FOCUS, DOCKER, KUBERNETES, JIRA | yes | no |

## API (pilot)

| Call | Does |
|---|---|
| `GET /api/tasks` | the list, grouped by status: id, title, status (and how it reads), repo, branch, agent, model, working |
| `POST /api/tasks` | a new task (title, repo, description, agent, model, branch) |
| `GET /api/tasks/{id}/chat` | the chat's messages (`chat.Message`) |
| `POST /api/tasks/{id}/messages` | send; the budget check and queue-while-busy are `core`'s |
| `POST /api/tasks/{id}/stop` | stop the turn |
| `GET /api/events` | SSE: `task`, `message`, `turn`, `notice` |
| `GET/POST /api/ui` | which UI the app opens next time |

Outward actions (push, PR, post) stay on an explicit user action in the UI, as in the terminal UI;
the guard hook is unchanged.

## Steps

1. [x] **Spike.** The window loads `http://127.0.0.1:<port>` and an SSE stream arrives live
   (`NSAllowsLocalNetworking` in `Info.plist`).
2. [x] **`internal/core`:** send, stop, events, the queue and the budget. The budget, plan note, agent
   arguments and worktree setup are shared with the terminal UI (`core/rules.go`).
3. [x] **`crew serve`:** the API, the token, the lock, `httptest` tests.
4. [x] **`internal/web/static`:** the sidebar (tasks by status, a dot for a reply, one while working),
   the chat (a name and time per message, runs by one sender grouped, tool calls folded), the composer.
5. [x] **`/ui web|terminal`** and a button in the page; the window loads the page in web mode.
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
