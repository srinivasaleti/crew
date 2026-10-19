# crew

crew is a developer workspace for your Mac, in one window you drive from the keyboard. It brings the
work around your code together:

- **TASKS**: coding agents (Claude Code or Gemini CLI) working on tasks in parallel, each in its own
  git worktree and branch, with a chat, a plan, an editor, the diff, the pull request and the cost.
- **PULL REQUESTS**: the PRs waiting for your review and your own, across every repo your GitHub
  account can see. Read them, chat about them, and have Claude draft a review you post as you.
- **REQUESTS**: an HTTP client for the APIs you work on, with environments, keys kept in the macOS
  Keychain, and a Claude that writes and sends requests without ever seeing a key.
- **REMINDERS**: reminders that fire once or repeat (every day, weekday, week, month…), with a
  Claude chat that sets, moves and pauses them for you.
- **FOCUS**: a focus timer (pomodoro or flow), move and eye breaks that feel like breaks, and
  your screen time across the whole Mac, with a daily limit and an end of day.
- **JIRA**: the Jira issues assigned to you that aren't done, grouped by space. ⏎ opens an issue's
  card: every detail, the description and the comments, where you can write a comment or have
  Claude draft one (it goes to Jira only on your ⏎). `n` works on it in a crew task, so Claude gets the
  issue and a worktree. It uses Atlassian's CLI (`acli`), signed in through your browser.

Around them: shells, a file finder and editor, a command palette, notifications, and setup checks.
Turn on only the sections you use (home: `t`, `p`, `r`, `m`, `f`).

crew is built on the CLIs you already have: `claude` (or `gemini`) for the agent work, `gh` for
GitHub, `git` for worktrees. Nothing is hosted: your tasks, chats and requests stay on your Mac, and
browsing (PRs, diffs, files, requests) costs no tokens. Only what you send to an agent does.

## Setup

You need:

- macOS, with Apple's command line tools (`make setup` asks to install them)
- Go 1.25 or newer, git, and make
- [Claude Code](https://docs.claude.com/en/docs/claude-code) (`claude`), signed in: the agent for
  tasks, PR chats and reviews, and the REQUESTS chat
- the GitHub CLI (`gh`), signed in with `gh auth login`: for PULL REQUESTS and a task's PR tab
- Atlassian's CLI (`acli`), signed in with `acli jira auth login --web`: for JIRA (`make install-app`
  installs it with curl into `~/.local/bin`; crew signs it in from JIRA or Setup)
  (`make setup` installs it with Homebrew)
