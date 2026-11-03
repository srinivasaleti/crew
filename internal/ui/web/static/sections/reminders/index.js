// REMINDERS: a quick-add line on top, the reminders by what they'll do next (Upcoming, Repeating,
// Paused, Done), and a Chat tab where Claude manages them with `crew remind` (locked down to that).
// Parsing, firing and the lock-down are crew's (internal/reminders); this draws and sends.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, Chat, Form, Empty, Toolbar, css } from '../../lib.js';

css(import.meta.url);

const alertFor = 10 * 60000; // a reminder that fired stays on the page this long, as on the terminal's help line
let fired = []; // [{text, at}] that fired lately, kept while you're on another section

// watch counts the reminders still due today for the rail's badge, and keeps what fires.
export function watch(setBadge) {
  const count = () => api('GET', '/reminders').then(({ reminders }) => {
    const end = new Date();
    end.setHours(23, 59, 59);
    setBadge(reminders.filter((r) => !r.done && !r.paused && new Date(r.at) <= end).length || '');
  });
  count();
  const t = setInterval(count, 60000);
  const off = on((ev) => {
    if (ev.kind !== 'reminders') return;
    if (ev.text) fired = ev.text.split('\n').map((text) => ({ text, at: Date.now() })).concat(fired);
    count();
  });
  return () => { clearInterval(t); off(); };
}

// rel is how far away a time is: "in 5m", "in 3h", "in 2d", "overdue".
function rel(at) {
  const m = Math.round((new Date(at) - Date.now()) / 60000);
  if (m < 0) return 'overdue';
  if (m < 60) return 'in ' + Math.max(m, 1) + 'm';
  if (m < 48 * 60) return 'in ' + Math.round(m / 60) + 'h';
  return 'in ' + Math.round(m / 1440) + 'd';
}

