# crew

crew is a Mac app for running several coding agents at once, each on its own task. Every task
gets its own git worktree and branch, a chat with Claude Code, an editor, its pull request and its
cost, all in one window. You start tasks, let the agents work in parallel, and step in where one
needs you.

crew is a harness around the `claude` CLI: the agent work happens in Claude Code, and crew handles
the tasks, worktrees, review and bookkeeping around it.

## What it offers

- **Tasks in parallel.** Each task runs in its own worktree (`<repo>.worktrees/<task>`) on its own
  branch, so agents never step on each other or on your checkout.
- **A chat per task.** Messages go to Claude Code headless; replies stream in as they're written and
  render as Markdown. While Claude works you see each tool call in full with the start of its output
  (a subagent's steps nested under it); afterwards they fold into one line, and ⌘R shows them again. A message you send while the agent works
  stops its current step and goes straight in, so you can steer it (Claude keeps what it had done).
  `esc` or `ctrl+x` just stops a turn.
- **An overview of everything.** The TASKS row opens a table of all tasks, the ones that need you
  first (a reply is ready, CI failed, a reviewer asked for changes), with status, agent state, PR,
  what's left to do, cost and last activity. Filter by chip (Needs you, Working, each status,
  Archived), by repo, or search; it pages to fit the window.
- **A plan for bigger tasks.** `/plan <goal>` has Claude read the code and write a short checklist
  plan with crew's `plan` skill (YAGNI, KISS, DRY; 3–10 verifiable steps). The Plan tab shows the
  steps and progress; Claude marks steps `[~]` doing and `[x]` done as it works, and you can send it
  a step (`s`), the next one (`n`), change a mark (`x`), or edit the plan (`e`).
  When a choice is yours (an approach, a trade-off, a suggestion beyond the goal), Claude asks in
  the plan with options and a recommendation; they show as **Claude asks** above the steps, and `⏎`
  answers one (a letter picks an option). Claude then updates the plan.
- **Files and an editor.** A file tree with git change marks (`M`, `A`, `U`), a finder (`ctrl+p`),
  and a built-in editor that works like a normal one: type to edit, `ctrl+s` saves, `ctrl+z` undoes,
  shift+arrows and the mouse select, `ctrl+f`/`ctrl+r` find and replace, plus VS Code-style line
  keys. `ctrl+e` opens the file in VS Code.
- **Changes.** The task's full diff against where its branch started, committed or not.
- **The pull request.** Checks, reviews and comments, grouped by reviewer, with what's still to do
  marked. Open a comment to see it as Markdown with the code it's on; ask Claude about it, have
  Claude fix it, reply in the thread, or resolve it, without leaving crew. `P` pushes the branch and
  opens the PR.
- **Pull requests to review, and yours.** A PULL REQUESTS section lists the open PRs where your
  review is requested from you, and your own open PRs, in any repo your GitHub account can see (no
  tokens). Open one for its conversation (the same view as a task's PR tab), its diff, or a review:
  `v` has Claude review it (with a read-only checkout when the repo is on your machine, else from the
  diff) into a summary, risks and comments on lines; keep, drop or edit each, pick approve, comment
  or request changes, and post it as you (⏎ twice).
- **Usage.** Cost, tokens, time, lines changed and context size per task and per turn, read from
  Claude Code's own logs.
- **Shells.** Up to four shells per task, in the task's worktree, next to the chat (or in your home
  folder from the home screen). The wheel scrolls back through their output.
- **Notifications.** A macOS notification when Claude finishes or a task's PR changes (a failing
  check, a review, new comments, merged) while you're looking elsewhere.

## Setup

You need:

