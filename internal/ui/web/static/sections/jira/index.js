// JIRA: your open issues as a list by project (starred first, hidden ones left out), and an issue
// full width with its Issue, Comments and Chat tabs, as PULL REQUESTS does. Nothing goes to Jira but
// on its own click: Post, Save, a status. crew (internal/jira through /api/jira) reads and writes.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, ago, openLink, Markdown, Chat, Form, Empty, Search, useListKeys, css, Menu, Modal, ask } from '../../lib.js';

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
// filters are the chips above the list: what each keeps.
const filters = [
  ['all', 'All', () => true],
  ['new', 'To do', (i) => i.Category === 'new'],
  ['indeterminate', 'In progress', (i) => i.Category === 'indeterminate'],
  ['starred', '★ Starred', (i, p) => p.starred.includes(i.Project)],
  ['task', 'With a task', (i, p, d) => !!d.tasks[i.Key]],
];

// ProjectHead is a project's heading: fold it, star it, and its menu (star, hide, open in Jira).
function ProjectHead({ name, project, n, folded, fold, starred, pref, site }) {
  return html`<div class="group-head">
    <button class="group fold" onClick=${fold}><span class="caret">${folded ? '▸' : '▾'}</span> ${name}${name !== project ? html` <span class="jira-key">${project}</span>` : ''} <span class="n">${n}</span></button>
    <button class=${'ghost small star' + (starred ? ' on' : '')} title=${starred ? 'Unstar (s)' : 'Star: pin it on top (s)'} onClick=${() => pref({ Star: project, On: !starred })}>${starred ? '★' : '☆'}</button>
    <${Menu} label="⋯" title="More" items=${[
      { label: (starred ? 'Unstar ' : 'Star ') + name, run: () => pref({ Star: project, On: !starred }) },
      { label: 'Hide ' + name, run: () => pref({ Hide: project, On: true }) },
      ...(site ? [{ label: 'Open in Jira ↗', run: () => openLink(site + '/browse/' + project) }] : []),
    ]} />
  </div>`;
}

// Row is one issue on a line: its type, key, summary, then its task, status and priority.
function Row({ i, on, open, task }) {
  return html`<div class=${'item list-row' + (on ? ' on' : '')} onClick=${() => open(i.Key)}>
    <${Type} name=${i.Type} />
    <div class="item-main"><div class="item-title"><span class="jira-key">${i.Key}</span> ${i.Summary}</div>
      <div class="item-sub jira-item-row">${task && html`<span class="tag" title=${'task: ' + task.label}>${task.working ? '● working' : task.icon + ' task'}</span>`}
        <${Pill} status=${i.Status} category=${i.Category} /><${Pri} name=${i.Priority} /></div></div></div>`;
}

// Hidden lists the projects you hid, each with Show.
function Hidden({ prefs, pref, spaces, onClose }) {
  return html`<${Modal} title="Hidden projects" onClose=${onClose}>
    ${prefs.hidden.length ? prefs.hidden.map((h) => html`<div key=${h} class="hidden-row"><span>${(spaces && spaces[h]) || h} <span class="jira-key">${h}</span></span><button class="small" onClick=${() => pref({ Hide: h, On: false })}>Show</button></div>`)
      : html`<p class="meta">Nothing hidden.</p>`}
    <div class="btns"><button class="primary" onClick=${onClose}>Done</button></div><//>`;
}

