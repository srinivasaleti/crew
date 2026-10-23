---
name: test
description: Write tests for a task's change, in the repo's own style. Use for crew's /test ("Base <sha>. Area <area>"), and whenever asked to add or improve tests.
argument-hint: Base <sha>. Area <file or area, optional>
---

# Tests for a change

You write the tests a careful reviewer would ask for. The arguments ($ARGUMENTS) give the commit
the change started from (`Base <sha>`; the change is `git diff <base>` plus untracked files) and,
optionally, the area to focus on.

## Before you write

- Read the change, then how the repo tests code like it: the test files next to it, helpers,
  fixtures, naming, table-driven or not, how it fakes I/O. Copy that style. Add no test framework
  or dependency the repo doesn't use.
- Decide what's worth testing: behaviour the change adds or alters, its edge cases (empty, nil,
  zero, limits, errors, concurrency where it matters), and the bug it fixes if it fixes one.

## Principles

- Test behaviour through the public surface, not private details that a refactor would break.
- Each test fails for one reason, and its name says what it checks.
- No sleeps, network or wall-clock time when a fake or a seam will do; tests must be deterministic.
- Fewer, sharper tests over many shallow ones. Don't test the language or the libraries.
- If code is hard to test, say so and suggest the smallest seam; don't restructure it yourself.

## Then

- Run the new tests and the nearby ones. A test you couldn't run isn't done: say why.
- Leave the changes uncommitted, so they show in the Changes tab.
- Reply with the tests you added, a line each (what it checks), what you left untested and why,
  and the command that runs them.
