// What every section's page shares: the API, the one event stream, Markdown, the chat, small parts.
// Pages draw and send; crew (internal/core and the services) decides everything.
import { html, useState, useEffect, useRef } from './vendor/preact-htm.js';

export { html, useState, useEffect, useRef };

// api calls crew; a failure comes back as an Error with crew's own words.
export async function api(method, path, body) {
  const res = await fetch('/api' + path, { method, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// One event stream for the whole page; on(fn) listens until the returned function is called.
// fn({kind: 'open'}) runs on every (re)connect, so a page can reload what it missed.
const listeners = new Set();
let stream;
export function on(fn) {
  if (location.search.includes('noevents')) return () => {}; // a headless check of the page: no stream, so it settles
  if (!stream) {
    stream = new EventSource('/api/events');
    stream.onopen = () => listeners.forEach((l) => l({ kind: 'open' }));
    stream.onmessage = (e) => { const ev = JSON.parse(e.data); listeners.forEach((l) => l(ev)); };
  }
  listeners.add(fn);
  return () => listeners.delete(fn);
}

// useEvents runs fn for each event while the component is on screen.
export function useEvents(fn, deps = []) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => on((ev) => ref.current(ev)), deps);
}

// useLoad is what load() returns, reloaded when deps change or reload() is called.
export function useLoad(load, deps = []) {
  const [state, setState] = useState({ data: null, err: '', loading: true });
  const reload = () => load().then((data) => setState({ data, err: '', loading: false }), (e) => setState((s) => ({ ...s, err: e.message, loading: false })));
  useEffect(() => { setState((s) => ({ ...s, loading: true })); reload(); }, deps);
  return { ...state, reload };
}

export const clock = (at) => (at ? new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '');
export const base = (p) => (p || '').split('/').filter(Boolean).pop();
export const ago = (at) => {
  if (!at) return '';
  const s = (Date.now() - new Date(at)) / 1000;
  if (s < 60) return 'now';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  if (s < 86400) return Math.floor(s / 3600) + 'h';
  return Math.floor(s / 86400) + 'd';
};
export const openLink = (url) => api('POST', '/open', { URL: url });

// Link opens in your browser (the window stays on crew).
export function Link({ href, children }) {
  return html`<a href=${href} onClick=${(e) => { e.preventDefault(); openLink(href); }}>${children || href}</a>`;
}

// inline is a line's Markdown: `code`, **bold**, *em*, [text](url) and bare links.
function inline(text) {
  const out = [];
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(\[[^\]]+\]\([^)\s]+\))|(https?:\/\/[^\s)]+)/g;
  let at = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > at) out.push(text.slice(at, m.index));
    const t = m[0];
    if (m[1]) out.push(html`<code>${t.slice(1, -1)}</code>`);
    else if (m[2]) out.push(html`<b>${t.slice(2, -2)}</b>`);
    else if (m[3]) out.push(html`<i>${t.slice(1, -1)}</i>`);
    else if (m[4]) { const [, label, url] = t.match(/\[([^\]]+)\]\(([^)]+)\)/); out.push(html`<${Link} href=${url}>${label}<//>`); }
    else out.push(html`<${Link} href=${t} />`);
    at = m.index + t.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
}

