// PULL REQUESTS: the PRs waiting for your review, yours, and the rest in your attached repos, by repo
// (starred first; hidden repos and orgs left out), as a list or beside the open PR, on five tabs
// (Overview, Conversation, Files, Summary, Chat). Reading costs no tokens (gh); only Summary and Chat
// talk to an agent. Posting (a comment on a line, a review) asks first.
import { html, useState, useEffect, api, on, useEvents, useLoad, ago, openLink, Markdown, Chat, Modal, Empty, Search, useListKeys, ask, css, Menu, DiffView, Summary } from '../../lib.js';

css(import.meta.url);

const TABS = ['Overview', 'Conversation', 'Files', 'Summary', 'Chat'];
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

// Overview is the PR's description (long ones folded), checks and reviews; its state is in the header.
function Overview({ pr }) {
  const [full, setFull] = useState(false);
  const long = pr.Body.split('\n').length > 30 || pr.Body.length > 2500;
  return html`<div class="scroll"><div class="pr-body">
    ${pr.Body.trim() ? html`<div class=${'pr-desc' + (long && !full ? ' folded' : '')}><${Markdown} text=${pr.Body} html=${true} /></div>
        ${long && html`<button class="small pr-more" onClick=${() => setFull(!full)}>${full ? 'Show less' : 'Show all'}</button>`}`
      : html`<p class="meta">No description.</p>`}
    <div class="pr-section">Checks</div>
    ${pr.Checks && pr.Checks.length ? pr.Checks.map((c) => html`<div class="check" key=${c.Name}>
        <span class=${'st ' + c.State}>${checkMark[c.State] || '?'}</span>
        ${c.URL ? html`<a href=${c.URL} onClick=${(e) => { e.preventDefault(); openLink(c.URL); }}>${c.Name}</a>` : c.Name}</div>`)
      : html`<div class="meta">No checks.</div>`}
    <div class="pr-section">Reviews</div>
    ${pr.Reviews && pr.Reviews.length ? pr.Reviews.map((r, i) => html`<div class="card" key=${i}>
        <div class="card-head"><b>@${r.Author}</b> ${(decision[r.State] || [, r.State.toLowerCase().replace('_', ' ')])[1]}<span class="meta">${ago(r.At)}</span></div>
        ${r.Body.trim() && html`<div class="card-body"><${Markdown} text=${r.Body} html=${true} /></div>`}</div>`)
      : html`<div class="meta">No reviews yet.</div>`}
  </div></div>`;
}

// isBot tells the review bots from people: they write long, so their comments start folded.
const isBot = (a) => /\[bot\]$|^(coderabbitai|cubic-dev-ai|copilot|github-actions|sonarcloud|codecov|dependabot|renovate)/i.test(a || '');

// Said is one comment: who, when, and the text, a long one (or a bot's) folded with Show more.
function Said({ c }) {
  const bot = isBot(c.Author);
  const long = c.Body.split('\n').length > 14 || c.Body.length > 1200;
  const [full, setFull] = useState(!(long || (bot && c.Body.length > 400)));
  return html`<div class="said">
    <span class=${'avatar-s' + (bot ? ' bot' : '')}>${(c.Author || '?')[0].toUpperCase()}</span>
    <div class="said-main">
      <div class="said-head"><b>@${c.Author}</b>${bot && html`<span class="tag">bot</span>`}<span class="meta">${ago(c.At)}</span></div>
      <div class=${'said-body' + (full ? '' : ' folded')}><${Markdown} text=${c.Body} html=${true} /></div>
      ${!full ? html`<button class="ghost small more" onClick=${() => setFull(true)}>Show more</button>`
        : (long || bot) && c.Body.length > 400 && html`<button class="ghost small more" onClick=${() => setFull(false)}>Show less</button>`}
    </div></div>`;
}

// Code is the lines a thread is on, as GitHub sends them (its diff hunk), coloured like a diff.
function Code({ hunk }) {
  const lines = hunk.split('\n').filter((l) => !l.startsWith('@@')).slice(-6);
  return html`<div class="thread-code">${lines.map((l, i) => html`<div key=${i} class=${l[0] === '+' ? 'add' : l[0] === '-' ? 'del' : ''}>${l || ' '}</div>`)}</div>`;
}

