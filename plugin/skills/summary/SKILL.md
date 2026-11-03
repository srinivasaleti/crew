---
name: summary
description: Write a task's summary for a person catching up on it, with Mermaid diagrams of how the change works. Use for crew's Summary ("Summary file <path>. Base <sha>"), and whenever asked to summarise a task or explain what it changed.
argument-hint: Summary file <path>. Base <sha>
---

# A task's summary

You write what someone needs to understand this task quickly: what it's for, where it stands, what
changed and how it works, drawn where a picture says it faster. The arguments ($ARGUMENTS) give the
file to write it to and the commit the work started from (`Base <sha>`: the change is
`git diff <base>` plus uncommitted work, and `git log <base>..HEAD`).

## Before you write

- Read the diff and the commits, the task's description, its plan (if it has one) and the
  conversation so far. Describe what the code does now, not what was intended: check every claim
  against the code.
- Change no code. Write only the summary file.

## The file

Write it to the summary file's path, in Markdown:

```markdown
# <the task, in a few words>

<two or three sentences: what it's for, the approach, and where it stands>

## Status
- <done, in progress, blocked, a line each; the plan's progress if it has one>

## What changed
- <a line per change worth knowing, grouped by area, naming the files>

## How it works
<a Mermaid diagram of the flow the change adds or alters, and a sentence or two on it>

## Open
- <what's left, risks, decisions waiting on the user; leave the section out if there's none>
```

## Diagrams

- One to three, in ```mermaid blocks. Each must earn its place: a flow between parts, a sequence of
  calls, states, or how data moves. No diagram that only lists files.
- Pick the type that fits: `flowchart LR` for parts and flow, `sequenceDiagram` for calls over time,
  `stateDiagram-v2` for states. Keep each under ~15 nodes; name nodes after the real code (a
  function, a route, a file).
- Quote labels that have spaces or punctuation (`A["core.Send"]`), and use no styling or colours:
  the page applies its own theme.
- If nothing changed yet, say so and draw how the part of the code the task touches works today.

## Then

Reply with one line: that the summary is written, and its headline.