- optional: [Gemini CLI](https://github.com/google-gemini/gemini-cli) (`npm install -g @google/gemini-cli`),
  to give tasks to Gemini instead of Claude

Each section needs only its own tools: REQUESTS works with none of them (its chat needs Claude), and
PULL REQUESTS without an agent (reviews and its chat need Claude).

```bash
git clone git@github.com:srinivasaleti/crew.git
cd crew
make setup
make install-app     # builds crew.app and puts it in /Applications
```

Open **crew** from Spotlight or Applications. Home (`⌘⇧H`) shows whether Claude Code, Gemini CLI,
the GitHub CLI and git are installed and signed in (**Get started** until you have a task, then
**Setup**), and the sections, each on or off. `d` opens Setup, where `⏎` on a tool installs it or
signs in, in a shell panel. `make doctor` (or `crew doctor`) runs the same checks in a terminal.

`make install-app` again after pulling changes updates the app; `make uninstall-app` removes it, and
`make run` builds it and opens it without installing. The app reads your login shell's environment,
so it finds `claude`, `gemini`, `gh` and `git` as your terminal does. It's signed only for this Mac;
sharing it needs signing with an Apple Developer ID.

### Signing in again

crew checks the CLIs when it starts and when its window comes back to the front, with their own
status commands (no tokens). If an agent's login expires or you're signed out, the help line says so
(**Claude is signed out · ⌘L signs in**), and a message that failed because of it says so in the
chat.

`⌘L` (or `/login`) signs in again in a shell panel: `claude auth login` opens the browser; for
Gemini it opens `gemini`, where you pick *Login with Google*, then `/quit`. When it closes, crew
checks again, and `⏎` in an empty message box sends the message that failed, in the same session. A
signed-out GitHub CLI gets the same banner, and `⌘L` runs `gh auth login`. If you use an API key
(`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`) and it's rejected, crew tells you to set a new one instead.

## TASKS: agents on tasks in parallel

Press `n` in TASKS, name a task, pick any git repo on your machine and the agent (Claude or Gemini),
and send a message. The first message creates the worktree and starts the agent.

- **In parallel, isolated.** Each task runs in its own worktree (`<repo>.worktrees/<task>`) on its own
  branch, so agents never step on each other or on your checkout.
- **Or on a branch you already have.** The New task form's **Branch** field takes an existing branch
  (`feat/billing`) or a PR (`#1330`). Git won't check one branch out twice, so if that branch is
  already in a worktree the task works there; otherwise crew checks it out into
  `<repo>.worktrees/<branch>`. For a task that's already started, `/branch <branch or #PR>` moves it,
  and Files, Changes, its PR and `@` suggestions follow. Claude Code keeps a conversation per folder,
  so the agent starts a fresh one there and is told the task again. `/branch cleanup` then removes
  the worktree crew first made for the task, if nothing happened in it.
- **A chat per task.** Messages go to the agent headless; replies stream in as they're written and
  render as Markdown. While it works you see each tool call in full with the start of its output
  (a subagent's steps nested under it); afterwards they fold into one line, and ⌘R shows them again.
  A message you send while the agent works stops its current step and goes straight in, so you can
  steer it (it keeps what it had done). `esc` or `⌘X` just stops a turn.
- **An overview of everything.** The TASKS row opens a table of all tasks, the ones that need you
  first (a reply is ready, CI failed, a reviewer asked for changes), with status, agent state, PR,
  what's left to do, cost and last activity. Filter by chip (Needs you, Working, each status,
  Archived), by repo, or search; it pages to fit the window.
- **Split a task into subtasks that run in parallel.** `/split <goal>` has Claude propose subtasks
  with crew's `split` skill: ones that run at once never touch the same files, and shared groundwork
  comes first (`After: 1`). Claude only proposes, and a hook stops agents from writing crew's task
  files. The Plan tab shows the proposal: `space` leaves a subtask out, `e` edits it, `x` discards
  it, and `⏎` twice applies it. crew then makes a task per subtask, each branching from this task's
  branch in its own worktree, and starts the ones that wait on nothing. TASKS lists subtasks under
  their task (`2/4 subtasks`), and `m` on a finished one (or `/merge` in it) merges its branch into
  the parent's. That's local, nothing is pushed. A clash undoes the merge and sets the subtask to
  Blocked. A clean merge marks it Done and starts the subtasks that waited on it. The parent opens
  one PR as usual.
- **A plan for bigger tasks.** `/plan <goal>` has Claude read the code and write a short checklist
  plan with crew's `plan` skill (YAGNI, KISS, DRY; 3–10 verifiable steps). The Plan tab shows the
  steps and progress; Claude marks steps `[~]` doing and `[x]` done as it works, and you can send it
  a step (`s`), the next one (`n`), change a mark (`x`), or edit the plan (`e`).
  When a choice is yours (an approach, a trade-off, a suggestion beyond the goal), Claude asks in
  the plan with options and a recommendation; they show as **Claude asks** above the steps, and `⏎`
  answers one (a letter picks an option). Claude then updates the plan.
- **Files and an editor.** A file tree with git change marks (`M`, `A`, `U`), a finder (`⌘P`),
  and a built-in editor that works like a normal one: type to edit, `⌘S` saves, `⌘Z` undoes,
  shift+arrows and the mouse select, `⌘F`/`⌘R` find and replace, plus VS Code-style line
  keys. `⌘E` opens the file in VS Code.
- **Changes.** The task's full diff against where its branch started, committed or not.
- **Its pull request.** Checks, reviews and comments, grouped by reviewer, with what's still to do
  marked. Open a comment to see it as Markdown with the code it's on; ask the agent about it, have
  it fix it, reply in the thread, or resolve it, without leaving crew. `P` pushes the branch and
  opens the PR (on a second `P`).
- **Usage.** Cost, tokens, time, lines changed and context size per task and per turn, read from
  Claude Code's own logs.

### Gemini

A task can use Gemini instead of Claude: in the New task form, the **Agent** row (`←`/`→`) picks
Claude or Gemini. crew runs [Gemini CLI](https://github.com/google-gemini/gemini-cli) headless
(`gemini --output-format stream-json`), one run per message, resuming the task's session like it
does for Claude. Gemini runs with `--approval-mode yolo` (it approves its own tool calls) and can
read and write the task's plan folder.

What works with Gemini: the chat, its tool calls and their output, resuming, `/new`, `/model`
(Default, Pro, Flash, Flash Lite), stopping a turn, signing in again, and everything that isn't the
agent (Files, Changes, PR, shells). Not yet: the Usage tab and costs (crew reads them from Claude
Code's logs), `/compact` (Gemini compresses on its own), `/plan` and Claude Code's own commands and
skills.

## PULL REQUESTS: review and follow your PRs

The PULL REQUESTS section lists the open PRs where your review is requested, and your own open PRs,
in any repo your GitHub account can see. It reads GitHub with `gh`, so browsing costs no tokens.

- **Conversation.** Checks, reviews and comments grouped by reviewer, the same view as a task's PR
  tab: open a comment as Markdown with its code, reply, or resolve the thread.
- **Diff.** The PR's changes.
- **Review.** `v` has Claude review it (in a read-only checkout when the repo is on your machine,
  else from the diff) into a summary, risks and comments on lines. Keep, drop or edit each, pick
  approve, comment or request changes, and post it as you (⏎ twice).
- **Chat.** `c` in the list opens a chat with Claude about that PR, with its own session, like a
  task's chat (`/compact`, `/new`, `/model`, usage). Claude works in a checkout of the PR when the
  repo is attached, else reads it with `gh`. `c` on a comment and `a` on all feedback ask in that
  chat too.

Nothing goes to GitHub until you press the key for it: replying, resolving and posting a review are
always yours.

## REQUESTS: an API client

REQUESTS is an HTTP client for the APIs you work on, kept as plain `.http` files.

- **Applications and folders.** `n` adds an application, each with a Requests and an Environments tab
  (`tab` switches). Requests are grouped in folders (`f` adds one, ⏎ folds it, `m` moves a request),
  A to Z, and crew remembers what you fold. Every page has a row of buttons for what you can do
  there (`+ Request`, `+ Folder`, `Delete folder`…); deleting asks first.
- **Environments.** Dev, staging, prod: each has a base URL, an auth method (Bearer token or API key
  header) and its key, kept in the macOS Keychain, never in a file. `E` switches the application's
  environment, which changes every request's URL and key.
- **Requests.** A request opens in an editor laid out like Postman (`n` for a new one, ⏎ on one): its
  name and environment, the method and URL with where it goes, then tabs (`⌘1` `⌘2` `⌘3`, ←/→ on them, or a click):
  - **Params**: the URL's query as Key/Value rows (in step with the URL), and its Path Variables,
    every `{{name}}` it uses, whose values are kept in the environment for every request.
  - **Headers**: one row per header; space turns one off (kept in the file as `# Name: value`). The
    environment's auth header is added on its own.
  - **Body**: JSON with line numbers, checked as you type; `⌘B` beautifies it.

  Typing in a table's empty last row adds a row; clearing one removes it. Send (`⌘R`) shows the
  response below, with its Body and Headers; Save (`⌘S`), Delete and Close work with the keys or the
  mouse.
- **A chat that never sees a key.** `c` has Claude write and send requests: crew fills keys in only
  when it sends, only to the environment's own hosts, hides them in what comes back, and runs that
  Claude with nothing but the requests folder and `crew req`.
- **From a terminal.** `crew req apps` lists applications and environments (key names only),
  `crew req list` the requests, and `crew req run <app>/<file>[#n] [--env <name>]` sends one.

## REMINDERS: once or repeating

REMINDERS has two tabs.

- **List** (`⌘1`): every reminder, the next to fire first, with its time, how it repeats and what
  it says. Chips filter Today, Repeating, Paused, Done and Deleted; `/` searches. `n` adds one and
  `⏎` edits one in a small form: what, when (`4pm`, `30m`, `tomorrow 9am`, `fri 5pm`, `nov 1 10am`),
  and how it repeats (never, every day, weekday, week, month, year, or custom: `2 weeks`,
  `mon,wed,fri`, `3 months`), with when it fires next as you type. `p` pauses or resumes one, `x`
  deletes it (to Deleted, where `u` restores it).
- **Chat** (`⌘2`, or `c` in the list): tell Claude what you want, like "stand-up every weekday at
  4pm", "move the timesheet one to Fridays 5:30", "pause everything next week, I'm on leave", or
  "what's coming up tomorrow?". Claude changes your reminders with `crew remind` and nothing else
  (no files, no other commands), and says exactly what it changed. `/compact`, `/new` and `/model`
  work as in a task.

When one is due, crew plays a sound (Glass; `CREW_REMINDER_SOUND` picks another macOS sound, or
`off`), you get a macOS notification, and the help line shows it for 10 minutes, so it can't slip
by. A repeating reminder then
moves to its next time; a one-time one goes to Done. One that came due while crew was closed fires
once when crew opens. Reminders are kept in `~/.crew/reminders/reminders.json`.

From a terminal: `crew remind list`, `crew remind add --at 4pm --every weekday stand-up`,
`crew remind edit 3 --at "fri 5:30pm"`, `crew remind delete 3`, `crew remind pause all --until "mon 9am"`.

## FOCUS: pace your day

FOCUS keeps you working in good stretches and resting in between. It has nothing to do with tasks
or agents, it's all local and costs no tokens. The plan behind it is in [docs/focus-prd.md](docs/focus-prd.md).

- **Focus timer.** `⌘⇧F` from anywhere (or `s` on the FOCUS page, `/focus`, `/focus 50`) starts a
  session; `⌘⇧F` again stops it. In **pomodoro** mode it counts down 25 minutes, then a 5-minute
  break starts, and every 4th session a 15-minute one. In **flow** mode it counts up, and when you
  stop it a break of a fifth of what you worked starts (at least a minute). The help line shows
  `● focus 18:42` wherever you are, and the sidebar's FOCUS row shows the timer. The timer keeps
  going if you quit crew. Opening crew starts a session (settings: "Start when crew opens").
- **More time.** Two minutes before a session ends, a notification and the help line say so;
  `⌘⇧E` from anywhere adds 15 minutes. With time's up on screen, `1` `2` `3` keep going for 5, 15 or
  25 more minutes. Twice per session at most, so a session can't quietly run for hours.
- **Breaks.** A move break every 30 minutes of activity, 3 minutes long (it waits for a focus
  session to end): research finds a few minutes of movement every half hour helps, and hourly
  doesn't. Every 20 minutes the help line quietly asks you to look 20 feet away for 20 seconds
  (no notification). Being away from the Mac as long as the break counts as taking it.
- **The break screen.** When a focus session or a move break's time is up, crew's window comes to the
  front, filling the screen above every app, wherever you are, and goes in three steps:
  1. **Time's up**: "25m of focus done · time for a 5m break". `⏎` starts the break; `1` `2` `3`
     keep going 5, 15 or 25 more minutes (twice at most). Unanswered, the break starts on its own
     after 30 seconds.
  2. **The break** counts down with what to do (`◐  Short break  4:12`). Ending it early is `e`, then
     `y` to confirm.
  3. **Break done**: `⏎` starts the next focus session and the window goes back as it was; `esc` is
     "not yet". Otherwise the next session starts on its own 15 seconds after you're back at the Mac.

  "Auto start" in settings (on by default) is what starts the break and the next session on their
  own; off, each waits for you.

  Settings' Break screen: `full` (the above), `crew` (the same steps inside crew's window only) or
  `off` (a notification, and the break starts on its own). macOS doesn't let an app lock you out, so
  ⌘Tab and Force Quit still work.
- **Screen time.** crew counts the time you're active on the whole Mac (any app), from macOS's idle
  time: a minute without the keyboard or mouse and you're away. With a daily limit (6 hours by
  default), a notification and the help line warn you at 80% and at 100%; nothing is locked. An end
  of day (e.g. `19:00`) tells you to wrap up. The FOCUS page shows what's next, **Today** (active
  time, sessions, breaks taken and skipped) and **This week**.