- macOS, with Apple's command line tools (`make setup` asks to install them)
- Go 1.25 or newer, git, and make
- [Claude Code](https://docs.claude.com/en/docs/claude-code) (`claude`), signed in
- the GitHub CLI (`gh`), signed in with `gh auth login`, for the PR tab (`make setup` installs it with
  Homebrew)

```bash
git clone git@github.com:srinivasaleti/crew.git
cd crew
make setup
make install-app     # builds crew.app and puts it in /Applications
```

Open **crew** from Spotlight or Applications. Press ⌘N, name a task, pick any git repo on your
machine, and send a message. The first message creates the worktree and starts the agent.

`make install-app` again after pulling changes updates the app; `make uninstall-app` removes it, and
`make run` builds it and opens it without installing.

The app reads your login shell's environment, so it finds `claude`, `gh` and `git` as your terminal
does. It's signed only for this Mac; sharing it needs signing with an Apple Developer ID.

## Using it

The window has three areas: the task list on the left, the main pane in the middle (with tabs), and
shells on the right when you open some. The area with the keys has a violet border, its selection is
a highlighted bar, and the badge at the bottom left names it.

| Keys | What they do |
|---|---|
| `⌘⌥←` `⌘⌥→` `⌘⌥↑` `⌘⌥↓` | Move between the task list, the main pane and the shells (`⇧`+arrows too) |
| `⌘[` / `⌘]` | Previous / next panel |
| `⌘1` … `⌘6` | Tabs: Chat, Plan, PR, Files, Changes, Usage (`⌘⇧[` / `⌘⇧]` previous / next; `←` `→` in an empty chat box) |
| `⌘N` | New task |
| `⌘D` | New shell in the task's worktree |
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
| drag | The task list's right border resizes it; clicking a group's ▾/▸ folds it |
| `⌘` `+` `−` `0` | Zoom |
| `ctrl+d` | Delete a task (its worktree is kept) |
| `ctrl+c` | Quit (or ⌘Q) |

A ⌘ letter not listed does what ctrl and that letter do. **To change the ⌘ shortcuts**, type `/keys` in
a task's chat: it opens `~/.crew/keys` (one `keys = action` per line, every action listed at the top)
in the editor; save it and the change works on the next key press.

In the message box: `⏎` sends, `⇧⏎` adds a line, `@` suggests files, `/` suggests commands (crew's and
your Claude Code commands, skills and agents). Sending while Claude works stops its current step and
sends your message right away.

**Selecting and links.** Drag over text to select it, inside one panel at a time (the task list, the
chat or a tab, a shell), as if each were its own window; it's copied when you let go, and ⌘C copies
it again. Click a link to open it. The editor has its own selection (drag or shift+arrows, then ⌘C).

crew's own commands:

| Command | What it does |
|---|---|
| `/model` | Pick the task's model (Opus, Sonnet, Haiku, or an id) |
| `/plan [goal]` | Have Claude write the task's plan (without a goal, it plans the task's description) |
| `/compact` | Summarise the conversation so later messages re-send far less |
| `/new [message]` | Start a fresh Claude session in the same worktree (see below) |
| `/shell` | Open a shell in the task's worktree |
| `/keys` | Change the ⌘ shortcuts |

Each tab and popup lists its own keys on the bottom line.

## Tokens and context

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
  In the chat, that quoted feedback is folded to its first lines (`ctrl+r` shows it all).
- **Plans survive new sessions.** After `/new`, Claude is pointed at the task's plan, so it picks up
  at the current step with a small context.
- **The PR tab is free.** It reads GitHub with `gh` on your machine; only sending something to the
  agent costs tokens.

## Settings

| Variable | Default | What it does |
|---|---|---|
| `CREW_HOME` | `~/.crew` | Where crew keeps tasks, chats and the repo list |
| `CREW_AUTO_COMPACT` | off | Context size (tokens) from which crew compacts before sending |
| `CREW_ARCHIVE_DAYS` | `7` | Archive done tasks untouched this many days (`0` turns it off) |
| `CREW_NOTIFY` | on | `0` turns off notifications (Claude finished, CI failed, a review, new comments, merged) |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Where Claude Code's commands, skills and session logs are read from |

Set them in your shell's profile (e.g. `~/.zshrc`); the app reads your login shell's environment.

Agents run with `--permission-mode auto`, so Claude Code approves tool calls without asking (no one
is there to answer prompts in headless mode).

## Where things live

- `~/.crew/tasks/<id>.md`: each task, as a small header plus its description
- `~/.crew/chats/<id>.jsonl`: each task's chat
- `~/.crew/repos`: the repos you've used, one path per line
- `~/.crew/keys`: your ⌘ shortcuts, if you changed any (`/keys`)
- `~/.crew/plans/<id>.md`: each task's plan (agents get access to this folder with `--add-dir`)
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
| `crew:review` | Reviews a pull request into a summary, risks and line comments (`plugin/skills/review/SKILL.md`) |

The build records where this checkout is, and crew reads the skills from it on each run, so editing a
skill or adding one (`plugin/skills/<name>/SKILL.md`) needs no rebuild. If the checkout moves, run
`make install-app` again.

## Development

```bash
make run       # build the app and open it (crew.app in this folder)
make build     # just the crew binary the app runs
```

The app (`desktop/`) is only a window around the crew binary; every feature lives in crew itself.

The code is in `internal/`, one package per job, with the UI in `internal/ui` split into files by
area. [CLAUDE.md](CLAUDE.md) describes the layout and the practices to follow, and
[docs/next-tasks.md](docs/next-tasks.md) is the backlog.
