// The chat every section uses: messages, tool calls in a side pane, and the message box.
import { html, useState, useEffect, useRef } from '../vendor/preact-htm.js';
import { api, base, clock, on, useEvents } from './api.js';
import { Markdown } from './markdown.js';

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
    return html`<div class=${'msg' + (first ? ' first' : '')} key=${i}><${Copy} text=${m.text} />
      ${first ? html`<div class=${'avatar ' + (you ? 'you' : 'agent')}>${you ? 'Y' : agent[0]}</div>` : html`<div class="gutter"><time>${clock(m.at)}</time></div>`}
      <div class="body">${first && html`<div class="who">${you ? 'You' : agent}<time>${clock(m.at)}</time></div>`}
      ${you ? html`<div class="text">${m.text}</div>` : html`<${Markdown} text=${m.text} />`}</div></div>`;
  });
  if (live) out.push(html`<div class="msg first" key="live"><div class="avatar agent">${agent[0]}</div><div class="body"><div class="who">${agent}<span class="typing">writing…</span></div><${Markdown} live text=${live} /></div></div>`);
  return out;
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
