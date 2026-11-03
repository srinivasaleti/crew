// What DOCKER's and KUBERNETES' pages share: tabs with counts, filter chips, a dense sortable table,
// asking before a change (typing the context's name on production), a thing's page (Logs, Inspect or
// Describe, Chat) and Cleanup. The rules (what a change runs, what Cleanup lists) are crew's.
import { html, useState, useEffect, useRef, api, ago, Modal, Chat, Empty } from '../lib.js';

export { ago };

// The section's own styles, added once.
if (!document.querySelector('link[data-ctr]')) {
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  l.href = 'sections/containers.css';
  l.dataset.ctr = '1';
  document.head.appendChild(l);
}

const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);

// useEvery runs fn now and every ms while the window is shown, until deps change.
export function useEvery(fn, ms, deps) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    ref.current();
    const t = setInterval(() => !document.hidden && ref.current(), ms);
    return () => clearInterval(t);
  }, deps);
}

// useKeys calls the handler for a key while focus isn't in a field and no dialog is open.
export function useKeys(handler, deps) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || e.ctrlKey || document.querySelector('.scrim')) return;
      if (ref.current(e.key, e) !== false) return;
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, deps || []);
}

// St is a state in its colour: ok green, bad red, else dim.
export const St = ({ ok, bad, children }) => html`<span class=${'st ' + (bad ? 'bad' : ok ? 'ok' : 'dim')}>${children}</span>`;

// Tabs are a section's tabs, each with its count (red when something there is wrong).
export const Tabs = ({ tabs, cur, set }) => html`<div class="tabs">${tabs.map((t, i) => html`<button key=${t.label} class=${i === cur ? 'on' : ''} onClick=${() => set(i)}>
  ${t.label}${t.count != null ? html`<span class=${'count' + (t.bad ? ' bad' : '')}>${t.count}</span>` : ''}</button>`)}</div>`;

// Chips filter a tab's rows.
export const Chips = ({ chips, cur, set }) => chips && chips.length ? html`<div class="chips">${chips.map((c) => html`<button key=${c.id} class=${c.id === cur ? 'on' : ''} onClick=${() => set(c.id)}>${c.label}</button>`)}</div>` : '';

// Grid is a dense table: click a header to sort, a row to select it, twice (or ⏎) to open it; ↑↓/jk
// move. cols: [{label, cell(row), sort?(row), cls?}]; acts(row) are the buttons on the row's right.
export function Grid({ cols, rows, sel, setSel, open, acts, empty }) {
  const [sort, setSort] = useState({ i: -1, dir: 1 });
  const list = sort.i < 0 || !cols[sort.i].sort ? rows : rows.slice().sort((a, b) => {
    const x = cols[sort.i].sort(a), y = cols[sort.i].sort(b);
    return (x < y ? -1 : x > y ? 1 : 0) * sort.dir;
  });
  useKeys((k, e) => {
    const i = list.findIndex((r) => r.key === sel);
    if (k === 'ArrowDown' || k === 'j') { e.preventDefault(); list.length && setSel(list[Math.min(i + 1, list.length - 1)].key); }
    if (k === 'ArrowUp' || k === 'k') { e.preventDefault(); list.length && setSel(list[Math.max(i - 1, 0)].key); }
    if (k === 'Enter' && i >= 0) open(list[i]);
  }, [list, sel]);
  useEffect(() => {
    const on = document.querySelector('table.grid tr.on');
    on && on.scrollIntoView({ block: 'nearest' });
  }, [sel]);
  if (!rows.length) return html`<${Empty} title=${empty} />`;
  return html`<div class="ctr-table"><table class="grid">
    <thead><tr>${cols.map((c, i) => html`<th key=${c.label} class=${c.sort ? 'sort' : ''} onClick=${() => c.sort && setSort({ i, dir: sort.i === i ? -sort.dir : 1 })}>
      ${c.label}${sort.i === i ? (sort.dir > 0 ? ' ↑' : ' ↓') : ''}</th>`)}<th></th></tr></thead>
    <tbody>${list.map((r) => html`<tr key=${r.key} class=${r.key === sel ? 'on' : ''} onClick=${() => (r.key === sel ? open(r) : setSel(r.key))}>
      ${cols.map((c) => html`<td key=${c.label} class=${c.cls || ''}>${c.cell(r)}</td>`)}
      <td class="row-acts">${(acts ? acts(r) : []).map((a) => html`<button key=${a.label} class=${a.danger ? 'danger' : ''} title=${a.title || a.label}
        disabled=${a.disabled} onClick=${(e) => { e.stopPropagation(); a.run(); }}>${a.label}</button>`)}</td></tr>`)}</tbody>
  </table></div>`;
}

