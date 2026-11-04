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
