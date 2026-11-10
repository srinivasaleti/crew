// The open task: its header (title, details, PR, status, ⋯), the brief, and its Chat, Plan and Changes tabs.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, base, ago, ask, Chat, Empty, Toolbar, Markdown, Menu, Summary } from '../../lib.js';
import { money, typing, PRChip, deleteTask } from './common.js';
import { Changes } from './changes.js';
import { Plan } from './plan.js';

// StatusMenu is the task's status chip; a click lists the statuses to pick.
function StatusMenu({ task }) {
  const [list, setList] = useState([]);
  useEffect(() => { api('GET', '/tasks/statuses').then(setList); }, []);
  const pick = (k) => api('POST', `/tasks/${task.id}/status`, { Status: k });
  return html`<${Menu} label=${html`<span class=${'s-' + task.status}>${task.icon}</span> ${task.label} ▾`} title="Status (s)"
    items=${list.map((s) => ({ label: html`<span class=${'s-' + s.key}>${s.icon}</span> ${s.label}`, run: () => pick(s.key) }))} />`;
}

// ModelMenu is the task's model button: the model its agent uses from the next message on (/model in
// the chat does the same). The list and which one is on are crew's (core.Models).
function ModelMenu({ task }) {
  const [m, setM] = useState(null);
  useEffect(() => { api('GET', `/tasks/${task.id}/models`).then(setM); }, [task.id, task.model, task.agent]);
  if (!m) return '';
  const cur = m.choices[m.current];
  const label = cur && cur.value ? cur.label : m.model || 'Default model';
  return html`<${Menu} label=${'◇ ' + label + ' ▾'} title="Model: from the next message on (/model)"
    items=${m.choices.map((c, i) => ({ label: (i === m.current ? '✓ ' : '') + c.label, key: c.value ? '' : "the agent's own", run: () => api('POST', `/tasks/${task.id}/model`, { Model: c.value || 'default' }) }))} />`;
}

// RepoMenu is the task's repo button: move it to another repo (core.SetRepo). The box filters the
// attached ones by name or path; a typed path or "Choose folder…" (the Mac's picker) moves it to any
// git folder, which the server attaches. Once it has started, its next message starts over there, so it asks first.
function RepoMenu({ task, say }) {
  const [repos, setRepos] = useState([]);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const load = () => api('GET', '/tasks/repos').then(setRepos).catch(() => {});
  useEffect(() => { load(); }, []);
  useEffect(() => {
    if (!open) return;
    setQ(''); setSel(0); load();
    const close = () => setOpen(false);
    setTimeout(() => addEventListener('click', close), 0);
    return () => removeEventListener('click', close);
  }, [open]);
  const move = async (r) => {
    setOpen(false);
    if (!r || r === task.repo) return;
    if (task.started && !(await ask(`Move to ${base(r)}? Its worktree here is removed, and its next message starts fresh in ${base(r)} (the chat and plan stay).`, 'Move'))) return;
    api('POST', `/tasks/${task.id}/repo`, { Repo: r }).then(load, (e) => say(e.message));
  };
  const choose = () => { setOpen(false); api('POST', '/tasks/choose-folder').then((r) => move(r.path)).catch((e) => say(e.message)); };
  // Every word typed must be in the name or path; names that start with it come first.
  const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const hits = repos.filter((r) => words.every((w) => r.toLowerCase().includes(w)))
    .sort((a, b) => (base(b).toLowerCase().startsWith(words[0] || '') - base(a).toLowerCase().startsWith(words[0] || '')));
  const items = [
    ...hits.map((r) => ({ label: (r === task.repo ? '✓ ' : '') + base(r), key: r, run: () => move(r) })),
    ...(/[\/~]/.test(q.trim()) && !repos.includes(q.trim()) ? [{ label: 'Add ' + q.trim(), key: 'a git folder', run: () => move(q.trim()) }] : []),
    { label: 'Choose folder…', key: 'any git folder', run: choose },
  ];
  const keys = (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSel((sel + 1) % items.length); }
    if (e.key === 'ArrowUp') { e.preventDefault(); setSel((sel - 1 + items.length) % items.length); }
    if (e.key === 'Enter') { e.preventDefault(); items[Math.min(sel, items.length - 1)].run(); }
    if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); }
  };
  return html`<span class="menu-wrap"><button title="Repo: move the task to another one" onClick=${() => setOpen(!open)}>${'▣ ' + base(task.repo) + ' ▾'}</button>
    ${open && html`<div class="menu right repo-menu" onClick=${(e) => e.stopPropagation()}>
      <input autoFocus value=${q} placeholder="Search repos, or type a path" onInput=${(e) => { setQ(e.target.value); setSel(0); }} onKeyDown=${keys} />
      <div class="repo-list">${items.map((it, i) => html`<div key=${it.label} class=${'menu-item' + (i === sel ? ' on' : '')} onMouseEnter=${() => setSel(i)} onClick=${it.run}>${it.label}<span class="meta">${it.key}</span></div>`)}</div>
    </div>`}</span>`;
}

