// The sections the web UI has, in the rail's order (crew says which are on: /api/sections). Each is a
// folder, sections/<key>/: index.js's default export is the page ({ setBadge, go }), an optional
// watch(setBadge) keeps the rail's badge current while you're elsewhere, and style.css and any smaller
// parts sit beside it. A section without a folder yet says so.
export const sections = {
  home: { title: 'Home', icon: 'c', about: '' },
  tasks: { title: 'Tasks', icon: '<svg viewBox="0 0 24 24"><rect x="3" y="3" width="18" height="18" rx="4"/><path d="m8 12 3 3 5-6"/></svg>', about: 'tasks, each with its own worktree, branch and agent chat' },
  prs: { title: 'Pull requests', icon: '<svg viewBox="0 0 24 24"><circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M6 8.5v7M18 15.5V10a3 3 0 0 0-3-3h-3m2-2.5L11.5 7 14 9.5"/></svg>', about: 'PRs waiting for your review, yours, and the rest in your repos, from GitHub (gh)' },
  reqs: { title: 'Requests', icon: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>', about: 'an HTTP client for your APIs, with a locked-down Claude chat' },
  reminders: { title: 'Reminders', icon: '<svg viewBox="0 0 24 24"><path d="M6 16v-5a6 6 0 1 1 12 0v5l2 2H4z"/><path d="M10 21a2 2 0 0 0 4 0"/></svg>', about: 'reminders, once or repeating, and a Claude chat that manages them' },
  focus: { title: 'Focus', icon: '<svg viewBox="0 0 24 24"><circle cx="12" cy="13" r="8"/><path d="M12 9v4l3 2M9 2h6"/></svg>', about: 'a focus timer, breaks, and your screen time on the Mac' },
  jira: { title: 'Jira', icon: '<svg viewBox="0 0 24 24"><path d="M12 2 2 12l10 10 10-10z"/><path d="m12 7.5-4.5 4.5 4.5 4.5 4.5-4.5z"/></svg>', about: 'your open Jira issues; work on one in a task, with Claude' },
  docker: { title: 'Docker', icon: '<svg viewBox="0 0 24 24"><path d="M21 8 12 3 3 8v8l9 5 9-5z"/><path d="m3 8 9 5 9-5M12 13v8"/></svg>', about: 'containers, images, volumes and networks, with logs and cleanup' },
  kube: { title: 'Kubernetes', icon: '<svg viewBox="0 0 24 24"><path d="m12 2 8.5 4.5L22 16l-6 6H8l-6-6 1.5-9.5z"/><circle cx="12" cy="12" r="3"/></svg>', about: 'pods, deployments, logs, read with kubectl' },
};
