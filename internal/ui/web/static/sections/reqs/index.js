// REQUESTS: an HTTP client like Postman's. Applications, their folders and requests on the left; the
// open request's editor and its response on the right; an application's environments and keys; the
// locked-down chat. Every rule (auth, keys only to the env's hosts, redaction) is crew's
// (internal/requests): this page draws and sends, and never sees a key's value after you type it.
import { html, useState, useEffect, useRef, api, useLoad, Toolbar, Search, Form, Empty, Chat, Modal, Menu, useListKeys, ask, css } from '../../lib.js';

css(import.meta.url);

const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);
const folderName = (f, def) => f || def;

// newRequest is an empty request in an application's folder.
const newRequest = (app, folder) => ({ App: app, Folder: folder || '', File: '', Index: 0, Name: '', Method: 'GET', URL: '{{baseUrl}}/', Headers: [], Off: [], Body: '', NoAuth: false, Tags: [] });

// resolve fills in an environment's plain values, to show where a request goes (keys stay as names).
const resolve = (s, env) => (env ? s.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (m, n) => (env.Vars && n in env.Vars ? env.Vars[n] : m)) : s);

function Method({ m }) {
  return html`<span class=${'rq-method rq-' + (m || 'GET').toLowerCase()}>${m}</span>`;
}

// JSON draws a parsed value with objects and arrays you can fold (the browser's own <details>).
function JSONView({ v, name, depth = 0 }) {
  const key = name !== undefined ? html`<span class="rq-key">${JSON.stringify(name)}</span>: ` : '';
  if (v === null || typeof v !== 'object') {
    const cls = v === null ? 'null' : typeof v;
    return html`<div class="rq-jl">${key}<span class=${'rq-' + cls}>${JSON.stringify(v)}</span></div>`;
  }
  const arr = Array.isArray(v);
  const entries = arr ? v.map((x, i) => [undefined, x, i]) : Object.entries(v).map(([k, x]) => [k, x, k]);
  const [open, close] = arr ? ['[', ']'] : ['{', '}'];
  if (!entries.length) return html`<div class="rq-jl">${key}${open}${close}</div>`;
  return html`<details class="rq-jd" open=${depth < 3}><summary>${key}${open}<span class="rq-count">${entries.length} ${arr ? 'items' : 'keys'}</span></summary>
    <div class="rq-jin">${entries.map(([k, x, id]) => html`<${JSONView} key=${id} v=${x} name=${k} depth=${depth + 1} />`)}</div>${close}</details>`;
}

// Response is what came back: its status (coloured by kind), time and size, then the body (JSON you
// can fold, or raw) or its headers.
function Response({ res, err, sending, height }) {
  const [tab, setTab] = useState('body');
  const [raw, setRaw] = useState(false);
  const [copied, setCopied] = useState(false);
  const box = height ? { height: height + 'px' } : null;
  if (sending) return html`<div class="rq-res" style=${box}><div class="rq-res-bar"><span class="meta"><span class="dot work"></span> Sending…</span></div></div>`;
  if (err) return html`<div class="rq-res" style=${box}><div class="rq-res-bar"><span class="rq-status rq-bad">Not sent</span></div><pre class="err rq-err">${err}</pre></div>`;
  if (!res) return html`<div class="rq-res idle"><span class="meta">Response · ⌘⏎ sends the request</span></div>`;
  let parsed;
  try { parsed = JSON.parse(res.Body); } catch { parsed = undefined; }
  const kind = res.Code >= 500 ? 'rq-bad' : res.Code >= 400 ? 'rq-warn4' : res.Code >= 300 ? 'rq-redir' : 'rq-ok';
  const json = parsed !== undefined && typeof parsed === 'object';
  const copy = () => navigator.clipboard.writeText(res.Body).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1200); });
  return html`<div class="rq-res" style=${box}>
    <div class="rq-res-bar"><span class=${'rq-status ' + kind}>${res.Status}</span>
      <span class="meta">${res.Ms} ms · ${res.Size}</span><span class="meta rq-url" title=${res.URL}>${res.URL}</span>
      <div class="tabs rq-tabs"><button class=${tab === 'body' ? 'on' : ''} onClick=${() => setTab('body')}>Body</button>
        <button class=${tab === 'headers' ? 'on' : ''} onClick=${() => setTab('headers')}>Headers <span class="meta">${res.Headers.length}</span></button></div>
      ${tab === 'body' && json && html`<span class="rq-seg"><button class=${!raw ? 'on' : ''} onClick=${() => setRaw(false)}>Pretty</button><button class=${raw ? 'on' : ''} onClick=${() => setRaw(true)}>Raw</button></span>`}
      <button class="small" onClick=${copy}>${copied ? '✓ Copied' : 'Copy'}</button></div>
    <div class="rq-res-body">${tab === 'headers'
      ? html`<table class="grid"><tbody>${res.Headers.map(([k, v]) => html`<tr key=${k}><td class="rq-hname">${k}</td><td class="rq-mono">${v}</td></tr>`)}</tbody></table>`
      : json && !raw ? html`<div class="rq-json"><${JSONView} v=${parsed} /></div>`
      : html`<pre class="rq-plain">${(json ? JSON.stringify(parsed, null, 2) : res.Body) || '(empty)'}</pre>`}</div></div>`;
}

// varsIn are the {{variables}} a request uses, in its URL, the headers it sends and its body, once each.
const varsIn = (...texts) => [...new Set(texts.join('\n').match(/\{\{\s*[\w.-]+\s*\}\}/g) || [])].map((m) => m.slice(2, -2).trim());

