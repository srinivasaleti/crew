// TASKS: the tasks by status on the left, the open task's chat on the right.
import { html, useState, useEffect, api, on, useEvents, useLoad, base, Chat, Form, Empty, Search, useListKeys, Toolbar } from '../lib.js';

const unread = new Set(); // tasks with a reply you haven't opened, shared by the page and the badge
let openID = '';

// watch counts what needs you (replies you haven't read) for the rail.
export function watch(setBadge) {
  const count = () => api('GET', '/tasks').then((l) => setBadge(l.filter((t) => t.working).length ? '●' : unread.size || ''));
  count();
  return on((ev) => {
    if (ev.kind === 'message' && ev.msg.role === 'agent' && ev.id !== openID) unread.add(ev.id);
    if (ev.kind === 'task' || ev.kind === 'turn' || ev.kind === 'message') count();
  });
}

export default function Tasks({ setBadge }) {
  const { data: list, reload } = useLoad(() => api('GET', '/tasks'));
  const [sel, setSel] = useState(decodeURIComponent((location.hash.split('/')[1] || '')));
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState('');
  const [, bump] = useState(0);
  useEvents((ev) => { if (ev.kind === 'task' || ev.kind === 'open') reload(); if (ev.kind === 'message') bump((n) => n + 1); });
  const tasks = (list || []).filter((t) => !q || (t.title + ' ' + t.repo).toLowerCase().includes(q.toLowerCase()));
  const open = (id) => { setSel(id); openID = id; unread.delete(id); location.hash = 'tasks/' + id; };
  useEffect(() => { openID = sel; unread.delete(sel); }, [sel]);
  useListKeys(tasks.map((t) => t.id), sel, open);
  useEffect(() => {
    const k = (e) => { if (e.key === 'n' && !/input|textarea/i.test(document.activeElement.tagName) && !e.metaKey) { e.preventDefault(); setAdding(true); } };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  const task = (list || []).find((t) => t.id === sel);
  let group = null;
  return html`<div class="split">
    <section class="col">
      <div class="col-head"><b>Tasks</b><button class="primary small" title="New task (n)" onClick=${() => setAdding(true)}>+ New</button></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} /></div>
      <div class="col-list">${tasks.length ? tasks.flatMap((t) => {
        const head = t.label !== group ? ((group = t.label), [html`<div class="group" key=${'g' + t.label}>${t.label}</div>`]) : [];
        return head.concat(html`<div key=${t.id} class=${'item' + (t.id === sel ? ' on' : '') + (unread.has(t.id) ? ' unread' : '')} onClick=${() => open(t.id)}>
          <span class=${'status s-' + t.status}>${t.icon}</span>
          <div class="item-main"><div class="item-title">${t.title}</div><div class="item-sub">${base(t.repo)}${t.branch ? ' · ' + t.branch : ''}</div></div>
          ${t.working ? html`<span class="dot work" title="working"></span>` : unread.has(t.id) && html`<span class="dot" title="new reply"></span>`}</div>`);
      }) : html`<div class="group">${list ? (q ? 'Nothing matches' : 'No tasks yet') : 'Loading…'}</div>`}</div>
    </section>
    ${task ? html`<main class="page">
        <${Toolbar} title=${task.title} sub=${html`<span class=${'chip s-' + task.status}>${task.icon} ${task.label}</span> ${base(task.repo)}${task.branch ? ' · ' + task.branch : ''} · ${task.agent}${task.model ? ' · ' + task.model : ''}`} />
        <${Chat} key=${task.id} id=${task.id} base=${'/tasks/' + task.id} working=${task.working} agent=${task.agent === 'gemini' ? 'Gemini' : 'Claude'}
          empty="Say what to do. The first message sets up the task's worktree and branch." /></main>`
      : html`<main class="page"><${Empty} title=${list && list.length ? 'Pick a task' : 'No tasks yet'}>${html`<p>Each task gets its own worktree, branch and agent.</p><button class="primary" onClick=${() => setAdding(true)}>New task (n)</button>`}<//></main>`}
    ${adding && html`<${Form} title="New task" action="Create" onClose=${() => setAdding(false)}
      fields=${[{ key: 'Title', label: 'Name' }, { key: 'Repo', label: 'Repo folder', placeholder: '~/code/project' }, { key: 'Branch', label: 'Branch (optional: an existing one)' }, { key: 'Body', label: 'Description', type: 'textarea' }]}
      submit=${async (v) => { const t = await api('POST', '/tasks', v); await reload(); open(t.id); }} />`}
  </div>`;
}
