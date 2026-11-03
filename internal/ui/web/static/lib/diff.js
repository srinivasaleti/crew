// A diff read like a pull request, shared by TASKS' Changes and a PR's Files: the changed files as a
// folder tree, and each file's diff with old and new line numbers. Files start closed and draw only
// when opened; big or generated ones wait for Load diff. With comment, each line can take a comment
// (a PR's: it goes to GitHub as you).
import { html, useState, useRef, useMemo } from '../vendor/preact-htm.js';
import { ask } from './ui.js';

// parseDiff turns git's unified diff into files: path, status (A M D R), counts, and hunks of lines.
export function parseDiff(diff) {
  return diff.split(/^(?=diff --git )/m).filter((c) => c.startsWith('diff --git ')).map((c) => {
    const head = c.slice(0, c.search(/^@@/m) >= 0 ? c.search(/^@@/m) : c.length);
    const to = (head.match(/^\+\+\+ b\/(.*)$/m) || [])[1], from = (head.match(/^--- a\/(.*)$/m) || [])[1];
    const git = head.match(/^diff --git a\/(.*?) b\/(.*)$/m) || [];
    const renamed = (head.match(/^rename to (.*)$/m) || [])[1];
    const path = renamed || to || from || git[2] || 'file';
    const status = /^new file mode/m.test(head) || /^--- \/dev\/null/m.test(head) ? 'A'
      : /^deleted file mode/m.test(head) || /^\+\+\+ \/dev\/null/m.test(head) ? 'D' : renamed ? 'R' : 'M';
    const file = { path, status, from: renamed ? (head.match(/^rename from (.*)$/m) || [])[1] : '', binary: /^Binary files/m.test(head), add: 0, del: 0, size: c.length, hunks: [] };
    let o = 0, n = 0, h = null;
    for (const l of c.slice(head.length).split('\n')) {
      const m = l.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)/);
      if (m) { o = +m[1]; n = +m[2]; h = { head: l, at: m[3].trim(), lines: [] }; file.hunks.push(h); continue; }
      if (!h || l === '') continue;
      if (l[0] === '+') { h.lines.push({ t: 'add', n: n++, text: l.slice(1) }); file.add++; }
      else if (l[0] === '-') { h.lines.push({ t: 'del', o: o++, text: l.slice(1) }); file.del++; }
      else if (l[0] === '\\') h.lines.push({ t: 'meta', text: l.slice(2) });
      else h.lines.push({ t: 'ctx', o: o++, n: n++, text: l.slice(1) });
    }
    return file;
  });
}

// diffTree nests files into folders, joining a folder that only holds one folder (a/b/c), as GitHub does.
export function diffTree(files) {
  const root = { name: '', dirs: {}, files: [] };
  for (const f of files) {
    const parts = f.path.split('/');
    let d = root;
    for (const p of parts.slice(0, -1)) d = d.dirs[p] ||= { name: p, dirs: {}, files: [] };
    d.files.push(f);
  }
  const squash = (d) => {
    for (const k of Object.keys(d.dirs)) {
      let c = d.dirs[k];
      while (!c.files.length && Object.keys(c.dirs).length === 1) { const only = Object.values(c.dirs)[0]; c = { ...only, name: c.name + '/' + only.name }; }
      d.dirs[k] = squash(c);
    }
    return d;
  };
  return squash(root);
}

const counts = (f) => html`<span class="cstat">${f.add ? html`<span class="add">+${f.add}</span>` : ''}${f.del ? html` <span class="del">−${f.del}</span>` : ''}</span>`;

// talks is a file's comment count (its threads that are still open), 💬 n, when it has any.
const talks = (ts) => {
  const n = (ts || []).filter((t) => !t.Resolved).length;
  return ts && ts.length ? html`<span class=${'ctalk' + (n ? '' : ' done')} title=${ts.length + ' thread' + (ts.length > 1 ? 's' : '') + (n ? ', ' + n + ' open' : ', all resolved')}>💬 ${ts.length}</span>` : '';
};

// Tree draws a folder's folders (foldable) then its files.
function Tree({ d, depth, at, go, shut, flip, talk }) {
  return html`${Object.values(d.dirs).sort((a, b) => a.name.localeCompare(b.name)).map((c) => {
    const key = depth + ':' + c.name;
    return html`<div key=${key}>
      <div class="cnode cn-dir" style=${{ paddingLeft: 6 + depth * 12 + 'px' }} onClick=${() => flip(key)}><span class="caret">${shut[key] ? '▸' : '▾'}</span> ${c.name}</div>
      ${!shut[key] && html`<${Tree} d=${c} depth=${depth + 1} at=${at} go=${go} shut=${shut} flip=${flip} talk=${talk} />`}</div>`;
  })}${d.files.map((f) => html`<div key=${f.path} class=${'cnode cn-file' + (at === f.path ? ' on' : '')} style=${{ paddingLeft: 18 + depth * 12 + 'px' }} title=${f.path} onClick=${() => go(f.path)}>
      <span class=${'cst s' + f.status}>${f.status}</span><span class="cname">${f.path.split('/').pop()}</span>${talks(talk[f.path])}${counts(f)}</div>`)}`;
}

