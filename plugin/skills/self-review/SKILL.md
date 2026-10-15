---
name: self-review
description: Review your own task's change before it becomes a PR, then fix what the review found when asked. Use for crew's /self-review ("Review. Base <sha>"), and when asked to fix its findings ("fix all", "fix 1 3").
argument-hint: Review. Base <sha>
---

# Self-review

You review the change this task made, the way a careful colleague would before it goes to a PR,
and later fix what you found when asked. The arguments ($ARGUMENTS) say the commit the task started
from (`Base <sha>`). The change is everything since then, committed or
not: `git diff <base>` plus untracked files (`git status`).

## Principles (for the review and the fixes)

- Simplicity first. The best fix is the smallest one that is correct.
- YAGNI: no options, layers or "for later" code nobody asked for. Removing code is a good finding.
- KISS: the boring approach that works. Fewer files, fewer moving parts.
- DRY: one place for each rule. Reuse what the repo already has instead of adding a copy.
- Follow the repo's own conventions (its CLAUDE.md, the code around the change) over general taste.

## Review

Read the diff, then the code around it (callers, tests, conventions). Change nothing.

Look for, in this order:

1. Correctness: bugs, missed edge cases, error handling that loses data, races.
2. Security: input at trust boundaries, auth, secrets.
3. Needless complexity: an abstraction with one user, copied code, dead code, leftover debug
   output, code the task didn't need.
4. Tests, where the risky part has none and the repo writes tests.

Reply with numbered findings, most important first, one line each plus an indented line for the fix:

```
1. must-fix · path/file.go:42 · <what's wrong and why it matters>
   fix: <the simplest fix, in a sentence>
2. should-fix · …
3. nit · …
```

- `must-fix` is wrong or unsafe, `should-fix` is worth doing before the PR, `nit` is optional.
  Few nits; none is fine.
- No finding is better than a weak one. Don't praise, restate the code or pad the list.
- If the change looks good, say so in one line.
- End with: "Reply \"fix all\" or \"fix 1 3\" to fix them."

## Fix

When asked to fix findings (all, or the numbers given), fix them from your last self-review in this
chat. If there's none in the chat, review first, then stop and reply with the findings instead of
fixing.

- Make the simplest fix for each. Don't refactor around it or fix things nobody listed.
- Double-check before anything complex. If a fix needs more than a small, local change (a new
  abstraction, a change across several files, an API or behaviour change), don't make it. Finish
  the simple ones, then end your reply with a `## Questions` section, as crew shows it as buttons:

```
## Questions

1. Finding 3 needs <the complex change>. Do it?
   - <the simple way, or leave it> (recommended: <why>)
   - <the complex change>
```

- Leave the changes uncommitted, so they can be checked in the Changes tab.
- Build and run the repo's checks if it has quick ones (its CLAUDE.md says which).
- Reply with what you fixed, a line each, and anything you skipped and why.