// VarChip is one {{variable}} where the request uses it, with its value in the environment: a click
// edits a plain value in place (⏎ saves it to the environment); a key shows ●●● and sets its value in
// the Keychain, never shown; one the environment doesn't have can be added as either.
function VarChip({ name, app, env, reload }) {
  const [edit, setEdit] = useState(null); // {as: 'value' | 'key', text}
  const [err, setErr] = useState('');
  const key = env.Keys.find((k) => k.Name === name);
  const has = env.Vars && name in env.Vars;
  const save = async () => {
    try {
      if (edit.as === 'key') await api('POST', '/reqs/keys', { App: app.Name, Env: env.Name, Name: name, Value: edit.text });
      else await api('POST', '/reqs/vars', { App: app.Name, Env: env.Name, Name: name, Value: edit.text });
      setEdit(null); setErr(''); reload();
    } catch (e) { setErr(e.message); }
  };
  if (edit) return html`<span class="rq-var editing"><span class="rq-vname">${name}</span>
    <input autofocus type=${edit.as === 'key' ? 'password' : 'text'} value=${edit.text} placeholder=${edit.as === 'key' ? 'value, to the Keychain' : 'value'}
      onInput=${(e) => setEdit({ ...edit, text: e.target.value })} onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); save(); } if (e.key === 'Escape') setEdit(null); }} />
    <button class="small primary" onClick=${save}>Save</button><button class="ghost small" onClick=${() => setEdit(null)}>✕</button>
    ${err && html`<span class="err">${err}</span>`}</span>`;
  if (key) return html`<button class=${'rq-var key' + (key.Set ? '' : ' missing')} title=${key.Set ? 'A key: its value is in the Keychain. Click to replace it.' : 'A key with no value yet. Click to set it.'}
    onClick=${() => setEdit({ as: 'key', text: '' })}><span class="rq-vname">${name}</span> ${key.Set ? '●●●' : 'set it'}</button>`;
  if (has) return html`<button class="rq-var" title=${'In ' + env.Name + '. Click to change it.'} onClick=${() => setEdit({ as: 'value', text: env.Vars[name] })}>
    <span class="rq-vname">${name}</span> ${env.Vars[name] || html`<i>empty</i>`}</button>`;
  return html`<span class="rq-var missing" title=${'Not in ' + env.Name}><span class="rq-vname">${name}</span> not set
    <button class="ghost small" onClick=${() => setEdit({ as: 'value', text: '' })}>+ value</button><button class="ghost small" onClick=${() => setEdit({ as: 'key', text: '' })}>+ key</button></span>`;
}

// ValueRow is one plain value as a table row: its name, then its value, edited in place (⏎ or leaving
// the field saves it), and ✕ to remove it.
function ValueRow({ app, env, name, reload }) {
  const [v, setV] = useState(env.Vars[name] || '');
  const [state, setState] = useState(''); // '', 'saved' or an error
  useEffect(() => setV(env.Vars[name] || ''), [env.Name, env.Vars[name]]);
  const save = async () => {
    if (v === (env.Vars[name] || '')) return;
    try { await api('POST', '/reqs/vars', { App: app.Name, Env: env.Name, Name: name, Value: v }); setState('saved'); setTimeout(() => setState(''), 1500); reload(); } catch (e) { setState(e.message); }
  };
  const remove = async () => { try { await api('POST', '/reqs/vars', { App: app.Name, Env: env.Name, Name: name, Remove: true }); reload(); } catch (e) { setState(e.message); } };
  return html`<tr>
    <td class="rq-vt-name">${name}</td>
    <td><input value=${v} onInput=${(e) => setV(e.target.value)} onBlur=${save} onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); e.target.blur(); } if (e.key === 'Escape') setV(env.Vars[name] || ''); }} />
      ${state && state !== 'saved' && html`<div class="err">${state}</div>`}</td>
    <td class="rq-vt-act">${state === 'saved' ? html`<span class="rq-jok">✓</span>` : name !== 'baseUrl' && html`<button class="ghost small" title="Remove it" onClick=${remove}>✕</button>`}</td>
  </tr>`;
}

// KeyRow is one key as a table row: its name, whether it's set, and a password field to set or replace
// its value in the Keychain (never shown), or Clear to remove it. A new key's row has a name field.
function KeyRow({ app, env, k, reload }) {
  const [name, setName] = useState('');
  const [v, setV] = useState('');
  const [err, setErr] = useState('');
  const set = async () => {
    try { await api('POST', '/reqs/keys', { App: app.Name, Env: env.Name, Name: k ? k.Name : name.trim(), Value: v }); setV(''); setName(''); setErr(''); reload(); } catch (e) { setErr(e.message); }
  };
  const clear = async () => {
    if (!(await ask('Remove the value of ' + k.Name + ' from the Keychain?', 'Remove'))) return;
    await api('POST', '/reqs/keys/delete', { App: app.Name, Env: env.Name, Name: k.Name }); reload();
  };
  return html`<tr>
    <td class="rq-vt-name">${k ? html`${k.Name}${env.AuthKey === k.Name && html` <span class="tag">auth</span>`}` : html`<input placeholder="new key, like token" value=${name} onInput=${(e) => setName(e.target.value)} />`}</td>
    <td><div class="rq-vt-key">${k && html`<span class=${k.Set ? 'rq-jok' : 'rq-jbad'}>${k.Set ? '● set' : '○ not set'}</span>`}
      <input type="password" autocomplete="off" placeholder=${k && k.Set ? 'new value, to replace it' : 'value, to the Keychain'} value=${v} onInput=${(e) => setV(e.target.value)} onKeyDown=${(e) => e.key === 'Enter' && v && set()} />
      <button class="small" disabled=${!v || (!k && !name.trim())} onClick=${set}>${k ? (k.Set ? 'Replace' : 'Set') : '+ Key'}</button></div>
      ${err && html`<div class="err">${err}</div>`}</td>
    <td class="rq-vt-act">${k && k.Set && html`<button class="ghost small" title="Remove its value from the Keychain" onClick=${clear}>Clear</button>`}</td>
  </tr>`;
}

// Variables is an application's environments without leaving the request: a tab for each, then its
// values (edit, remove, add) and its keys (set, replace, clear, add), as tables.
function Variables({ app, reload, onClose }) {
  const [name, setName] = useState(app.Current);
  const [nv, setNv] = useState({ n: '', v: '' });
  const [err, setErr] = useState('');
  const env = app.Envs.find((e) => e.Name === name) || app.Envs[0];
  if (!env) return '';
  const vars = Object.keys(env.Vars || {}).sort((a, b) => (a === 'baseUrl' ? -1 : b === 'baseUrl' ? 1 : a.localeCompare(b)));
  const add = async () => {
    try { await api('POST', '/reqs/vars', { App: app.Name, Env: env.Name, Name: nv.n.trim(), Value: nv.v }); setNv({ n: '', v: '' }); setErr(''); reload(); } catch (e) { setErr(e.message); }
  };
  return html`<${Modal} title=${app.Name + ' · variables'} wide=${true} onClose=${onClose}>
    <div class="filters rq-envtabs">${app.Envs.map((e) => html`<button key=${e.Name} class=${'fchip' + (e.Name === env.Name ? ' on' : '')} onClick=${() => setName(e.Name)}>${e.Name}${e.Name === app.Current ? html` <span class="n">in use</span>` : ''}</button>`)}</div>
    <div class="rq-vt-h">Values</div><div class="meta">Plain: crew and Claude can read them. ⏎ or leaving a field saves it.</div>
    <table class="rq-vt"><tbody>
      ${vars.map((n) => html`<${ValueRow} key=${env.Name + ':' + n} app=${app} env=${env} name=${n} reload=${reload} />`)}
      <tr class="rq-vt-new"><td><input placeholder="new name, like tenant" value=${nv.n} onInput=${(e) => setNv({ ...nv, n: e.target.value })} /></td>
        <td><input placeholder="value" value=${nv.v} onInput=${(e) => setNv({ ...nv, v: e.target.value })} onKeyDown=${(e) => e.key === 'Enter' && nv.n.trim() && add()} /></td>
        <td class="rq-vt-act"><button class="small" disabled=${!nv.n.trim()} onClick=${add}>+ Add</button></td></tr>
    </tbody></table>
    ${err && html`<p class="err">${err}</p>`}
    <div class="rq-vt-h">Keys</div><div class="meta">In the macOS Keychain, sent only to ${env.Hosts.join(', ') || 'the base URL'}, and never shown here.</div>
    <table class="rq-vt"><tbody>
      ${env.Keys.map((k) => html`<${KeyRow} key=${env.Name + ':' + k.Name} app=${app} env=${env} k=${k} reload=${reload} />`)}
      <${KeyRow} key=${env.Name + ':new'} app=${app} env=${env} k=${null} reload=${reload} />
    </tbody></table>
    <div class="btns"><button class="primary" onClick=${onClose}>Done</button></div><//>`;
}

