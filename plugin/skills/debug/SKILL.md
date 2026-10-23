---
name: debug
description: Find a bug's root cause and fix it. Use for crew's /debug ("Symptom <what's wrong>"), and whenever asked why something fails, crashes or behaves wrongly. Evidence first, the smallest fix, and a test that would have caught it.
argument-hint: Symptom <error, log or what's wrong>
---

# Debugging

You find why something is wrong and fix it at its root. The symptom (an error, a log, a behaviour)
is in the arguments ($ARGUMENTS) or the message.

## Principles

- Root cause, not symptom. A fix that hides the error (a catch, a nil check, a retry, a sleep) is
  not a fix unless the root cause is that the case is legitimate.
- Evidence over guesses. Every claim about the cause points at code, output or a test you ran.
- The smallest change that's correct. Fix where every caller passes through, not each caller.
- Don't change what you don't need to: no refactors, renames or cleanups on the way.

## Steps

1. **Reproduce.** Find the shortest way to make it happen: a test, a command, a request. If you
   can't reproduce it, say what you tried and what you'd need, and stop. Don't fix blind.
2. **Read the path.** Trace the code from the entry point to where it goes wrong.
3. **Hypotheses.** List the likely causes, most likely first, and how to tell each apart. Check
   them one at a time (a log line, a test, a debugger, reading the data) until one is proven.
4. **Fix** the proven cause.
5. **Test.** Add a test that fails without the fix and passes with it, in the repo's own style.
   If the repo has no tests for this area, say how you checked it instead.
6. **Check.** Run the repo's quick checks (its CLAUDE.md says which, else build and the nearby
   tests). Leave the changes uncommitted, so they show in the Changes tab.

## Reply

```
**Cause:** <one or two sentences, with file:line>
**Evidence:** <what proved it>
**Fix:** <what changed and why it's the right place>
**Test:** <the test that guards it, or how it was checked>
```

If the fix needs a decision (a behaviour change, an API change, more than one place), make the
safe part, then end with a `## Questions` section: numbered, options as indented bullets, one
marked `(recommended: …)`.
