#!/bin/sh
# crew's PreToolUse hook. Agents work locally and propose; crew does what reaches others, on the
# user's key. It blocks an agent from:
#  - writing crew's task files (subtasks are made by crew, after the user applies a split);
#  - pushing, or posting to GitHub (opening, merging or closing PRs and issues, comments, reviews,
#    releases, or writing through gh api), which crew does on its PR tab (P, D, r, x).
# The tool's input arrives as JSON on stdin; exit 2 blocks the call and tells Claude why.
# ponytail: a plain text match; a command built in pieces (an alias, a script it writes) gets through.
input=$(cat)
home="${CREW_HOME:-$HOME/.crew}"
case "$input" in
*"$home/tasks"* | *".crew/tasks"* | *'$CREW_HOME/tasks'*)
	echo "crew's task files are crew's: only the user makes tasks and subtasks, from crew. To split a task, write the split proposal file and stop; the user applies it on the Plan tab." >&2
	exit 2
	;;
esac
# Only Bash runs commands; the outward ones, wherever they appear in it.
case "$input" in
*'"tool_name":"Bash"'* | *'"tool_name": "Bash"'*) ;;
*) exit 0 ;;
esac
if printf '%s' "$input" | grep -Eq 'git([[:space:]]+-[^[:space:]]+([[:space:]]+[^-[:space:]][^[:space:]]*)?)*[[:space:]]+push([^[:alnum:]_-]|$)|gh[[:space:]]+(pr[[:space:]]+(create|merge|close|reopen|comment|review|edit|ready)|issue[[:space:]]+(create|close|reopen|comment|edit)|release[[:space:]]+create)([^[:alnum:]_-]|$)|gh[[:space:]]+api[[:space:]].*(-X|--method)[[:space:]=]*(POST|PATCH|PUT|DELETE)'; then
	echo "Pushing and posting to GitHub are the user's, from crew: commit your work and stop. Say what should be pushed or posted (a PR, a reply, a comment) and the user does it on the task's PR tab (P pushes or opens the PR; drafted replies go with D)." >&2
	exit 2
fi
exit 0