// Confirm asks before a change. On a production context (typed is its name) the name must be typed.
// yes(typedName) runs it; its error shows here.
export function Confirm({ title, text, warn, typed, action = 'Yes', danger, yes, onClose }) {
  const [v, setV] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const ok = !typed || v.trim() === typed;
  const go = async (e) => {
    e && e.preventDefault();
    if (!ok || busy) return;
    setBusy(true);
    try { await yes(v.trim()); onClose(); } catch (x) { setErr(x.message); setBusy(false); }
  };
  return html`<${Modal} title=${title} onClose=${onClose}><form class="form" onSubmit=${go}>
    ${typed && html`<div><span class="prod">PRODUCTION</span> <b>${typed}</b></div>`}
    <div>${text}</div>
    ${warn && html`<div class="warn-text">${warn}</div>`}
    ${typed && html`<label class="typed">Type <b>${typed}</b> to go ahead<input autofocus value=${v} onInput=${(e) => setV(e.target.value)} /></label>`}
    ${err && html`<div class="err">${err}</div>`}
    <div class="btns"><button type="button" onClick=${onClose}>Cancel</button>
      <button autofocus=${!typed} class=${danger ? 'primary danger-bg' : 'primary'} disabled=${!ok || busy}>${busy ? '…' : action}</button></div>
  </form><//>`;
}

// Logs marks error and warning lines, and the search's matches.
function LogText({ text, q }) {
  const lines = text.split('\n').filter((l) => !q || l.toLowerCase().includes(q.toLowerCase()));
  return lines.map((l, i) => {
    const u = l.toUpperCase();
    const cls = /ERROR|PANIC|FATAL|EXCEPTION/.test(u) ? 'e' : u.includes('WARN') ? 'w' : '';
    if (!q) return html`<div key=${i} class=${cls}>${l || ' '}</div>`;
    const at = l.toLowerCase().indexOf(q.toLowerCase());
    return html`<div key=${i} class=${cls}>${l.slice(0, at)}<mark>${l.slice(at, at + q.length)}</mark>${l.slice(at + q.length)}</div>`;
  });
}

// pretty indents inspect's JSON; describe's text stays as it is.
const pretty = (s) => { try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; } };

// Thing is a container or object on its own page: Logs that follow the end, Inspect or Describe, and
// a chat locked down to crew ctr. read(logs) reads; chat() is the chat's id (made the first time).
export function Thing({ title, sub, prod, panes, read, chat, onBack, actions, start }) {
  const [pane, setPane] = useState(Math.max(panes.indexOf(start), 0));
  const [out, setOut] = useState({});
  const [err, setErr] = useState({});
  const [follow, setFollow] = useState(true);
  const [q, setQ] = useState('');
  const [chatID, setChatID] = useState('');
  const pre = useRef();
  const name = panes[pane];
  const load = (p) => read(p === 'Logs').then((s) => { setOut((o) => ({ ...o, [p]: s })); setErr((e) => ({ ...e, [p]: '' })); },
    (e) => setErr((x) => ({ ...x, [p]: e.message })));
  const toChat = async () => {
    if (!chatID) setChatID(await chat());
    setPane(panes.indexOf('Chat'));
  };
  useEffect(() => { if (name === 'Chat') toChat().catch((e) => setErr((x) => ({ ...x, Chat: e.message }))); else load(name); }, [pane]);
  useEvery(() => name === 'Logs' && follow && load('Logs'), 3000, [pane, follow]);
  useEffect(() => { if (name === 'Logs' && follow && pre.current) pre.current.scrollTop = pre.current.scrollHeight; }, [out.Logs, pane, follow]);
  const askLogs = async () => {
    const id = chatID || (await chat());
    setChatID(id);
    const last = (out.Logs || '').split('\n').slice(-80).join('\n');
    await api('POST', `/tasks/${id}/messages`, { Text: "What's going wrong here? The last lines of its logs:\n\n```\n" + last + '\n```' });
    setPane(panes.indexOf('Chat'));
  };
  useKeys((k) => {
    if (k === 'Escape') onBack();
    if (k === 'Tab') setPane((pane + 1) % panes.length);
    if (k === 'f' && name === 'Logs') setFollow(!follow);
    if (k === 'r' && name !== 'Chat') load(name);
    if (k === 'a') name === 'Logs' && out.Logs ? askLogs() : toChat();
  }, [pane, follow, out, chatID]);
  return html`<main class="page">
    <header class="bar"><button class="ghost" title="Back (esc)" onClick=${onBack}>←</button>
      <div class="bar-title"><h1>${title}</h1><span class="meta">${sub}</span>${prod && html`<span class="prod">PRODUCTION</span>`}</div>
      <div class="bar-actions">${actions}</div></header>
    <div class="tabs">${panes.map((p, i) => html`<button key=${p} class=${i === pane ? 'on' : ''} onClick=${() => setPane(i)}>${p}</button>`)}</div>
    ${name === 'Chat' ? (chatID ? html`<${Chat} key=${chatID} id=${chatID} base=${'/tasks/' + chatID} agent="Claude"
        placeholder=${'Ask Claude about ' + title + '…'}
        empty=${'Ask Claude about ' + title + ": why it crashed, what's using the disk, what an error means. It reads logs and inspect or describe output with crew ctr, and can't change anything: a fix comes back as a command for you to run."} />`
      : html`<${Empty} title=${err.Chat || 'Opening the chat…'} />`)
    : html`<div class="thing-bar">
        ${name === 'Logs' && html`<label class="meta"><input type="checkbox" style="width:auto" checked=${follow} onChange=${() => setFollow(!follow)} /> follow (f)</label>`}
        <input class="search" placeholder=${name === 'Logs' ? 'Show lines with…' : 'Find…'} value=${q} onInput=${(e) => setQ(e.target.value)} />
        <span class="grow"></span>
        ${name === 'Logs' && html`<button onClick=${askLogs} disabled=${!out.Logs}>Ask Claude about these logs (a)</button>`}
        <button onClick=${() => load(name)}>Read again (r)</button></div>
      <pre class="logs" ref=${pre} onScroll=${(e) => { const el = e.target; if (name === 'Logs' && el.scrollHeight - el.scrollTop - el.clientHeight > 40 && follow) setFollow(false); }}>${
        err[name] ? html`<div class="e">${err[name]}</div>`
        : out[name] == null ? html`<div class="meta">Reading…</div>`
        : !out[name] ? html`<div class="meta">${name === 'Logs' ? 'No logs yet.' : 'Nothing.'}</div>`
        : html`<${LogText} text=${name === 'Logs' ? out[name] : pretty(out[name])} q=${q} />`}</pre>`}
  </main>`;
}

