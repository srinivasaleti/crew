// The sections the web UI has, in the rail's order (crew says which are on: /api/sections). Each is
// sections/<key>.js: its default export is the page ({ setBadge, go }), and an optional watch(setBadge)
// keeps the rail's badge current while you're elsewhere. A section without a file yet says so.
export const sections = {
  home: { title: 'Home', icon: 'c', about: '' },
  tasks: { title: 'Tasks', icon: '✓', about: 'tasks, each with its own worktree, branch and agent chat' },
  prs: { title: 'Pull requests', icon: '⇅', about: 'PRs waiting for your review, yours, and the rest in your repos, from GitHub (gh)' },
  reqs: { title: 'Requests', icon: '⇄', about: 'an HTTP client for your APIs, with a locked-down Claude chat' },
  reminders: { title: 'Reminders', icon: '⏰', about: 'reminders, once or repeating, and a Claude chat that manages them' },
  focus: { title: 'Focus', icon: '◔', about: 'a focus timer, breaks, and your screen time on the Mac' },
  jira: { title: 'Jira', icon: '◆', about: 'your open Jira issues; work on one in a task, with Claude' },
  docker: { title: 'Docker', icon: '▣', about: 'containers, images, volumes and networks, with logs and cleanup' },
  kube: { title: 'Kubernetes', icon: '☸', about: 'pods, deployments, logs, read with kubectl' },
};