// Thread is a conversation on a line: where, the code, the comments, then Reply and Resolve, as you.
// A resolved one starts folded to its header. inline: it's drawn under its line in the diff, so it
// leaves out the code.
function Thread({ t, url, onPosted, inline }) {
  const [open, setOpen] = useState(!t.Resolved);
  const [reply, setReply] = useState(null); // the reply being written, or null
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const act = async (path, body, what) => {
    if (what && !(await ask(what, 'Post'))) return;
    setBusy(true); setErr('');
    try { await api('POST', path, body); setReply(null); onPosted(); } catch (e) { setErr(e.message); }
    setBusy(false);
  };
  return html`<div class=${'thread' + (t.Resolved ? ' resolved' : '')}>
    <div class="thread-head" onClick=${() => setOpen(!open)}>
      <span class="caret">${open ? '▾' : '▸'}</span><code>${t.Path}${t.Line ? ':' + t.Line : ''}</code>
      ${t.Resolved && html`<span class="chip ok">resolved</span>`}${t.Outdated && html`<span class="chip">outdated</span>`}
      <span class="meta">${t.Comments.length} comment${t.Comments.length === 1 ? '' : 's'}${!open && t.Comments[0] ? ' · @' + t.Comments[0].Author : ''}</span>
      <span class="grow"></span>
      ${t.ID && html`<button class="small" disabled=${busy} title=${t.Resolved ? 'Open the thread again on GitHub' : 'Mark it resolved on GitHub'}
        onClick=${(e) => { e.stopPropagation(); act('/prs/resolve', { URL: url, Thread: t.ID, Resolved: !t.Resolved }); }}>${t.Resolved ? 'Reopen' : '✓ Resolve'}</button>`}
    </div>
    ${open && html`${t.DiffHunk && !inline && html`<${Code} hunk=${t.DiffHunk} />`}
      ${t.Comments.map((c, j) => html`<${Said} key=${j} c=${c} />`)}
      ${t.ID && (reply === null
        ? html`<div class="thread-foot"><button class="small" onClick=${() => setReply('')}>Reply</button></div>`
        : html`<div class="thread-reply"><textarea rows="3" autofocus placeholder="Reply on GitHub…" value=${reply} onInput=${(e) => setReply(e.target.value)} />
            <div class="btns">${err && html`<span class="err">${err}</span>`}<button onClick=${() => setReply(null)}>Cancel</button>
              <button class="primary" disabled=${busy || !reply.trim()} onClick=${() => act('/prs/reply', { URL: url, Thread: t.ID, Body: reply }, 'Post this reply on GitHub, as you?')}>${busy ? 'Posting…' : 'Reply'}</button></div></div>`)}
      ${err && reply === null && html`<div class="err thread-err">${err}</div>`}`}
  </div>`;
}

