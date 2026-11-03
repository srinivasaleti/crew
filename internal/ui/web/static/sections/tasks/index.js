// TASKS: the tasks by status on the left (with what each has cost, its plan's progress, when it last
// moved), and the open task on the right: its chat, plan and changes, its status, and editing,
// archiving or deleting it.
import { html, useState, useEffect, api, on, useEvents, useLoad, ask, Form, Empty, Search, useListKeys, css } from '../../lib.js';
import { unread, money, typing, deleteTask } from './common.js';
import { Task } from './task.js';
import { Row } from './row.js';
import { Board } from './board.js';
import { ShellPane } from './shell.js';

css(import.meta.url);

let openID = '';

// watch shows the rail what needs you: ● while an agent works, else the replies you haven't read.
export function watch(setBadge) {
  const count = () => api('GET', '/tasks').then((l) => setBadge(l.some((t) => t.working) ? '●' : unread.size || ''));
  count();
  return on((ev) => {
    if (ev.kind === 'message' && ev.msg.role === 'agent' && ev.id !== openID) unread.add(ev.id);
    if (ev.kind === 'task' || ev.kind === 'turn' || ev.kind === 'message') count();
  });
}

// filters are the chips above the list: what each keeps.
const filters = [
  ['all', 'All', () => true],
  ['working', 'Working', (t) => t.working],
  ['needs', 'Needs you', (t) => t.asks || unread.has(t.id)],
  ['pr', 'In PR', (t) => t.pr && t.pr.state === 'OPEN' || t.status === 'in-pr'],
  ['done', 'Done', (t) => t.status === 'done' || (t.pr && t.pr.state === 'MERGED')],
];

// fold remembers the groups you folded, per browser.
const folds = (() => { try { return JSON.parse(localStorage.getItem('crew.tasks.folded') || '{}'); } catch { return {}; } })();
const saveFolds = () => { try { localStorage.setItem('crew.tasks.folded', JSON.stringify(folds)); } catch {} };

// shellOpen is the tasks whose shell panel shows (the shells themselves run in crew).
const shellOpen = new Set();

// The task view's panels, left to right, for ⌘⇧←/→: the list, the task, its shell.
const panels = () => ['.tasks-col .col-list', '.task-page', '.shell-pane'].map((q) => document.querySelector(q)).filter(Boolean);

// focusPanel puts the keys in a panel: the shell's terminal, the task's message box, or the list.
function focusPanel(el) {
  const into = el.matches('.shell-pane') ? el.querySelector('textarea') : el.matches('.task-page') ? el.querySelector('.compose textarea') : null;
  (into || el).focus();
}

// views: how the tasks show, remembered per browser.
const savedView = () => { try { return localStorage.getItem('crew.tasks.view') || 'board'; } catch { return 'board'; } };

