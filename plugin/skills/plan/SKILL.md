---
name: plan
description: Write or follow a task's plan. Use whenever asked to plan work, and whenever the task has a plan file. Writes a short checklist plan; never changes code while planning.
argument-hint: Plan file <path>. Goal <goal>
---

# Planning

You are writing (or working through) the plan for one task. Read first, then plan the smallest change
that does the job. The plan file's path is in the arguments ($ARGUMENTS) or in the message.

## Before you write

- Read the code the goal touches and trace the real flow end to end. Don't plan from guesses.
- Look for what already exists: helpers, patterns, similar features. Reuse before adding.

## Principles

- YAGNI: plan only what the goal needs now. No "for later", no options nobody asked for.
- KISS: the most boring approach that works. Fewer files, fewer moving parts.
- DRY: one place for each rule or piece of logic; extend what's there instead of copying it.
- Fix root causes, not symptoms. A bug fix goes where every caller passes through.
- No new dependency unless a few lines can't do it; say why if you add one.

## The plan file

Write it to the plan file's path, and only that file: change no code while planning.

```markdown
# Plan: <goal in a few words>

<one or two sentences: the approach and why>

- [ ] 1. <step: one verifiable change, about one commit>
      <optional, indented: files it changes, notes, how to check it>
- [ ] 2. …

## Notes

- <what you found that shapes the plan, a line each; leave the section out if there's none>

## Questions

1. <a decision you need from the user, as a question>
   - <option> (recommended: <why, in a few words>)
   - <option>
2. Also <something worth doing beyond the goal>?
   - yes
   - no (recommended: <why>)
```

- Nothing but the title and the one or two sentences goes before the steps; findings go in Notes.

## Asking

Nobody can answer you mid-turn, so questions go in the plan's `## Questions` section and the user
answers them later (crew shows them above the steps).

- Ask when a choice changes the plan (an approach, an API, a trade-off), when the goal is unclear,
  or when you'd suggest something beyond the goal. Don't guess on these, and don't ask about what
  the code already answers.
- Give 2–4 options and mark the one you'd pick `(recommended: <why>)`. Put a suggestion as a
  yes/no question.
- Plan the steps around your recommended options, and say which steps depend on which question.
- When answers come back, update the steps to match, delete the answered questions (keep the
  decision as a line in Notes if it matters later), and reply in a sentence or two.
- Keep a step's title short (under ~70 characters); details go in its indented lines.

- 3 to 10 steps. A step you can't check is too vague; one that touches everything is too big.
- Name the files each step changes.
- Say how to check each step (build, test, or what to look at).
- When the plan is written, reply with a short summary and stop. Don't start on the steps.

## Working through a plan

- Read the plan before you start, and again at the start of a new session.
- Mark a step `[~]` when you start it and `[x]` when it's done and checked. Edit only its mark.
- Mark it `[!]` with a one-line reason under it when it's blocked, then move on to the next step you can do.
- If a step needs a decision from the user, add it to `## Questions`, mark the step `[!]`
  ("waiting on question N") and stop.
- If the plan turns out wrong, fix the plan first, then the code. Keep it short.
- Work on the step you were asked for; don't run ahead unless asked to continue.
