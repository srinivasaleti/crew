---
name: summary
description: Write a task's summary, product first: what it is and how it behaves for a user, then where it stands, how it works, and the changes by area, with Mermaid diagrams. Use for crew's Summary ("Summary file <path>. Base <sha>"), and whenever asked to summarise a task or explain what it changed.
argument-hint: Summary file <path>. Base <sha>
---

# A task's summary

You write for someone catching up on this task. They read top down and may stop anywhere, so the
most useful part comes first: what it is for a user, then how it behaves, then the code. The
arguments ($ARGUMENTS) give the file to write it to and the commit the work started from
(`Base <sha>`: the change is `git diff <base>` plus uncommitted work, and `git log <base>..HEAD`).

## Before you write

- Read the diff and the commits, the task's description, its plan (if it has one) and the
  conversation so far. Describe what the code does now, not what was intended: check every claim
  against the code. Something planned but not built is "planned", never described as working.
- Change no code. Write only the summary file.

## The file

Write it to the summary file's path, in Markdown, in this order:

```markdown
# <what it is, in product words>

<2–3 sentences: what a user can now do (or what stops going wrong), who it's for, why it matters.
No file names or code words here.>

## How it behaves
1. <the user's flow, 3–6 steps: where they start, what they do, what they see>
- <edge cases a user would notice: empty states, errors, limits, permissions>

<Diagram 1: the user's flow>

### How to try it
1. <the shortest steps to see it yourself: where to go, what to do, what to expect>

## Status
- <done, in progress or blocked; the plan's progress (e.g. 4/6) if it has one>
- <what was verified (tests run, a check by hand) and what wasn't, honestly>

## How it works
<the path through the system in a few lines: UI → API → service → storage, naming the real parts>

<Diagram 2, only if it adds to diagram 1: how the parts talk>

## What changed
<N files, +A −D>
- **<area>**: <what changed there, in a line> (`<key file>`, `<key file>`)

## Open
- <what's left, risks, decisions waiting on the user with your recommendation; leave it out if none>
```

- Up to "What changed" it should fit on one screen. Short and plain; no filler.
- "What changed" is by area (the page, the API, a service, a skill, docs), one line each, never
  file by file: the task's Changes tab lists every file.
- If no code has changed yet, write "How it behaves" from the task and plan, mark it planned, and
  leave out "How to try it".

## Diagrams

- At most two, in ```mermaid blocks, and each must earn its place. Diagram 1 is the user's flow
  (`flowchart LR`: screens, actions and choices, in the user's words). Diagram 2 is how the parts
  talk (`sequenceDiagram` for a request over time, or `flowchart` for parts), naming real code (a
  function, a route, a file). No diagram that only lists files.
- Keep each under ~15 nodes. Quote labels with spaces or punctuation (`A["core.Send"]`), and use no
  styling or colours: the page applies its own theme.

## Then

Reply with one line: that the summary is written, and its headline.
