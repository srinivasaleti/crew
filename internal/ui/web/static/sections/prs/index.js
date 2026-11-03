// PULL REQUESTS: the PRs waiting for your review, yours, and the rest in your attached repos, by repo
// (starred first; hidden repos and orgs left out), as a list or beside the open PR, on four tabs (Overview, Conversation, Files, Chat). Reading costs no
// tokens (gh); only the Chat tab talks to an agent. Posting (a comment, a review) asks first.
import { html, useState, useEffect, api, on, useEvents, useLoad, ago, openLink, Markdown, Chat, Modal, Empty, Search, useListKeys, Toolbar, ask, css, Menu } from '../../lib.js';

css(import.meta.url);

const TABS = ['Overview', 'Conversation', 'Files', 'Chat'];
const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);

// watch shows how many PRs wait for your review.
export function watch(setBadge) {
  const count = () => api('GET', '/prs').then((l) => setBadge(l.items.filter((it) => it.Review).length || ''));
  count();
  return on((ev) => (ev.kind === 'prs' || ev.kind === 'open') && count());
}


function Mark({ it }) {
  if (it.Draft) return html`<span class="pr-mark" title="draft">◌</span>`;
  if (it.Review) return html`<span class="pr-mark review" title="waiting for your review">●</span>`;
  if (it.Mine) return html`<span class="pr-mark mine" title="yours">⇡</span>`;
  return html`<span class="pr-mark">○</span>`;
}

const checkMark = { pass: '✓', fail: '✗', pending: '●', skip: '○' };

// checksChip sums a PR's checks into one chip.
function checksChip(checks) {
  if (!checks || !checks.length) return '';
  const fail = checks.filter((c) => c.State === 'fail').length;
  const pend = checks.filter((c) => c.State === 'pending').length;
  if (fail) return html`<span class="chip bad">✗ ${fail} failing</span>`;
  if (pend) return html`<span class="chip wait">● ${pend} running</span>`;
  return html`<span class="chip ok">✓ checks pass</span>`;
}

const decision = { APPROVED: ['ok', '✓ approved'], CHANGES_REQUESTED: ['bad', '✗ changes requested'], REVIEW_REQUIRED: ['wait', 'review required'] };

function Overview({ pr, onReview }) {
  const [cls, label] = decision[pr.ReviewDecision] || [];
  return html`<div class="scroll"><div class="pr-body">
    <div class="pr-chips">
      ${pr.State === 'MERGED' ? html`<span class="chip merged">merged</span>` : pr.State === 'CLOSED' ? html`<span class="chip bad">closed</span>` : pr.Draft ? html`<span class="chip">draft</span>` : html`<span class="chip ok">open</span>`}
      ${label && html`<span class=${'chip ' + cls}>${label}</span>`}
      ${checksChip(pr.Checks)}
      ${pr.MergeState === 'DIRTY' && html`<span class="chip bad">conflicts</span>`}
      <span class="chip"><span style="color: var(--ok)">+${pr.Additions}</span> <span style="color: var(--warn)">−${pr.Deletions}</span></span>
    </div>
    ${pr.Body.trim() ? html`<${Markdown} text=${pr.Body} />` : html`<p class="meta">No description.</p>`}
    ${pr.State === 'OPEN' && html`<div class="btns" style="justify-content: flex-start; margin-top: 14px">
      <button onClick=${() => onReview('APPROVE')}>✓ Approve</button>
      <button onClick=${() => onReview('REQUEST_CHANGES')}>✗ Request changes</button>
      <button onClick=${() => onReview('COMMENT')}>Comment as a review</button></div>`}
    <div class="pr-section">Checks</div>
    ${pr.Checks && pr.Checks.length ? pr.Checks.map((c) => html`<div class="check" key=${c.Name}>
        <span class=${'st ' + c.State}>${checkMark[c.State] || '?'}</span>
        ${c.URL ? html`<a href=${c.URL} onClick=${(e) => { e.preventDefault(); openLink(c.URL); }}>${c.Name}</a>` : c.Name}</div>`)
      : html`<div class="meta">No checks.</div>`}
    <div class="pr-section">Reviews</div>
    ${pr.Reviews && pr.Reviews.length ? pr.Reviews.map((r, i) => html`<div class="card" key=${i}>
        <div class="card-head"><b>@${r.Author}</b> ${(decision[r.State] || [, r.State.toLowerCase().replace('_', ' ')])[1]}<span class="meta">${ago(r.At)}</span></div>
        ${r.Body.trim() && html`<div class="card-body"><${Markdown} text=${r.Body} /></div>`}</div>`)
      : html`<div class="meta">No reviews yet.</div>`}
  </div></div>`;
}

