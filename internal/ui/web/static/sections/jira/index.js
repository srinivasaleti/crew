// JIRA: your open issues by space on the left, the open issue on the right with its Issue, Comments
// and Chat tabs. Nothing goes to Jira but on its own click: Post, Save, a status. crew (internal/jira
// through /api/jira) reads and writes; this draws and sends.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, ago, openLink, Markdown, Chat, Form, Empty, Search, Toolbar, useListKeys, css } from '../../lib.js';

css(import.meta.url);

// watch keeps the rail's badge at how many open issues are yours.
export function watch(setBadge) {
  const count = () => api('GET', '/jira').then((d) => setBadge(d.issues.length || ''), () => {});
  count();
  return on((ev) => (ev.kind === 'jira' || ev.kind === 'open') && count());
}

const TYPES = { bug: ['jt-bug', '●'], story: ['jt-story', '▲'], epic: ['jt-epic', '⚡'], 'sub-task': ['jt-sub', '↳'], subtask: ['jt-sub', '↳'] };
const Type = ({ name }) => {
  const [cls, glyph] = TYPES[(name || '').toLowerCase()] || ['jt-task', '✓'];
  return html`<span class=${'jira-type ' + cls} title=${name}>${glyph}</span>`;
};
const Pill = ({ status, category }) => html`<span class=${'jira-pill jp-' + (category || 'new')}>${status}</span>`;
// Pri is a priority, short: "P4 - No Trade Impact" is P4; Highest and P1/P2 are red.
const Pri = ({ name }) => {
  const short = (name || '').split(' - ')[0];
  const cls = /^(highest|high|p1|p2|critical|blocker)$/i.test(short) ? 'hi' : /^(medium|p3)$/i.test(short) ? 'mid' : '';
  return short ? html`<span class=${'jira-pri ' + cls} title=${name}>${short}</span>` : '';
};
const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);
const TABS = ['Issue', 'Comments', 'Chat'];
const CHIPS = [['all', 'All'], ['new', 'To do'], ['indeterminate', 'In progress'], ['task', 'With a task']];

export default function Jira({ go }) {
  const list = useLoad(() => api('GET', '/jira'));
  const [sel, setSel] = useState(decodeURIComponent(location.hash.split('/')[1] || ''));
  const [q, setQ] = useState('');
  const [chip, setChip] = useState('all');
  useEvents((ev) => (ev.kind === 'jira' || ev.kind === 'open') && list.reload());
  const d = list.data;
  const issues = (d ? d.issues : []).filter((i) => {
    if (q && !(i.Key + ' ' + i.Summary + ' ' + i.Status + ' ' + i.Type).toLowerCase().includes(q.toLowerCase())) return false;
    if (chip === 'task') return d.tasks[i.Key];
    return chip === 'all' || i.Category === chip;
  });
  // by space, in the order the spaces first appear (the list is newest first)
  const spaces = [];
  for (const i of issues) if (!spaces.includes(i.Project)) spaces.push(i.Project);
  const ordered = spaces.flatMap((s) => issues.filter((i) => i.Project === s));
  const open = (key) => { setSel(key); location.hash = 'jira/' + key; };
  useListKeys(ordered.map((i) => i.Key), sel, open);
  useEffect(() => { if (!sel && ordered.length) open(ordered[0].Key); }, [ordered.length]); // open the first, so the page isn't empty
  const issue = d && d.issues.find((i) => i.Key === sel);

  let body;
  if (!d) body = html`<main class="page"><${Empty} title=${list.err ? "Couldn't reach crew" : 'Reading your Jira issues…'}>${list.err}<//></main>`;
  else if (!d.installed || (d.read && !d.ok)) body = html`<main class="page"><${Setup} d=${d} reload=${() => api('POST', '/jira/refresh')} /></main>`;
  else if (sel) body = html`<${IssuePage} key=${sel} issueKey=${sel} listed=${issue} task=${d.tasks[sel]} go=${go} onOpen=${open} />`;
  else body = html`<main class="page"><${Empty} title=${d.issues.length ? 'Pick an issue' : d.read ? 'No open issues assigned to you' : 'Reading your Jira issues…'}>
    <p>↑↓ move · ⏎ opens · / filters</p><//></main>`;

  return html`<div class="split">
    <section class="col">
      <div class="col-head"><b>Jira</b><button class="small" title="Read Jira now" disabled=${d && d.loading} onClick=${() => api('POST', '/jira/refresh')}>${d && d.loading ? 'Reading…' : '↻'}</button></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} /></div>
      <div class="jira-chips">${CHIPS.map(([id, label]) => html`<button key=${id} class=${chip === id ? 'on' : ''} onClick=${() => setChip(id)}>${label}</button>`)}</div>
      <div class="col-list">${d && d.err && d.ok === false && d.read ? '' : d && d.err ? html`<div class="group err">${d.err}</div>` : ''}
        ${ordered.length ? spaces.map((s) => html`<div key=${s}>
          <div class="group">${(d.spaces && d.spaces[s]) || s}</div>
          ${ordered.filter((i) => i.Project === s).map((i) => html`<div key=${i.Key} class=${'item' + (i.Key === sel ? ' on' : '')} onClick=${() => open(i.Key)}>
            <${Type} name=${i.Type} />
            <div class="item-main"><div class="item-title">${i.Summary}</div>
              <div class="item-sub jira-item-row"><span class="jira-key">${i.Key}</span><${Pill} status=${i.Status} category=${i.Category} />
                ${d.tasks[i.Key] && html`<span title=${'task: ' + d.tasks[i.Key].label}>${d.tasks[i.Key].working ? '●' : d.tasks[i.Key].icon}</span>`}</div></div>
            <${Pri} name=${i.Priority} /></div>`)}</div>`)
        : html`<div class="group">${d ? (q || chip !== 'all' ? 'Nothing matches' : d.read ? 'No open issues' : 'Loading…') : 'Loading…'}</div>`}</div>
    </section>
    ${body}
  </div>`;
}

