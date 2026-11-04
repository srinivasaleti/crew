// The chat every section uses: messages, tool calls in a side pane, and the message box.
import { html, useState, useEffect, useRef } from '../vendor/preact-htm.js';
import { api, base, clock, on, useEvents } from './api.js';
import { Markdown } from './markdown.js';

// Messages as Slack shows them: a name and time on the first of a run by one sender, tool calls
// folded into one line that opens the side pane, and errors in red.
export function Messages({ msgs, live, onActs, agent = 'Agent', onAsk }) {
  const items = [];
  let acts = null, last = null;
  for (const [k, m] of msgs.entries()) {
    if (m.role === 'ask') { items.push({ ask: m, open: k === msgs.length - 1 || msgs.slice(k + 1).every((x) => x.role === 'ask') }); acts = null; last = null; continue; }
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
    if (it.ask) return html`<${AskCard} key=${i} m=${it.ask} open=${it.open} onAsk=${onAsk} />`;
    if (it.note) return html`<div class="msg error" key=${i}><div class="gutter"></div><div class="body"><${Markdown} text=${it.note.text} /></div></div>`;
    const { m, first } = it;
    const you = m.role === 'you';
    return html`<div class=${'msg' + (first ? ' first' : '')} key=${i}><${Copy} text=${m.text} />
      ${first ? html`<div class=${'avatar ' + (you ? 'you' : 'agent')}>${you ? 'Y' : agent[0]}</div>` : html`<div class="gutter"><time>${clock(m.at)}</time></div>`}
      <div class="body">${first && html`<div class="who">${you ? 'You' : agent}<time>${clock(m.at)}</time></div>`}
      ${you ? html`<div class="text">${m.text}</div>` : html`<${Markdown} text=${m.text} />`}</div></div>`;
  });
  if (live) out.push(html`<div class="msg first" key="live"><div class="avatar agent">${agent[0]}</div><div class="body"><div class="who">${agent}<span class="typing">writing…</span></div><${Markdown} live text=${live} /></div></div>`);
  return out;
}

// AskCard is an MCP call crew's gate stopped (it would change something): which server and tool,
// what it would send, and Allow once, Always in this chat, or Deny. Once the chat has moved on, it
// shows as asked.
function AskCard({ m, open, onAsk }) {
  let p = {};
  try { p = JSON.parse(m.text); } catch {}
  const [, server = '', tool = p.tool || ''] = (p.tool || '').match(/^mcp__(.+)__([^_].*)$/) || [];
  const [sent, setSent] = useState('');
  const answer = (a) => { setSent(a); onAsk && onAsk(p.tool, a).catch(() => setSent('')); };
  const input = p.input ? JSON.stringify(p.input, null, 2) : '';
  return html`<div class="ask-card">
    <div class="ask-head"><b>Claude wants to ${tool.replace(/_/g, ' ').replace(/([a-z])([A-Z])/g, '$1 $2').toLowerCase()}</b><span class="meta">needs your OK · ${server.replace(/^claude_ai_/, '').replace(/_/g, ' ')}</span></div>
    ${input && html`<pre class="ask-input">${input.length > 1500 ? input.slice(0, 1500) + '\n…' : input}</pre>`}
    ${open && onAsk && !sent ? html`<div class="ask-btns"><button class="primary small" onClick=${() => answer('once')}>Allow once</button>
        <button class="small" title="Every call to this tool in this chat goes ahead" onClick=${() => answer('always')}>Always in this chat</button>
        <button class="small danger" onClick=${() => answer('deny')}>Deny</button></div>`
      : html`<div class="meta">${sent ? { once: 'Allowed once', always: 'Allowed in this chat', deny: 'Denied' }[sent] : 'Asked'}</div>`}
  </div>`;
}

