// The sections the web UI has, in the rail's order (crew says which are on: /api/sections). Each is a
// folder, sections/<key>/: index.js's default export is the page ({ setBadge, go }), an optional
// watch(setBadge) keeps the rail's badge current while you're elsewhere, and style.css and any smaller
// parts sit beside it. A section without a folder yet says so.
// ponytail: DOCKER and KUBERNETES are out of the web UI for now (the terminal UI keeps them). Their
// pages are still in sections/docker, sections/kube and sections/containers: to bring them back, list
// them here again and import web/sections/containers in web.go.
export const sections = {
  home: { title: 'Home', icon: 'c', about: '' },
  settings: { title: 'Settings', icon: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M4.9 4.9 7 7M17 17l2.1 2.1M2 12h3M19 12h3M4.9 19.1 7 17M17 7l2.1-2.1"/></svg>', about: '' },
  tasks: { title: 'Tasks', icon: '<svg viewBox="0 0 24 24"><rect x="3" y="3" width="18" height="18" rx="4"/><path d="m8 12 3 3 5-6"/></svg>', about: 'tasks, each with its own worktree, branch and agent chat' },
  prs: { title: 'Pull requests', icon: '<svg viewBox="0 0 24 24"><circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M6 8.5v7M18 15.5V10a3 3 0 0 0-3-3h-3m2-2.5L11.5 7 14 9.5"/></svg>', about: 'PRs waiting for your review, yours, and the rest in your repos, from GitHub (gh)' },
  jira: { title: 'Jira', icon: '<svg viewBox="0 0 24 24"><path d="M12 2 2 12l10 10 10-10z"/><path d="m12 7.5-4.5 4.5 4.5 4.5 4.5-4.5z"/></svg>', about: 'your open Jira issues; work on one in a task, with Claude' },
  mcp: { title: 'MCP', icon: '<svg viewBox="0 0 24 24"><circle cx="6" cy="12" r="2.5"/><circle cx="18" cy="6" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M8.3 11 15.7 7M8.3 13l7.4 4"/></svg>', about: 'your MCP servers (Jira, Outlook, GitHub, Slack…) and a chat that uses them; writes ask you first' },
  reqs: { title: 'Requests', icon: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>', about: 'an HTTP client for your APIs, with a locked-down Claude chat' },
  reminders: { title: 'Reminders', icon: '<svg viewBox="0 0 24 24"><path d="M6 16v-5a6 6 0 1 1 12 0v5l2 2H4z"/><path d="M10 21a2 2 0 0 0 4 0"/></svg>', about: 'reminders, once or repeating, and a Claude chat that manages them' },
  todos: { title: 'To do', icon: '<svg viewBox="0 0 24 24"><path d="M9 6h11M9 12h11M9 18h11"/><path d="m3 6 1.5 1.5L7 5M3 12l1.5 1.5L7 11"/><circle cx="5" cy="18" r="1.5"/></svg>', about: 'checklists: lists, due days, priorities; turn an item into a task' },
  notes: { title: 'Notes', icon: '<svg viewBox="0 0 24 24"><path d="M6 3h9l4 4v14H6z"/><path d="M14 3v5h5M9 13h7M9 17h5"/></svg>', about: 'Markdown notes in ~/.crew/notes, with a Claude chat that works on them' },
  focus: { title: 'Focus', icon: '<svg viewBox="0 0 24 24"><circle cx="12" cy="13" r="8"/><path d="M12 9v4l3 2M9 2h6"/></svg>', about: 'a focus timer, breaks, and your screen time on the Mac' },
};
