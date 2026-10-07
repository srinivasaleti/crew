---
name: review
description: Review a pull request for crew. Use when asked to review a PR; writes a summary, the real risks, and comments on lines of the diff. Never changes code.
argument-hint: PR <url>. Diff <file>.
---

# Reviewing a pull request

You're reviewing someone's PR so a human can post your review. Be the reviewer you'd want: brief,
specific, and only about what matters. The PR's URL and the file with its diff are in the arguments
($ARGUMENTS). If you're in a checkout of the PR, read the code around the diff (callers, tests,
conventions); otherwise review from the diff alone. Change nothing.

## What to look for, in this order

1. Correctness: bugs, broken edge cases, wrong assumptions, races, error handling that loses data.
2. Security: auth and permission checks, injection, secrets, unsafe input at trust boundaries.
3. Design: does it fit how the codebase already does this? Reuse over new code; no needless
   abstraction (YAGNI, KISS, DRY).
4. Tests: is the risky part covered?
5. Only then readability. Skip style the formatter or linter handles.

## Comments

- Put each comment on the exact new line it's about (a `+` line or an unchanged line in the diff).
- One issue per comment. Say what's wrong, why it matters, and what to do instead; show a short
  fix when it helps.
- Mark severity: `must-fix` (blocks merging), `should-fix`, or `nit`. Few nits; none is fine.
- Don't comment to praise, restate the code, or ask questions the code already answers.
- No comment is better than a weak one. Three good comments beat twelve.

## Summary and verdict

- Summary: two or three sentences on what the PR does and your overall take.
- Risks: the few things most likely to go wrong in production, if any.
- Verdict: `request_changes` if anything is must-fix, `approve` if it's good to merge as is, else `comment`.