// Copy is a message's copy button, shown while the pointer is over it.
function Copy({ text }) {
  const [done, setDone] = useState(false);
  const copy = () => navigator.clipboard.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1200); });
  return html`<button class="copy small" title="Copy this message" onClick=${copy}>${done ? 'Copied' : 'Copy'}</button>`;
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
// a PR's, REQUESTS') is this one. With commands, typing / lists base + '/commands' (crew's and Claude Code's).
export function Chat({ id, base: root, working, empty, agent, placeholder, commands }) {
  const [msgs, setMsgs] = useState([]);
  const [live, setLive] = useState('');
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [acts, setActs] = useState(null);
  const [busy, setBusy] = useState(working);
  const [cmds, setCmds] = useState([]);
  const [pick, setPick] = useState(0);
  const [argHint, setArgHint] = useState('');
  const [shut, setShut] = useState(null); // the text esc closed the suggestions at
  const end = useRef();
  const box = useRef();
  useEffect(() => { if (commands) api('GET', root + '/commands').then(setCmds).catch(() => setCmds([])); }, [id, commands]);
  const sugg = shut === text ? [] : suggest(cmds, text);
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
  const send = async (e, value) => {
    e && e.preventDefault();
    const t = value ?? text;
    if (!t.trim()) return;
    setText(''); setArgHint('');
    try { await api('POST', root + '/messages', { Text: t }); setErr(''); } catch (x) { setErr(x.message); setText(t); }
  };
  const choose = (c, run) => {
    if (run && c.run) return send(null, c.insert);
    setText(c.insert); setArgHint(c.hint); setPick(0); box.current && box.current.focus();
  };
  const key = (e) => {
    if (sugg.length) {
      const k = { ArrowDown: 1, ArrowUp: -1 }[e.key];
      if (k) { e.preventDefault(); return setPick((pick + k + sugg.length) % sugg.length); }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey && !e.isComposing)) { e.preventDefault(); return choose(sugg[Math.min(pick, sugg.length - 1)], e.key === 'Enter'); }
      if (e.key === 'Escape') { e.preventDefault(); return setShut(text); }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) send(e);
    if (e.key === 'Escape' && busy) api('POST', root + '/stop');
  };
  const grow = (e) => { setText(e.target.value); setPick(0); };
  // The box fits its text, up to 220px, and shrinks back when it's sent or cleared.
  useEffect(() => { const b = box.current; if (b) { b.style.height = 'auto'; b.style.height = Math.min(b.scrollHeight, 220) + 'px'; } }, [text]);
  return html`<div class="chat">
    <div class="msgs">${msgs.length || live ? html`<${Messages} msgs=${msgs} live=${live} onActs=${setActs} agent=${agent} onAsk=${(tool, a) => api('POST', root + '/ask', { Tool: tool, Answer: a })} />` : html`<div class="empty">${empty || 'Say what to do.'}</div>`}
      ${busy && !live && html`<div class="working"><span class="dot work"></span> working…</div>`}<div ref=${end}></div></div>
    <div class="compose">${sugg.length ? html`<ul class="sugg">${sugg.map((c, i) => html`<li key=${c.tag + c.name} class=${i === Math.min(pick, sugg.length - 1) ? 'on' : ''} onMouseDown=${(e) => { e.preventDefault(); choose(c, true); }}>
        <b>${c.name}</b><span class="desc">${c.desc}</span><span class="tag">${c.tag}</span></li>`)}</ul>` : ''}<form onSubmit=${send}>
      <textarea ref=${box} rows="1" placeholder=${busy ? 'Message (sending stops the agent and steers it)' : placeholder || 'Message'} value=${text} onInput=${grow} onKeyDown=${key}></textarea>
      ${busy ? html`<button type="button" onClick=${() => api('POST', root + '/stop')}>Stop</button>` : ''}
      <button class="primary" disabled=${!text.trim()}>Send</button></form>
      <div class="hint">${err ? html`<span class="err">${err}</span>` : sugg.length ? '↑↓ choose · tab inserts · ⏎ runs · esc closes' : argHint && text.startsWith('/') ? argHint : busy ? '⏎ sends · esc stops' : '⏎ sends · ⇧⏎ new line'}</div></div>
    ${acts && html`<${Pane} title="Actions" onClose=${() => setActs(null)}><${Actions} acts=${acts} /><//>`}</div>`;
}

// suggest is the commands matching what's typed (a / at the start, no space yet): names that start
// with it, then names that have it, then descriptions; the list keeps crew's first, then the repo's.
function suggest(cmds, text) {
  if (!text.startsWith('/') || /\s/.test(text)) return [];
  const q = text.slice(1).toLowerCase();
  const rank = (c) => {
    const n = c.name.replace(/^\//, '').toLowerCase();
    return n.startsWith(q) ? 0 : n.includes(q) ? 1 : q && c.desc.toLowerCase().includes(q) ? 2 : -1;
  };
  return cmds.map((c) => [rank(c), c]).filter(([r]) => r >= 0).sort((a, b) => a[0] - b[0]).slice(0, 8).map(([, c]) => c);
}