The FOCUS page's keys: `s` start or stop, `b` a break now, `+`/`−` add or take off 5 minutes, `e`
settings. Settings are a form (`⌘S` saves): the timer mode and lengths, move and eye breaks, the
break screen, the daily limit, the end of day, and Do Not Disturb. For Do Not Disturb during a
session, make two Shortcuts (one that sets Focus on, one that turns it off) and name them there.
Settings are kept in `~/.crew/focus/settings.json`; until you save them, the `CREW_FOCUS_*`
variables below are the defaults.

## Everywhere

- **Shells.** Up to four shells next to the main pane, in the task's worktree (or your home folder
  from home). The wheel scrolls back through their output. `⌘T` opens one.
- **The command palette.** `⌘⇧P` lists every command and shortcut; type to find one, ⏎ runs it.
- **Notifications.** A macOS notification, with a sound, when a reminder is due, an agent finishes
  or a PR changes (a failing check, a review, new comments, merged) while you're looking elsewhere.
  They come from **crew** itself: the first time, macOS asks whether crew may send them. Setup
  (`d` on home) shows whether they're allowed; if they're off, the help line says so, and `⌘L` (or
  `⏎` on Notifications in Setup) asks macOS again, or opens System Settings → Notifications, where
  you turn crew on. `⏎` on it when they're allowed sends a test. Outside the app (`crew doctor`), they
  go through `osascript`, which macOS files under Script Editor. If macOS refuses one, the app sends
  it through `osascript` instead, Setup shows ✗ with the reason, and `~/.crew/notify/log` lists the
  last 50 sends. Allowed but still nothing? In System Settings → Notifications → crew, check the
  alert style isn't **None**, and that no Focus mode is on.
