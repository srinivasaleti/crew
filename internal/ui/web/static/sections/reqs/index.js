// REQUESTS: an HTTP client like Postman's. Applications, their folders and requests on the left; the
// open request's editor and its response on the right; an application's environments and keys; the
// locked-down chat. Every rule (auth, keys only to the env's hosts, redaction) is crew's
// (internal/requests): this page draws and sends, and never sees a key's value after you type it.
import { html, useState, useEffect, useRef, api, useLoad, Toolbar, Search, Form, Empty, Chat, useListKeys, ask, css } from '../../lib.js';

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

// Response is what came back: status, time and size, then the body (JSON you can fold) or headers.
function Response({ res, err, sending }) {
  const [tab, setTab] = useState('body');
  if (sending) return html`<div class="rq-res"><div class="empty">Sending…</div></div>`;
  if (err) return html`<div class="rq-res"><div class="rq-res-bar"><span class="rq-status rq-bad">Not sent</span></div><pre class="err rq-err">${err}</pre></div>`;
  if (!res) return html`<div class="rq-res"><div class="empty">⌘⏎ sends the request; its response shows here.</div></div>`;
  let parsed;
  try { parsed = JSON.parse(res.Body); } catch { parsed = undefined; }
  const ok = res.Code < 400;
  return html`<div class="rq-res">
    <div class="rq-res-bar"><span class=${'rq-status ' + (ok ? 'rq-ok' : 'rq-bad')}>${res.Status}</span>
      <span class="meta">${res.Ms} ms · ${res.Size}</span><span class="meta rq-url">${res.URL}</span>
      <div class="tabs rq-tabs"><button class=${tab === 'body' ? 'on' : ''} onClick=${() => setTab('body')}>Body</button>
        <button class=${tab === 'headers' ? 'on' : ''} onClick=${() => setTab('headers')}>Headers <span class="meta">${res.Headers.length}</span></button>
        <button onClick=${() => navigator.clipboard && navigator.clipboard.writeText(res.Body)}>Copy body</button></div></div>
    <div class="rq-res-body">${tab === 'headers'
      ? html`<table class="grid"><tbody>${res.Headers.map(([k, v]) => html`<tr key=${k}><td class="rq-hname">${k}</td><td class="rq-mono">${v}</td></tr>`)}</tbody></table>`
      : parsed !== undefined && typeof parsed === 'object' ? html`<div class="rq-json"><${JSONView} v=${parsed} /></div>`
      : html`<pre class="rq-plain">${res.Body || '(empty)'}</pre>`}</div></div>`;
}