// QuickAdd takes one line ("stand-up at 4pm every weekday") and shows what it makes as you type.
function QuickAdd({ onAdded }) {
  const [text, setText] = useState('');
  const [preview, setPreview] = useState(null);
  const [err, setErr] = useState('');
  const box = useRef();
  useEffect(() => {
    if (!text.trim()) { setPreview(null); setErr(''); return; }
    const t = setTimeout(() => api('POST', '/reminders/preview', { Quick: text }).then((p) => { setPreview(p); setErr(''); }, (e) => { setPreview(null); setErr(e.message); }), 200);
    return () => clearTimeout(t);
  }, [text]);
  useEffect(() => {
    const k = (e) => { if (e.key === 'n' && !/input|textarea|select/i.test(document.activeElement.tagName) && !e.metaKey) { e.preventDefault(); box.current && box.current.focus(); } };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  const add = async (e) => {
    e.preventDefault();
    try { await api('POST', '/reminders', { Quick: text }); setText(''); onAdded(); } catch (x) { setErr(x.message); }
  };
  return html`<form class="rem-quick" onSubmit=${add}>
    <input ref=${box} placeholder="Remind me… stand-up at 4pm every weekday (n)" value=${text} onInput=${(e) => setText(e.target.value)} />
    <button class="primary" disabled=${!preview}>Add</button>
    <div class="rem-preview">${preview ? html`<b>${preview.text}</b> · ${preview.when} <span class="meta">(${rel(preview.at)})</span>${preview.every ? html` · ${preview.repeats}` : ''}`
      : err && text.trim() ? html`<span class="meta">${err}</span>` : html`<span class="meta">Try: call mom tomorrow 9am · stretch in 30m · pay rent nov 1 every month</span>`}</div>
  </form>`;
}

// Row is one reminder, with its actions on hover (and as keys on the selected one).
function Row({ r, on: selected, select, act, edit }) {
  return html`<div class=${'rem-row' + (selected ? ' on' : '') + (r.done ? ' done' : '')} onClick=${select} onDblClick=${edit}>
    <div class="rem-when"><div>${r.done ? 'fired ' + r.when : r.when}</div><div class="meta">${r.done ? '' : r.paused ? 'paused' : rel(r.at)}</div></div>
    <div class="rem-what"><div class="rem-text">${r.text}</div>${r.every && html`<div class="meta">↻ ${r.repeats}${r.next && !r.paused ? ' · then ' + r.next : ''}</div>`}</div>
    <div class="rem-acts">
      <button class="small" title="Edit (e)" onClick=${(e) => { e.stopPropagation(); edit(); }}>Edit</button>
      ${!r.done && html`<button class="small" title=${r.paused ? 'Resume (p)' : 'Pause (p)'} onClick=${(e) => { e.stopPropagation(); act(r.paused ? 'resume' : 'pause'); }}>${r.paused ? 'Resume' : 'Pause'}</button>`}
      <button class="small danger" title="Delete (x)" onClick=${(e) => { e.stopPropagation(); act('delete'); }}>Delete</button>
    </div></div>`;
}

function List() {
  const { data, reload } = useLoad(() => api('GET', '/reminders'));
  const [sel, setSel] = useState(null);
  const [editing, setEditing] = useState(null);
  const [undo, setUndo] = useState(null); // a reminder just deleted, to bring back
  const [, tick] = useState(0);
  useEvents((ev) => { if (ev.kind === 'reminders' || ev.kind === 'open') reload(); }, []);
  useEffect(() => { const t = setInterval(() => tick((n) => n + 1), 30000); return () => clearInterval(t); }, []); // "in 5m" moves on
  const rs = (data && data.reminders) || [];
  const groups = [
    ['Upcoming', rs.filter((r) => !r.done && !r.paused && !r.every)],
    ['Repeating', rs.filter((r) => !r.done && !r.paused && r.every)],
    ['Paused', rs.filter((r) => !r.done && r.paused)],
    ['Done', rs.filter((r) => r.done).reverse()],
  ].filter(([, l]) => l.length);
  const order = groups.flatMap(([, l]) => l.map((r) => r.id));
  const act = async (r, action) => {
    await api('POST', `/reminders/${r.id}/${action}`);
    if (action === 'delete') setUndo(r);
    reload();
  };
  useEffect(() => {
    const k = (e) => {
      if (/input|textarea|select/i.test(document.activeElement.tagName) || e.metaKey || editing) return;
      const i = order.indexOf(sel);
      const r = rs.find((x) => x.id === sel);
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); setSel(order[Math.min(i + 1, order.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); setSel(order[Math.max(i - 1, 0)]); }
      if (!r) return;
      if (e.key === 'e' || e.key === 'Enter') { e.preventDefault(); setEditing(r); }
      if (e.key === 'p' && !r.done) act(r, r.paused ? 'resume' : 'pause');
      if (e.key === 'x' || e.key === 'Backspace') act(r, 'delete');
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [order.join(), sel, editing]);
  const recent = fired.filter((f) => Date.now() - f.at < alertFor);
  return html`<div class="scroll rem">
    ${recent.map((f, i) => html`<div class="rem-fired" key=${i}>◷ Reminder: <b>${f.text}</b><span class="meta"> · ${Math.max(1, Math.round((Date.now() - f.at) / 60000))}m ago</span>
      <button class="ghost small" onClick=${() => { fired = fired.filter((x) => x !== f); tick((n) => n + 1); }}>✕</button></div>`)}
    <${QuickAdd} onAdded=${reload} />
    ${undo && html`<div class="rem-undo">Deleted “${undo.text}” <button class="small" onClick=${async () => { await api('POST', `/reminders/${undo.id}/restore`); setUndo(null); reload(); }}>Undo</button>
      <button class="ghost small" onClick=${() => setUndo(null)}>✕</button></div>`}
    ${!data ? html`<div class="meta">Loading…</div>` : groups.length === 0 ? html`<${Empty} title="No reminders">Type one above, or ask Claude in the Chat tab.<//>`
      : groups.map(([name, l]) => html`<div key=${name}><div class="group">${name} · ${l.length}</div>
          ${l.map((r) => html`<${Row} key=${r.id} r=${r} on=${r.id === sel} select=${() => setSel(r.id)} act=${(a) => act(r, a)} edit=${() => setEditing(r)} />`)}</div>`)}
    <div class="hint">↑↓ choose · e edit · p pause · x delete · n new</div>
    ${editing && html`<${Form} title="Edit reminder" onClose=${() => setEditing(null)}
      initial=${{ Text: editing.text, When: '', Every: editing.every || 'never' }}
      fields=${[{ key: 'Text', label: 'What' }, { key: 'When', label: 'When (empty keeps ' + editing.when + ')', placeholder: '4pm · tomorrow 9am · fri 5pm · nov 1 10am' },
        { key: 'Every', label: 'Repeats', placeholder: 'never · day · weekday · week · month · 2 weeks · mon,wed,fri' }]}
      submit=${async (v) => { await api('PUT', `/reminders/${editing.id}`, v); reload(); }} />`}
  </div>`;
}

// ChatTab is REMINDERS' chat: Claude with nothing but `crew remind`.
function ChatTab() {
  const { data, err } = useLoad(() => api('GET', '/reminders/chat'));
  if (err) return html`<${Empty} title="The chat didn't open">${err}<//>`;
  if (!data) return html`<div class="empty">Loading…</div>`;
  return html`<${Chat} id=${data.id} base=${'/tasks/' + data.id} agent="Claude"
    empty="Ask Claude to add, move, pause or clear reminders: “remind me to stretch every hour until 6pm”. It can only run crew remind."
    placeholder="Ask about your reminders" />`;
}

export default function Reminders() {
  const [tab, setTab] = useState(location.hash.endsWith('/chat') ? 1 : 0);
  const go = (i) => { setTab(i); location.hash = 'reminders' + (i ? '/chat' : ''); };
  return html`<main class="page">
    <${Toolbar} title="Reminders" sub="once or repeating · they fire while crew is open" />
    <div class="tabs">${['List', 'Chat'].map((n, i) => html`<button key=${n} class=${tab === i ? 'on' : ''} onClick=${() => go(i)}>${n}</button>`)}</div>
    ${tab === 0 ? html`<${List} />` : html`<${ChatTab} />`}
  </main>`;
}