- **Sections.** Home turns TASKS, PULL REQUESTS, REQUESTS, REMINDERS and FOCUS on or off (`t`, `p`,
  `r`, `m`, `f`).
  A section that's off has no row in the sidebar and does no background work (PULL REQUESTS stops
  polling GitHub, FOCUS stops counting and nudging).

## Using it

The window has three areas: the sidebar on the left (home and the sections), the main pane in the
middle (with tabs), and shells on the right when you open some. The area with the keys has a violet border, its selection is
a highlighted bar, and the badge at the bottom left names it.

| Keys | What they do |
|---|---|
| `⌘⌥←` `⌘⌥→` `⌘⌥↑` `⌘⌥↓` | Move between the task list, the main pane and the shells (`⇧`+arrows too) |
| `⌘⇧H` | Home: setup, and turning sections on or off (`t` TASKS, `p` PULL REQUESTS, `r` REQUESTS, `m` REMINDERS, `f` FOCUS, `j` JIRA, or click one) |
| `⌘⇧F` | Start a focus session, or stop the one running (FOCUS) |
| `⌘⇧E` | Add 15 minutes to the focus session running, twice at most (FOCUS) |
| `⌘⌥T` | Next panel: the task list, the main pane, then each shell (`⌘[` / `⌘]` previous / next) |
| `tab` / `⇧tab` | Next / previous tab: a task's tabs, a PR's panes, REQUESTS' tabs, a list's filters |
| `↑` `↓` | Move between rows: the sidebar, a list, REQUESTS, the Files tree, the PR tab |
| `←` `→` | Fold / unfold folders and groups; `←` on a row goes up to its folder or group |
| `⏎` / `esc` | Go into the task, PR, request or file / come back out |
| `⌘1` … `⌘6` | Jump to that tab; every tab bar shows its numbers. A task: Chat, Plan, PR, Files, Changes, Usage. A PR: Conversation, Chat, Diff, Review. An application: Requests, Environments (`⌘⇧[` / `⌘⇧]` previous / next) |
| `n` (in TASKS) | New task |
| `⌘T` or `⌘D` | New shell in the task's worktree |
| `⌘W` | Close the shell you're in, or the file open in the editor |
| `⌘K` | Clear the shell you're in |
| `⌘F` | Find in the editor, or search the tasks or PRs |
| `⌘↑` or `esc` | Back to the list: a task to all tasks, a PR to all PRs |
| `⌘⇧↑` / `⌘⇧↓` | Previous / next task or PR, in the list's order and filter |
| `⌘P` | Find a file in the task |
| `⌘C` / `⌘V` | Copy your selection / paste |
| `⌘S` | Save in the editor; elsewhere, the status menu (todo, in progress, in PR, blocked, done, archive) |
| `⌘O` | Archived tasks (in the TASKS overview) |
| `⌘X` or `esc` | Stop the agent's turn |
| `⌘R` | Show every tool call in the chat |
| `⌘⇧P` or `⌘/` | Every command and shortcut: type to find one, ⏎ runs it |
| `⌘⇧↵` | Maximise the panel you're in (hides the task list); again to restore |
| `⌘⌥⇧←` / `⌘⌥⇧→` | Shells wider / narrower (or drag the border) |
| drag | The task list's right border resizes it |
| `⌘` `+` `−` `0` | Zoom |
| `⌘⌫` | Delete a task (its worktree is kept); `x` on its row in TASKS |
| `⌘L` | Fix what the help line says is missing: sign in to Claude or GitHub, or open Setup |
| `⌘Q` | Quit, after asking (`y` or `⌘Q` again); the Dock's Quit and the window's close button ask too |

