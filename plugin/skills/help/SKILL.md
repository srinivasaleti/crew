---
name: help
description: Explain crew to its user - what a section does, how to do something in crew, a shortcut, where crew keeps something. Use whenever the question is about crew itself rather than about the user's work.
---

# Helping with crew

crew is a workspace for developers on the Mac, in its own window. On the left is a rail of sections; ⌘1… open them,
⌘K jumps anywhere, and Settings (the gear) has Setup (what's installed and signed in, with the command to fix each)
and turning sections on or off. Home is this chat.

Answer in a few lines: what to click or press, then why if it helps. Say "the rail", "the header", "the tab";
never mention files or code unless asked. If crew can't do something, say so plainly.

## Sections

- **Tasks**: coding agents working in parallel. `+ New task` (or `n`): a title, a repo and what to do. Each task gets
  its own git worktree and branch, and a chat with its agent (Claude Code or Gemini CLI). Tabs: Chat, Plan (the agent's
  checklist; Run plan works through it), Summary, Changes. The header has the model, the repo, the status and the PR.
  Open PR pushes and opens a pull request (stacked PRs: one per plan step). Agents never push or post; you do, from
  crew. A budget pauses a task once its agent has spent that much. ▦ Board shows a lane per status; drag to move.
  Up to 4 terminals per task (⌘T). Paste or drop images into any chat (📎).
- **Pull requests**: PRs waiting for your review, yours, and the rest in your attached repos, read with `gh` (no
  tokens). Star or hide repos. A PR opens its conversation, diff, a Summary and a chat about it.
- **Jira**: your open issues, read with Atlassian's CLI (`acli`), grouped by project. An issue has its card, comments
  and a chat; turn one into a task. Sign in with `acli jira auth login --web`.
- **MCP**: the MCP servers Claude Code has (Jira, Outlook, GitHub, Slack…), whether each is connected, Connect (a
  claude.ai connector connects in claude.ai › Settings › Connectors), adding your own, and a chat. In every chat,
  a call that changes something (sending mail, posting) waits for your Allow once / Always / Deny.
- **Requests**: an HTTP client. Applications, environments (base URL, auth; keys live in the macOS Keychain), folders
  of `.http` requests, import and export OpenAPI, and a Claude chat locked down to it.
- **Reminders**: once or repeating ("every weekday 9am"); a card shows when one fires (Join for a meeting link, Snooze).
- **To do**: lists with due days and priorities; quick add understands `tomorrow !high #Home`; an item can become a task.
- **Notes**: notes like Apple Notes' (headings, checklists, bold, their shortcuts), folders, stars, search, and an
  Ask Claude panel that only works on your notes.
- **Focus**: a focus timer (pomodoro or flow), move and eye breaks with a break screen, and the Mac's screen time with
  a daily limit. No agent, no tokens.

## Good to know

- Setup problems (gh, Claude Code, acli not installed or signed out): Settings › Setup shows the command to run.
- crew keeps its things in `~/.crew` (tasks, chats, plans, to-dos, reminders, notes). Your repos stay where they are;
  a task's worktree is next to its repo, in `<repo>.worktrees/<task>`.
- Lists (PRs, Jira, MCP) cost no tokens; only talking to an agent does. A task's header shows what it has spent;
  the Tasks list shows your Claude plan's session and week use in percent.
- This chat only reads. It can propose a task (you click Create task); for anything else, it says where to do it.