export default function Tasks() {
  const [view, setViewState] = useState(savedView); // 'board', or 'task': the list beside the open task
  const setView = (v) => { setViewState(v); try { localStorage.setItem('crew.tasks.view', v); } catch {} };
  const [archived, setArchived] = useState(false);
  const { data: list, reload } = useLoad(() => api('GET', '/tasks' + (archived ? '?archived=1' : '')), [archived]);
  const repos = useLoad(() => api('GET', '/tasks/repos'));
  const [sel, setSel] = useState(decodeURIComponent(location.hash.split('/')[1] || ''));
  const [form, setForm] = useState(null); // 'new', or the task being edited
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState('all');
  const [note, setNote] = useState('');
  const [, bump] = useState(0);
  useEvents((ev) => { if (ev.kind === 'task' || ev.kind === 'open' || (ev.kind === 'turn' && !ev.busy)) reload(); if (ev.kind === 'message') bump((n) => n + 1); });
  const keep = filters.find((f) => f[0] === filter)[2];
  const tasks = (list || []).filter((t) => keep(t) && (!q || (t.title + ' ' + t.repo + ' ' + (t.branch || '') + ' ' + (t.pr ? '#' + t.pr.number : '')).toLowerCase().includes(q.toLowerCase())));
  const open = (id) => { setSel(id); openID = id; unread.delete(id); location.hash = 'tasks/' + id; };
  useEffect(() => { if (view !== 'board' && list && !list.some((t) => t.id === sel) && tasks.length) open(tasks[0].id); }, [list, view]);
  useEffect(() => { openID = view === 'board' ? '' : sel; if (openID) unread.delete(sel); }, [sel, view]);
  const group = (t) => (t.archived ? 'Archived' : t.label);
  // A group starts folded, except the open task's; folding or unfolding one is remembered.
  const cur = (list || []).find((t) => t.id === sel);
  const openGroup = cur ? group(cur) : '';
  const folded = (g) => folds[g] ?? g !== openGroup;
  const shown = tasks.filter((t) => !folded(group(t)));
  useListKeys(view === 'board' ? [] : shown.map((t) => t.id), sel, open);
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || document.querySelector('.scrim')) return;
      if (e.key === 'n') { e.preventDefault(); setForm('new'); }
      if (e.key === 'b') setView(view === 'board' ? 'task' : 'board');
      if (e.key === 'Escape' && view === 'task') setView('board');
      if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && view === 'task' && openGroup && folded(openGroup) !== (e.key === 'ArrowLeft')) fold(openGroup);
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });
  // ⌘ keys of the task view, caught before the shell or a text box takes them.
  useEffect(() => {
    const k = (e) => {
      if (!e.metaKey || document.querySelector('.scrim')) return;
      if (e.key === 't' && !e.shiftKey && sel) {
        e.preventDefault(); e.stopPropagation();
        setView('task');
        if (shellOpen.has(sel)) { const p = document.querySelector('.shell-pane'); p && focusPanel(p); }
        else { shellOpen.add(sel); bump((n) => n + 1); } // the panel focuses its terminal when it opens
      }
      if (e.shiftKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight') && view === 'task') {
        e.preventDefault(); e.stopPropagation();
        const ps = panels(), at = ps.findIndex((p) => p.contains(document.activeElement));
        const to = ps[Math.max(0, Math.min(ps.length - 1, (at < 0 ? 1 : at) + (e.key === 'ArrowLeft' ? -1 : 1)))];
        to && focusPanel(to);
      }
    };
    addEventListener('keydown', k, true);
    return () => removeEventListener('keydown', k, true);
  });
  const task = (list || []).find((t) => t.id === sel);
  const total = (list || []).reduce((n, t) => n + t.cost, 0);
  const groups = [];
  for (const t of tasks) {
    const g = group(t);
    if (!groups.length || groups[groups.length - 1].name !== g) groups.push({ name: g, items: [] });
    groups[groups.length - 1].items.push(t);
  }
  const del = (t) => deleteTask(t).then((n) => { if (n === null) return; setNote(n); if (t.id === sel) setSel(''); reload(); }, (e) => ask(e.message));
  const fold = (g) => { folds[g] = !folded(g); saveFolds(); bump((n) => n + 1); };
  const head = html`<div class="col-head"><div><b>Tasks</b> <span class="meta">${(list || []).filter((t) => !t.archived).length}${total ? ' · ' + money(total) + ' spent' : ''}</span></div>
      <div class="head-btns"><button class="small" title=${view === 'board' ? 'Task view (b)' : 'Board (b · esc)'} onClick=${() => setView(view === 'board' ? 'task' : 'board')}>${view === 'board' ? '☰ Task view' : '▦ Board'}</button>
        <button class="primary small" title="New task (n)" onClick=${() => setForm('new')}>+ New task</button></div></div>
    <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search tasks, repos, branches, #PR" /></div>
    <div class="filters">${filters.map(([k, label, f]) => {
      const n = k === 'all' ? 0 : (list || []).filter(f).length;
      return html`<button key=${k} class=${'fchip' + (filter === k ? ' on' : '')} onClick=${() => setFilter(k)}>${label}${n ? html` <span class="n">${n}</span>` : ''}</button>`;
    })}</div>`;
  const forms = html`${form === 'new' && html`<${Form} title="New task" action="Create" onClose=${() => setForm(null)} initial=${{ Agent: 'claude', Repo: (repos.data || [])[0] || '' }}
      fields=${[
        { key: 'Title', label: 'Name', placeholder: 'What to do, in a few words' },
        { key: 'Repo', label: 'Repo folder', type: 'combo', options: repos.data || [], placeholder: '~/code/project', hint: 'an attached repo, or any git folder' },
        { key: 'Body', label: 'Description', type: 'textarea', placeholder: 'What the agent should know: the goal, links, constraints' },
        { key: 'Agent', label: 'Agent', type: 'select', options: [{ value: 'claude', label: 'Claude Code' }, { value: 'gemini', label: 'Gemini CLI' }] },
        { key: 'Model', label: 'Model', type: 'combo', options: ['opus', 'sonnet', 'haiku'], placeholder: "the agent's default", hint: 'optional' },
        { key: 'Branch', label: 'Branch', placeholder: 'leave empty for a new branch', hint: 'optional: work on an existing branch or PR' },
      ]}
      submit=${async (v) => { const t = await api('POST', '/tasks', v); await reload(); open(t.id); setView('task'); }} />`}
    ${form && form !== 'new' && html`<${Form} title="Edit task" onClose=${() => setForm(null)} initial=${{ Title: form.title, Body: form.body }}
      fields=${[{ key: 'Title', label: 'Name' }, { key: 'Body', label: 'Description', type: 'textarea', rows: 8, hint: "a new session of the agent is told this" }]}
      submit=${(v) => api('POST', `/tasks/${form.id}/edit`, v)} />`}`;
  if (view === 'board') return html`<main class="page board-page">${head}
      ${list ? html`<${Board} tasks=${tasks} del=${del} open=${(id) => { open(id); setView('task'); }} />` : html`<div class="col-empty">Loading…</div>`}
      <div class="col-foot"><label class="toggle"><input type="checkbox" checked=${archived} onChange=${(e) => setArchived(e.target.checked)} /> Show archived</label><span class="meta">drag a card to change its status · b task view · n new · / search</span></div>
      ${forms}</main>`;
  return html`<div class="split">
    <section class="col tasks-col">
      ${head}
      <div class="col-list" tabIndex="-1">${groups.length ? groups.map((g) => html`<div key=${g.name}>
          <button class="group fold" onClick=${() => fold(g.name)}><span class="caret">${folded(g.name) ? '▸' : '▾'}</span> ${g.name} <span class="n">${g.items.length}</span></button>
          ${!folded(g.name) && g.items.map((t) => html`<${Row} key=${t.id} t=${t} on=${t.id === sel} open=${open} del=${del} />`)}</div>`)
        : html`<div class="col-empty">${list ? (q || filter !== 'all' ? 'Nothing matches.' : 'No tasks yet.') : 'Loading…'}</div>`}</div>
      <div class="col-foot"><label class="toggle"><input type="checkbox" checked=${archived} onChange=${(e) => setArchived(e.target.checked)} /> Show archived</label><span class="meta">↑↓ move · ←→ fold · ⌘T shell · ⌘⇧←/→ panels · b board</span></div>
    </section>
    ${task ? html`<${Task} task=${task} edit=${(t) => setForm(t)} onDeleted=${(n) => { setNote(n); setSel(''); reload(); }} />`
      : html`<main class="page"><${Empty} title=${list && list.length ? 'Pick a task' : 'Start your first task'}>${html`<p>${note || 'Each task gets its own worktree, branch and agent, so several can run at once.'}</p><button class="primary" onClick=${() => setForm('new')}>New task (n)</button>`}<//></main>`}
    ${task && shellOpen.has(task.id) && html`<${ShellPane} key=${task.id} id=${task.id} onEnd=${() => { shellOpen.delete(task.id); bump((n) => n + 1); }} />`}
    ${forms}
  </div>`;
}
