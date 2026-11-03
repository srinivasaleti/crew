// The open task: its header (title, details, PR, status, ⋯), the brief, and its Chat, Plan and Changes tabs.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, base, ago, ask, Chat, Empty, Toolbar, Markdown, Menu } from '../../lib.js';
import { money, typing, PRChip, deleteTask } from './common.js';
import { Changes } from './changes.js';

// StatusMenu is the task's status chip; a click lists the statuses to pick.
function StatusMenu({ task }) {
  const [list, setList] = useState([]);
  useEffect(() => { api('GET', '/tasks/statuses').then(setList); }, []);
  const pick = (k) => api('POST', `/tasks/${task.id}/status`, { Status: k });
  return html`<${Menu} label=${html`<span class=${'s-' + task.status}>${task.icon}</span> ${task.label} ▾`} title="Status (s)"
    items=${list.map((s) => ({ label: html`<span class=${'s-' + s.key}>${s.icon}</span> ${s.label}`, run: () => pick(s.key) }))} />`;
}

// Plan is the task's plan, its steps marked as the agent works through them.
// Write plan (or Rewrite) and the box under it go to Claude in the task's own chat, with crew's plan
// skill (core.PlanTurn): with no plan the box is what to plan; with one, a change or your answers to
// its questions. Claude's last reply shows above the box, so the talk stays on this tab.
function Plan({ task }) {
  const id = task.id;
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/plan`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const has = !!(data && data.plan);
  const send = (ask) => { setErr(''); api('POST', `/tasks/${id}/plan`, { Ask: ask }).then(() => setText(''), (e) => setErr(e.message)); };
  const key = (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(text); } };
  const marks = { ' ': '☐', x: '☑', '~': '◐', '!': '⚠' };
  const reply = task.last && task.last.role === 'agent' ? task.last.text : '';
  return html`<div class="plan-tab">
    <div class="plan-head">
      <button class=${has ? 'small' : 'primary small'} disabled=${task.working} title=${has ? 'Claude writes the plan again from the task' : 'Claude reads the code and writes a plan; it changes no code'}
        onClick=${async () => { if (!has || (await ask('Write the plan again from the task? Claude replaces the current plan.', 'Rewrite'))) send(has ? '' : text); }}>${has ? 'Rewrite plan' : 'Write plan'}</button>
      ${task.working ? html`<span class="meta"><span class="dot work"></span> Claude is working…</span>` : html`<span class="meta">${has ? 'Claude marks the steps as it works through them' : 'Read first, then plan: no code changes'}</span>`}
    </div>
    <div class="scroll">${!data ? '' : has
      ? html`<div class="doc plan"><${Markdown} text=${data.plan.replace(/^(\s*[-*] )\[([ x~!])\]/gm, (_, b, m) => b + marks[m] + ' ')} /></div>`
      : html`<${Empty} title="No plan yet">${html`<p>Write plan has Claude read the code and write a short checklist for this task. Say what to plan in the box below, or leave it empty to plan the whole task.</p>`}<//>`}</div>
    ${reply && html`<div class="plan-reply"><span class="meta">Claude</span><${Markdown} text=${reply} /></div>`}
    <div class="compose"><form onSubmit=${(e) => { e.preventDefault(); send(text); }}>
      <textarea rows="2" value=${text} onInput=${(e) => setText(e.target.value)} onKeyDown=${key}
        placeholder=${has ? 'Ask Claude to change the plan, or answer its questions…' : 'What to plan (optional: empty plans the whole task)'} />
      <button class="primary">${has ? 'Send' : 'Plan'}</button></form>
      <div class="hint">${err ? html`<span class="err">${err}</span>` : '⏎ sends · ⇧⏎ new line · goes to the task\'s chat too'}</div></div>
  </div>`;
}

// Summary is the task explained for someone catching up: what it's for, where it stands, what changed
// and how it works, with diagrams. Claude writes it with crew's summary skill (core.SummaryTurn) from
// the diff, the plan and the chat; Update writes it again as the work moves on.
function Summary({ task }) {
  const id = task.id;
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/summary`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  const [err, setErr] = useState('');
  const [copied, setCopied] = useState(false);
  const doc = useRef();
  const has = !!(data && data.summary);
  const write = () => { setErr(''); api('POST', `/tasks/${id}/summary`).catch((e) => setErr(e.message)); };
  const copy = () => navigator.clipboard.writeText(data.summary).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); });
  // The sections, for the chips: the intro (what it is), then each ## heading, in order.
  const sections = has ? ['What it is', ...[...data.summary.matchAll(/^## (.+)$/gm)].map((m) => m[1].trim())] : [];
  const jump = (i) => {
    const el = i === 0 ? doc.current : doc.current && doc.current.querySelectorAll('.h2')[i - 1];
    el && el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };
  return html`<div class="plan-tab">
    <div class="plan-head">
      <button class=${has ? 'small' : 'primary small'} disabled=${task.working} onClick=${write}
        title="Claude reads the diff, the plan and the chat, then writes the summary; it changes no code">${has ? '↻ Update' : 'Write summary'}</button>
      ${task.working ? html`<span class="meta"><span class="dot work"></span> Claude is working…</span>`
        : err ? html`<span class="err">${err}</span>`
        : html`<span class="meta">${has ? (data.at ? 'Written ' + (ago(data.at) === 'now' ? 'just now' : ago(data.at) + ' ago') : '') : 'What it is, how it behaves, where it stands, what changed'}</span>`}
      ${has && html`<span class="grow"></span><button class="small" title="Copy as Markdown, for a PR or a message" onClick=${copy}>${copied ? '✓ Copied' : 'Copy'}</button>`}
    </div>
    ${sections.length > 1 && html`<nav class="sum-nav">${sections.map((s, i) => html`<button key=${i} class="fchip" onClick=${() => jump(i)}>${s}</button>`)}</nav>`}
    <div class="scroll">${!data ? '' : has ? html`<div class="doc summary" ref=${doc}><${Markdown} text=${data.summary} /></div>`
      : html`<${Empty} title="No summary yet">${html`<p>Write summary has Claude read the task's diff, plan and chat and explain it: what it's for, where it stands, what changed, and how it works, drawn as diagrams.</p>`}<//>`}</div>
  </div>`;
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
    <nav class="tabs">${[['chat', 'Chat', 'c'], ['plan', 'Plan' + (task.plan ? ' ' + task.plan : ''), 'p'], ['summary', 'Summary', 'u'], ['changes', 'Changes', 'd']].map(([k, l, key]) => html`<button key=${k} class=${tab === k ? 'on' : ''} title=${'(' + key + ')'} onClick=${() => setTab(k)}>${l}</button>`)}</nav>
    ${tab === 'chat' ? html`<${Chat} key=${task.id} id=${task.id} base=${'/tasks/' + task.id} working=${task.working} agent=${task.agent === 'gemini' ? 'Gemini' : 'Claude'}
        empty=${html`<div class="big">Say what to do</div><p>The first message sets up the task's own worktree and branch, then the agent starts.</p>
          <div class="starters">${starters.map((s) => html`<button key=${s} class="small" onClick=${() => { const b = document.querySelector('.compose textarea'); if (b) { b.value = s; b.dispatchEvent(new Event('input', { bubbles: true })); b.focus(); } }}>${s}</button>`)}</div>`} />`
      : tab === 'plan' ? html`<${Plan} key=${task.id} task=${task} />` : tab === 'summary' ? html`<${Summary} key=${task.id} task=${task} />` : html`<${Changes} key=${task.id} id=${task.id} />`}
  </main>`;
}