Every shortcut is a ⌘ key. A ⌘ letter not listed does what crew's ctrl key for it does (in the
editor, the editor's: `⌘D` selects the next match, `⌘K` deletes the line, `⌘/` comments). **To change the ⌘ shortcuts**, type `/keys` in
a task's chat: it opens `~/.crew/keys` (one `keys = action` per line, every action listed at the top)
in the editor; save it and the change works on the next key press.

In the message box: `⏎` sends, `⇧⏎` adds a line, `@` suggests files, `/` suggests commands (crew's and
your Claude Code commands, skills and agents). Sending while Claude works stops its current step and
sends your message right away.

**Selecting and links.** Drag over text to select it, inside one panel at a time (the task list, the
chat or a tab, a shell), as if each were its own window; it's copied when you let go, and ⌘C copies
it again. Click a link to open it. The editor has its own selection (drag or shift+arrows, then ⌘C).

crew's own commands, in any chat:

| Command | What it does |
|---|---|
| `/model` | Pick the chat's model (Claude's or Gemini's, or an id) |
| `/plan [goal]` | Have Claude write the task's plan (without a goal, it plans the task's description) |
| `/split [goal]` | Have Claude propose subtasks for agents to work on in parallel (Plan tab); `⏎` twice there applies it |
| `/merge` | In a subtask: merge its branch into its parent's (local) |
| `/self-review` | Have Claude review the task's change before a PR: numbered findings, no code changed |
| `/fix-review [note]` | Have the agent fix the review feedback on the task's PR: failing checks, open comments and change requests (as `a` on the PR tab); a note goes after it. Where a comment needs no change, the agent drafts a reply instead: the PR tab marks it ✎, `D` twice posts every draft, `r` on one edits it first. To fix a self-review's findings, reply "fix all" or "fix 1 3" |
| `/compact` | Summarise the conversation so later messages re-send far less |
| `/new [message]` | Start a fresh Claude session in the same worktree (see below) |
| `/shell` | Open a shell in the task's worktree |
| `/branch [branch \| #PR \| cleanup]` | Where the task works; move it to an existing branch or PR's worktree; remove the unused worktree crew made |
| `/focus [minutes]` | Start a focus session, or stop it (FOCUS) |
| `/doctor` | Setup: check Claude Code, Gemini CLI, the GitHub CLI and git, and install or sign in |
| `/login` | Sign in to the task's agent again, in a shell panel |
| `/keys` | Change the ⌘ shortcuts |

