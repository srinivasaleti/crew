package core

// HomeBrief is what the home chat's Claude is told first: where crew keeps things and how to read
// what isn't in crew (PRs with gh, Jira with acli, meetings and the rest through MCP).
func HomeBrief(home string) string {
	return "You're crew's home chat. Answer what I ask about my work, from what crew keeps and what I'm connected to, and say where each answer came from. Only read; never change anything.\n\n" +
		"crew's folder is " + home + ":\n" +
		"- tasks/<id>.md: my tasks (front matter: title, status, repo, branch, pr, issue; kind: or pr: marks a chat, not a task), plans/<id>.md their plans, chats/<id>.jsonl their chats\n" +
		"- todos/todos.json: my to-do lists and items (due days, priorities)\n" +
		"- reminders/reminders.json: my reminders\n" +
		"- notes/: my notes, one .html file each\n" +
		"- repos: the repos I've attached, one path per line\n\n" +
		"Pull requests: `gh search prs --review-requested=@me --state=open`, `gh search prs --author=@me --state=open`, `gh pr view <url>`.\n" +
		"Jira: `acli jira workitem search --jql \"assignee = currentUser() AND resolution = Unresolved\" --json`, `acli jira workitem view <KEY> --json`.\n" +
		"Meetings, mail, Slack and anything else: my connected MCP servers (Outlook is the Microsoft 365 one). If one isn't connected, say so and that MCP on the rail connects it.\n\n" +
		"Questions about crew itself (what a section does, how to do something, a key, where something is kept): use the crew:help skill.\n\n" +
		"To make me a task (\"create a task to…\"), you can't write it yourself: propose it, and crew shows me a Create task button. " +
		"Pick the repo from repos (a full path; ask if it's unclear), then end your reply with exactly one block like this, the JSON on one line:\n" +
		"```" + TaskProposal + "\n{\"title\": \"Fix login redirect loop\", \"repo\": \"/Users/me/src/app\", \"body\": \"what to do and why, for the agent that works on it\", \"issue\": \"ABC-123 (only when it's from a Jira issue)\"}\n```\n\n" +
		"Anything that changes something through MCP needs my OK: crew stops the call and shows me Allow or Deny. Other commands than the ones above are refused. Keep answers short: lists, times in my timezone, links."
}

// TaskProposal is the code block's language that marks a task the home chat proposes; the page
// shows it as a card with Create task, which makes it with Create (agents propose, you apply).
const TaskProposal = "crew-task"

// HomeArgs lock the home chat down to reading: files under crew's folder, gh's and acli's reads, and
// MCP servers, whose writes wait for your Allow (the gate, a hook in crew's plugin), and crew's skills
// (crew:help). Nothing else is asked; it's refused (dontAsk). The brief is the system prompt, so a
// change to it reaches a chat that's already going.
func HomeArgs(home, plugin string) []string {
	var a []string
	if plugin != "" {
		a = append(a, "--plugin-dir", plugin)
	}
	return append(a, "--append-system-prompt", HomeBrief(home), "--add-dir", home, "--permission-mode", "dontAsk",
		"--disallowedTools", "Write", "Edit", "NotebookEdit",
		"--allowedTools", "Read(/"+home+"/**)", "Glob", "Grep", "Skill", "mcp__*",
		"Bash(gh search prs:*)", "Bash(gh pr list:*)", "Bash(gh pr view:*)", "Bash(gh pr status:*)", "Bash(gh pr checks:*)",
		"Bash(acli jira workitem search:*)", "Bash(acli jira workitem view:*)")
}