// query splits a URL into what's before ? and its parameters, as written ({{variables}} kept).
const query = (url) => {
  const i = url.indexOf('?');
  if (i < 0) return [url, []];
  return [url.slice(0, i), url.slice(i + 1).split('&').filter(Boolean).map((p) => { const j = p.indexOf('='); return j < 0 ? [p, ''] : [p.slice(0, j), p.slice(j + 1)]; })];
};
const withQuery = (base, params) => base + (params.length ? '?' + params.map(([k, v]) => (v === '' ? k : k + '=' + v)).join('&') : '');

// formatJSON pretty-prints a JSON body with {{variables}} in it: each goes out as a number nobody
// writes, through JSON.parse, and back.
function formatJSON(body) {
  const vars = [];
  const marked = body.replace(/\{\{\s*[\w.-]+\s*\}\}/g, (m) => String(987654321000 + vars.push(m) - 1));
  const out = JSON.stringify(JSON.parse(marked), null, 2);
  return out.replace(/9876543210\d\d/g, (n) => vars[+n - 987654321000] ?? n);
}

// resHeight is the response's height in px, as you last dragged the split (0: the stylesheet's).
let resHeight = 0;

// dragSplit resizes the response from the line above it, keeping room for the request.
function dragSplit(e) {
  const res = e.currentTarget.nextElementSibling, page = e.currentTarget.parentElement, y = e.clientY, h = res.offsetHeight;
  e.preventDefault();
  const move = (m) => { resHeight = Math.max(80, Math.min(page.offsetHeight - 220, h - (m.clientY - y))); res.style.height = resHeight + 'px'; };
  const up = () => { removeEventListener('mousemove', move); removeEventListener('mouseup', up); };
  addEventListener('mousemove', move);
  addEventListener('mouseup', up);
}

// jsonNote says whether a body that looks like JSON is valid ({{variables}} allowed), and where not.
function jsonNote(body) {
  const t = body.trim();
  if (!t || !'{['.includes(t[0])) return null;
  try { formatJSON(body); return { ok: true, text: '✓ valid JSON' }; } catch (e) {
    const at = /position (\d+)/.exec(e.message);
    const line = at ? body.slice(0, +at[1]).split('\n').length : 0;
    return { ok: false, text: '✗ ' + (line ? 'line ' + line + ': ' : '') + e.message.replace(/^JSON\.parse: |^JSON Parse error: /, '').replace(/ in JSON at position \d+.*$/, '') };
  }
}

// tabIndent makes Tab indent by two spaces in a code box, instead of leaving it.
function tabIndent(e, set) {
  if (e.key !== 'Tab' || e.metaKey || e.shiftKey) return;
  e.preventDefault();
  const b = e.target, s = b.selectionStart, v = b.value;
  set(v.slice(0, s) + '  ' + v.slice(b.selectionEnd));
  requestAnimationFrame(() => { b.selectionStart = b.selectionEnd = s + 2; });
}