// The page: a list view (your issues by project, full width) and an issue view (full width). A click
// or ⏎ opens an issue; ← Jira, esc or b goes back.
export default function Jira({ go }) {
  const list = useLoad(() => api('GET', '/jira'));
  const [sel, setSel] = useState(decodeURIComponent(location.hash.split('/')[1] || ''));
  const [view, setView] = useState(sel ? 'issue' : 'list');
  const [q, setQ] = useState('');
  const [chip, setChip] = useState('all');
  const [folds, setFolds] = useState({});
  const [showHidden, setShowHidden] = useState(false);
  useEvents((ev) => (ev.kind === 'jira' || ev.kind === 'open') && list.reload());
  const d = list.data;
  const prefs = (d && d.prefs) || {};
  prefs.starred ||= []; prefs.hidden ||= [];
  const all = d ? d.issues : [];
  const keep = filters.find((f) => f[0] === chip)[2];
  const issues = all.filter((i) => keep(i, prefs, d) && (!q || (i.Key + ' ' + i.Summary + ' ' + i.Status + ' ' + i.Type).toLowerCase().includes(q.toLowerCase())));
  // by project: starred first, then in the order they first appear (the list is newest first)
  const projects = [];
  for (const i of issues) if (!projects.includes(i.Project)) projects.push(i.Project);
  projects.sort((a, b) => (prefs.starred.includes(b) ? 1 : 0) - (prefs.starred.includes(a) ? 1 : 0));
  const folded = (p) => !!folds[p];
  const shown = projects.flatMap((p) => (folded(p) ? [] : issues.filter((i) => i.Project === p)));
  const site = (all.find((i) => i.URL) || {}).URL?.split('/browse/')[0] || '';
  const open = (key) => { setSel(key); setView('issue'); location.hash = 'jira/' + key; };
  const pref = (m) => api('POST', '/jira/prefs', m).catch((e) => ask(e.message));
  const cur = all.find((i) => i.Key === sel);
  useListKeys(view === 'list' ? shown.map((i) => i.Key) : [], sel, setSel);
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || document.querySelector('.scrim')) return;
      if (e.key === 'b' || (e.key === 'Escape' && view === 'issue')) setView(view === 'list' && sel ? 'issue' : 'list');
      if (view !== 'list') return;
      if (e.key === 'Enter' && cur) open(cur.Key);
      if (e.key === 'r') api('POST', '/jira/refresh');
      if (e.key === 's' && cur) pref({ Star: cur.Project, On: !prefs.starred.includes(cur.Project) });
      if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && cur) setFolds((f) => ({ ...f, [cur.Project]: e.key === 'ArrowLeft' }));
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });
  if (!d) return html`<main class="page"><${Empty} title=${list.err ? "Couldn't reach crew" : 'Reading your Jira issues…'}>${list.err}<//></main>`;
  if (!d.installed || (d.read && !d.ok)) return html`<main class="page"><${Setup} d=${d} reload=${() => api('POST', '/jira/refresh')} /></main>`;
  if (view === 'issue' && sel) return html`<${IssuePage} key=${sel} issueKey=${sel} listed=${cur} task=${d.tasks[sel]} go=${go} onOpen=${open} onBack=${() => setView('list')} />`;
  return html`<main class="page list-page">
    <div class="col-head"><div><b>Jira</b> <span class="meta">${all.length} open, assigned to you</span></div>
      <div class="head-btns"><button class="small" title="Read Jira now (r)" disabled=${d.loading} onClick=${() => api('POST', '/jira/refresh')}>${d.loading ? 'Reading…' : '↻'}</button></div></div>
    <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search key, summary, status, type" /></div>
    <div class="filters">${filters.map(([k, label, fn]) => {
      const n = k === 'all' ? 0 : all.filter((i) => fn(i, prefs, d)).length;
      return html`<button key=${k} class=${'fchip' + (chip === k ? ' on' : '')} onClick=${() => setChip(k)}>${label}${n ? html` <span class="n">${n}</span>` : ''}</button>`;
    })}</div>
    <div class="col-list">
      ${d.err && html`<div class="item-sub err" style="padding: 4px 14px">${d.err}</div>`}
      ${projects.length ? projects.map((p) => {
        const mine = issues.filter((i) => i.Project === p);
        return html`<div key=${p}>
          <${ProjectHead} name=${(d.spaces && d.spaces[p]) || p} project=${p} n=${mine.length} folded=${folded(p)} fold=${() => setFolds((f) => ({ ...f, [p]: !folded(p) }))}
            starred=${prefs.starred.includes(p)} pref=${pref} site=${site} />
          ${!folded(p) && mine.map((i) => html`<${Row} key=${i.Key} i=${i} on=${i.Key === sel} open=${open} task=${d.tasks[i.Key]} />`)}</div>`;
      }) : html`<div class="col-empty">${d.read ? (q || chip !== 'all' ? 'Nothing here' : 'No open issues assigned to you') : 'Reading your Jira issues…'}</div>`}
    </div>
    <div class="col-foot">${d.hidden || prefs.hidden.length ? html`<button class="ghost small" onClick=${() => setShowHidden(true)}>${d.hidden ? d.hidden + ' hidden' : 'Hidden projects'}</button>` : html`<span></span>`}
      <span class="meta">⏎ opens · s star · ←→ fold</span></div>
    ${showHidden && html`<${Hidden} prefs=${prefs} pref=${pref} spaces=${d.spaces} onClose=${() => setShowHidden(false)} />`}
  </main>`;
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