// Editor is a request: its method, URL, headers (each on or off) and body, sent or saved with keys.
function Editor({ q, app, data, onSaved, onDeleted }) {
  const [r, setR] = useState(q);
  const [rows, setRows] = useState([]);
  const [tab, setTab] = useState('headers');
  const [res, setRes] = useState(null);
  const [err, setErr] = useState('');
  const [sending, setSending] = useState(false);
  const [note, setNote] = useState('');
  const [folder, setFolder] = useState(q.Folder);
  const [dirty, setDirty] = useState(!q.File);
  useEffect(() => {
    setR(q); setFolder(q.Folder); setDirty(!q.File); setNote(''); setRes(null); setErr('');
    setRows([...(q.Headers || []).map(([n, v]) => ({ n, v, on: true })), ...(q.Off || []).map(([n, v]) => ({ n, v, on: false }))]);
  }, [q.ID || q.App + ':new']);
  const env = app.Envs.find((e) => e.Name === app.Current);
  const edit = (patch) => { setR({ ...r, ...patch }); setDirty(true); };
  const editRows = (next) => { setRows(next); setDirty(true); };
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
  return html`<main class="page rq-page">
    <${Toolbar} title=${html`<input class="rq-name" placeholder="Request name" value=${r.Name} onInput=${(e) => edit({ Name: e.target.value })} />`}
      sub=${html`${app.Name} · <select class="rq-folder" value=${folder} onChange=${(e) => { setFolder(e.target.value); setDirty(true); }}>
        <option value="">${data.DefaultFolder}</option>${app.Folders.map((f) => html`<option value=${f}>${f}</option>`)}</select>
        ${dirty ? html`<span class="rq-dirty">unsaved</span>` : note && html`<span class="rq-saved">${note}</span>`}`}>
      <button title="Save (⌘S)" onClick=${save}>Save</button><button class="danger" onClick=${del}>Delete</button><//>
    <div class="rq-line">
      <select class="rq-msel" value=${r.Method} onChange=${(e) => edit({ Method: e.target.value })}>${data.Methods.map((m) => html`<option value=${m}>${m}</option>`)}</select>
      <input class="rq-url-in" value=${r.URL} placeholder="{{baseUrl}}/path" onInput=${(e) => edit({ URL: e.target.value })} onKeyDown=${(e) => e.key === 'Enter' && !e.metaKey && send()} />
      <button class="primary" disabled=${sending} title="Send (⌘⏎)" onClick=${send}>${sending ? 'Sending…' : 'Send'}</button>
    </div>
    <div class="rq-to meta">→ ${resolve(r.URL, env)}${env ? html` · <b>${env.Name}</b>` : ' · no environment: add one on the application'}</div>
    <div class="tabs"><button class=${tab === 'headers' ? 'on' : ''} onClick=${() => setTab('headers')}>Headers <span class="meta">${rows.filter((x) => x.on).length}</span></button>
      <button class=${tab === 'body' ? 'on' : ''} onClick=${() => setTab('body')}>Body${r.Body ? ' •' : ''}</button>
      <button class=${tab === 'auth' ? 'on' : ''} onClick=${() => setTab('auth')}>Auth</button></div>
    <div class="rq-edit">${tab === 'headers' ? html`<table class="rq-heads"><tbody>
        ${rows.map((x, i) => html`<tr key=${i} class=${x.on ? '' : 'rq-off'}>
          <td><input type="checkbox" checked=${x.on} title="Send this header" onChange=${() => editRows(rows.map((y, j) => (j === i ? { ...y, on: !y.on } : y)))} /></td>
          <td><input placeholder="Name" value=${x.n} onInput=${(e) => editRows(rows.map((y, j) => (j === i ? { ...y, n: e.target.value } : y)))} /></td>
          <td><input placeholder="Value, {{variable}} for a key" value=${x.v} onInput=${(e) => editRows(rows.map((y, j) => (j === i ? { ...y, v: e.target.value } : y)))} /></td>
          <td><button class="ghost" title="Remove" onClick=${() => editRows(rows.filter((_, j) => j !== i))}>✕</button></td></tr>`)}
        </tbody></table><button class="small" onClick=${() => editRows([...rows, { n: '', v: '', on: true }])}>+ Header</button>`
      : tab === 'body' ? html`<textarea class="rq-body" spellcheck="false" placeholder="Body (JSON, form…), {{variables}} filled in when sent" value=${r.Body} onInput=${(e) => edit({ Body: e.target.value })}></textarea>`
      : html`<div class="rq-auth"><p>Auth: <b>${authHint}</b></p>
          <label class="rq-check"><input type="checkbox" checked=${!r.NoAuth} onChange=${() => edit({ NoAuth: !r.NoAuth })} /> Use the environment's auth</label>
          <p class="meta">Keys are filled in only when the request is sent, only to ${env ? env.Hosts.join(', ') || 'the base URL' : "the environment's hosts"}, and never shown here.</p></div>`}</div>
    <${Response} res=${res} err=${err} sending=${sending} />
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

// Forms: a new application, a folder, an environment (new or changed), a key's value.
function Forms({ form, data, close, reload }) {
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
    initial=${{ Name: e ? e.Name : '', Base: e ? e.Base : '', AuthKind: e ? e.AuthKind : '', AuthHeader: e ? e.AuthHeader : '', AuthKey: e ? e.AuthKey : '', Hosts: e ? (e.Extra || []).join(', ') : '', Vars: vars, Insecure: e && e.Insecure ? 'insecure' : '' }}
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
    main = req && app ? html`<${Editor} key=${sel} q=${req} app=${app} data=${data}
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
              <button class="ghost small" title="New request" onClick=${(e) => { e.stopPropagation(); newReq(a.Name); }}>+</button></div>
            ${open && ['', ...a.Folders].map((f) => {
              const inF = reqs.filter((r) => r.Folder === f);
              if (!inF.length && (q || f === '')) return '';
              return html`<div key=${f}><div class="rq-folder-row">${folderName(f, data.DefaultFolder)}
                  <button class="ghost small" title=${'New request in ' + folderName(f, data.DefaultFolder)} onClick=${() => newReq(a.Name, f)}>+</button></div>
                ${inF.map((r) => html`<div key=${r.ID} class=${'item rq-item' + (sel === 'req:' + r.ID ? ' on' : '')} onClick=${() => go('req:' + r.ID)}>
                  <${Method} m=${r.Method} /><div class="item-main"><div class="item-title">${r.Name}</div></div></div>`)}</div>`;
            })}</div>`;
        })}
      </div>
    </section>
    ${main}
    ${form && html`<${Forms} form=${form} data=${data} close=${() => setForm(null)} reload=${reload} />`}
  </div>`;
}
