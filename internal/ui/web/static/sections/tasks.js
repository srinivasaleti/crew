// TASKS: the tasks by status on the left (with what each has cost, its plan's progress, when it last
// moved), and the open task on the right: its chat, plan and changes, its status, and editing,
// archiving or deleting it.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, base, ago, ask, openLink, Chat, Form, Empty, Search, useListKeys, Toolbar, Markdown } from '../lib.js';

if (!document.querySelector('link[href="sections/tasks.css"]')) document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="sections/tasks.css">');

const unread = new Set(); // tasks with a reply you haven't opened, shared by the page and the badge
let openID = '';
const money = (n) => (n ? '$' + n.toFixed(2) : '');
const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);

// watch shows the rail what needs you: ● while an agent works, else the replies you haven't read.
export function watch(setBadge) {
  const count = () => api('GET', '/tasks').then((l) => setBadge(l.some((t) => t.working) ? '●' : unread.size || ''));
  count();
  return on((ev) => {
    if (ev.kind === 'message' && ev.msg.role === 'agent' && ev.id !== openID) unread.add(ev.id);
    if (ev.kind === 'task' || ev.kind === 'turn' || ev.kind === 'message') count();
  });
}

// Menu is a button that opens a list of actions.
function Menu({ label, title, items }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    setTimeout(() => addEventListener('click', close), 0);
    return () => removeEventListener('click', close);
  }, [open]);
  return html`<span class="menu-wrap"><button title=${title} onClick=${() => setOpen(!open)}>${label}</button>
    ${open && html`<div class="menu right">${items.map((it) => html`<div key=${it.label} class=${'menu-item' + (it.danger ? ' danger' : '')} onClick=${it.run}>${it.label}${it.key && html`<span class="meta">${it.key}</span>`}</div>`)}</div>`}</span>`;
}

// StatusMenu is the task's status chip; a click lists the statuses to pick.
function StatusMenu({ task }) {
  const [list, setList] = useState([]);
  useEffect(() => { api('GET', '/tasks/statuses').then(setList); }, []);
  const pick = (k) => api('POST', `/tasks/${task.id}/status`, { Status: k });
  return html`<${Menu} label=${html`<span class=${'s-' + task.status}>${task.icon}</span> ${task.label} ▾`} title="Status (s)"
    items=${list.map((s) => ({ label: html`<span class=${'s-' + s.key}>${s.icon}</span> ${s.label}`, run: () => pick(s.key) }))} />`;
}

// PRChip is a task's PR: its number, then merged/closed or its checks (✓ ✗ ●) and review.
function PRChip({ pr, big }) {
  if (!pr) return '';
  const st = pr.state === 'MERGED' ? ['merged', 'merged'] : pr.state === 'CLOSED' ? ['closed', ''] :
    pr.checks === 'fail' ? ['✗', 'bad'] : pr.checks === 'pending' ? ['●', 'wait'] : pr.checks === 'pass' ? ['✓', 'ok'] : ['', ''];
  const review = pr.state !== 'OPEN' ? '' : pr.review === 'APPROVED' ? ' approved' : pr.review === 'CHANGES_REQUESTED' ? ' changes asked' : '';
  return html`<span class=${'chip pr ' + st[1]} title="Open on GitHub" onClick=${(e) => { e.stopPropagation(); openLink(pr.url); }}>${pr.draft ? 'draft ' : ''}#${pr.number}${st[0] ? ' ' + st[0] : ''}${big ? review : ''}</span>`;
}