// Markdown draws an agent's reply or a GitHub/Jira comment: code blocks, headings, lists, quotes,
// tables kept as text, paragraphs.
export function Markdown({ text, live }) {
  const blocks = [];
  const lines = (text || '').split('\n');
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (l.startsWith('```')) {
      const code = [];
      for (i++; i < lines.length && !lines[i].startsWith('```'); i++) code.push(lines[i]);
      blocks.push(html`<pre>${code.join('\n')}</pre>`);
    } else if (/^#{1,6} /.test(l)) {
      const n = l.match(/^#+/)[0].length;
      blocks.push(html`<div class=${'h h' + Math.min(n, 3)}>${inline(l.slice(n + 1))}</div>`);
    } else if (/^\s*([-*]|\d+\.) /.test(l)) {
      const items = [];
      const ordered = /^\s*\d+\./.test(l);
      for (; i < lines.length && /^\s*([-*]|\d+\.) /.test(lines[i]); i++) items.push(html`<li>${inline(lines[i].replace(/^\s*([-*]|\d+\.) /, ''))}</li>`);
      i--;
      blocks.push(ordered ? html`<ol>${items}</ol>` : html`<ul>${items}</ul>`);
    } else if (l.startsWith('>')) {
      blocks.push(html`<blockquote>${inline(l.replace(/^>\s?/, ''))}</blockquote>`);
    } else if (l.startsWith('|')) {
      const rows = [];
      for (; i < lines.length && lines[i].startsWith('|'); i++) rows.push(lines[i]);
      i--;
      blocks.push(html`<pre class="table">${rows.join('\n')}</pre>`);
    } else if (l.trim()) {
      const para = [l];
      for (; i + 1 < lines.length && lines[i + 1].trim() && !/^(```|#{1,6} |\s*([-*]|\d+\.) |>|\|)/.test(lines[i + 1]); i++) para.push(lines[i + 1]);
      blocks.push(html`<p>${inline(para.join('\n'))}</p>`);
    }
  }
  return html`<div class=${'md' + (live ? ' live' : '')}>${blocks}</div>`;
}

// Messages as Slack shows them: a name and time on the first of a run by one sender, tool calls
// folded into one line that opens the side pane, and errors in red.
export function Messages({ msgs, live, onActs, agent = 'Agent' }) {
  const items = [];
  let acts = null, last = null;
  for (const m of msgs) {
    if (m.role === 'tool' || m.role === 'result') {
      if (!acts) items.push((acts = { acts: [] }));
      acts.acts.push(m);
      continue;
    }
    acts = null;
    if (m.role === 'error') { items.push({ note: m }); last = null; continue; }
    const first = !last || last.role !== m.role || new Date(m.at) - new Date(last.at) > 5 * 60000;
    items.push({ m, first });
    last = m;
  }
  const out = items.map((it, i) => {
    if (it.acts) {
      const calls = it.acts.filter((a) => a.role === 'tool');
      const lastCall = calls.length ? calls[calls.length - 1].text : '';
      return html`<button class="acts" key=${i} onClick=${() => onActs && onActs(it.acts)}>
        <span class="n">${calls.length}</span> action${calls.length === 1 ? '' : 's'}<span class="last">${lastCall.slice(0, 90)}</span></button>`;
    }
    if (it.note) return html`<div class="msg error" key=${i}><div class="gutter"></div><div class="body"><${Markdown} text=${it.note.text} /></div></div>`;
    const { m, first } = it;
    const you = m.role === 'you';
    return html`<div class=${'msg' + (first ? ' first' : '')} key=${i}>
      ${first ? html`<div class=${'avatar ' + (you ? 'you' : 'agent')}>${you ? 'Y' : agent[0]}</div>` : html`<div class="gutter"><time>${clock(m.at)}</time></div>`}
      <div class="body">${first && html`<div class="who">${you ? 'You' : agent}<time>${clock(m.at)}</time></div>`}
      ${you ? html`<div class="text">${m.text}</div>` : html`<${Markdown} text=${m.text} />`}</div></div>`;
  });
  if (live) out.push(html`<div class="msg first" key="live"><div class="avatar agent">${agent[0]}</div><div class="body"><div class="who">${agent}<span class="typing">writing…</span></div><${Markdown} live text=${live} /></div></div>`);
  return out;
}

// Pane is the panel beside a page (like a Slack thread).
export function Pane({ title, onClose, children }) {
  return html`<aside class="pane"><header><b>${title}</b><button class="ghost" title="Close (esc)" onClick=${onClose}>✕</button></header>
    <div class="pane-body">${children}</div></aside>`;
}

// Actions lists a run of tool calls with what each returned.
export function Actions({ acts }) {
  return acts.map((a, i) => html`<div key=${i} class=${'line ' + a.role}>${a.text}</div>`);
}

// Chat is a conversation with an agent, over a chat's routes: base + '/chat' (GET, the messages),
// '/messages' (POST {Text}) and '/stop'. id is the chat's id in crew's events. Every chat (a task's,
// a PR's, REQUESTS') is this one.
export function Chat({ id, base: root, working, empty, agent, placeholder }) {
  const [msgs, setMsgs] = useState([]);
  const [live, setLive] = useState('');
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [acts, setActs] = useState(null);
  const [busy, setBusy] = useState(working);
  const end = useRef();
  const box = useRef();
  const load = () => api('GET', root + '/chat').then(setMsgs).catch((e) => setErr(e.message));
  useEffect(() => { setMsgs([]); setLive(''); setErr(''); setActs(null); load(); box.current && box.current.focus(); }, [id]);
  useEffect(() => setBusy(working), [working]);
  useEvents((ev) => {
    if (ev.kind === 'open') return load();
    if (ev.id !== id) return;
    if (ev.kind === 'message') { setMsgs((l) => [...l, ev.msg]); if (ev.msg.role === 'agent') setLive(''); }
    if (ev.kind === 'delta') setLive((l) => l + ev.text);
    if (ev.kind === 'turn') { setBusy(!!ev.busy); if (!ev.busy) setLive(''); }
  }, [id]);
  useEffect(() => end.current && end.current.scrollIntoView({ block: 'end' }), [msgs, live]);
  const send = async (e) => {
    e && e.preventDefault();
    if (!text.trim()) return;
    const t = text;
    setText('');
    try { await api('POST', root + '/messages', { Text: t }); setErr(''); } catch (x) { setErr(x.message); setText(t); }
  };
  const key = (e) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) send(e);
    if (e.key === 'Escape' && busy) api('POST', root + '/stop');
  };
  const grow = (e) => { setText(e.target.value); e.target.style.height = 'auto'; e.target.style.height = Math.min(e.target.scrollHeight, 220) + 'px'; };
  return html`<div class="chat">
    <div class="msgs">${msgs.length || live ? html`<${Messages} msgs=${msgs} live=${live} onActs=${setActs} agent=${agent} />` : html`<div class="empty">${empty || 'Say what to do.'}</div>`}
      ${busy && !live && html`<div class="working"><span class="dot work"></span> working…</div>`}<div ref=${end}></div></div>
    <div class="compose"><form onSubmit=${send}>
      <textarea ref=${box} rows="1" placeholder=${busy ? 'Message (sending stops the agent and steers it)' : placeholder || 'Message'} value=${text} onInput=${grow} onKeyDown=${key}></textarea>
      ${busy ? html`<button type="button" onClick=${() => api('POST', root + '/stop')}>Stop</button>` : ''}
      <button class="primary" disabled=${!text.trim()}>Send</button></form>
      <div class="hint">${err ? html`<span class="err">${err}</span>` : busy ? '⏎ sends · esc stops' : '⏎ sends · ⇧⏎ new line'}</div></div>
    ${acts && html`<${Pane} title="Actions" onClose=${() => setActs(null)}><${Actions} acts=${acts} /><//>`}</div>`;
}

// Modal is a dialog over the page; esc or a click outside closes it.
export function Modal({ title, onClose, children, wide }) {
  useEffect(() => {
    const k = (e) => e.key === 'Escape' && onClose();
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <div class=${'modal' + (wide ? ' wide' : '')}><h2>${title}</h2>${children}</div></div>`;
}

// Form is a modal form: fields is [{key, label, placeholder?, type?: 'textarea'|'select', options?}],
// submit(values) returns a promise; its error shows in the form.
export function Form({ title, fields, initial = {}, submit, onClose, action = 'Save' }) {
  const [v, setV] = useState(initial);
  const [err, setErr] = useState('');
  const [saving, setSaving] = useState(false);
  const go = async (e) => {
    e.preventDefault();
    setSaving(true);
    try { await submit(v); onClose(); } catch (x) { setErr(x.message); setSaving(false); }
  };
  return html`<${Modal} title=${title} onClose=${onClose}><form class="form" onSubmit=${go}>
    ${fields.map((f, i) => html`<label key=${f.key}>${f.label}${
      f.type === 'textarea' ? html`<textarea rows=${f.rows || 4} placeholder=${f.placeholder} value=${v[f.key] || ''} onInput=${(e) => setV({ ...v, [f.key]: e.target.value })}></textarea>`
      : f.type === 'select' ? html`<select value=${v[f.key] || ''} onChange=${(e) => setV({ ...v, [f.key]: e.target.value })}>${f.options.map((o) => html`<option value=${o.value ?? o}>${o.label ?? o}</option>`)}</select>`
      : html`<input autofocus=${i === 0} placeholder=${f.placeholder} type=${f.type || 'text'} value=${v[f.key] || ''} onInput=${(e) => setV({ ...v, [f.key]: e.target.value })} />`}</label>`)}
    ${err && html`<div class="err">${err}</div>`}
    <div class="btns"><button type="button" onClick=${onClose}>Cancel</button><button class="primary" disabled=${saving}>${saving ? '…' : action}</button></div>
  </form><//>`;
}

// Empty is a page's placeholder: what's missing and what to do.
export const Empty = ({ title, children }) => html`<div class="empty"><div class="big">${title}</div>${children}</div>`;

// Toolbar is a page's header: title, what it's about, and its actions on the right.
export const Toolbar = ({ title, sub, children }) => html`<header class="bar"><div class="bar-title"><h1>${title}</h1>${sub && html`<span class="meta">${sub}</span>`}</div><div class="bar-actions">${children}</div></header>`;

// Search is a filter box; ⌘F or / focuses it.
export function Search({ value, onInput, placeholder = 'Filter' }) {
  const ref = useRef();
  useEffect(() => {
    const k = (e) => {
      if ((e.metaKey && e.key === 'f') || (e.key === '/' && !/input|textarea|select/i.test(document.activeElement.tagName))) { e.preventDefault(); ref.current && ref.current.focus(); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<input ref=${ref} class="search" placeholder=${placeholder + ' (/)'} value=${value} onInput=${(e) => onInput(e.target.value)} />`;
}

// useListKeys moves a selection through items with ↑↓ (j/k) while focus isn't in a field.
export function useListKeys(items, sel, setSel) {
  useEffect(() => {
    const k = (e) => {
      if (/input|textarea|select/i.test(document.activeElement.tagName) || e.metaKey) return;
      const i = items.findIndex((x) => x === sel);
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); setSel(items[Math.min(i + 1, items.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); setSel(items[Math.max(i - 1, 0)]); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [items, sel]);
}