// big is a file not worth drawing until asked: a lockfile, a minified or map file, or a large diff
// (over 500 lines, or 200 KB: a one-line file can be huge).
const bulky = /(^|\/)(package-lock\.json|npm-shrinkwrap\.json|yarn\.lock|pnpm-lock\.yaml|go\.sum|Cargo\.lock|poetry\.lock|Gemfile\.lock|composer\.lock|Pipfile\.lock)$|\.min\.(js|css)$|\.map$/;
export const big = (f) => bulky.test(f.path) || f.add + f.del > 500 || f.size > 200e3;

// File is one file's diff: a sticky header, then (open, and loaded when it's big) its hunks as rows
// of old line, new line, code. A closed file draws only its header, so a big change stays quick.
// With comment, a line's + opens a box under it (draft is the line being commented, if it's here).
// threads are the file's comment threads, drawn under their line by thread() (one whose line the
// diff doesn't show, like an outdated one, goes at the top).
function File({ f, open, toggle, loaded, load, comment, draft, setDraft, threads = [], thread }) {
  const at = (side, line) => side + ':' + line;
  const placed = new Set(f.hunks.flatMap((h) => h.lines.filter((l) => l.t !== 'meta').map((l) => (l.t === 'del' ? at('LEFT', l.o) : at('RIGHT', l.n)))));
  const on = (key) => threads.filter((t) => !t.Outdated && at(t.Side || 'RIGHT', t.Line) === key);
  const loose = threads.filter((t) => t.Outdated || !placed.has(at(t.Side || 'RIGHT', t.Line)));
  const row = (l, key) => {
    const side = l.t === 'del' ? 'LEFT' : 'RIGHT', line = l.t === 'del' ? l.o : l.n;
    const here = draft && draft.key === key;
    const ts = l.t === 'meta' || !thread ? [] : on(at(side, line));
    return html`<tr key=${key} class=${'c' + l.t + (here ? ' cdraft' : '') + (ts.length ? ' ctalked' : '')}>
        <td class="ln">${comment && l.t !== 'meta' && html`<button class="cplus" title="Comment on this line" onClick=${() => setDraft({ key, path: f.path, line, side })}>+</button>`}${l.o ?? ''}</td>
        <td class="ln">${l.n ?? ''}</td><td class="code"><span class="sign">${l.t === 'add' ? '+' : l.t === 'del' ? '−' : ' '}</span>${l.text}</td></tr>
      ${ts.map((t) => html`<tr class="cthread" key=${t.ID}><td colspan="3">${thread(t)}</td></tr>`)}
      ${here && html`<tr class="cbox"><td colspan="3"><${LineComment} draft=${draft} comment=${comment} close=${() => setDraft(null)} /></td></tr>`}`;
  };
  const body = () => f.binary ? html`<div class="cnote">Binary file</div>`
    : !f.hunks.length ? html`<div class="cnote">${f.status === 'R' ? 'Renamed, no changes' : 'No changes to show'}</div>`
    : big(f) && !loaded ? html`<div class="cnote">Large diff: ${f.add + f.del} line${f.add + f.del === 1 ? '' : 's'}, ${Math.round(f.size / 1024)} KB${bulky.test(f.path) ? ', generated' : ''} · <button class="small" onClick=${load}>Load diff</button></div>`
    : html`${thread && loose.length > 0 && html`<div class="cloose"><div class="meta">${loose.length} thread${loose.length > 1 ? 's' : ''} on lines this diff no longer shows</div>${loose.map((t) => html`<div key=${t.ID}>${thread(t)}</div>`)}</div>`}
      <table class=${'cdiff' + (comment ? ' commentable' : '')}>${f.hunks.map((h, i) => html`<tbody key=${i}>
          <tr class="chunk"><td colspan="3">${h.head.replace(/ @@.*/, ' @@')}${h.at && html` <span class="cat">${h.at}</span>`}</td></tr>
          ${h.lines.map((l, j) => row(l, f.path + ':' + i + ':' + j))}
        </tbody>`)}</table>`;
  return html`<section class=${'cfile' + (open ? '' : ' shut')} id=${'cf-' + f.path}>
    <header onClick=${toggle}><span class="caret">${open ? '▾' : '▸'}</span><span class=${'cst s' + f.status}>${f.status}</span>
      <span class="cpath">${f.from ? f.from + ' → ' : ''}${f.path}</span>${talks(threads.length ? threads : null)}${counts(f)}</header>
    ${open && body()}
  </section>`;
}