Each tab and popup lists its own keys on the bottom line.

## Tokens and context

This is about agent chats: tasks, PR chats, and the REQUESTS and REMINDERS chats. Everything else in crew costs
no tokens.

**What is sent, and when.** Opening a task, switching tabs, browsing the overview, the PR tab and the
file tree never talk to Claude. Nothing is sent while you type. When you send a message, crew runs
`claude -p --resume <session>` with only your new message (the first message also carries the task's
title and description).

Inside Claude Code, the model has no memory between calls, so each turn re-sends the conversation so
far (your messages, its replies, tool calls, files it read). Prompt caching makes the unchanged part
cheap, but the cost of a turn still grows with the length of the task's conversation. The header's
`ctx` figure is that size.

**How crew keeps it small:**

- **Warnings.** `ctx` turns amber from 100k tokens and red from 150k, with a hint to `/compact`.
- **`/compact`** asks Claude Code to summarise the conversation; later turns re-send the summary.
- **Auto-compact (optional).** Set `CREW_AUTO_COMPACT=120000` (any token count) and crew runs
  `/compact` before your next message once the context reaches it. Your message goes right
  after the compaction.
- **`/new`** starts a new session for the same task: same worktree, branch and files, but Claude
  starts without the earlier conversation (it gets the task description again). Use it when a task
  moves on, for example from building a feature to working through review comments. Cost and usage
  keep counting every session.
