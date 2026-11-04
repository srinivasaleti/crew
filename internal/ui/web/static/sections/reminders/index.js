// REMINDERS: a quick-add line on top, the reminders by what they'll do next (Upcoming, Repeating,
// Paused, Done), and a Chat tab where Claude manages them with `crew remind` (locked down to that).
// Parsing, firing and the lock-down are crew's (internal/reminders); this draws and sends.
import { html, useState, useEffect, useRef, api, on, clock, openLink, useEvents, useLoad, Chat, Form, Empty, Toolbar, css } from '../../lib.js';

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

// joinLabel is the button for a reminder's link: Join Teams, Join Zoom, Join Meet, or Open link.
function joinLabel(url) {
  const host = new URL(url).hostname;
  if (/teams\.(microsoft|live)\.com$/.test(host)) return 'Join Teams';
  if (/zoom\.us$/.test(host)) return 'Join Zoom';
  if (host === 'meet.google.com') return 'Join Meet';
  return 'Open link';
}

// Overlay is a reminder that fired, as Slack shows a meeting: a card in the bottom-left corner over
// whatever page you're on, until you close it. Join opens its link; Snooze reminds you in 10 minutes.
export function Overlay() {
  const [cards, setCards] = useState([]); // [{text, at, url}]
  useEffect(() => on((ev) => {
    if (ev.kind !== 'reminders' || !ev.text) return;
    const now = Date.now();
    setCards((l) => [...ev.text.split('\n').map((text) => ({ text, at: now, url: (text.match(/https?:\/\/\S+/) || [])[0] })), ...l].slice(0, 4));
  }), []);
  const close = (c) => setCards((l) => l.filter((x) => x !== c));
  const snooze = async (c) => { await api('POST', '/reminders', { Text: c.text, When: '10m' }); close(c); };
  if (!cards.length) return '';
  return html`<div class="sec sec-reminders" style="display: contents"><div class="rem-toasts">${cards.map((c, i) => {
    const title = c.url ? c.text.replace(c.url, '').trim() || c.text : c.text;
    return html`<div class="rem-toast" key=${c.at + ':' + i}>
      <div class="rem-toast-head"><b>${title}</b><button class="ghost" title="Close" onClick=${() => close(c)}>✕</button></div>
      <div class="rem-toast-sub">Now · reminder at ${clock(new Date(c.at))}</div>
      <div class="rem-toast-acts">
        ${c.url && html`<button class="primary" onClick=${() => { openLink(c.url); close(c); }}>${joinLabel(c.url)}</button>`}
        <button onClick=${() => snooze(c)}>Snooze 10 min</button>
        ${!c.url && html`<button class="primary" onClick=${() => close(c)}>Done</button>`}
      </div></div>`;
  })}</div></div>`;
}

// REPEATS are the ways a reminder repeats, for the pickers (the service reads each: ParseEvery).
const REPEATS = [{ value: '', label: "Doesn't repeat" }, { value: 'day', label: 'Every day' }, { value: 'weekday', label: 'Every weekday' },
  { value: 'week', label: 'Every week' }, { value: '2 weeks', label: 'Every 2 weeks' }, { value: 'month', label: 'Every month' }, { value: 'year', label: 'Every year' }];