// LineComment is the box under a line: write, then Post (asking first: it goes out as you).
function LineComment({ draft, comment, close }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const post = async () => {
    if (!text.trim() || !(await ask(`Post this comment on ${draft.path}:${draft.line} on GitHub, as you?`, 'Post'))) return;
    setBusy(true); setErr('');
    try { await comment({ ...draft, body: text }); close(); } catch (e) { setErr(e.message); setBusy(false); }
  };
  return html`<div class="line-comment">
    <textarea rows="3" autofocus placeholder=${'Comment on line ' + draft.line + (draft.side === 'LEFT' ? ' (removed)' : '')} value=${text}
      onInput=${(e) => setText(e.target.value)} onKeyDown=${(e) => { if (e.key === 'Enter' && e.metaKey) post(); if (e.key === 'Escape') close(); }} />
    <div class="btns">${err && html`<span class="err">${err}</span>`}<span class="meta">⌘⏎ posts · esc cancels</span>
      <button onClick=${close}>Cancel</button><button class="primary" disabled=${busy || !text.trim()} onClick=${post}>${busy ? 'Posting…' : 'Post comment'}</button></div>
  </div>`;
}

// DiffView is the whole view for a diff (git's text) and its one-line summary: the tree, then the
// files, all closed at first. Opening one (its header, or the tree) draws just that file; Expand all
// opens the rest but leaves big ones for a click. What's open stays open when the diff reloads.
// threads (a PR's) are drawn in place by thread(t), and counted on the files.
export function DiffView({ diff, summary, reload, comment, threads, thread }) {
  const [at, setAt] = useState('');
  const [shut, setShut] = useState({}); // folded folders in the tree
  const [open, setOpen] = useState({}); // files open on the right
  const [loaded, setLoaded] = useState({}); // big files you chose to see
  const [draft, setDraft] = useState(null); // the line being commented
  const pane = useRef();
  const files = useMemo(() => parseDiff(diff || ''), [diff]);
  const root = useMemo(() => diffTree(files), [files]);
  const talk = useMemo(() => { const m = {}; for (const t of threads || []) (m[t.Path] ||= []).push(t); return m; }, [threads]);
  if (!files.length) return html`<div class="scroll"><div class="changes-head"><b>${summary || 'No changes'}</b>${reload && html`<button class="small" onClick=${reload}>↻ Refresh</button>`}</div></div>`;
  const set = (fn, p, v) => fn((m) => ({ ...m, [p]: v }));
  const go = (p) => {
    setAt(p); set(setOpen, p, true); set(setLoaded, p, true); // picked by name: show it, big or not
    setTimeout(() => { const el = document.getElementById('cf-' + p); el && pane.current && pane.current.scrollTo({ top: el.offsetTop - pane.current.offsetTop, behavior: 'smooth' }); }, 0);
  };
  const all = files.every((f) => open[f.path]);
  const toggleAll = () => setOpen(all ? {} : Object.fromEntries(files.map((f) => [f.path, true])));
  return html`<div class="changes">
    <aside class="ctree">
      <div class="changes-head"><b>${files.length} file${files.length > 1 ? 's' : ''}</b>${reload && html`<button class="small" title="Read the changes again" onClick=${reload}>↻</button>`}</div>
      ${summary && html`<div class="meta csum">${summary}</div>`}
      <${Tree} d=${root} depth=${0} at=${at} go=${go} shut=${shut} flip=${(k) => setShut((s) => ({ ...s, [k]: !s[k] }))} talk=${talk} />
    </aside>
    <div class="cfiles" ref=${pane}>
      <div class="cfiles-head"><span class="meta">${comment ? 'Click a file to read it · + on a line comments on it' : 'Click a file to read it'}</span><button class="small" onClick=${toggleAll}>${all ? 'Collapse all' : 'Expand all'}</button></div>
      ${files.map((f) => html`<${File} key=${f.path} f=${f} open=${!!open[f.path]} loaded=${!!loaded[f.path]} comment=${comment}
        draft=${draft && draft.path === f.path ? draft : null} setDraft=${setDraft} threads=${talk[f.path]} thread=${thread}
        toggle=${() => { setAt(f.path); set(setOpen, f.path, !open[f.path]); }} load=${() => set(setLoaded, f.path, true)} />`)}
    </div>
  </div>`;
}