// Setup says what to run when acli isn't installed or signed in.
function Setup({ d, reload }) {
  return html`<${Empty} title=${d.installed ? "Jira isn't signed in" : "Atlassian's CLI isn't installed"}>
    <div class="jira-setup">
      <p>crew reads Jira with Atlassian's CLI (acli). ${d.installed ? 'Sign it in, in a terminal (your browser opens):' : 'Install it, then sign it in, in a terminal:'}</p>
      ${!d.installed && html`<pre>${d.install}</pre>`}<pre>${d.signIn}</pre>
      ${d.err && html`<p class="err">${d.err}</p>`}
      <button class="primary" onClick=${reload}>I've done it · read Jira</button></div><//>`;
}

// IssuePage is an issue: its header and actions, and its tabs.
function IssuePage({ issueKey, listed, task, go, onOpen }) {
  const card = useLoad(() => api('GET', '/jira/' + issueKey), [issueKey]);
  const [tab, setTab] = useState(0);
  const [note, setNote] = useState('');
  const [working, setWorking] = useState(false);
  useEvents((ev) => ev.kind === 'jira' && card.reload(), [issueKey]);
  const i = (card.data && card.data.issue) || listed || { Key: issueKey, Summary: '' };
  const say = (t) => { setNote(t); setTimeout(() => setNote((n) => (n === t ? '' : n)), 4000); };
  const act = async (what, fn) => { say(what + '…'); try { await fn(); say(what + ' ✓'); card.reload(); } catch (e) { say("Couldn't: " + e.message); } };
  const work = async () => {
    if (task) { location.hash = 'tasks/' + task.id; go('tasks'); return; }
    setWorking(true);
  };
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey) return;
      if (e.key === 'Tab') { e.preventDefault(); setTab((t) => (t + (e.shiftKey ? 2 : 1)) % 3); }
      if (e.key === 'n') work();
      if (e.key === 'o' && i.URL) openLink(i.URL);
      if (e.key === 'r') card.reload();
      if (e.key === 'c') setTab(1);
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });
  const comments = card.data ? card.data.comments : [];
  return html`<main class="page">
    <${Toolbar} title=${html`<span class="jira-key" style="font-size:13px">${i.Key}</span> ${i.Summary}`}
      sub=${html`<${Type} name=${i.Type} /> ${i.Type} · <${Pill} status=${i.Status} category=${i.Category} /> <${Pri} name=${i.Priority} />${note && html` · <span>${note}</span>`}`}>
      <button class="primary" title="n" onClick=${work}>${task ? 'Open its task' : 'Work on it'}</button>
      <button title="o" disabled=${!i.URL} onClick=${() => openLink(i.URL)}>Open in Jira</button>
      <button class="ghost" title="Read it again (r)" onClick=${card.reload}>↻</button>
    <//>
    <div class="tabs">${TABS.map((t, n) => html`<button key=${t} class=${tab === n ? 'on' : ''} onClick=${() => setTab(n)}>${t}${n === 1 && comments.length ? ' ' + comments.length : ''}</button>`)}</div>
    ${tab === 0 && html`<${IssueTab} card=${card} i=${i} act=${act} onOpen=${onOpen} />`}
    ${tab === 1 && html`<${CommentsTab} card=${card} i=${i} act=${act} />`}
    ${tab === 2 && html`<${ChatTab} i=${i} />`}
    ${working && html`<${Form} title=${'Work on ' + i.Key} action="Make the task" onClose=${() => setWorking(false)}
      fields=${[{ key: 'Repo', label: 'Repo folder', placeholder: '~/code/project' }, { key: 'Branch', label: 'Branch (optional: an existing one)' }]}
      submit=${async (v) => { const t = await api('POST', '/jira/' + i.Key + '/work', v); location.hash = 'tasks/' + t.id; go('tasks'); }} />`}
  </main>`;
}