function Conversation({ pr, onPosted }) {
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [posting, setPosting] = useState(false);
  const post = async () => {
    if (!(await ask('Post this comment on GitHub, as you?', 'Post'))) return;
    setPosting(true);
    try { await api('POST', '/prs/comment', { URL: pr.URL, Body: text }); setText(''); setErr(''); onPosted(); } catch (e) { setErr(e.message); }
    setPosting(false);
  };
  const threads = pr.Threads || [];
  const items = [...(pr.Comments || []).map((c) => ({ at: c.At, c })), ...threads.map((t) => ({ at: (t.Comments[0] || {}).At, t }))]
    .sort((a, b) => new Date(a.at) - new Date(b.at));
  return html`<div class="scroll">
    ${!items.length && html`<p class="meta">No comments yet.</p>`}
    ${pr.ThreadsErr && html`<p class="err">Line comments couldn't be read: ${pr.ThreadsErr}</p>`}
    ${items.map((x, i) => x.c ? html`<div class="card" key=${i}>
        <div class="card-head"><b>@${x.c.Author}</b><span class="meta">${ago(x.c.At)}</span></div>
        <div class="card-body"><${Markdown} text=${x.c.Body} /></div></div>`
      : html`<div class=${'card' + (x.t.Resolved ? ' resolved' : '')} key=${i}>
        <div class="card-head"><code>${x.t.Path}${x.t.Line ? ':' + x.t.Line : ''}</code>
          ${x.t.Resolved && html`<span class="chip ok">resolved</span>`}${x.t.Outdated && html`<span class="chip">outdated</span>`}</div>
        ${x.t.DiffHunk && html`<pre class="hunk">${x.t.DiffHunk.split('\n').slice(-6).join('\n')}</pre>`}
        ${x.t.Comments.map((c, j) => html`<div class="reply" key=${j}><b>@${c.Author}</b> <span class="meta">${ago(c.At)}</span><${Markdown} text=${c.Body} /></div>`)}</div>`)}
    <div class="composer">
      <textarea rows="4" placeholder="Write a comment for GitHub…" value=${text} onInput=${(e) => setText(e.target.value)}></textarea>
      ${err && html`<div class="err">${err}</div>`}
      <div class="btns"><button class="primary" disabled=${!text.trim() || posting} onClick=${post}>${posting ? 'Posting…' : 'Comment on GitHub'}</button>
        <span class="meta">it posts as you, after you confirm</span></div>
    </div></div>`;
}

// files splits a unified diff into its files.
function files(diff) {
  const out = [];
  for (const chunk of diff.split(/^diff --git /m).slice(1)) {
    const lines = chunk.split('\n');
    const name = (lines[0].match(/ b\/(.+)$/) || [, lines[0]])[1];
    const at = lines.findIndex((l) => l.startsWith('@@'));
    const body = at < 0 ? [] : lines.slice(at); // a binary file or a rename has no hunks
    const add = body.filter((l) => l.startsWith('+')).length, del = body.filter((l) => l.startsWith('-')).length;
    out.push({ name, body, add, del });
  }
  return out;
}

function Files({ url }) {
  const { data, err, loading } = useLoad(() => api('GET', '/prs/diff?url=' + encodeURIComponent(url)), [url]);
  const [shut, setShut] = useState({});
  if (loading && !data) return html`<div class="scroll meta">Reading the diff…</div>`;
  if (err) return html`<div class="scroll err">${err}</div>`;
  const fs = files(data.diff);
  return html`<div class="scroll">
    <div class="files-sum">${fs.length} file${fs.length === 1 ? '' : 's'} changed</div>
    ${fs.map((f) => html`<div class="file" key=${f.name}>
      <div class="file-head" onClick=${() => setShut({ ...shut, [f.name]: !shut[f.name] })}>${shut[f.name] ? '▸' : '▾'} ${f.name}
        <span class="add">+${f.add}</span><span class="del">−${f.del}</span></div>
      ${!shut[f.name] && html`<pre>${f.body.map((l, i) => html`<span key=${i} class=${'dl' + (l.startsWith('@@') ? ' hunk' : l.startsWith('+') ? ' add' : l.startsWith('-') ? ' del' : '')}>${l || ' '}</span>`)}</pre>`}
    </div>`)}</div>`;
}

function PRChat({ it }) {
  const [chat, setChat] = useState(null);
  const [err, setErr] = useState('');
  useEffect(() => { setChat(null); api('POST', '/prs/chat', { URL: it.URL }).then(setChat, (e) => setErr(e.message)); }, [it.URL]);
  if (err) return html`<div class="scroll err">${err}</div>`;
  if (!chat) return html`<div class="scroll meta">Opening the chat…</div>`;
  return html`<div class="chat-note">${chat.attached ? 'Claude works in a checkout of this PR.' : "Claude reads it with gh (its repo isn't attached to crew, so there's no checkout)."} Nothing goes to GitHub unless you ask.</div>
    <${Chat} key=${chat.id} id=${chat.id} base=${'/tasks/' + chat.id} agent="Claude" empty="Ask Claude about this PR: what it does, whether it's safe, how to test it." />`;
}

// ReviewForm posts a review as you, after you write it and confirm.
function ReviewForm({ url, event, onClose, onPosted }) {
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const title = { APPROVE: 'Approve', REQUEST_CHANGES: 'Request changes', COMMENT: 'Comment as a review' }[event];
  const go = async (e) => {
    e.preventDefault();
    try { await api('POST', '/prs/review', { URL: url, Event: event, Body: text }); onPosted(); onClose(); } catch (x) { setErr(x.message); }
  };
  return html`<${Modal} title=${title} onClose=${onClose}><form class="form" onSubmit=${go}>
    <label>${event === 'APPROVE' ? 'A note (optional)' : 'What to change, or your comment'}<textarea autofocus rows="5" value=${text} onInput=${(e) => setText(e.target.value)}></textarea></label>
    ${err && html`<div class="err">${err}</div>`}
    <div class="btns"><button type="button" onClick=${onClose}>Cancel</button><button class="primary">Post on GitHub, as you</button></div>
  </form><//>`;
}