- **Slim PR feedback.** When you ask Claude about a PR comment, or to fix one or all of them, crew
  sends the comment cleaned of GitHub's HTML and without bots' `<details>` blocks (configs,
  summaries, and the "Prompt for AI agents" copy of the comment), plus the code the comment is on.
  In the chat, that quoted feedback is folded to its first lines (`⌘R` shows it all).
- **Plans survive new sessions.** After `/new`, Claude is pointed at the task's plan, so it picks up
  at the current step with a small context.
- **The PR tab is free.** It reads GitHub with `gh` on your machine; only sending something to the
  agent costs tokens.

## Settings

| Variable | Default | What it does |
|---|---|---|
| `CREW_HOME` | `~/.crew` | Where crew keeps tasks, chats, requests and the repo list. One crew runs per folder; a different `CREW_HOME` runs a separate crew |
| `CREW_AUTO_COMPACT` | off | Context size (tokens) from which crew compacts before sending |
| `CREW_ARCHIVE_DAYS` | `7` | Archive done tasks untouched this many days (`0` turns it off) |
| `CREW_NOTIFY` | on | `0` turns off notifications (an agent finished, CI failed, a review, new comments, merged) |
| `CREW_FOCUS_MIN` / `CREW_BREAK_MIN` | `25` / `5` | A focus session's and its break's length, in minutes (a default until FOCUS's settings are saved with `e`) |
| `CREW_REMINDER_SOUND` | `Glass` | The sound a reminder plays (any in `/System/Library/Sounds`), or `off` |
| `CREW_EYE_BREAKS` | `20` | Minutes between eye breaks (`0` turns them off; a default, as above) |
| `CREW_SIT_MIN` | `30` | Minutes between move breaks (`0` turns them off; a default, as above) |
| `CREW_FOCUS_ON` / `CREW_FOCUS_OFF` | none | Shortcuts crew runs when a focus session starts and ends (e.g. Do Not Disturb; a default, as above) |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Where Claude Code's commands, skills and session logs are read from |

Set them in your shell's profile (e.g. `~/.zshrc`); the app reads your login shell's environment.

Agents approve their own tool calls, since no one is there to answer prompts in headless mode:
Claude Code runs with `--permission-mode auto`, Gemini CLI with `--approval-mode yolo`. The REQUESTS
chat is the exception: its Claude is locked down to the requests folder and `crew req`.

## Where things live

- `~/.crew/tasks/<id>.md`: each task, as a small header plus its description
- `~/.crew/chats/<id>.jsonl`: each task's chat (and each PR chat's and the REQUESTS chat's)
- `~/.crew/plans/<id>.md`: each task's plan (agents get access to this folder)
- `~/.crew/splits/<id>.md`: a proposed split into subtasks, until you apply or discard it (agents get access to this folder)
- `~/.crew/repos`: the repos you've used, one path per line
- `~/.crew/requests/<app>/`: REQUESTS: `.http` requests in folders and `env/<env>.env` environments
  (key names only; the keys are in the macOS Keychain)
- `~/.crew/prchats/<id>`: where a PR chat runs when the PR's repo isn't on this machine
- `~/.crew/sections`: the sections you turned off
- `~/.crew/notify/`: notifications on their way from crew to its app, and `status` (whether macOS allows them)
- `~/.crew/focus/`: FOCUS: `clock.json` (the timer) and `days/<date>.json` (screen time)
- `~/.crew/reminders/`: REMINDERS: `reminders.json`, and where its chat runs
- `~/.crew/keys`: your ⌘ shortcuts, if you changed any (`/keys`)
- `<repo>.worktrees/<id>`: each task's worktree, on branch `<id>` (you can switch it, e.g. to `feat/…`)
- Claude Code's session logs under `~/.claude/projects/`, read for usage

Deleting a task removes its file, chat and plan but keeps its worktree and branch.

## Skills

crew gives the agents it runs its own Claude Code skills, from the [`plugin/`](plugin) folder of this
repo, loaded with `claude --plugin-dir` on every run. They're crew's only: nothing goes into
`~/.claude` or into your projects, and your own Claude Code sessions don't see them. In Claude they're
named `crew:<skill>`.

| Skill | What it does |
|---|---|
| `crew:plan` | Writes and follows a task's plan (`plugin/skills/plan/SKILL.md`) |
| `crew:split` | Proposes splitting a task into parallel subtasks, for you to apply (`plugin/skills/split/SKILL.md`). A hook (`plugin/hooks/`) stops agents from writing crew's task files themselves |
| `crew:self-review` | Reviews a task's own change before its PR (`/self-review`), then fixes the findings simply when you ask ("fix all", "fix 1 3"), asking before anything complex (`plugin/skills/self-review/SKILL.md`) |
| `crew:review` | Reviews a pull request into a summary, risks and line comments (`plugin/skills/review/SKILL.md`) |
| `crew:requests` | Makes and sends HTTP requests in REQUESTS without touching a key (`plugin/skills/requests/SKILL.md`) |

The build records where this checkout is, and crew reads the skills from it on each run, so editing a
skill or adding one (`plugin/skills/<name>/SKILL.md`) needs no rebuild. If the checkout moves, run
`make install-app` again.

## Development

```bash
make run       # build the app and open it (crew.app in this folder)
make build     # just the crew binary the app runs
```

The app (`desktop/`) is only a window around the crew binary; every feature lives in crew itself.

The code is in `internal/`, in layers: `app` (crew's window, home and a task's workspace) uses the
`sections` (TASKS, PULL REQUESTS, REQUESTS, REMINDERS, FOCUS, JIRA, each a plugin of its own), which
use the `widgets` (chat, PR view, files, shells, tables), which use the services (agents, tasks, git,
gh…). [CLAUDE.md](CLAUDE.md) describes the layout and the practices to follow, and
[docs/next-tasks.md](docs/next-tasks.md) is the backlog.
