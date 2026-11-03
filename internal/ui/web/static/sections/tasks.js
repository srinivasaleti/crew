// TASKS: the tasks by status on the left (with what each has cost, its plan's progress, when it last
// moved), and the open task on the right: its chat, plan and changes, its status, and editing,
// archiving or deleting it.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, base, ago, ask, openLink, Chat, Form, Empty, Search, useListKeys, Toolbar, Markdown } from '../lib.js';
import { ShellPane } from './shell.js';

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

// PRChip is a task's PR. Small (a row): its number and ✓ ✗ ● for its checks. Big (the task's header):
// the same in words, with the review.
function PRChip({ pr, big }) {
  if (!pr) return '';
  const tone = pr.state === 'MERGED' ? 'merged' : pr.state === 'CLOSED' ? '' : pr.checks === 'fail' ? 'bad' : pr.checks === 'pending' ? 'wait' : pr.checks === 'pass' ? 'ok' : '';
  const go = (e) => { e.stopPropagation(); openLink(pr.url); };
  if (!big) {
    const mark = pr.state === 'MERGED' ? 'merged' : pr.state === 'CLOSED' ? 'closed' : { fail: '✗', pending: '●', pass: '✓' }[pr.checks] || '';
    return html`<span class=${'chip pr ' + tone} title="Open on GitHub" onClick=${go}>${pr.draft ? 'draft ' : ''}#${pr.number}${mark ? ' ' + mark : ''}</span>`;
  }
  const parts = [(pr.draft ? 'Draft PR #' : 'PR #') + pr.number];
  if (pr.state !== 'OPEN') parts.push(pr.state.toLowerCase());
  else {
    if (pr.checks) parts.push({ fail: 'checks failed', pending: 'checks running', pass: 'checks pass' }[pr.checks]);
    if (pr.review === 'APPROVED') parts.push('approved');
    if (pr.review === 'CHANGES_REQUESTED') parts.push('changes requested');
  }
  const look = pr.state === 'OPEN' && pr.review === 'CHANGES_REQUESTED' && tone !== 'bad' ? 'wait' : tone; // asked changes outrank green checks
  return html`<button class=${'chip pr big ' + look} title="Open on GitHub" onClick=${go}>${parts.join(' · ')} ↗</button>`;
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

// deleteTask asks first, saying what the worktree would lose, then deletes the task. It gives the
// note to show, or null when you said no.
async function deleteTask(task) {
  const { loss } = await api('GET', `/tasks/${task.id}/delete`);
  const what = task.started ? (loss ? `Its worktree has ${loss}, which will be lost. ` : 'Its worktree and branch go too. ') : '';
  if (!(await ask(`Delete “${task.title}”? ${what}This can't be undone.`, 'Delete'))) return null;
  return (await api('POST', `/tasks/${task.id}/delete`)).note || '';
}

const trash = html`<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 4h11M6 4V2.5h4V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5"/></svg>`;

function Task({ task, onDeleted, edit }) {
  const [tab, setTab] = useState('chat');
  const [note, setNote] = useState('');
  useEffect(() => setTab('chat'), [task.id]);
  const say = (t) => { setNote(t); setTimeout(() => setNote((n) => (n === t ? '' : n)), 5000); };
  const del = () => deleteTask(task).then((n) => n !== null && onDeleted(n), (e) => say(e.message));
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
  const sub = [base(task.repo) + (task.branch ? ' / ' + task.branch : ''), task.agent + (task.model ? ' · ' + task.model : ''), spent, task.plan && 'plan ' + task.plan].filter(Boolean).join('  ·  ');
  return html`<main class="page task-page" tabIndex="-1">
    <${Toolbar} title=${html`<span class="editable" title="Edit (e)" onClick=${() => edit(task)}>${task.title}</span>`} sub=${sub}>
      ${note && html`<span class="meta">${note}</span>`}
      ${task.asks && !task.working && html`<span class="chip wait">needs your answer</span>`}
      ${task.working && html`<span class="chip ok"><span class="dot work"></span> working</span>`}
      ${task.archived && html`<span class="chip">archived</span>`}
      <${PRChip} pr=${task.pr} big=${true} />
      <${StatusMenu} task=${task} />
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
// moved, the chat's last word, then a quiet line of where it runs and what it cost, with chips only
// for what wants a look (its PR, a question for you).
function Row({ t, on, open, del }) {
  const agent = t.agent === 'gemini' ? 'Gemini' : 'Claude';
  const needs = t.asks && !t.working;
  const last = t.working ? '' : t.last ? preview(t.last, agent) : '';
  const where = [base(t.repo) + (t.branch ? ' · ' + t.branch : ''), t.plan && 'plan ' + t.plan, money(t.cost)].filter(Boolean).join(' · ');
  const cls = 'trow' + (on ? ' on' : '') + (unread.has(t.id) ? ' unread' : '') + (t.archived ? ' dim' : '') + (t.status === 'done' ? ' done' : '');
  return html`<div class=${cls} onClick=${() => open(t.id)}>
    <span class=${'status s-' + t.status}>${t.working ? html`<span class="dot work"></span>` : t.icon}</span>
    <div class="trow-main">
      <div class="trow-top"><span class="trow-title" title=${t.title}>${t.parent ? html`<span class="meta">↳ </span>` : ''}${t.title}</span>
        ${unread.has(t.id) && html`<span class="dot" title="new reply"></span>`}<span class="meta trow-ago">${ago(t.updated)}</span>
        <button class="ghost small trow-del" title="Delete task" onClick=${(e) => { e.stopPropagation(); del(t); }}>${trash}</button></div>
      <div class="trow-last" title=${last}>${t.working ? html`<i>${agent} is working…</i>` : last || html`<i>not started</i>`}</div>
      <div class="trow-foot"><span class="trow-where">${where}</span><${PRChip} pr=${t.pr} />${needs && html`<span class="chip wait">needs you</span>`}</div>
    </div>
  </div>`;
}

// Board is the tasks in a lane per status. Drag a card to another lane to change its status; a
// click opens the task.
// ponytail: no order within a lane (task files have none); add an order field if you want to rank cards.
function Board({ tasks, open, del }) {
  const { data: statuses } = useLoad(() => api('GET', '/tasks/statuses'));
  const [over, setOver] = useState('');
  const drop = (e, key) => {
    e.preventDefault();
    setOver('');
    const id = e.dataTransfer.getData('text/plain'), t = tasks.find((x) => x.id === id);
    if (t && t.status !== key) api('POST', `/tasks/${id}/status`, { Status: key });
  };
  return html`<div class="board">${(statuses || []).map((s) => {
    const items = tasks.filter((t) => t.status === s.key);
    return html`<section key=${s.key} class=${'lane' + (over === s.key ? ' over' : '')}
        onDragOver=${(e) => { e.preventDefault(); setOver(s.key); }} onDragLeave=${(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(''); }} onDrop=${(e) => drop(e, s.key)}>
      <div class="lane-head"><span class=${'s-' + s.key}>${s.icon}</span> ${s.label} <span class="n">${items.length}</span></div>
      <div class="lane-list">${items.map((t) => html`<div key=${t.id} class="tcard" draggable="true" onDragStart=${(e) => e.dataTransfer.setData('text/plain', t.id)}>
        <${Row} t=${t} on=${false} open=${open} del=${del} /></div>`)}</div>
    </section>`;
  })}</div>`;
}

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
