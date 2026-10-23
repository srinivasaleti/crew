---
name: pr
description: Write a pull request's title and description from the task, its plan and its diff. Use for crew's /pr ("Description file <path>. Base <sha>"), and whenever asked to write or update a PR description.
argument-hint: Description file <path>. Base <sha>
---

# A pull request's description

You write the description a reviewer wants to read: what changed, why, how it was checked, and
what to look at. The arguments ($ARGUMENTS) give the file to write it to and the commit the change
started from (`Base <sha>`: the change is `git diff <base>` and `git log <base>..HEAD`).

## Before you write

- Read the diff and the commits, the task's description and plan (if it has one), and the
  conversation so far. Describe what the code does, not what was intended: check claims against
  the diff.
- If the repo has a PR template (`.github/pull_request_template.md` and similar), follow its
  sections; fill each from the change, and leave out any that asks for secrets or credentials.

## The file

Write it to the description file's path, and only that file. The first line is the title, as a
heading; the rest is the body, in Markdown.

```markdown
# <title: what it does, in the imperative, under ~70 characters>

<one or two sentences: the change and why>

## What changed
- <a line per change a reviewer should know about, grouped by area>

## How it was tested
- <what was run, and what wasn't, honestly>

## Notes for reviewers
- <risks, decisions, follow-ups, what to look at first; leave the section out if there's none>
```

- Short and plain. No filler, no marketing, no restating the diff line by line.
- Say what wasn't tested. Never claim a check you didn't run.
- Link the issue if the task has one.

## Then

crew opens the PR with this title and body when the user presses P. If the branch already has an
open PR, update it with `gh pr edit --title "<title>" --body-file <a file with the body>`; if that's
refused, say the description is ready in the file. Reply with the title and a one-line summary.