// preview is a message as one line: who said it, and its start without Markdown.
function preview(m, agent) {
  if (!m) return '';
  const text = m.text.replace(/```[\s\S]*?```/g, ' ').replace(/[#*`>|_]/g, '').replace(/\s+/g, ' ').trim();
  return (m.role === 'you' ? 'You: ' : m.role === 'error' ? '⚠ ' : agent + ': ') + text;
}

// filters are the chips above the list: what each keeps.
const filters = [
  ['all', 'All', () => true],
  ['working', 'Working', (t) => t.working],
  ['needs', 'Needs you', (t) => t.asks || unread.has(t.id)],
  ['pr', 'In PR', (t) => t.pr && t.pr.state === 'OPEN' || t.status === 'in-pr'],
  ['done', 'Done', (t) => t.status === 'done' || (t.pr && t.pr.state === 'MERGED')],
];

// Plan is the task's plan, its steps marked as the agent works through them.
function Plan({ id }) {
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/plan`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  if (!data) return html`<div class="scroll"></div>`;
  if (!data.plan) return html`<div class="scroll"><${Empty} title="No plan yet">${html`<p>Ask the agent for one in the chat, e.g. "Plan this before you change anything". The steps show here as it works through them.</p>`}<//></div>`;
  const marks = { ' ': '☐', x: '☑', '~': '◐', '!': '⚠' };
  return html`<div class="scroll"><div class="doc plan"><${Markdown} text=${data.plan.replace(/^(\s*[-*] )\[([ x~!])\]/gm, (_, b, m) => b + marks[m] + ' ')} /></div></div>`;
}

// Changes is the task's diff, a section per file, + and − lines coloured.
function Changes({ id }) {
  const { data, err, reload } = useLoad(() => api('GET', `/tasks/${id}/changes`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  if (err) return html`<div class="scroll"><div class="err">${err}</div></div>`;
  if (!data) return html`<div class="scroll"></div>`;
  const [stat, ...files] = data.diff.split(/^(?=diff --git )/m);
  return html`<div class="scroll"><div class="changes-head"><b>${data.summary}</b><button class="small" onClick=${reload}>↻ Refresh</button></div>
    ${files.map((f, i) => {
      const name = (f.match(/^diff --git a\/(.*?) b\//) || [, 'file'])[1];
      const add = (f.match(/^\+(?!\+\+)/gm) || []).length, del = (f.match(/^-(?!--)/gm) || []).length;
      return html`<details key=${i} open class="file"><summary>${name}<span class="stat"><span class="add">+${add}</span> <span class="del">−${del}</span></span></summary><pre class="diff">${f.split('\n').slice(1).map((l) =>
        html`<span class=${l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : ''}>${l}\n</span>`)}</pre></details>`;
    })}${!files.length && stat.trim() && html`<pre>${stat}</pre>`}</div>`;
}

// Starters are a few first messages for a task that hasn't started, to put in the box.
const starters = ['Read the code this touches and plan the change before editing anything.', 'Go ahead and do it, then commit.', 'Explain how this part of the code works first.'];

function Task({ task, onDeleted, edit }) {
  const [tab, setTab] = useState('chat');
  const [note, setNote] = useState('');
  useEffect(() => setTab('chat'), [task.id]);
  const say = (t) => { setNote(t); setTimeout(() => setNote((n) => (n === t ? '' : n)), 5000); };
  const del = async () => {
    const { loss } = await api('GET', `/tasks/${task.id}/delete`);
    const what = task.started ? (loss ? `Its worktree has ${loss}, which will be lost. ` : 'Its worktree and branch go too. ') : '';
    if (!(await ask(`Delete “${task.title}”? ${what}This can't be undone.`, 'Delete'))) return;
    try { const r = await api('POST', `/tasks/${task.id}/delete`); onDeleted(r.note); } catch (e) { say(e.message); }
  };
  const archive = () => api('POST', `/tasks/${task.id}/status`, { Status: task.archived ? 'unarchive' : 'archive' });
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || document.querySelector('.scrim')) return;
      if (e.key === 'e') { e.preventDefault(); edit(task); }
      if (e.key === 'c') setTab('chat');
      if (e.key === 'p') setTab('plan');
      if (e.key === 'd') setTab('changes');
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [task]);
  const spent = task.cost ? money(task.cost) + (task.budget ? ' of ' + money(task.budget) : '') : '';
  const sub = html`<${StatusMenu} task=${task} /><span class="meta">${base(task.repo)}${task.branch ? ' · ' + task.branch : ''} · ${task.agent}${task.model ? ' · ' + task.model : ''}${spent ? ' · ' + spent : ''}${task.plan ? ' · plan ' + task.plan : ''}</span>`;
  return html`<main class="page">
    <${Toolbar} title=${html`<span class="editable" title="Edit (e)" onClick=${() => edit(task)}>${task.title}</span>`} sub=${sub}>
      ${note && html`<span class="meta">${note}</span>`}
      <${PRChip} pr=${task.pr} big=${true} />
      ${task.asks && !task.working && html`<span class="chip wait">needs your answer</span>`}
      ${task.working && html`<span class="chip ok"><span class="dot work"></span> working</span>`}
      ${task.archived && html`<span class="chip">archived</span>`}
      <${Menu} label="⋯" title="More" items=${[
        { label: 'Edit name and description', key: 'e', run: () => edit(task) },
        { label: task.archived ? 'Unarchive' : 'Archive', run: archive },
        { label: 'Delete…', danger: true, run: del },
      ]} /><//>
    ${task.body && html`<div class="brief" title="What the agent is told about this task">${task.body}</div>`}
    <nav class="tabs">${[['chat', 'Chat', 'c'], ['plan', 'Plan' + (task.plan ? ' ' + task.plan : ''), 'p'], ['changes', 'Changes', 'd']].map(([k, l, key]) => html`<button key=${k} class=${tab === k ? 'on' : ''} title=${'(' + key + ')'} onClick=${() => setTab(k)}>${l}</button>`)}</nav>
    ${tab === 'chat' ? html`<${Chat} key=${task.id} id=${task.id} base=${'/tasks/' + task.id} working=${task.working} agent=${task.agent === 'gemini' ? 'Gemini' : 'Claude'}
        empty=${html`<div class="big">Say what to do</div><p>The first message sets up the task's own worktree and branch, then the agent starts.</p>
          <div class="starters">${starters.map((s) => html`<button key=${s} class="small" onClick=${() => { const b = document.querySelector('.compose textarea'); if (b) { b.value = s; b.dispatchEvent(new Event('input', { bubbles: true })); b.focus(); } }}>${s}</button>`)}</div>`} />`
      : tab === 'plan' ? html`<${Plan} key=${task.id} id=${task.id} />` : html`<${Changes} key=${task.id} id=${task.id} />`}
  </main>`;
}

// fold remembers the groups you folded, per browser.
const folds = (() => { try { return JSON.parse(localStorage.getItem('crew.tasks.folded') || '{}'); } catch { return {}; } })();
const saveFolds = () => { try { localStorage.setItem('crew.tasks.folded', JSON.stringify(folds)); } catch {} };

// Row is one task in the list: its status (or a pulse while it works), its title and when it last
// moved, the chat's last word, and chips for its repo, PR, plan and cost.
function Row({ t, on, open }) {
  const agent = t.agent === 'gemini' ? 'Gemini' : 'Claude';
  const needs = t.asks && !t.working;
  return html`<div class=${'trow' + (on ? ' on' : '') + (unread.has(t.id) ? ' unread' : '') + (t.archived ? ' dim' : '')} onClick=${() => open(t.id)}>
    <span class=${'status s-' + t.status}>${t.working ? html`<span class="dot work"></span>` : t.icon}</span>
    <div class="trow-main">
      <div class="trow-top"><span class="trow-title">${t.parent ? html`<span class="meta">↳ </span>` : ''}${t.title}</span>
        ${unread.has(t.id) && html`<span class="dot" title="new reply"></span>`}<span class="meta">${ago(t.updated)}</span></div>
      <div class="trow-last">${t.working ? html`<i>${agent} is working…</i>` : t.last ? preview(t.last, agent) : html`<i>not started</i>`}</div>
      <div class="trow-chips">
        <span class="chip">${base(t.repo)}${t.branch ? ' · ' + t.branch : ''}</span>
        <${PRChip} pr=${t.pr} />
        ${needs && html`<span class="chip wait">needs you</span>`}
        ${t.plan && html`<span class="chip">plan ${t.plan}</span>`}
        ${t.cost ? html`<span class="chip">${money(t.cost)}</span>` : ''}
      </div>
    </div>
    <button class="ghost small trow-act" title=${t.archived ? 'Unarchive' : 'Archive'} onClick=${(e) => { e.stopPropagation(); api('POST', `/tasks/${t.id}/status`, { Status: t.archived ? 'unarchive' : 'archive' }); }}>${t.archived ? '↩' : '⤓'}</button>
  </div>`;
}

export default function Tasks() {
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
  useEffect(() => { if (list && !list.some((t) => t.id === sel) && tasks.length) open(tasks[0].id); }, [list]);
  useEffect(() => { openID = sel; unread.delete(sel); }, [sel]);
  const group = (t) => (t.archived ? 'Archived' : t.label);
  const shown = tasks.filter((t) => !folds[group(t)]);
  useListKeys(shown.map((t) => t.id), sel, open);
  useEffect(() => {
    const k = (e) => { if (e.key === 'n' && !typing() && !e.metaKey && !document.querySelector('.scrim')) { e.preventDefault(); setForm('new'); } };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  const task = (list || []).find((t) => t.id === sel);
  const total = (list || []).reduce((n, t) => n + t.cost, 0);
  const groups = [];
  for (const t of tasks) {
    const g = group(t);
    if (!groups.length || groups[groups.length - 1].name !== g) groups.push({ name: g, items: [] });
    groups[groups.length - 1].items.push(t);
  }
  const fold = (g) => { folds[g] = !folds[g]; saveFolds(); bump((n) => n + 1); };
  return html`<div class="split">
    <section class="col tasks-col">
      <div class="col-head"><div><b>Tasks</b> <span class="meta">${(list || []).filter((t) => !t.archived).length}${total ? ' · ' + money(total) + ' spent' : ''}</span></div>
        <button class="primary small" title="New task (n)" onClick=${() => setForm('new')}>+ New task</button></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search tasks, repos, branches, #PR" /></div>
      <div class="filters">${filters.map(([k, label, f]) => {
        const n = k === 'all' ? 0 : (list || []).filter(f).length;
        return html`<button key=${k} class=${'fchip' + (filter === k ? ' on' : '')} onClick=${() => setFilter(k)}>${label}${n ? html` <span class="n">${n}</span>` : ''}</button>`;
      })}</div>
      <div class="col-list">${groups.length ? groups.map((g) => html`<div key=${g.name}>
          <button class="group fold" onClick=${() => fold(g.name)}><span class="caret">${folds[g.name] ? '▸' : '▾'}</span> ${g.name} <span class="n">${g.items.length}</span></button>
          ${!folds[g.name] && g.items.map((t) => html`<${Row} key=${t.id} t=${t} on=${t.id === sel} open=${open} />`)}</div>`)
        : html`<div class="col-empty">${list ? (q || filter !== 'all' ? 'Nothing matches.' : 'No tasks yet.') : 'Loading…'}</div>`}</div>
      <div class="col-foot"><label class="toggle"><input type="checkbox" checked=${archived} onChange=${(e) => setArchived(e.target.checked)} /> Show archived</label><span class="meta">↑↓ move · n new · / search</span></div>
    </section>
    ${task ? html`<${Task} task=${task} edit=${(t) => setForm(t)} onDeleted=${(n) => { setNote(n); setSel(''); reload(); }} />`
      : html`<main class="page"><${Empty} title=${list && list.length ? 'Pick a task' : 'Start your first task'}>${html`<p>${note || 'Each task gets its own worktree, branch and agent, so several can run at once.'}</p><button class="primary" onClick=${() => setForm('new')}>New task (n)</button>`}<//></main>`}
    ${form === 'new' && html`<${Form} title="New task" action="Create" onClose=${() => setForm(null)} initial=${{ Agent: 'claude', Repo: (repos.data || [])[0] || '' }}
      fields=${[
        { key: 'Title', label: 'Name', placeholder: 'What to do, in a few words' },
        { key: 'Repo', label: 'Repo folder', type: 'combo', options: repos.data || [], placeholder: '~/code/project', hint: 'an attached repo, or any git folder' },
        { key: 'Body', label: 'Description', type: 'textarea', placeholder: 'What the agent should know: the goal, links, constraints' },
        { key: 'Agent', label: 'Agent', type: 'select', options: [{ value: 'claude', label: 'Claude Code' }, { value: 'gemini', label: 'Gemini CLI' }] },
        { key: 'Model', label: 'Model', type: 'combo', options: ['opus', 'sonnet', 'haiku'], placeholder: "the agent's default", hint: 'optional' },
        { key: 'Branch', label: 'Branch', placeholder: 'leave empty for a new branch', hint: 'optional: work on an existing branch or PR' },
      ]}
      submit=${async (v) => { const t = await api('POST', '/tasks', v); await reload(); open(t.id); }} />`}
    ${form && form !== 'new' && html`<${Form} title="Edit task" onClose=${() => setForm(null)} initial=${{ Title: form.title, Body: form.body }}
      fields=${[{ key: 'Title', label: 'Name' }, { key: 'Body', label: 'Description', type: 'textarea', rows: 8, hint: "a new session of the agent is told this" }]}
      submit=${(v) => api('POST', `/tasks/${form.id}/edit`, v)} />`}
  </div>`;
}
