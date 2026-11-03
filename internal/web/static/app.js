// crew's web UI: a Slack-like list of tasks and a chat per task. It draws and sends; every rule
// (budget, queue, worktrees, what a status is) is crew's (internal/core), so nothing is decided here.
import { html, render, useState, useEffect, useRef } from './vendor/preact-htm.js';

const api = async (method, path, body) => {
  const res = await fetch('/api' + path, { method, body: body && JSON.stringify(body) });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
};
const clock = (at) => (at ? new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '');
const base = (p) => (p || '').split('/').filter(Boolean).pop();

// A reply's ``` blocks as code, the rest as it is.
function Text({ text, live }) {
  const parts = text.split(/```[^\n]*\n?/);
  return html`<div class=${'text' + (live ? ' live' : '')}>${parts.map((p, i) => (i % 2 ? html`<pre>${p.replace(/\n$/, '')}</pre>` : p))}</div>`;
}

// Messages as Slack shows them: a name and time on the first of a run by one sender, and tool calls
// folded into one line you can open.
function Messages({ msgs, live, onActs }) {
  const items = [];
  let acts = null, last = null;
  msgs.forEach((m, i) => {
    if (m.role === 'tool' || m.role === 'result') {
      if (!acts) items.push((acts = { acts: [] }));
      acts.acts.push(m);
      return;
    }
    acts = null;
    if (m.role === 'error') { items.push({ note: m }); last = null; return; }
    const first = !last || last.role !== m.role || new Date(m.at) - new Date(last.at) > 5 * 60000;
    items.push({ m, first });
    last = m;
  });
  return items.map((it, i) => {
    if (it.acts) {
      const calls = it.acts.filter((a) => a.role === 'tool');
      return html`<div class="acts" key=${i} onClick=${() => onActs(it.acts)}>▸ ${calls.length} action${calls.length === 1 ? '' : 's'}${calls.length ? ': ' + calls[calls.length - 1].text.slice(0, 80) : ''}</div>`;
    }
    if (it.note) return html`<div class="msg error" key=${i}><div class="gutter"></div><${Text} text=${it.note.text} /></div>`;
    const { m, first } = it;
    const you = m.role === 'you';
    return html`<div class=${'msg' + (first ? ' first' : '')} key=${i}>
      ${first ? html`<div class=${'avatar ' + (you ? 'you' : 'agent')}>${you ? 'Y' : 'A'}</div>` : html`<div class="gutter"></div>`}
      <div class="body">${first && html`<div class="who">${you ? 'You' : 'Agent'}<time>${clock(m.at)}</time></div>`}<${Text} text=${m.text} /></div></div>`;
  }).concat(live ? html`<div class="msg first" key="live"><div class="avatar agent">A</div><div class="body"><${Text} live text=${live} /></div></div>` : []);
}

// The pane beside the chat (like a Slack thread): a run of tool calls with what they returned.
function Pane({ acts, onClose }) {
  return html`<aside class="pane"><header><b>Actions</b><button onClick=${onClose}>✕</button></header>
    <div class="lines">${acts.map((a) => html`<div class=${'line ' + a.role}>${a.text}</div>`)}</div></aside>`;
}

function NewTask({ onClose, onCreated }) {
  const [f, setF] = useState({ Title: '', Repo: '', Body: '', Branch: '' });
  const [err, setErr] = useState('');
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value });
  const submit = async (e) => {
    e.preventDefault();
    try { onCreated(await api('POST', '/tasks', f)); } catch (x) { setErr(x.message); }
  };
  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <form class="modal" onSubmit=${submit}>
      <h2>New task</h2>
      <label>Name<input autofocus value=${f.Title} onInput=${set('Title')} /></label>
      <label>Repo folder<input placeholder="~/code/project" value=${f.Repo} onInput=${set('Repo')} /></label>
      <label>Branch (optional, an existing one)<input value=${f.Branch} onInput=${set('Branch')} /></label>
      <label>Description<textarea rows="4" value=${f.Body} onInput=${set('Body')}></textarea></label>
      ${err && html`<div class="err">${err}</div>`}
      <div class="btns"><button type="button" onClick=${onClose}>Cancel</button><button class="primary">Create</button></div>
    </form></div>`;
}

function Chat({ task }) {
  const [msgs, setMsgs] = useState([]);
  const [live, setLive] = useState('');
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [pane, setPane] = useState(null);
  const end = useRef();
  const id = task.id;
  useEffect(() => {
    setMsgs([]); setLive(''); setErr(''); setPane(null);
    const load = () => api('GET', `/tasks/${id}/chat`).then(setMsgs);
    load();
    const es = new EventSource('/api/events');
    es.onopen = load; // events missed while disconnected
    es.onmessage = (e) => {
      const ev = JSON.parse(e.data);
      if (ev.id !== id) return;
      if (ev.kind === 'message') { setMsgs((l) => [...l, ev.msg]); if (ev.msg.role === 'agent') setLive(''); }
      if (ev.kind === 'delta') setLive((l) => l + ev.text);
      if (ev.kind === 'turn' && !ev.busy) setLive('');
    };
    return () => es.close();
  }, [id]);
  useEffect(() => end.current && end.current.scrollIntoView({ block: 'end' }), [msgs, live]);
  const send = async (e) => {
    e && e.preventDefault();
    if (!text.trim()) return;
    try { await api('POST', `/tasks/${id}/messages`, { Text: text }); setText(''); setErr(''); } catch (x) { setErr(x.message); }
  };
  const key = (e) => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) send(e); };
  return html`<div class="chatwrap"><div class="main">
    <header><h1>${task.title}</h1><span class="chip">${task.icon} ${task.label}</span>
      <span class="meta">${base(task.repo)}${task.branch ? ' · ' + task.branch : ''} · ${task.agent}${task.model ? ' · ' + task.model : ''}</span>
      ${task.working && html`<button onClick=${() => api('POST', `/tasks/${id}/stop`)}>Stop</button>`}</header>
    <div class="msgs">${msgs.length || live ? html`<${Messages} msgs=${msgs} live=${live} onActs=${setPane} />` : html`<div class="empty">Say what to do. The first message sets up the worktree.</div>`}<div ref=${end}></div></div>
    <div class="compose"><form onSubmit=${send}>
      <textarea rows="1" placeholder=${task.working ? 'Message (sending stops the agent and steers it)' : 'Message'} value=${text}
        onInput=${(e) => { setText(e.target.value); e.target.style.height = 'auto'; e.target.style.height = e.target.scrollHeight + 'px'; }} onKeyDown=${key}></textarea>
      <button class="primary">Send</button></form>
      <div class="hint">${err ? html`<span class="err">${err}</span>` : '⏎ sends · ⇧⏎ new line'}</div></div></div>
    ${pane && html`<${Pane} acts=${pane} onClose=${() => setPane(null)} />`}</div>`;
}