// Conversation is the PR's comments and line threads in time order, filtered by the chips, and a box
// to comment on the PR. Everything posts as you, after you confirm.
function Conversation({ pr, onPosted }) {
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [posting, setPosting] = useState(false);
  const [show, setShow] = useState('all');
  const post = async () => {
    if (!(await ask('Post this comment on GitHub, as you?', 'Post'))) return;
    setPosting(true);
    try { await api('POST', '/prs/comment', { URL: pr.URL, Body: text }); setText(''); setErr(''); onPosted(); } catch (e) { setErr(e.message); }
    setPosting(false);
  };
  const threads = pr.Threads || [];
  const open = threads.filter((t) => !t.Resolved).length;
  const items = [...(pr.Comments || []).map((c) => ({ at: c.At, c })), ...threads.map((t) => ({ at: (t.Comments[0] || {}).At, t }))]
    .filter((x) => show === 'all' || (show === 'open' ? x.t && !x.t.Resolved : x.c))
    .sort((a, b) => new Date(a.at) - new Date(b.at));
  const chips = [['all', 'All'], ['open', 'Open threads', open], ['comments', 'Comments', (pr.Comments || []).length]];
  return html`<div class="scroll"><div class="convo">
    <div class="filters convo-chips">${chips.map(([k, l, n]) => html`<button key=${k} class=${'fchip' + (show === k ? ' on' : '')} onClick=${() => setShow(k)}>${l}${n ? html` <span class="n">${n}</span>` : ''}</button>`)}</div>
    ${pr.ThreadsErr && html`<p class="err">Line comments couldn't be read: ${pr.ThreadsErr}</p>`}
    ${!items.length && html`<p class="meta">${show === 'open' ? 'No open threads.' : 'No comments yet.'}</p>`}
    ${items.map((x, i) => x.c ? html`<div class="thread comment" key=${'c' + i}><${Said} c=${x.c} /></div>`
      : html`<${Thread} key=${x.t.ID || 't' + i} t=${x.t} url=${pr.URL} onPosted=${onPosted} />`)}
    <div class="composer">
      <textarea rows="4" placeholder="Write a comment for GitHub…" value=${text} onInput=${(e) => setText(e.target.value)}></textarea>
      ${err && html`<div class="err">${err}</div>`}
      <div class="btns"><span class="meta">it posts as you, after you confirm</span><button class="primary" disabled=${!text.trim() || posting} onClick=${post}>${posting ? 'Posting…' : 'Comment on GitHub'}</button></div>
    </div></div></div>`;
}

// Files is the PR's diff in the shared DiffView (tree, line numbers, files closed at first), with its
// comment threads under their lines (reply and resolve there too), and a line's + to post a comment
// on that line to GitHub, as you (it asks first). onPosted reads the PR again, so a new one shows.
function Files({ url, threads, onPosted }) {
  const { data, err, loading, reload } = useLoad(() => api('GET', '/prs/diff?url=' + encodeURIComponent(url)), [url]);
  if (loading && !data) return html`<div class="scroll meta">Reading the diff…</div>`;
  if (err) return html`<div class="scroll err">${err}</div>`;
  const comment = (d) => api('POST', '/prs/line', { URL: url, Path: d.path, Line: d.line, Side: d.side, Body: d.body }).then(onPosted);
  return html`<${DiffView} diff=${data.diff} reload=${reload} comment=${comment} threads=${threads}
    thread=${(t) => html`<${Thread} t=${t} url=${url} onPosted=${onPosted} inline=${true} />`} />`;
}

// useChat is the PR's chat (a task file crew keeps for it), opened on first use: {id, attached}.
function useChat(url) {
  const [chat, setChat] = useState(null);
  const [err, setErr] = useState('');
  useEffect(() => { setChat(null); api('POST', '/prs/chat', { URL: url }).then(setChat, (e) => setErr(e.message)); }, [url]);
  return { chat, err };
}

// PRSummary is the Summary tab: Claude summarises the PR for its reviewer in the PR's chat, with the
// same skill as a task's summary (core.SummaryTurn, given the PR).
function PRSummary({ it }) {
  const { chat, err } = useChat(it.URL);
  const [busy, setBusy] = useState(false);
  useEvents((ev) => chat && ev.kind === 'turn' && ev.id === chat.id && setBusy(!!ev.busy), [chat]);
  if (err) return html`<div class="scroll err">${err}</div>`;
  if (!chat) return html`<div class="scroll meta">Opening…</div>`;
  return html`<${Summary} key=${chat.id} id=${chat.id} working=${busy} />`;
}

function PRChat({ it }) {
  const { chat, err } = useChat(it.URL);
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

// PRHead is the open PR's header. Line 1: back, the whole title (it wraps), and the actions. Line 2:
// its state at a glance (state, review, checks, conflicts, size), then where it's from: repo, number,
// author, head → base, when it moved. What the list knows shows until the PR itself is read.
function PRHead({ it, pr, onBack, reload, review }) {
  const [cls, label] = (pr && decision[pr.ReviewDecision]) || [];
  const state = !pr ? '' : pr.State === 'MERGED' ? html`<span class="chip merged">merged</span>` : pr.State === 'CLOSED' ? html`<span class="chip bad">closed</span>`
    : pr.Draft ? html`<span class="chip">draft</span>` : html`<span class="chip ok">open</span>`;
  return html`<header class="page-head">
    <div class="page-head-top">
      <button class="small back" title="Back to the list (esc)" onClick=${onBack}>←</button>
      <h1>${it.Title} <span class="page-num">#${it.Number}</span></h1>
      <div class="page-acts">
        ${pr && pr.State === 'OPEN' && html`<${Menu} label="Review ▾" title="Post a review as you (it asks first)" items=${[
          { label: '✓ Approve', run: () => review('APPROVE') },
          { label: '✗ Request changes', run: () => review('REQUEST_CHANGES') },
          { label: 'Comment as a review', run: () => review('COMMENT') },
        ]} />`}
        <button class="small" onClick=${reload} title="Read it again">↻</button>
        <button class="small" onClick=${() => openLink(it.URL)} title="Open on GitHub (o)">GitHub ↗</button>
      </div>
    </div>
    <div class="page-head-sub">
      ${state}${label && html`<span class=${'chip ' + cls}>${label}</span>`}${pr && checksChip(pr.Checks)}
      ${pr && pr.MergeState === 'DIRTY' && html`<span class="chip bad">conflicts</span>`}
      ${pr && html`<span class="chip"><span class="plus">+${pr.Additions}</span> <span class="minus">−${pr.Deletions}</span></span>`}
      <span class="page-meta">
        <span>${it.Repo}</span><span>${it.Mine ? 'you' : '@' + it.Author}</span>
        ${pr && pr.Head && html`<span><code>${pr.Head}</code> → <code>${pr.Base}</code></span>`}
        <span>updated ${ago(it.Updated) === 'now' ? 'just now' : ago(it.Updated) + ' ago'}</span>
      </span>
    </div>
  </header>`;
}

// PR is the open pull request, full width: its header (← back to the list with onBack), and its tabs.
function PR({ it, onBack }) {
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
    <${PRHead} it=${it} pr=${pr} onBack=${onBack} reload=${view.reload} review=${setReview} />
    <div class="tabs">${TABS.map((t, i) => html`<button key=${t} class=${i === tab ? 'on' : ''} onClick=${() => setTab(i)}>${t}</button>`)}</div>
    ${tab === 4 ? html`<${PRChat} it=${it} />`
      : tab === 3 ? html`<${PRSummary} it=${it} />`
      : tab === 2 ? html`<${Files} url=${it.URL} threads=${pr && pr.Threads} onPosted=${view.reload} />`
      : view.err ? html`<div class="scroll err">${view.err}</div>`
      : !pr ? html`<div class="scroll meta">Reading the PR…</div>`
      : tab === 0 ? html`<${Overview} pr=${pr} />`
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
  return html`<div class="group-head">
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
  return html`<div class=${'item list-row' + (on ? ' on' : '')} onClick=${() => open(it.URL)}>
    <${Mark} it=${it} />
    <div class="item-main"><div class="item-title">${it.Title}${it.Draft ? html`<span class="tag">draft</span>` : ''}</div>
      <div class="item-sub">#${it.Number} · ${it.Mine ? 'you' : '@' + it.Author} · ${ago(it.Updated)}${it.Review ? html`<span class="tag review">review${it.Team ? ' · team' : ''}</span>` : ''}</div></div></div>`;
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

// The page: a list view (all PRs, by repo, full width) and a PR view (the open PR, full width). A click
// or ⏎ opens a PR; ← Pull requests, esc or b goes back to the list.
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
  useListKeys(view === 'list' ? shown.map((x) => x.URL) : [], sel, setSel); // the list's: an open PR uses ↑↓ to scroll
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
      <div class="head-btns"><button class="small" title="Read the list again (r)" onClick=${() => api('POST', '/prs/refresh')}>${data && data.loading ? '…' : '↻'}</button></div></div>
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
      <span class="meta">⏎ opens · s star · ←→ fold</span></div>
    ${showHidden && html`<${Hidden} prefs=${prefs} pref=${pref} onClose=${() => setShowHidden(false)} />`}`;
  if (view === 'list' || !it) return html`<main class="page list-page">${head}${list}</main>`;
  return html`<${PR} key=${it.URL} it=${it} onBack=${() => setView('list')} />`;
}