function PR({ it }) {
  const [tab, setTab] = useState(0);
  const [review, setReview] = useState('');
  const view = useLoad(() => api('GET', '/prs/view?url=' + encodeURIComponent(it.URL)), [it.URL]);
  useEffect(() => {
    const k = (e) => {
      if (e.key === 'Tab' && !typing()) { e.preventDefault(); setTab((t) => (t + (e.shiftKey ? TABS.length - 1 : 1)) % TABS.length); }
      if (e.key === 'o' && !typing() && !e.metaKey) openLink(it.URL);
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [it.URL]);
  const pr = view.data;
  return html`<main class="page">
    <${Toolbar} title=${'#' + it.Number + ' ' + it.Title}
      sub=${it.Repo + ' · @' + it.Author + (pr && pr.Head ? ' · ' + pr.Head + ' → ' + pr.Base : '') + ' · updated ' + ago(it.Updated)}>
      ${pr && checksChip(pr.Checks)}
      <button onClick=${() => view.reload()} title="Read it again">↻</button>
      <button onClick=${() => openLink(it.URL)} title="Open on GitHub (o)">Open on GitHub</button><//>
    <div class="tabs">${TABS.map((t, i) => html`<button key=${t} class=${i === tab ? 'on' : ''} onClick=${() => setTab(i)}>${t}</button>`)}</div>
    ${tab === 3 ? html`<${PRChat} it=${it} />`
      : tab === 2 ? html`<${Files} url=${it.URL} />`
      : view.err ? html`<div class="scroll err">${view.err}</div>`
      : !pr ? html`<div class="scroll meta">Reading the PR…</div>`
      : tab === 0 ? html`<${Overview} pr=${pr} onReview=${setReview} />`
      : html`<${Conversation} pr=${pr} onPosted=${view.reload} />`}
    ${review && html`<${ReviewForm} url=${it.URL} event=${review} onClose=${() => setReview('')} onPosted=${view.reload} />`}
  </main>`;
}

// filters are the chips above the list: what each keeps.
const filters = [
  ['review', 'To review', (it) => it.Review],
  ['mine', 'Yours', (it) => it.Mine],
  ['starred', '★ Starred', (it, p) => p.starred.includes(it.Repo)],
  ['all', 'All', () => true],
];

// byRepo groups PRs by repo: starred repos first, then by name; newest first inside (as listed).
function byRepo(items, prefs) {
  const groups = {};
  for (const it of items) (groups[it.Repo] ||= []).push(it);
  const star = (r) => (prefs.starred.includes(r) ? 0 : 1);
  return Object.keys(groups).sort((a, b) => star(a) - star(b) || a.localeCompare(b)).map((repo) => ({ repo, items: groups[repo] }));
}

// RepoHead is a repo's heading: fold it, star it, and its menu (star, hide it or its org, GitHub).
function RepoHead({ g, folded, fold, starred, pref }) {
  const org = g.repo.split('/')[0];
  return html`<div class="repo-head">
    <button class="group fold" onClick=${fold}><span class="caret">${folded ? '▸' : '▾'}</span> ${g.repo} <span class="n">${g.items.length}</span></button>
    <button class=${'ghost small star' + (starred ? ' on' : '')} title=${starred ? 'Unstar (s)' : 'Star: pin it on top (s)'} onClick=${() => pref({ Star: g.repo, On: !starred })}>${starred ? '★' : '☆'}</button>
    <${Menu} label="⋯" title="More" items=${[
      { label: starred ? 'Unstar ' + g.repo : 'Star ' + g.repo, run: () => pref({ Star: g.repo, On: !starred }) },
      { label: 'Hide this repo', run: () => pref({ Hide: g.repo, On: true }) },
      { label: 'Hide everything from ' + org, run: () => pref({ Hide: org, On: true }) },
      { label: 'Open on GitHub ↗', run: () => openLink('https://github.com/' + g.repo + '/pulls') },
    ]} />
  </div>`;
}

// Row is one PR: its mark, title, and number · author · age (· team).
function Row({ it, on, open }) {
  return html`<div class=${'item pr-row' + (on ? ' on' : '')} onClick=${() => open(it.URL)}>
    <${Mark} it=${it} />
    <div class="item-main"><div class="item-title">${it.Title}${it.Draft ? html`<span class="pr-tag">draft</span>` : ''}</div>
      <div class="item-sub">#${it.Number} · ${it.Mine ? 'you' : '@' + it.Author} · ${ago(it.Updated)}${it.Review ? html`<span class="pr-tag review">review${it.Team ? ' · team' : ''}</span>` : ''}</div></div></div>`;
}

// Hidden lists the repos and orgs you hid, each with Show.
function Hidden({ prefs, pref, onClose }) {
  const [repos, orgs] = [prefs.hidden.filter((h) => h.includes('/')), prefs.hidden.filter((h) => !h.includes('/'))];
  const list = (title, l) => l.length > 0 && html`<div class="pr-section">${title}</div>${l.map((h) => html`<div key=${h} class="hidden-row"><span>${h}</span><button class="small" onClick=${() => pref({ Hide: h, On: false })}>Show</button></div>`)}`;
  return html`<${Modal} title="Hidden" onClose=${onClose}>
    ${prefs.hidden.length ? html`${list('Repos', repos)}${list('Orgs', orgs)}` : html`<p class="meta">Nothing hidden.</p>`}
    <p class="meta">Review requests still show from hidden repos: someone is waiting on you.</p>
    <div class="btns"><button class="primary" onClick=${onClose}>Done</button></div><//>`;
}

// The page: a list view (all PRs, by repo, full width) and a PR view (the list narrow, the open PR
// beside it), as TASKS does. A click opens a PR; b switches views, esc goes back to the list.
export default function PRs() {
  const { data, reload } = useLoad(() => api('GET', '/prs'));
  const [sel, setSel] = useState(decodeURIComponent(location.hash.split('/').slice(1).join('/')));
  const [view, setView] = useState(sel ? 'pr' : 'list');
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState('');
  const [folds, setFolds] = useState({});
  const [showHidden, setShowHidden] = useState(false);
  useEvents((ev) => (ev.kind === 'prs' || ev.kind === 'open') && reload());
  const all = (data && data.items) || [];
  const prefs = (data && data.prefs) || {};
  prefs.starred ||= []; prefs.hidden ||= [];
  const f = filter || (all.some((it) => it.Review) ? 'review' : 'all'); // to review first, when there's any
  const keep = filters.find((x) => x[0] === f)[2];
  const items = all.filter((it) => keep(it, prefs) && (!q || (it.Title + ' ' + it.Repo + ' ' + it.Author + ' #' + it.Number).toLowerCase().includes(q.toLowerCase())));
  const groups = byRepo(items, prefs);
  // A repo starts open when it's starred or has a PR for you (or there are few), else folded.
  const folded = (g) => folds[g.repo] ?? !(prefs.starred.includes(g.repo) || g.items.some((it) => it.Review || it.Mine) || groups.length <= 3);
  const shown = groups.flatMap((g) => (folded(g) ? [] : g.items));
  const open = (url) => { setSel(url); setView('pr'); location.hash = 'prs/' + url; };
  const it = all.find((x) => x.URL === sel);
  const pref = (m) => api('POST', '/prs/prefs', m).catch((e) => ask(e.message));
  const fold = (repo, v) => setFolds((x) => ({ ...x, [repo]: v }));
  useListKeys(shown.map((x) => x.URL), sel, (url) => { setSel(url); if (view === 'pr') open(url); });
  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || document.querySelector('.scrim')) return;
      const g = it && groups.find((x) => x.repo === it.Repo);
      if (e.key === 'r') api('POST', '/prs/refresh');
      if (e.key === 'b') setView(view === 'list' ? (it ? 'pr' : 'list') : 'list');
      if (e.key === 'Escape' && view === 'pr') setView('list');
      if (e.key === 'Enter' && view === 'list' && it) open(it.URL);
      if (e.key === 's' && it) pref({ Star: it.Repo, On: !prefs.starred.includes(it.Repo) });
      if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && g) fold(g.repo, e.key === 'ArrowLeft');
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });
  const head = html`<div class="col-head"><div><b>Pull requests</b> <span class="meta">${all.length}</span></div>
      <div class="head-btns">${view === 'pr' && html`<button class="small" title="Back to the list (b · esc)" onClick=${() => setView('list')}>☰ List</button>`}
        <button class="small" title="Read the list again (r)" onClick=${() => api('POST', '/prs/refresh')}>${data && data.loading ? '…' : '↻'}</button></div></div>
    <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search title, repo, author, #PR" /></div>
    <div class="filters">${filters.map(([k, label, fn]) => {
      const n = k === 'all' ? 0 : all.filter((x) => fn(x, prefs)).length;
      return html`<button key=${k} class=${'fchip' + (f === k ? ' on' : '')} onClick=${() => setFilter(k)}>${label}${n ? html` <span class="n">${n}</span>` : ''}</button>`;
    })}</div>`;
  const list = html`<div class="col-list">
      ${data && !data.gh && html`<div class="col-empty">Install the GitHub CLI (gh) to see pull requests</div>`}
      ${data && data.err && html`<div class="item-sub err" style="padding: 4px 14px">${data.err}</div>`}
      ${groups.length ? groups.map((g) => html`<div key=${g.repo}>
          <${RepoHead} g=${g} folded=${folded(g)} fold=${() => fold(g.repo, !folded(g))} starred=${prefs.starred.includes(g.repo)} pref=${pref} />
          ${!folded(g) && g.items.map((x) => html`<${Row} key=${x.URL} it=${x} on=${x.URL === sel} open=${open} />`)}</div>`)
        : html`<div class="col-empty">${!data || data.loading ? 'Reading pull requests…' : q || f !== 'all' ? 'Nothing here' : 'No open pull requests'}</div>`}
    </div>
    <div class="col-foot">${data && data.hidden ? html`<button class="ghost small" onClick=${() => setShowHidden(true)}>${data.hidden} hidden</button>`
      : prefs.hidden.length ? html`<button class="ghost small" onClick=${() => setShowHidden(true)}>Hidden repos</button>` : html`<span></span>`}
      <span class="meta">${view === 'list' ? '⏎ opens · s star · ←→ fold' : 'esc list'}</span></div>
    ${showHidden && html`<${Hidden} prefs=${prefs} pref=${pref} onClose=${() => setShowHidden(false)} />`}`;
  if (view === 'list' || !it) return html`<main class="page pr-list-page">${head}${list}</main>`;
  return html`<div class="split">
    <section class="col prs-col">${head}${list}</section>
    <${PR} key=${it.URL} it=${it} />
  </div>`;
}