// IssueTab is the issue's details, status menu, labels, description (each editable) and links.
function IssueTab({ card, i, act, onOpen }) {
  const [editing, setEditing] = useState(false);
  const [menu, setMenu] = useState(false);
  if (!card.data) return html`<div class="scroll">${card.err ? html`<p class="err">Couldn't read it: ${card.err}</p>` : html`<p class="meta">Reading it from Jira…</p>`}</div>`;
  const { detail: d, statuses } = card.data;
  const fields = [['Assignee', d.Assignee], ['Reporter', d.Reporter], ['Created', d.Created && !d.Created.startsWith('0001') ? new Date(d.Created).toLocaleString() : ''],
    ['Updated', d.Updated && !d.Updated.startsWith('0001') ? ago(d.Updated) + ' ago' : ''], ['Parent', d.Parent], ['Components', (d.Components || []).join(', ')]].filter((f) => f[1]);
  const groups = {};
  for (const it of d.Items || []) (groups[it.Kind] ||= []).push(it);
  return html`<div class="scroll">
    <div class="jira-fields">
      <div><div class="k">Status</div><div class="v jira-menu">
        <button onClick=${() => setMenu(!menu)}><${Pill} status=${i.Status} category=${i.Category} /> ▾</button>
        ${menu && html`<div class="drop">${statuses.filter((s) => s !== i.Status).map((s) => html`<button key=${s} onClick=${() => { setMenu(false); act('Moving it to ' + s, () => api('POST', '/jira/' + i.Key + '/status', { To: s })); }}>${s}</button>`)}
          <div class="meta" style="padding:4px 10px">Jira allows only its workflow's next steps.</div></div>`}</div></div>
      ${fields.map(([k, v]) => html`<div key=${k}><div class="k">${k}</div><div class="v">${v}</div></div>`)}
      <div><div class="k">Labels</div><div class="v jira-labels">${(d.Labels || []).length ? d.Labels.map((l) => html`<span key=${l} class="jira-label">${l}</span>`) : html`<span class="meta">none</span>`}</div></div>
    </div>
    ${editing ? html`<${Edit} i=${i} d=${d} act=${act} done=${() => setEditing(false)} />` : html`
      <div class="jira-section"><h3>Description</h3><button class="small" title="Edit the summary, description and labels" onClick=${() => setEditing(true)}>Edit</button></div>
      ${i.Description ? html`<${Markdown} text=${i.Description} />` : html`<p class="meta">No description · Edit adds one</p>`}`}
    ${Object.entries(groups).map(([kind, items]) => html`<div key=${kind} class="jira-links">
      <div class="jira-section"><h3>${kind} (${items.length})</h3></div>
      ${items.map((it) => html`<div class="row" key=${it.Name}>
        ${it.Rel && html`<span class="rel">${it.Rel}</span>`}
        ${it.URL ? html`<a href=${it.URL} onClick=${(e) => { e.preventDefault(); openLink(it.URL); }}>${it.Name}</a>` : html`<a href=${'#jira/' + it.Name} onClick=${(e) => { e.preventDefault(); onOpen(it.Name); }}>${it.Name}</a>`}
        <span>${it.Info}</span>${it.Status && html`<span class="meta">${it.Status}</span>`}${it.Added && !it.Added.startsWith('0001') && html`<span class="meta">${ago(it.Added)} ago</span>`}</div>`)}</div>`)}
    ${card.data.err && html`<p class="err">Couldn't read it all: ${card.data.err}</p>`}
  </div>`;
}

// Edit changes the summary, description and labels; Save sends only what changed.
function Edit({ i, d, act, done }) {
  const [v, setV] = useState({ Summary: i.Summary, Description: i.Description, Labels: (d.Labels || []).join(', ') });
  const set = (k) => (e) => setV({ ...v, [k]: e.target.value });
  const save = (e) => { e.preventDefault(); act('Saving to Jira', () => api('POST', '/jira/' + i.Key + '/edit', v).then(done)); };
  return html`<form class="jira-edit" onSubmit=${save} onKeyDown=${(e) => { if (e.key === 'Escape') done(); if (e.metaKey && e.key === 's') save(e); }}>
    <div class="jira-section"><h3>Editing ${i.Key}</h3></div>
    <label>Summary<input autofocus value=${v.Summary} onInput=${set('Summary')} /></label>
    <label>Description (saved as plain text)<textarea rows="12" value=${v.Description} onInput=${set('Description')}></textarea></label>
    <label>Labels (comma-separated)<input value=${v.Labels} onInput=${set('Labels')} /></label>
    <div class="btns"><span class="meta" style="margin-right:auto">⌘S saves to Jira · esc drops the edit</span><button type="button" onClick=${done}>Cancel</button><button class="primary">Save to Jira</button></div>
  </form>`;
}

// CommentsTab is the thread and a box: Post sends it; Draft with Claude fills the box for you to edit.
function CommentsTab({ card, i, act }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState('');
  const box = useRef();
  const comments = card.data ? card.data.comments : [];
  useEffect(() => box.current && box.current.focus(), []);
  const post = async () => {
    if (!text.trim()) return;
    setBusy('Posting…');
    try { await api('POST', '/jira/' + i.Key + '/comment', { Body: text }); setText(''); setBusy(''); card.reload(); } catch (e) { setBusy("Couldn't post: " + e.message + ' · your comment is still here'); }
  };
  const draft = async () => {
    setBusy('Claude is drafting a comment…');
    try { const r = await api('POST', '/jira/' + i.Key + '/draft'); setText(r.text); setBusy("Claude's draft · edit it, then Post"); box.current && box.current.focus(); } catch (e) { setBusy("Claude couldn't draft one: " + e.message); }
  };
  return html`<div class="scroll">
      ${!card.data && html`<p class="meta">Reading the comments…</p>`}
      ${card.data && !comments.length && html`<p class="meta">No comments yet.</p>`}
      ${comments.map((c, n) => html`<div class="jira-comment" key=${n}><div class="avatar">${(c.Author || '?')[0]}</div>
        <div class="body"><div class="who">${c.Author}${c.Created && !c.Created.startsWith('0001') && html`<time>${ago(c.Created)} ago</time>`}</div><${Markdown} text=${c.Body} /></div></div>`)}
    </div>
    <div class="jira-box">
      <textarea ref=${box} rows="3" placeholder="Write a comment…" value=${text} onInput=${(e) => setText(e.target.value)}
        onKeyDown=${(e) => { if (e.metaKey && e.key === 'Enter') post(); }}></textarea>
      <div class="row"><span class="jira-note">${busy || '⌘⏎ posts it to Jira'}</span>
        <button onClick=${draft} disabled=${busy === 'Claude is drafting a comment…'}>Draft with Claude</button>
        <button class="primary" disabled=${!text.trim() || busy === 'Posting…'} onClick=${post}>Post to Jira</button></div>
    </div>`;
}

// ChatTab is the chat about the issue, made the first time you open it.
function ChatTab({ i }) {
  const [id, setID] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => { api('POST', '/jira/' + i.Key + '/chat').then((r) => setID(r.id), (e) => setErr(e.message)); }, [i.Key]);
  if (!id) return html`<div class="scroll">${err ? html`<p class="err">${err}</p>` : html`<p class="meta">Opening the chat…</p>`}</div>`;
  return html`<${Chat} id=${id} base=${'/tasks/' + id} agent="Claude"
    empty=${'Ask Claude about ' + i.Key + '. It reads the issue with acli, and changes nothing in Jira unless you ask.'} />`;
}
