// TASKS: the tasks by status on the left, the open task's chat on the right.
import { html, useState, useEffect, api, on, useEvents, useLoad, base, Chat, Form, Empty, Search, useListKeys, Toolbar, Markdown } from '../lib.js';

// The page's own styles, once.
if (!document.querySelector('link[href="sections/tasks.css"]')) document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="sections/tasks.css">');

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

// StatusMenu is the task's status chip; a click lists the statuses to pick.
function StatusMenu({ task }) {
  const [open, setOpen] = useState(false);
  const [list, setList] = useState([]);
  useEffect(() => { if (open && !list.length) api('GET', '/tasks/statuses').then(setList); }, [open]);
  const pick = (k) => { setOpen(false); api('POST', `/tasks/${task.id}/status`, { Status: k }); };
  return html`<span class="menu-wrap"><button class=${'chip s-' + task.status} onClick=${() => setOpen(!open)}>${task.icon} ${task.label} ▾</button>
    ${open && html`<div class="menu" onMouseLeave=${() => setOpen(false)}>${list.map((s) => html`<div key=${s.key} class=${'menu-item s-' + s.key} onClick=${() => pick(s.key)}>${s.icon} ${s.label}</div>`)}</div>`}</span>`;
}

// Plan is the task's plan, its steps marked as the agent works through them.
function Plan({ id }) {
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/plan`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  if (!data) return html`<div class="scroll"></div>`;
  if (!data.plan) return html`<div class="scroll"><${Empty} title="No plan yet">${html`<p>Ask the agent to plan in the chat (in the terminal UI, /plan writes one with Claude's planning skill).</p>`}<//></div>`;
  const marks = { ' ': '☐', x: '☑', '~': '◐', '!': '⚠' };
  return html`<div class="scroll plan"><${Markdown} text=${data.plan.replace(/^(\s*[-*] )\[([ x~!])\]/gm, (_, b, m) => b + marks[m] + ' ')} /></div>`;
}

// Changes is the task's diff, a section per file, + and − lines coloured.
function Changes({ id }) {
  const { data, err, reload } = useLoad(() => api('GET', `/tasks/${id}/changes`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  if (err) return html`<div class="scroll"><div class="err">${err}</div></div>`;
  if (!data) return html`<div class="scroll"></div>`;
  const [stat, ...files] = data.diff.split(/^(?=diff --git )/m);
  return html`<div class="scroll"><div class="changes-head"><b>${data.summary}</b><button class="small" onClick=${reload}>Refresh</button></div>
    ${files.map((f, i) => {
      const name = (f.match(/^diff --git a\/(.*?) b\//) || [, 'file'])[1];
      return html`<details key=${i} open class="file"><summary>${name}</summary><pre class="diff">${f.split('\n').slice(1).map((l) =>
        html`<span class=${l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : ''}>${l}\n</span>`)}</pre></details>`;
    })}${!files.length && stat.trim() && html`<pre>${stat}</pre>`}</div>`;
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
  const [tab, setTab] = useState('chat');
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
        <${Toolbar} title=${task.title} sub=${html`<${StatusMenu} task=${task} /> ${base(task.repo)}${task.branch ? ' · ' + task.branch : ''} · ${task.agent}${task.model ? ' · ' + task.model : ''}`}>
          ${task.working && html`<span class="chip"><span class="dot work"></span> working</span>`}<//>
        <nav class="tabs">${[['chat', 'Chat'], ['plan', 'Plan'], ['changes', 'Changes']].map(([k, l]) => html`<button key=${k} class=${tab === k ? 'on' : ''} onClick=${() => setTab(k)}>${l}</button>`)}</nav>
        ${tab === 'chat' ? html`<${Chat} key=${task.id} id=${task.id} base=${'/tasks/' + task.id} working=${task.working} agent=${task.agent === 'gemini' ? 'Gemini' : 'Claude'}
          empty="Say what to do. The first message sets up the task's worktree and branch." />`
          : tab === 'plan' ? html`<${Plan} key=${task.id} id=${task.id} />` : html`<${Changes} key=${task.id} id=${task.id} />`}</main>`
      : html`<main class="page"><${Empty} title=${list && list.length ? 'Pick a task' : 'No tasks yet'}>${html`<p>Each task gets its own worktree, branch and agent.</p><button class="primary" onClick=${() => setAdding(true)}>New task (n)</button>`}<//></main>`}
    ${adding && html`<${Form} title="New task" action="Create" onClose=${() => setAdding(false)}
      fields=${[{ key: 'Title', label: 'Name' }, { key: 'Repo', label: 'Repo folder', placeholder: '~/code/project' }, { key: 'Branch', label: 'Branch (optional: an existing one)' }, { key: 'Body', label: 'Description', type: 'textarea' }]}
      submit=${async (v) => { const t = await api('POST', '/tasks', v); await reload(); open(t.id); }} />`}
  </div>`;
}