// pickDefaults is a date and time for the pickers: at's, or the next hour today.
function pickDefaults(at) {
  const d = at || new Date(Date.now() + 3600e3);
  const p = (n) => String(n).padStart(2, '0');
  return { Date: d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()), Time: p(d.getHours()) + ':' + (at ? p(d.getMinutes()) : '00'), Every: '' };
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
function QuickAdd({ onAdded, detailed }) {
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
    <input ref=${box} placeholder="Remind me to… stand-up every weekday at 4pm (n)" value=${text} onInput=${(e) => setText(e.target.value)} />
    <button class="primary" disabled=${!preview}>Add</button>
    <button type="button" title="Pick the date and time instead" onClick=${detailed}>📅 Pick a time</button>
    <div class="rem-preview">${preview ? html`<span class="rem-ok">✓</span> <b>${preview.text}</b> · ${preview.when} <span class="meta">(${rel(preview.at)})</span>${preview.every ? html` · ↻ ${preview.repeats}` : ''} <span class="meta">· ⏎ adds it</span>`
      : err && text.trim() ? html`<span class="err">${err.startsWith('say what and when') ? "It needs a time too: add “at 4pm”, “tomorrow 9am”, “in 30 minutes”, or use 📅 Pick a time." : err}</span>`
      : html`<span class="meta">Try: call mom tomorrow 9am · stretch in 30 minutes · standup every weekday at 10am · pay rent nov 1 every month</span>`}</div>
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
  const [picking, setPicking] = useState(false);
  const [, tick] = useState(0);
  useEvents((ev) => { if (ev.kind === 'reminders' || ev.kind === 'open') reload(); }, []);
  useEffect(() => { const t = setInterval(() => tick((n) => n + 1), 30000); return () => clearInterval(t); }, []); // "in 5m" moves on
  const rs = (data && data.reminders) || [];
  const day = (d) => { const x = new Date(d); x.setHours(0, 0, 0, 0); return x; };
  const today = day(new Date()), days = (r) => Math.round((day(r.at) - today) / 864e5);
  const once = rs.filter((r) => !r.done && !r.paused && !r.every);
  const groups = [
    ['Overdue', once.filter((r) => new Date(r.at) < new Date())],
    ['Today', once.filter((r) => new Date(r.at) >= new Date() && days(r) === 0)],
    ['Tomorrow', once.filter((r) => days(r) === 1)],
    ['This week', once.filter((r) => days(r) > 1 && days(r) < 7)],
    ['Later', once.filter((r) => days(r) >= 7)],
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
    <${QuickAdd} onAdded=${reload} detailed=${() => setPicking(true)} />
    ${picking && html`<${Form} title="New reminder" action="Add" onClose=${() => setPicking(false)} initial=${pickDefaults()}
      fields=${[{ key: 'Text', label: 'What' , placeholder: 'call mom' }, { key: 'Date', label: 'Day', type: 'date' }, { key: 'Time', label: 'Time', type: 'time' },
        { key: 'Every', label: 'Repeats', type: 'select', options: REPEATS }]}
      submit=${async (v) => { await api('POST', '/reminders', { Text: v.Text, When: v.Date + ' ' + v.Time, Every: v.Every }); reload(); }} />`}
    ${undo && html`<div class="rem-undo">Deleted “${undo.text}” <button class="small" onClick=${async () => { await api('POST', `/reminders/${undo.id}/restore`); setUndo(null); reload(); }}>Undo</button>
      <button class="ghost small" onClick=${() => setUndo(null)}>✕</button></div>`}
    ${!data ? html`<div class="meta">Loading…</div>` : groups.length === 0 ? html`<${Empty} title="No reminders">Type one above, or ask Claude in the Chat tab.<//>`
      : groups.map(([name, l]) => html`<div key=${name}><div class=${'group' + (name === 'Overdue' ? ' rem-late' : '')}>${name} · ${l.length}</div>
          ${l.map((r) => html`<${Row} key=${r.id} r=${r} on=${r.id === sel} select=${() => setSel(r.id)} act=${(a) => act(r, a)} edit=${() => setEditing(r)} />`)}</div>`)}
    <div class="hint">↑↓ choose · e edit · p pause · x delete · n new</div>
    ${editing && html`<${Form} title="Edit reminder" onClose=${() => setEditing(null)}
      initial=${{ Text: editing.text, ...pickDefaults(new Date(editing.at)), Every: REPEATS.some((o) => o.value === (editing.every || '')) ? editing.every || '' : editing.every }}
      fields=${[{ key: 'Text', label: 'What' }, { key: 'Date', label: 'Day', type: 'date' }, { key: 'Time', label: 'Time', type: 'time' },
        { key: 'Every', label: 'Repeats', type: 'combo', options: REPEATS.map((o) => o.value || 'never'), placeholder: 'never · day · weekday · week · month · mon,wed,fri' }]}
      submit=${async (v) => { await api('PUT', `/reminders/${editing.id}`, { Text: v.Text, When: v.Date + ' ' + v.Time, Every: v.Every || 'never' }); reload(); }} />`}
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