function App() {
  const [list, setList] = useState([]);
  const [sel, setSel] = useState(decodeURIComponent(location.hash.slice(1)));
  const [unread, setUnread] = useState({});
  const [adding, setAdding] = useState(false);
  const selRef = useRef(sel);
  selRef.current = sel;
  const reload = () => api('GET', '/tasks').then(setList);
  useEffect(() => {
    reload();
    const es = new EventSource('/api/events');
    es.onopen = reload;
    es.onmessage = (e) => {
      const ev = JSON.parse(e.data);
      if (ev.kind === 'task') reload();
      if (ev.kind === 'message' && ev.msg.role === 'agent' && ev.id !== selRef.current) setUnread((u) => ({ ...u, [ev.id]: true }));
    };
    return () => es.close();
  }, []);
  const toTerminal = async () => {
    if (list.some((t) => t.working) && !confirm('Agents are working. Switching to the terminal UI stops them. Switch?')) return;
    await api('POST', '/ui', { Mode: 'terminal' }); // crew quits, and the app opens the terminal UI
    document.body.textContent = 'Switching to the terminal UI…';
  };
  const open = (id) => { location.hash = id; setSel(id); setUnread((u) => ({ ...u, [id]: false })); };
  const task = list.find((t) => t.id === sel);
  let group = null;
  return html`<nav class="side"><header>crew <button title="New task" onClick=${() => setAdding(true)}>+</button></header>
    <div class="list">${list.length ? list.flatMap((t) => {
      const head = t.label !== group ? ((group = t.label), [html`<div class="group" key=${'g' + t.label}>${t.label}</div>`]) : [];
      return head.concat(html`<div key=${t.id} class=${'row' + (t.id === sel ? ' on' : '') + (unread[t.id] ? ' unread' : '')} onClick=${() => open(t.id)}>
        <span class="icon">${t.icon}</span><span class="title">${t.title}</span>${t.working ? html`<span class="dot work"></span>` : unread[t.id] && html`<span class="dot"></span>`}</div>`);
    }) : html`<div class="group">No tasks yet</div>`}</div>
    <footer><button onClick=${toTerminal}>Use the terminal UI</button></footer></footer></nav>
    ${task ? html`<${Chat} task=${task} key=${task.id} />` : html`<div class="main"><div class="empty">${list.length ? 'Pick a task' : 'Make a task with +'}</div></div>`}
    ${adding && html`<${NewTask} onClose=${() => setAdding(false)} onCreated=${(t) => { setAdding(false); reload().then(() => open(t.id)); }} />`}`;
}

render(html`<${App} />`, document.getElementById('app'));