// IssuePage is an issue, full width: its header (back, summary, Status ▾, work on it, Jira) and tabs.
function IssuePage({ issueKey, listed, task, go, onOpen, onBack }) {
  const card = useLoad(() => api('GET', '/jira/' + issueKey), [issueKey]);
  const [tab, setTab] = useState(0);
  const [note, setNote] = useState('');
  const [working, setWorking] = useState(false);
  useEvents((ev) => ev.kind === 'jira' && card.reload(), [issueKey]);
  const i = (card.data && card.data.issue) || listed || { Key: issueKey, Summary: '' };
  const det = card.data && card.data.detail;
  const say = (t) => { setNote(t); setTimeout(() => setNote((n) => (n === t ? '' : n)), 4000); };
  const act = async (what, fn) => { say(what + '…'); try { await fn(); say(what + ' ✓'); card.reload(); } catch (e) { say("Couldn't: " + e.message); } };
  const work = () => { if (task) { location.hash = 'tasks/' + task.id; go('tasks'); } else setWorking(true); };
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || document.querySelector('.scrim')) return;
      if (e.key === 'Tab') { e.preventDefault(); setTab((t) => (t + (e.shiftKey ? 2 : 1)) % 3); }
      if (e.key === 'n') work();
      if (e.key === 'o' && i.URL) openLink(i.URL);
      if (e.key === 'r') card.reload();
      if (e.key === 'c') setTab(1);
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });
  const comments = card.data ? card.data.comments || [] : [];
  const statuses = (card.data && card.data.statuses) || [];
  const when = (t) => t && !t.startsWith('0001') ? (ago(t) === 'now' ? 'just now' : ago(t) + ' ago') : '';
  return html`<main class="page">
    <header class="page-head">
      <div class="page-head-top">
        <button class="small back" title="Back to the list (esc)" onClick=${onBack}>←</button>
        <h1>${i.Summary || i.Key} <span class="page-num">${i.Key}</span></h1>
        <div class="page-acts">
          ${note && html`<span class="meta">${note}</span>`}
          ${statuses.length > 0 && html`<${Menu} label=${html`<${Pill} status=${i.Status} category=${i.Category} /> ▾`} title="Move it on (Jira allows only its workflow's next steps)"
            items=${statuses.filter((st) => st !== i.Status).map((st) => ({ label: st, run: () => act('Moving it to ' + st, () => api('POST', '/jira/' + i.Key + '/status', { To: st })) }))} />`}
          <button class="small primary" title="n" onClick=${work}>${task ? 'Open its task' : 'Work on it'}</button>
          <button class="small" title="Read it again (r)" onClick=${card.reload}>↻</button>
          <button class="small" title="Open in Jira (o)" disabled=${!i.URL} onClick=${() => openLink(i.URL)}>Jira ↗</button>
        </div>
      </div>
      <div class="page-head-sub">
        <span class="jira-type-chip"><${Type} name=${i.Type} /> ${i.Type}</span>
        ${!statuses.length && html`<${Pill} status=${i.Status} category=${i.Category} />`}<${Pri} name=${i.Priority} />
        ${det && (det.Labels || []).map((l) => html`<span key=${l} class="jira-label">${l}</span>`)}
        <span class="page-meta">
          ${det && det.Assignee && html`<span>${det.Assignee}</span>`}
          ${det && det.Reporter && det.Reporter !== det.Assignee && html`<span>reported by ${det.Reporter}</span>`}
          ${det && det.Parent && html`<span>in ${det.Parent.split(' ')[0]}</span>`}
          ${det && when(det.Updated) && html`<span>updated ${when(det.Updated)}</span>`}
        </span>
      </div>
    </header>
    <div class="tabs">${TABS.map((t, n) => html`<button key=${t} class=${tab === n ? 'on' : ''} onClick=${() => setTab(n)}>${t}${n === 1 && comments.length ? ' ' + comments.length : ''}</button>`)}</div>
    ${tab === 0 && html`<${IssueTab} card=${card} i=${i} act=${act} onOpen=${onOpen} />`}
    ${tab === 1 && html`<${CommentsTab} card=${card} i=${i} />`}
    ${tab === 2 && html`<${ChatTab} i=${i} />`}
    ${working && html`<${Form} title=${'Work on ' + i.Key} action="Make the task" onClose=${() => setWorking(false)}
      fields=${[{ key: 'Repo', label: 'Repo folder', placeholder: '~/code/project' }, { key: 'Branch', label: 'Branch (optional: an existing one)' }]}
      submit=${async (v) => { const t = await api('POST', '/jira/' + i.Key + '/work', v); location.hash = 'tasks/' + t.id; go('tasks'); }} />`}
  </main>`;
}