// Starters are a few first messages for a task that hasn't started, to put in the box.
const starters = ['Read the code this touches and plan the change before editing anything.', 'Go ahead and do it, then commit.', 'Explain how this part of the code works first.'];

export function Task({ task, onDeleted, edit }) {
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
      if (e.key === 'u') setTab('summary');
      if (e.key === 'd') setTab('changes');
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [task]);
  const spent = task.cost ? money(task.cost) + (task.budget ? ' of ' + money(task.budget) : '') : '';
  const sub = [base(task.repo) + (task.branch ? ' / ' + task.branch : ''), task.agent, spent, task.plan && 'plan ' + task.plan].filter(Boolean).join('  ·  ');
  return html`<main class="page task-page" tabIndex="-1">
    <${Toolbar} title=${html`<span class="editable" title="Edit (e)" onClick=${() => edit(task)}>${task.title}</span>`} sub=${sub}>
      ${note && html`<span class="meta">${note}</span>`}
      ${task.asks && !task.working && html`<span class="chip wait">needs your answer</span>`}
      ${task.working && html`<span class="chip ok"><span class="dot work"></span> working</span>`}
      ${task.archived && html`<span class="chip">archived</span>`}
      <${PRChip} pr=${task.pr} big=${true} />
      <${RepoMenu} task=${task} say=${say} />
      <${ModelMenu} task=${task} />
      <${StatusMenu} task=${task} />
      <${Menu} label="⋯" title="More" items=${[
        { label: 'Edit name and description', key: 'e', run: () => edit(task) },
        { label: task.archived ? 'Unarchive' : 'Archive', run: archive },
        { label: 'Delete…', danger: true, run: del },
      ]} /><//>
    ${task.body && html`<div class="brief" title="What the agent is told about this task">${task.body}</div>`}
    <nav class="tabs">${[['chat', 'Chat', 'c'], ['plan', 'Plan' + (task.plan ? ' ' + task.plan : ''), 'p'], ['summary', 'Summary', 'u'], ['changes', 'Changes', 'd']].map(([k, l, key]) => html`<button key=${k} class=${tab === k ? 'on' : ''} title=${'(' + key + ')'} onClick=${() => setTab(k)}>${l}</button>`)}</nav>
    ${tab === 'chat' ? html`<${Chat} key=${task.id} id=${task.id} base=${'/tasks/' + task.id} commands working=${task.working} agent=${task.agent === 'gemini' ? 'Gemini' : 'Claude'}
        empty=${html`<div class="big">Say what to do</div><p>The first message sets up the task's own worktree and branch, then the agent starts.</p>
          <div class="starters">${starters.map((s) => html`<button key=${s} class="small" onClick=${() => { const b = document.querySelector('.compose textarea'); if (b) { b.value = s; b.dispatchEvent(new Event('input', { bubbles: true })); b.focus(); } }}>${s}</button>`)}</div>`} />`
      : tab === 'plan' ? html`<${Plan} key=${task.id} task=${task} />` : tab === 'summary' ? html`<${Summary} key=${task.id} id=${task.id} working=${task.working} />` : html`<${Changes} key=${task.id} id=${task.id} />`}
  </main>`;
}
