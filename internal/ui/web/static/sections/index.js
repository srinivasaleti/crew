// The sections the web UI has, in the rail's order (crew says which are on: /api/sections). Each is
// sections/<key>.js: its default export is the page ({ setBadge, go }), and an optional watch(setBadge)
// keeps the rail's badge current while you're elsewhere. A section without a file yet says so.
export const sections = {
  tasks: { title: 'Tasks', icon: '✓' },
  prs: { title: 'Pull requests', icon: '⇅' },
  reqs: { title: 'Requests', icon: '⇄' },
  reminders: { title: 'Reminders', icon: '⏰' },
  focus: { title: 'Focus', icon: '◔' },
  jira: { title: 'Jira', icon: '◆' },
  docker: { title: 'Docker', icon: '▣' },
  kube: { title: 'Kubernetes', icon: '☸' },
};