// IssueTab is the issue's description (in a card; Edit changes it, the summary and labels), its details
// and its links. Its status is in the header.
function IssueTab({ card, i, act, onOpen }) {
  const [editing, setEditing] = useState(false);
  if (!card.data) return html`<div class="scroll">${card.err ? html`<p class="err">Couldn't read it: ${card.err}</p>` : html`<p class="meta">Reading it from Jira…</p>`}</div>`;
  const { detail: d } = card.data;
  const fields = [['Assignee', d.Assignee], ['Reporter', d.Reporter], ['Created', d.Created && !d.Created.startsWith('0001') ? new Date(d.Created).toLocaleString() : ''],
    ['Updated', d.Updated && !d.Updated.startsWith('0001') ? ago(d.Updated) + ' ago' : ''], ['Parent', d.Parent], ['Components', (d.Components || []).join(', ')]].filter((f) => f[1]);
  const groups = {};
  for (const it of d.Items || []) (groups[it.Kind] ||= []).push(it);
  return html`<div class="scroll">
    <div class="jira-fields">
      ${fields.map(([k, v]) => html`<div key=${k}><div class="k">${k}</div><div class="v">${v}</div></div>`)}
      <div><div class="k">Labels</div><div class="v jira-labels">${(d.Labels || []).length ? d.Labels.map((l) => html`<span key=${l} class="jira-label">${l}</span>`) : html`<span class="meta">none</span>`}</div></div>
    </div>
    ${editing ? html`<${Edit} i=${i} d=${d} act=${act} done=${() => setEditing(false)} />` : html`
      <div class="jira-section"><h3>Description</h3><button class="small" title="Edit the summary, description and labels" onClick=${() => setEditing(true)}>Edit</button></div>
      ${i.Description ? html`<div class="jira-desc"><${Markdown} text=${i.Description} /></div>` : html`<p class="meta">No description · Edit adds one</p>`}`}
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

// Said is one comment as a card: an initial, who and when, the text (a long one folded), and Reply.
function Said({ c, reply }) {
  const long = c.Body.split('\n').length > 16 || c.Body.length > 1400;
  const [full, setFull] = useState(!long);
  const when = c.Created && !c.Created.startsWith('0001') ? (ago(c.Created) === 'now' ? 'just now' : ago(c.Created) + ' ago') : '';
  return html`<div class="said">
    <span class="avatar-s">${(c.Author || '?')[0].toUpperCase()}</span>
    <div class="said-main">
      <div class="said-head"><b>${c.Author}</b><span class="meta">${when}</span><span class="grow"></span>
        <button class="ghost small" title="Reply: quote it in the box below" onClick=${() => reply(c)}>Reply</button></div>
      <div class=${'said-body' + (full ? '' : ' folded')}><${Markdown} text=${c.Body} /></div>
      ${long && html`<button class="ghost small more" onClick=${() => setFull(!full)}>${full ? 'Show less' : 'Show more'}</button>`}
    </div></div>`;
}

// quote is a comment as the start of a reply: who said it, then their words as a quote. Jira's
// comments are flat, so a reply is a new comment that quotes the one it answers.
// ponytail: names the person in text; an @mention that notifies needs their Atlassian account id.
const quote = (c) => c.Author + ' wrote:\n' + c.Body.split('\n').slice(0, 6).map((l) => '> ' + l).join('\n') + (c.Body.split('\n').length > 6 ? '\n> …' : '') + '\n\n';

// CommentsTab is the comments as cards, oldest first, and a box: Post sends it as you; Reply quotes a
// comment into it; Draft with Claude fills it for you to edit.
function CommentsTab({ card, i }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState('');
  const box = useRef();
  const comments = card.data ? card.data.comments || [] : [];
  const post = async () => {
    if (!text.trim()) return;
    setBusy('Posting…');
    try { await api('POST', '/jira/' + i.Key + '/comment', { Body: text }); setText(''); setBusy(''); card.reload(); } catch (e) { setBusy("Couldn't post: " + e.message + ' · your comment is still here'); }
  };
  const draft = async () => {
    setBusy('Claude is drafting a comment…');
    try { const r = await api('POST', '/jira/' + i.Key + '/draft'); setText(r.text); setBusy("Claude's draft · edit it, then Post"); box.current && box.current.focus(); } catch (e) { setBusy("Claude couldn't draft one: " + e.message); }
  };
  const reply = (c) => {
    setText((t) => quote(c) + t);
    setTimeout(() => { const b = box.current; if (b) { b.focus(); b.setSelectionRange(b.value.length, b.value.length); b.scrollIntoView({ block: 'nearest' }); } }, 0);
  };
  return html`<div class="scroll"><div class="jira-convo">
      ${!card.data && html`<p class="meta">Reading the comments…</p>`}
      ${card.data && !comments.length && html`<p class="meta">No comments yet. Write the first below.</p>`}
      ${comments.map((c, n) => html`<div class="jira-card" key=${n}><${Said} c=${c} reply=${reply} /></div>`)}
      <div class="composer">
        <textarea ref=${box} rows="4" placeholder=${'Comment on ' + i.Key + '…'} value=${text} onInput=${(e) => setText(e.target.value)}
          onKeyDown=${(e) => { if (e.metaKey && e.key === 'Enter') post(); }}></textarea>
        <div class="btns"><span class="meta">${busy || '⌘⏎ posts it to Jira, as you'}</span>
          <span><button onClick=${draft} disabled=${busy === 'Claude is drafting a comment…'}>Draft with Claude</button>
          <button class="primary" disabled=${!text.trim() || busy === 'Posting…'} onClick=${post}>Post to Jira</button></span></div>
      </div>
    </div></div>`;
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