// Cleanup lists what can go, ticked by what's safe, shows exactly what goes, then removes it.
// load(ns) reads the list ({Title, Note, Namespaces, Opts}); run(labels) removes the picked ones.
export function Cleanup({ load, run, onClose, done }) {
  const [d, setD] = useState(null);
  const [on, setOn] = useState({});
  const [ns, setNs] = useState('');
  const [preview, setPreview] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setD(null);
    load(ns).then((x) => { setD(x); setOn(Object.fromEntries(x.Opts.map((o) => [o.Label, o.On]))); setPreview(false); }, (e) => setErr(e.message));
  }, [ns]);
  const has = (o) => o.Names.length > 0 || o.Size;
  const picked = d ? d.Opts.filter((o) => on[o.Label] && has(o)) : [];
  const go = async () => {
    setBusy(true);
    try { await run(picked.map((o) => o.Label)); done(); onClose(); } catch (x) { setErr(x.message); setBusy(false); }
  };
  return html`<${Modal} title=${d ? d.Title : 'Cleanup'} onClose=${onClose} wide>
    ${!d ? html`<div class="meta">${err || 'Reading what can go…'}</div>`
    : d.Note ? html`<div>${d.Note}</div><div class="btns"><button onClick=${onClose}>Close</button></div>`
    : html`<div>
        ${d.Namespaces.length > 0 && html`<label class="meta">Namespace <select style="width:auto" value=${ns || d.Namespaces[0]} onChange=${(e) => setNs(e.target.value)}>
          ${d.Namespaces.map((n) => html`<option key=${n} value=${n}>${n}</option>`)}</select></label>`}
        ${preview ? html`<div class="warn-text" style="margin:8px 0">These go now; this can't be undone:</div>
            ${picked.map((o) => html`<div key=${o.Label} class="cleanup-opt"><span></span><b>${o.Label}</b><span></span><span class="meta">${o.Size}</span>
              <div class="names">${o.Names.join('\n') || o.Size}</div></div>`)}`
          : d.Opts.map((o) => html`<div key=${o.Label} class="cleanup-opt">
            <input type="checkbox" style="width:auto" disabled=${!has(o)} checked=${on[o.Label] && has(o)} onChange=${() => setOn({ ...on, [o.Label]: !on[o.Label] })} />
            <span>${o.Label}${o.Warn && html` <span class="warn-text">· ${o.Warn}</span>`}</span>
            <span class="meta">${o.Names.length || (o.Size ? '' : 0)}</span><span class="meta">${o.Size}</span></div>`)}
        ${err && html`<div class="err">${err}</div>`}
        <div class="btns">${preview ? html`<button onClick=${() => setPreview(false)}>Back</button><button class="primary" disabled=${busy} onClick=${go}>${busy ? 'Removing…' : 'Remove them'}</button>`
          : html`<button onClick=${onClose}>Cancel</button><button class="primary" disabled=${!picked.length} onClick=${() => setPreview(true)}>Show exactly what goes</button>`}</div>
      </div>`}
  <//>`;
}

// Missing is the page when the CLI isn't there or can't reach anything.
export const Missing = ({ title, err, retry }) => html`<main class="page"><${Empty} title=${title}>${html`<pre class="err" style="white-space:pre-wrap;text-align:left">${err}</pre><button onClick=${retry}>Try again</button>`}<//></main>`;