// curl is the request as a curl command, with the environment's plain values filled in and keys left
// as {{name}}: a secret never goes to the clipboard.
function curl(r, rows, env) {
  const q = (s) => "'" + s.replace(/'/g, "'\\''") + "'";
  const parts = ['curl', '-X', r.Method, q(resolve(r.URL, env))];
  if (env && !r.NoAuth && env.AuthKind === 'bearer') parts.push('-H', q('Authorization: Bearer {{' + env.AuthKey + '}}'));
  if (env && !r.NoAuth && env.AuthKind === 'header') parts.push('-H', q(env.AuthHeader + ': {{' + env.AuthKey + '}}'));
  for (const x of rows) if (x.on && x.n.trim()) parts.push('-H', q(x.n.trim() + ': ' + resolve(x.v, env)));
  if (r.Body) parts.push('--data-raw', q(resolve(r.Body, env)));
  if (env && env.Insecure) parts.push('--insecure');
  return parts.join(' ');
}

// Editor is a request: its name, folder, method, URL and the environment it's sent with; the
// variables it uses, each editable where it is; its params, headers (each on or off), body and auth;
// and the response.
function Editor({ q, app, data, reload, onSaved, onDeleted }) {
  const [r, setR] = useState(q);
  const [rows, setRows] = useState([]);
  const [tab, setTab] = useState('params');
  const [res, setRes] = useState(null);
  const [err, setErr] = useState('');
  const [sending, setSending] = useState(false);
  const [note, setNote] = useState('');
  const [folder, setFolder] = useState(q.Folder);
  const [dirty, setDirty] = useState(!q.File);
  const [look, setLook] = useState(false);
  useEffect(() => {
    setR(q); setFolder(q.Folder); setDirty(!q.File); setNote(''); setRes(null); setErr('');
    setRows([...(q.Headers || []).map(([n, v]) => ({ n, v, on: true })), ...(q.Off || []).map(([n, v]) => ({ n, v, on: false }))]);
    setTab(q.URL.includes('?') ? 'params' : q.Body ? 'body' : 'params');
  }, [q.ID || q.App + ':new']);
  const env = app.Envs.find((e) => e.Name === app.Current);
  const edit = (patch) => { setR({ ...r, ...patch }); setDirty(true); };
  const editRows = (next) => { setRows(next); setDirty(true); };
  const [base, params] = query(r.URL);
  const editParams = (next) => edit({ URL: withQuery(base, next) });
  const used = varsIn(r.URL, ...rows.filter((x) => x.on).map((x) => x.v), r.Body);
  const request = () => ({ ...r, Headers: rows.filter((x) => x.on && x.n.trim()).map((x) => [x.n.trim(), x.v]), Off: rows.filter((x) => !x.on && x.n.trim()).map((x) => [x.n.trim(), x.v]) });
  const send = async () => {
    setSending(true); setErr('');
    try { setRes(await api('POST', '/reqs/send', { Request: request(), Env: app.Current })); } catch (e) { setRes(null); setErr(e.message); }
    setSending(false);
  };
  const save = async () => {
    try {
      const saved = await api('POST', '/reqs/save', { Request: request(), Folder: folder });
      setDirty(false); setNote('Saved ✓'); onSaved(saved);
    } catch (e) { setNote(''); setErr(e.message); }
  };
  const del = async () => {
    if (!r.File) return onDeleted();
    if (!(await ask('Delete ' + (r.Name || 'this request') + '?', 'Delete'))) return;
    try { await api('POST', '/reqs/delete', { Request: q }); onDeleted(); } catch (e) { setErr(e.message); }
  };
  const switchEnv = async (name) => { await api('POST', '/reqs/current', { App: app.Name, Env: name }); reload(); };
  const copyCurl = () => navigator.clipboard.writeText(curl(r, rows, env)).then(() => { setNote('cURL copied ✓'); setTimeout(() => setNote(''), 1500); });
  const format = () => { try { edit({ Body: formatJSON(r.Body) }); } catch (e) { setNote("It isn't JSON: " + e.message); } };
  const ref = useRef({});
  ref.current = { send, save };
  useEffect(() => {
    const k = (e) => {
      if (e.metaKey && e.key === 'Enter') { e.preventDefault(); ref.current.send(); }
      if (e.metaKey && e.key === 's') { e.preventDefault(); ref.current.save(); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  const authHint = r.NoAuth ? 'none for this request' : env ? env.AuthLabel : 'no environment yet';
  const kv = (list, set, placeholder) => html`<table class="rq-heads"><tbody>
      ${list.map((x, i) => html`<tr key=${i}><td><input placeholder="Name" value=${x[0]} onInput=${(e) => set(list.map((y, j) => (j === i ? [e.target.value, y[1]] : y)))} /></td>
        <td><input placeholder=${placeholder} value=${x[1]} onInput=${(e) => set(list.map((y, j) => (j === i ? [y[0], e.target.value] : y)))} /></td>
        <td><button class="ghost" title="Remove" onClick=${() => set(list.filter((_, j) => j !== i))}>✕</button></td></tr>`)}
    </tbody></table>`;
  return html`<main class="page rq-page">
    <header class="rq-top">
      <span class="rq-crumb">${app.Name}</span><span class="rq-sep">/</span>
      <select class="rq-folder" title="Its folder (Save moves it)" value=${folder} onChange=${(e) => { setFolder(e.target.value); setDirty(true); }}>
        <option value="">${data.DefaultFolder}</option>${app.Folders.map((f) => html`<option value=${f}>${f}</option>`)}</select>
      <span class="rq-sep">/</span>
      <input class="rq-name" placeholder="Request name" value=${r.Name} onInput=${(e) => edit({ Name: e.target.value })} />
      ${dirty ? html`<span class="rq-dirty">unsaved</span>` : note && html`<span class="rq-saved">${note}</span>`}
      <span class="rq-fill"></span>
      <button class="small" title="Copy as a curl command (keys stay {{name}})" onClick=${copyCurl}>cURL</button>
      <button class="small" title="Save (⌘S)" onClick=${save}>Save</button><button class="small danger" onClick=${del}>Delete</button>
    </header>
    <div class="rq-line">
      <select class=${'rq-msel rq-' + r.Method.toLowerCase()} value=${r.Method} onChange=${(e) => edit({ Method: e.target.value })}>${data.Methods.map((m) => html`<option value=${m}>${m}</option>`)}</select>
      <input class="rq-url-in" value=${r.URL} placeholder="{{baseUrl}}/path" onInput=${(e) => edit({ URL: e.target.value })} onKeyDown=${(e) => e.key === 'Enter' && !e.metaKey && send()} />
      ${app.Envs.length ? html`<select class="rq-envpick" title="The environment it's sent with" value=${app.Current} onChange=${(e) => switchEnv(e.target.value)}>${app.Envs.map((e) => html`<option value=${e.Name}>${e.Name}</option>`)}</select>
        <button class="small" title="See and change the environments' values and keys" disabled=${!env} onClick=${() => setLook(true)}>Variables</button>` : ''}
      <button class="primary" disabled=${sending} title="Send (⌘⏎)" onClick=${send}>${sending ? 'Sending…' : 'Send'}</button>
    </div>
    <div class="rq-to"><span class="meta">→ ${resolve(r.URL, env)}</span>${!env && html`<span class="meta"> · no environment: add one on the application</span>`}</div>
    ${env && used.length > 0 && html`<div class="rq-vars"><span class="meta">Uses</span>${used.map((n) => html`<${VarChip} key=${n} name=${n} app=${app} env=${env} reload=${reload} />`)}</div>`}
    <section class="rq-req"><div class="tabs">
      <button class=${tab === 'params' ? 'on' : ''} onClick=${() => setTab('params')}>Params${params.length ? html` <span class="meta">${params.length}</span>` : ''}</button>
      <button class=${tab === 'headers' ? 'on' : ''} onClick=${() => setTab('headers')}>Headers <span class="meta">${rows.filter((x) => x.on).length}</span></button>
      <button class=${tab === 'body' ? 'on' : ''} onClick=${() => setTab('body')}>Body${r.Body ? ' •' : ''}</button>
      <button class=${tab === 'auth' ? 'on' : ''} onClick=${() => setTab('auth')}>Auth</button></div>
    <div class="rq-edit">${tab === 'params' ? html`${kv(params, editParams, 'Value, {{variable}} works')}
        <button class="small" onClick=${() => editParams([...params, ['', '']])}>+ Param</button>
        <p class="meta">Kept in step with the URL: edit either.</p>`
      : tab === 'headers' ? html`<table class="rq-heads"><tbody>
        ${rows.map((x, i) => html`<tr key=${i} class=${x.on ? '' : 'rq-off'}>
          <td><input type="checkbox" checked=${x.on} title="Send this header" onChange=${() => editRows(rows.map((y, j) => (j === i ? { ...y, on: !y.on } : y)))} /></td>
          <td><input placeholder="Name" value=${x.n} onInput=${(e) => editRows(rows.map((y, j) => (j === i ? { ...y, n: e.target.value } : y)))} /></td>
          <td><input placeholder="Value, {{variable}} for a key" value=${x.v} onInput=${(e) => editRows(rows.map((y, j) => (j === i ? { ...y, v: e.target.value } : y)))} /></td>
          <td><button class="ghost" title="Remove" onClick=${() => editRows(rows.filter((_, j) => j !== i))}>✕</button></td></tr>`)}
        </tbody></table><button class="small" onClick=${() => editRows([...rows, { n: '', v: '', on: true }])}>+ Header</button>`
      : tab === 'body' ? html`<div class="rq-bodybar">${(() => { const n = jsonNote(r.Body); return n ? html`<span class=${n.ok ? 'rq-jok' : 'rq-jbad'}>${n.text}</span>` : html`<span class="meta">{{variables}} are filled in when it's sent</span>`; })()}
            <button class="small" disabled=${!r.Body.trim()} onClick=${format}>Format JSON</button></div>
          <textarea class="rq-body" spellcheck="false" placeholder="Body (JSON, form…), {{variables}} filled in when it's sent" value=${r.Body}
            onInput=${(e) => edit({ Body: e.target.value })} onKeyDown=${(e) => tabIndent(e, (v) => edit({ Body: v }))}></textarea>`
      : html`<div class="rq-auth"><p>Auth: <b>${authHint}</b></p>
          <label class="rq-check"><input type="checkbox" checked=${!r.NoAuth} onChange=${() => edit({ NoAuth: !r.NoAuth })} /> Use the environment's auth</label>
          <p class="meta">Keys are filled in only when the request is sent, only to ${env ? env.Hosts.join(', ') || 'the base URL' : "the environment's hosts"}, and never shown here.</p></div>`}</div></section>
    ${(res || err || sending) && html`<div class="rq-split" title="Drag to resize" onMouseDown=${dragSplit}></div>`}
    <${Response} res=${res} err=${err} sending=${sending} height=${resHeight} />
    ${look && html`<${Variables} app=${app} reload=${reload} onClose=${() => setLook(false)} />`}
  </main>`;
}

// AppPage is an application: its environments (base URL, auth, keys set or not) and folders.
function AppPage({ app, reload, setForm, onNewRequest }) {
  const setCurrent = async (env) => { await api('POST', '/reqs/current', { App: app.Name, Env: env }); reload(); };
  const delEnv = async (env) => { if (await ask('Delete ' + env + ' and its keys?', 'Delete')) { await api('POST', '/reqs/envs/delete', { App: app.Name, Name: env }); reload(); } };
  const delFolder = async (f) => { if (await ask('Delete the folder ' + f + ' and its requests?', 'Delete')) { await api('POST', '/reqs/folders/delete', { App: app.Name, Name: f }); reload(); } };
  return html`<main class="page"><${Toolbar} title=${app.Name} sub=${app.Requests.length + ' requests · ' + app.Envs.length + ' environments'}>
      <button onClick=${() => setForm({ kind: 'env', app: app.Name })}>+ Environment</button>
      <button onClick=${() => setForm({ kind: 'folder', app: app.Name })}>+ Folder</button>
      <button class="primary" title="New request (n)" onClick=${onNewRequest}>+ Request</button><//>
    <div class="scroll">
      <h3 class="rq-h">Environments</h3>
      ${app.Envs.length ? html`<div class="rq-envs">${app.Envs.map((e) => html`<div key=${e.Name} class=${'rq-env' + (e.Name === app.Current ? ' on' : '')}>
          <div class="rq-env-head"><b>${e.Name}</b>${e.Name === app.Current ? html`<span class="chip">in use</span>` : html`<button class="small" onClick=${() => setCurrent(e.Name)}>Use</button>`}
            ${e.Insecure && html`<span class="chip rq-warn" title="Certificates aren't checked">insecure</span>`}
            <span class="rq-fill"></span><button class="small" onClick=${() => setForm({ kind: 'env', app: app.Name, env: e })}>Edit</button>
            <button class="small danger" onClick=${() => delEnv(e.Name)}>Delete</button></div>
          <div class="rq-mono">${e.Base || html`<span class="err">no base URL</span>`}</div>
          <div class="meta">${e.AuthLabel}${e.Hosts.length ? ' · keys go only to ' + e.Hosts.join(', ') : ''}</div>
          ${Object.keys(e.Vars).filter((k) => k !== 'baseUrl').length > 0 && html`<div class="meta">Values: ${Object.entries(e.Vars).filter(([k]) => k !== 'baseUrl').map(([k, v]) => k + '=' + v).join(' · ')}</div>`}
          <div class="rq-keys">${e.Keys.map((k) => html`<span key=${k.Name} class=${'rq-keychip' + (k.Set ? ' set' : '')} title=${k.Set ? 'Its value is in the Keychain' : 'No value yet'}>${k.Set ? '●' : '○'} ${k.Name}
              <button class="ghost small" onClick=${() => setForm({ kind: 'key', app: app.Name, env: e.Name, name: k.Name })}>${k.Set ? 'Replace' : 'Set'}</button>
              ${k.Set && html`<button class="ghost small danger" title="Remove its value from the Keychain" onClick=${async () => { if (await ask('Remove the value of ' + k.Name + '?', 'Remove')) { await api('POST', '/reqs/keys/delete', { App: app.Name, Env: e.Name, Name: k.Name }); reload(); } }}>✕</button>`}</span>`)}
            <button class="small" onClick=${() => setForm({ kind: 'key', app: app.Name, env: e.Name, name: '' })}>+ Key</button></div>
        </div>`)}</div>`
      : html`<p class="meta">No environments yet. One has a base URL and, if the API needs it, an auth key. <button class="small primary" onClick=${() => setForm({ kind: 'env', app: app.Name })}>Add one</button></p>`}
      <h3 class="rq-h">Folders</h3>
      <div class="rq-folders">${app.Folders.length ? app.Folders.map((f) => html`<span key=${f} class="chip">${f} <button class="ghost small danger" onClick=${() => delFolder(f)}>✕</button></span>`) : html`<span class="meta">Requests sit in the default folder until you add one.</span>`}</div>
    </div></main>`;
}

// ImportSpec brings an OpenAPI spec (a file, or pasted; YAML or JSON) into an application: by the
// spec's tags into folders, or all into one. It says what it added and skipped, and offers the spec's
// servers as an environment's base URL.
function ImportSpec({ app, folder, data, close, reload, addEnv }) {
  const [text, setText] = useState('');
  const [byTag, setByTag] = useState(!folder);
  const [into, setInto] = useState(folder || '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [done, setDone] = useState(null);
  const read = (e) => { const f = e.target.files[0]; if (f) f.text().then(setText); };
  const go = async () => {
    setBusy(true); setErr('');
    try { setDone(await api('POST', '/reqs/import', { App: app.Name, Spec: text, Folder: byTag ? '' : into, ByTag: byTag })); reload(); } catch (e) { setErr(e.message); }
    setBusy(false);
  };
  if (done) return html`<${Modal} title=${'Imported into ' + app.Name} onClose=${close}>
    <p><b>${done.Added}</b> request${done.Added === 1 ? '' : 's'} added${done.Folders && done.Folders.length ? html` in ${done.Folders.join(', ')}` : ''}${done.Skipped ? html`; <b>${done.Skipped}</b> already there, skipped` : ''}.</p>
    ${done.Servers && done.Servers.length > 0 && html`<div class="rq-vt-h">Servers in the spec</div>
      ${done.Servers.map((u) => html`<div key=${u} class="hidden-row"><code>${u}</code><button class="small" onClick=${() => addEnv(u)}>Add as environment</button></div>`)}`}
    <div class="btns"><button class="primary" onClick=${close}>Done</button></div><//>`;
  return html`<${Modal} title=${'Import OpenAPI into ' + app.Name} wide=${true} onClose=${close}>
    <div class="rq-imp">
      <label class="rq-file">Choose a file <input type="file" accept=".yaml,.yml,.json" onChange=${read} /></label><span class="meta">or paste the spec below (OpenAPI 3 or Swagger 2, YAML or JSON)</span>
    </div>
    <textarea class="rq-spec" rows="12" spellcheck="false" placeholder="openapi: 3.0.0…" value=${text} onInput=${(e) => setText(e.target.value)}></textarea>
    <div class="rq-imp-opts">
      <label class="rq-check"><input type="radio" checked=${byTag} onChange=${() => setByTag(true)} /> Folders from the spec's tags</label>
      <label class="rq-check"><input type="radio" checked=${!byTag} onChange=${() => setByTag(false)} /> All into one folder:</label>
      <input class="rq-into" list="rq-folders" placeholder=${data.DefaultFolder} value=${into} disabled=${byTag} onInput=${(e) => setInto(e.target.value)} />
      <datalist id="rq-folders">${app.Folders.map((f) => html`<option value=${f} />`)}</datalist>
    </div>
    <p class="meta">Each operation becomes a request: {param} becomes {{param}}, a JSON body comes from its example or schema, and one already here (same method and URL) is skipped.</p>
    ${err && html`<p class="err">${err}</p>`}
    <div class="btns"><button onClick=${close}>Cancel</button><button class="primary" disabled=${busy || !text.trim()} onClick=${go}>${busy ? 'Importing…' : 'Import'}</button></div><//>`;
}

// ExportSpec shows an application (or one folder) as an OpenAPI 3 spec, YAML or JSON, to copy or to
// save in Downloads. Keys stay {{name}}: a spec is something you share.
function ExportSpec({ app, folder, close }) {
  const [format, setFormat] = useState('yaml');
  const [text, setText] = useState('');
  const [note, setNote] = useState('');
  const [err, setErr] = useState('');
  const body = (save) => api('POST', '/reqs/export', { App: app.Name, Folder: folder || '', Format: format, Save: save });
  useEffect(() => { body(false).then((r) => { setText(r.text); setErr(''); }, (e) => setErr(e.message)); }, [format]);
  const copy = () => navigator.clipboard.writeText(text).then(() => { setNote('Copied ✓'); setTimeout(() => setNote(''), 1500); });
  const save = () => body(true).then((r) => setNote('Saved to ' + r.path.replace(/^\/Users\/[^/]+/, '~')), (e) => setErr(e.message));
  return html`<${Modal} title=${'Export ' + app.Name + (folder ? ' / ' + folder : '') + ' as OpenAPI'} wide=${true} onClose=${close}>
    <div class="rq-imp"><span class="rq-seg"><button class=${format === 'yaml' ? 'on' : ''} onClick=${() => setFormat('yaml')}>YAML</button><button class=${format === 'json' ? 'on' : ''} onClick=${() => setFormat('json')}>JSON</button></span>
      <span class="meta">Its environments' base URLs are the servers; keys stay {{name}}, never their values.</span></div>
    <textarea class="rq-spec" rows="16" readonly spellcheck="false" value=${text}></textarea>
    ${err && html`<p class="err">${err}</p>`}
    <div class="btns"><span class="meta" style="margin-right:auto">${note}</span><button onClick=${copy} disabled=${!text}>Copy</button>
      <button class="primary" onClick=${save} disabled=${!text}>Save to Downloads</button></div><//>`;
}

// Forms: a new application, a folder, an environment (new or changed), a key's value.
function Forms({ form, data, close, reload, after, setForm }) {
  const appOf = (n) => (data.Apps || []).find((a) => a.Name === n);
  if (form.kind === 'import') return html`<${ImportSpec} app=${appOf(form.app)} folder=${form.folder} data=${data} close=${close} reload=${reload}
    addEnv=${(base) => setForm({ kind: 'env', app: form.app, base })} />`;
  if (form.kind === 'export') return html`<${ExportSpec} app=${appOf(form.app)} folder=${form.folder} close=${close} />`;
  if (form.kind === 'rename-app') return html`<${Form} title=${'Rename ' + form.app} action="Rename" onClose=${close} initial=${{ To: form.app }}
    fields=${[{ key: 'To', label: 'New name (one word; its keys move with it in the Keychain)' }]}
    submit=${async (v) => { await api('POST', '/reqs/apps/rename', { Name: form.app, To: v.To }); await reload(); after('app:' + v.To.trim().toLowerCase()); }} />`;
  if (form.kind === 'rename-folder') return html`<${Form} title=${'Rename ' + form.app + ' / ' + form.folder} action="Rename" onClose=${close} initial=${{ To: form.folder }}
    fields=${[{ key: 'To', label: 'New name (one word)' }]}
    submit=${async (v) => { await api('POST', '/reqs/folders/rename', { App: form.app, Name: form.folder, To: v.To }); await reload(); }} />`;
  if (form.kind === 'rename-req') return html`<${Form} title="Rename the request" action="Rename" onClose=${close} initial=${{ Name: form.req.Name }}
    fields=${[{ key: 'Name', label: 'Name' }]}
    submit=${async (v) => { const saved = await api('POST', '/reqs/save', { Request: { ...form.req, Name: v.Name }, Folder: form.req.Folder }); await reload(); after('req:' + saved.ID); }} />`;
  if (form.kind === 'app') return html`<${Form} title="New application" action="Add" onClose=${close} fields=${[{ key: 'Name', label: 'Name (one word, like accounts)' }]}
    submit=${async (v) => { await api('POST', '/reqs/apps', v); reload(); }} />`;
  if (form.kind === 'folder') return html`<${Form} title=${form.app + ' · new folder'} action="Add" onClose=${close} fields=${[{ key: 'Name', label: 'Name (one word, like orgs)' }]}
    submit=${async (v) => { await api('POST', '/reqs/folders', { App: form.app, ...v }); reload(); }} />`;
  if (form.kind === 'key') return html`<${Form} title=${form.app + ' · ' + form.env + (form.name ? ' · ' + form.name : ' · new key')} action="Save to Keychain" onClose=${close}
    initial=${{ Name: form.name }}
    fields=${[...(form.name ? [] : [{ key: 'Name', label: 'Key name (one word, used as {{name}})' }]), { key: 'Value', label: "Value (goes to the macOS Keychain; it's never shown again)", type: 'password' }]}
    submit=${async (v) => { await api('POST', '/reqs/keys', { App: form.app, Env: form.env, Name: v.Name || form.name, Value: v.Value }); reload(); }} />`;
  const e = form.env;
  const vars = e ? Object.entries(e.Vars).filter(([k]) => k !== 'baseUrl').map(([k, v]) => k + '=' + v).join('; ') : '';
  return html`<${Form} title=${form.app + (e ? ' · ' + e.Name : ' · new environment')} action="Save" onClose=${close}
    initial=${{ Name: e ? e.Name : '', Base: e ? e.Base : form.base || '', AuthKind: e ? e.AuthKind : '', AuthHeader: e ? e.AuthHeader : '', AuthKey: e ? e.AuthKey : '', Hosts: e ? (e.Extra || []).join(', ') : '', Vars: vars, Insecure: e && e.Insecure ? 'insecure' : '' }}
    fields=${[...(e ? [] : [{ key: 'Name', label: 'Name (one word, like dev or staging)' }]),
      { key: 'Base', label: 'Base URL', placeholder: 'https://dev.api.example.com' },
      { key: 'AuthKind', label: 'Auth', type: 'select', options: [{ value: '', label: 'none' }, { value: 'bearer', label: 'Bearer token' }, { value: 'header', label: 'A key in a header' }] },
      { key: 'AuthHeader', label: 'Header (for a key in a header)', placeholder: 'X-Api-Key' },
      { key: 'AuthKey', label: 'Key name (for auth)', placeholder: 'token' },
      { key: 'Value', label: "The key's value (optional; to the Keychain, never shown again)", type: 'password' },
      { key: 'Hosts', label: 'Other hosts its keys may go to (comma-separated)', placeholder: 'auth.dev.example.com' },
      { key: 'Vars', label: 'Other values, as name=value; name=value', placeholder: 'tenant=acme' },
      { key: 'Insecure', label: 'Certificates', type: 'select', options: [{ value: '', label: 'checked' }, { value: 'insecure', label: 'insecure (a dev server with a self-signed one)' }] }]}
    submit=${async (v) => {
      const pairs = {};
      (v.Vars || '').split(';').forEach((p) => { const [k, ...rest] = p.split('='); if (k && k.trim() && rest.length) pairs[k.trim()] = rest.join('=').trim(); });
      await api('POST', '/reqs/envs', { App: form.app, Name: e ? e.Name : v.Name, New: !e, Base: v.Base, AuthKind: v.AuthKind, AuthHeader: v.AuthHeader, AuthKey: v.AuthKey, Value: v.Value, Hosts: v.Hosts, Vars: pairs, Insecure: v.Insecure === 'insecure' });
      reload();
    }} />`;
}

export default function Requests() {
  const { data, err, reload } = useLoad(() => api('GET', '/reqs'));
  const [sel, setSel] = useState(decodeURIComponent(location.hash.split('/').slice(1).join('/')) || ''); // "app:<name>", "req:<id>", "new:<app>" or "chat"
  const [folded, setFolded] = useState({});
  const [q, setQ] = useState('');
  const [form, setForm] = useState(null);
  const [draft, setDraft] = useState(null);
  const [chatID, setChatID] = useState('');
  const go = (s) => { setSel(s); location.hash = 'reqs/' + s; };
  useEffect(() => { if (sel === 'chat' && !chatID) api('GET', '/reqs/chat').then((c) => setChatID(c.ID)); }, [sel]);
  const apps = (data && data.Apps) || [];
  const match = (r) => !q || (r.Name + ' ' + r.Method + ' ' + r.URL).toLowerCase().includes(q.toLowerCase());
  // the tree's rows, in order, for ↑↓
  const order = [];
  for (const a of apps) {
    order.push('app:' + a.Name);
    if (folded[a.Name] && !q) continue;
    for (const f of ['', ...a.Folders]) for (const r of a.Requests.filter((r) => r.Folder === f && match(r))) order.push('req:' + r.ID);
  }
  useListKeys(order, sel, go);
  const newReq = (app, folder) => { setDraft(newRequest(app, folder)); go('new:' + app); };
  const delApp = async (a) => {
    if (!(await ask(`Delete ${a.Name}: its ${a.Requests.length} request${a.Requests.length === 1 ? '' : 's'}, ${a.Envs.length} environment${a.Envs.length === 1 ? '' : 's'} and their keys in the Keychain? This can't be undone.`, 'Delete'))) return;
    try { await api('POST', '/reqs/apps/delete', { Name: a.Name }); await reload(); go(''); } catch (e) { ask(e.message); }
  };
  const delFolder = async (app, f, n) => {
    if (!(await ask(`Delete the folder ${f}${n ? ` and its ${n} request${n === 1 ? '' : 's'}` : ''}?`, 'Delete'))) return;
    try { await api('POST', '/reqs/folders/delete', { App: app, Name: f }); await reload(); } catch (e) { ask(e.message); }
  };
  const delReq = async (r) => {
    if (!(await ask('Delete ' + (r.Name || 'this request') + '?', 'Delete'))) return;
    try { await api('POST', '/reqs/delete', { Request: r }); await reload(); if (sel === 'req:' + r.ID) go('app:' + r.App); } catch (e) { ask(e.message); }
  };
  const dupReq = async (r) => {
    try { const saved = await api('POST', '/reqs/save', { Request: { ...r, File: '', Index: 0, Name: r.Name + ' copy' }, Folder: r.Folder }); await reload(); go('req:' + saved.ID); } catch (e) { ask(e.message); }
  };
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey) return;
      if (e.key === 'n') {
        const a = sel.startsWith('app:') ? sel.slice(4) : sel.startsWith('req:') ? sel.slice(4).split('/')[0] : apps[0] && apps[0].Name;
        if (a) { e.preventDefault(); newReq(a); }
      }
      if (e.key === 'ArrowLeft' && sel.startsWith('app:')) setFolded({ ...folded, [sel.slice(4)]: true });
      if (e.key === 'ArrowRight' && sel.startsWith('app:')) setFolded({ ...folded, [sel.slice(4)]: false });
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [sel, apps, folded]);

  let main;
  const appOf = (name) => apps.find((a) => a.Name === name);
  if (sel === 'chat') {
    main = html`<main class="page"><${Toolbar} title="Requests chat" sub="Claude, locked down to crew req: it sends your requests and never sees a key" />
      ${chatID ? html`<${Chat} id=${chatID} base=${'/tasks/' + chatID} agent="Claude" empty="Ask about your APIs: “send accounts' me request on dev”, “write a request that lists orgs”." />` : html`<div class="empty">Opening the chat…</div>`}</main>`;
  } else if (sel.startsWith('req:') || sel.startsWith('new:')) {
    let req = null, app = null;
    if (sel.startsWith('new:')) { app = appOf(sel.slice(4)); req = draft && app && draft.App === app.Name ? draft : app && newRequest(app.Name); }
    else for (const a of apps) { const r = a.Requests.find((r) => r.ID === sel.slice(4)); if (r) { req = r; app = a; } }
    main = req && app ? html`<${Editor} key=${sel} q=${req} app=${app} data=${data} reload=${reload}
        onSaved=${async (saved) => { await reload(); go('req:' + saved.ID); }} onDeleted=${async () => { await reload(); go('app:' + app.Name); }} />`
      : html`<main class="page"><${Empty} title="That request isn't there any more" /></main>`;
  } else if (sel.startsWith('app:') && appOf(sel.slice(4))) {
    main = html`<${AppPage} app=${appOf(sel.slice(4))} reload=${reload} setForm=${setForm} onNewRequest=${() => newReq(sel.slice(4))} />`;
  } else {
    main = html`<main class="page"><${Empty} title=${apps.length ? 'Pick an application or a request' : 'No applications yet'}>
      <p>An application has environments (a base URL, an auth key in the Keychain) and requests in folders, as .http files Claude can read.</p>
      <button class="primary" onClick=${() => setForm({ kind: 'app' })}>New application</button><//></main>`;
  }

  return html`<div class="split">
    <section class="col">
      <div class="col-head"><b>Requests</b><span>
        <button class=${'small' + (sel === 'chat' ? ' primary' : '')} title="The locked-down Claude chat" onClick=${() => go('chat')}>Chat</button>
        <button class="small" title="New application" onClick=${() => setForm({ kind: 'app' })}>+ App</button></span></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Filter requests" /></div>
      <div class="col-list">
        ${err && html`<div class="group err">${err}</div>`}
        ${!data && !err && html`<div class="group">Loading…</div>`}
        ${data && !apps.length && html`<div class="group">No applications yet</div>`}
        ${apps.map((a) => {
          const reqs = a.Requests.filter(match);
          if (q && !reqs.length) return '';
          const open = !folded[a.Name] || q;
          return html`<div key=${a.Name}>
            <div class=${'item rq-app' + (sel === 'app:' + a.Name ? ' on' : '')} onClick=${() => go('app:' + a.Name)}>
              <span class="rq-fold" onClick=${(e) => { e.stopPropagation(); setFolded({ ...folded, [a.Name]: !folded[a.Name] }); }}>${open ? '▾' : '▸'}</span>
              <div class="item-main"><div class="item-title"><b>${a.Name}</b></div></div>
              ${a.Envs.length > 0 && html`<select class="rq-envsel" title="The environment its requests are sent with" value=${a.Current} onClick=${(e) => e.stopPropagation()}
                onChange=${async (e) => { await api('POST', '/reqs/current', { App: a.Name, Env: e.target.value }); reload(); }}>${a.Envs.map((e) => html`<option value=${e.Name}>${e.Name}</option>`)}</select>`}
              <span onClick=${(e) => e.stopPropagation()}><${Menu} label="⋯" title="More" items=${[
                { label: 'New request', key: 'n', run: () => newReq(a.Name) },
                { label: 'New folder', run: () => setForm({ kind: 'folder', app: a.Name }) },
                { label: 'Environments & keys', run: () => go('app:' + a.Name) },
                { label: 'Import OpenAPI…', run: () => setForm({ kind: 'import', app: a.Name }) },
                { label: 'Export OpenAPI…', run: () => setForm({ kind: 'export', app: a.Name }) },
                { label: 'Rename…', run: () => setForm({ kind: 'rename-app', app: a.Name }) },
                { label: 'Delete…', danger: true, run: () => delApp(a) },
              ]} /></span></div>
            ${open && ['', ...a.Folders].map((f) => {
              const inF = reqs.filter((r) => r.Folder === f);
              if (!inF.length && (q || f === '')) return '';
              return html`<div key=${f}><div class="rq-folder-row"><span class="rq-fname">${folderName(f, data.DefaultFolder)}</span>
                  <${Menu} label="⋯" title="More" items=${[
                    { label: 'New request here', run: () => newReq(a.Name, f) },
                    { label: 'Import OpenAPI here…', run: () => setForm({ kind: 'import', app: a.Name, folder: f }) },
                    ...(f ? [{ label: 'Export as OpenAPI…', run: () => setForm({ kind: 'export', app: a.Name, folder: f }) }] : []),
                    ...(f ? [{ label: 'Rename…', run: () => setForm({ kind: 'rename-folder', app: a.Name, folder: f }) },
                      { label: 'Delete…', danger: true, run: () => delFolder(a.Name, f, inF.length) }] : []),
                  ]} /></div>
                ${inF.map((r) => html`<div key=${r.ID} class=${'item rq-item' + (sel === 'req:' + r.ID ? ' on' : '')} onClick=${() => go('req:' + r.ID)}>
                  <${Method} m=${r.Method} /><div class="item-main"><div class="item-title">${r.Name}</div></div>
                  <span class="rq-rowmenu" onClick=${(e) => e.stopPropagation()}><${Menu} label="⋯" title="More" items=${[
                    { label: 'Rename…', run: () => setForm({ kind: 'rename-req', req: r }) },
                    { label: 'Duplicate', run: () => dupReq(r) },
                    { label: 'Delete…', danger: true, run: () => delReq(r) },
                  ]} /></span></div>`)}</div>`;
            })}</div>`;
        })}
      </div>
    </section>
    ${main}
    ${form && html`<${Forms} form=${form} data=${data} close=${() => setForm(null)} reload=${reload} after=${go} setForm=${setForm} />`}
  </div>`;
}
