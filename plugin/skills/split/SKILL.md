---
name: split
description: Propose splitting a task into subtasks that agents work on in parallel. Use for crew's /split ("Split file <path>. Goal <goal>"). Writes a proposal only; crew applies it after the user confirms.
argument-hint: Split file <path>. Goal <goal>
---

# Splitting a task

You propose how to split one task into subtasks that separate agents can work on at the same time,
each in its own worktree branched from this task's branch. The split file's path is in the
arguments ($ARGUMENTS).

**You only propose.** Write the split file and stop. Never make tasks, worktrees or branches, never
start agents, and change no code: crew shows the proposal to the user, and only the user applies it.
(crew's hook blocks writes to its task files.)

## Before you write

- Read the code the goal touches and trace the real flow. Don't split from guesses.
- Find where the work naturally separates: by module, by layer, by file.

## Principles

- Parallel subtasks never change the same files. Two that would are one subtask, or one comes
  `After` the other.
- Shared groundwork (a type, an interface, a helper the others use) is its own first subtask, and
  the ones that use it come after it.
- Fewer is better: 2 to 6 subtasks. Don't split work that one agent does well in one go; if the
  task shouldn't be split, say so in a sentence and write no file.
- Each subtask is a whole, checkable change (builds, its tests pass) and is about one PR's worth or
  less. YAGNI and KISS apply: only what the goal needs.

## The split file

Write it to the split file's path, and only that file.

```markdown
# Split: <goal in a few words>

<one or two sentences: how the work divides and why>

- [ ] 1. <subtask title, under ~60 characters>
      Files: <the files or folders it owns>
      <what its agent should do: enough to work alone, without this conversation>
- [ ] 2. <title>
      After: 1
      Files: …
      …

## Questions

1. <a decision you need from the user>
   - <option> (recommended: <why>)
   - <option>
```

- `After: 1, 3` lists the subtasks that must be done (merged into this task's branch) first; leave it
  out when a subtask can start straight away.
- The indented lines are all its agent gets besides its title, so name files, functions and how to
  check it.
- Questions are only for choices that change the split; leave the section out when there are none.

When it's written, reply with a short summary (how many subtasks, which run at once) and that the
user reviews and applies it on the Plan tab. Then stop.

## When asked to change it

Update the split file to match (answers to questions, "merge 2 and 3"), delete answered questions,
and reply in a sentence or two.
