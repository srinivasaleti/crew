#!/bin/sh
# crew's PreToolUse hook: blocks an agent from writing crew's task files, so subtasks are only ever
# made by crew, after the user applies a split (an agent writes the proposal, never the tasks).
# The tool's input arrives as JSON on stdin; exit 2 blocks the call and tells Claude why.
# ponytail: a plain text match on the path; a command that builds the path in pieces gets through.
input=$(cat)
home="${CREW_HOME:-$HOME/.crew}"
case "$input" in
*"$home/tasks"* | *".crew/tasks"* | *'$CREW_HOME/tasks'*)
	echo "crew's task files are crew's: only the user makes tasks and subtasks, from crew. To split a task, write the split proposal file and stop; the user applies it on the Plan tab." >&2
	exit 2
	;;
esac
exit 0
