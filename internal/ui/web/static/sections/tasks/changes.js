// The Changes tab, read like a pull request: the changed files as a folder tree on the left, and each
// file's diff on the right with old and new line numbers. Clicking a file in the tree goes to it.
import { html, useState, useRef, api, useEvents, useLoad } from '../../lib.js';

// parse turns git's unified diff into files: path, status (A M D R), counts, and hunks of lines.
export function parse(diff) {
  return diff.split(/^(?=diff --git )/m).filter((c) => c.startsWith('diff --git ')).map((c) => {
    const head = c.slice(0, c.search(/^@@/m) >= 0 ? c.search(/^@@/m) : c.length);
    const to = (head.match(/^\+\+\+ b\/(.*)$/m) || [])[1], from = (head.match(/^--- a\/(.*)$/m) || [])[1];
    const git = head.match(/^diff --git a\/(.*?) b\/(.*)$/m) || [];
    const renamed = (head.match(/^rename to (.*)$/m) || [])[1];
    const path = renamed || to || from || git[2] || 'file';
    const status = /^new file mode/m.test(head) || /^--- \/dev\/null/m.test(head) ? 'A'
      : /^deleted file mode/m.test(head) || /^\+\+\+ \/dev\/null/m.test(head) ? 'D' : renamed ? 'R' : 'M';
    const file = { path, status, from: renamed ? (head.match(/^rename from (.*)$/m) || [])[1] : '', binary: /^Binary files/m.test(head), add: 0, del: 0, hunks: [] };
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

// tree nests files into folders, joining a folder that only holds one folder (a/b/c), as GitHub does.
export function tree(files) {
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

// Tree draws a folder's folders (foldable) then its files.
function Tree({ d, depth, at, go, shut, flip }) {
  return html`${Object.values(d.dirs).sort((a, b) => a.name.localeCompare(b.name)).map((c) => {
    const key = depth + ':' + c.name;
    return html`<div key=${key}>
      <div class="cnode cn-dir" style=${{ paddingLeft: 6 + depth * 12 + 'px' }} onClick=${() => flip(key)}><span class="caret">${shut[key] ? '▸' : '▾'}</span> ${c.name}</div>
      ${!shut[key] && html`<${Tree} d=${c} depth=${depth + 1} at=${at} go=${go} shut=${shut} flip=${flip} />`}</div>`;
  })}${d.files.map((f) => html`<div key=${f.path} class=${'cnode cn-file' + (at === f.path ? ' on' : '')} style=${{ paddingLeft: 18 + depth * 12 + 'px' }} title=${f.path} onClick=${() => go(f.path)}>
      <span class=${'cst s' + f.status}>${f.status}</span><span class="cname">${f.path.split('/').pop()}</span>${counts(f)}</div>`)}`;
}

// File is one file's diff: a sticky header, then its hunks as rows of old line, new line, code.
function File({ f, open, toggle }) {
  return html`<section class="cfile" id=${'cf-' + f.path}>
    <header onClick=${toggle}><span class="caret">${open ? '▾' : '▸'}</span><span class=${'cst s' + f.status}>${f.status}</span>
      <span class="cpath">${f.from ? f.from + ' → ' : ''}${f.path}</span>${counts(f)}</header>
    ${open && (f.binary ? html`<div class="cnote">Binary file</div>` : !f.hunks.length ? html`<div class="cnote">${f.status === 'R' ? 'Renamed, no changes' : 'No changes to show'}</div>`
      : html`<table class="cdiff">${f.hunks.map((h, i) => html`<tbody key=${i}>
          <tr class="chunk"><td colspan="3">${h.head.replace(/ @@.*/, ' @@')}${h.at && html` <span class="cat">${h.at}</span>`}</td></tr>
          ${h.lines.map((l, j) => html`<tr key=${j} class=${'c' + l.t}><td class="ln">${l.o ?? ''}</td><td class="ln">${l.n ?? ''}</td><td class="code"><span class="sign">${l.t === 'add' ? '+' : l.t === 'del' ? '−' : ' '}</span>${l.text}</td></tr>`)}
        </tbody>`)}</table>`)}
  </section>`;
}

// Changes is the tab: the summary and Refresh, the tree, and the files.
// ponytail: draws every file at once; a page per file if agents ever change thousands of lines.
export function Changes({ id }) {
  const { data, err, reload } = useLoad(() => api('GET', `/tasks/${id}/changes`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  const [at, setAt] = useState('');
  const [shut, setShut] = useState({}); // folded folders in the tree
  const [closed, setClosed] = useState({}); // folded files on the right
  const pane = useRef();
  if (err) return html`<div class="scroll"><div class="err">${err}</div></div>`;
  if (!data) return html`<div class="scroll"></div>`;
  const files = parse(data.diff);
  if (!files.length) return html`<div class="scroll"><div class="changes-head"><b>${data.summary}</b><button class="small" onClick=${reload}>↻ Refresh</button></div></div>`;
  const go = (p) => { setAt(p); setClosed((c) => ({ ...c, [p]: false })); setTimeout(() => { const el = document.getElementById('cf-' + p); el && pane.current && pane.current.scrollTo({ top: el.offsetTop - pane.current.offsetTop, behavior: 'smooth' }); }, 0); };
  return html`<div class="changes">
    <aside class="ctree">
      <div class="changes-head"><b>${files.length} file${files.length > 1 ? 's' : ''}</b><button class="small" title="Read the changes again" onClick=${reload}>↻</button></div>
      <div class="meta csum">${data.summary}</div>
      <${Tree} d=${tree(files)} depth=${0} at=${at} go=${go} shut=${shut} flip=${(k) => setShut((s) => ({ ...s, [k]: !s[k] }))} />
    </aside>
    <div class="cfiles" ref=${pane}>${files.map((f) => html`<${File} key=${f.path} f=${f} open=${!closed[f.path]} toggle=${() => setClosed((c) => ({ ...c, [f.path]: !c[f.path] }))} />`)}</div>
  </div>`;
}
