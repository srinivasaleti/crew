// The note editor, like Apple Notes': you write normally, the first line is the title, and styles,
// lists and checklists come from the toolbar or their shortcuts. A note is HTML; what's loaded or
// pasted is cleaned to the few tags the editor makes (clean), so nothing in a note can run.
import { html, useState, useEffect, useRef, api, openLink, Menu } from '../../lib.js';

// STYLES are the paragraph styles, with Apple Notes' shortcuts (⇧⌘ and a key; code is the key's
// KeyboardEvent.code, so it works with any layout).
const STYLES = [
  { key: 'title', label: 'Title', keys: '⇧⌘T', code: 'KeyT', block: 'h1' },
  { key: 'heading', label: 'Heading', keys: '⇧⌘H', code: 'KeyH', block: 'h2' },
  { key: 'subheading', label: 'Subheading', keys: '⇧⌘J', code: 'KeyJ', block: 'h3' },
  { key: 'body', label: 'Body', keys: '⇧⌘B', code: 'KeyB', block: 'div' },
  { key: 'mono', label: 'Monostyled', keys: '⇧⌘M', code: 'KeyM', block: 'pre' },
  { key: 'bullets', label: '• Bulleted list', keys: '⇧⌘7', code: 'Digit7', list: '' },
  { key: 'dashes', label: '– Dashed list', keys: '⇧⌘8', code: 'Digit8', list: 'dashed' },
  { key: 'numbers', label: '1. Numbered list', keys: '⇧⌘9', code: 'Digit9', list: 'ol' },
];
// MARKS are the text styles: ⌘ and a key (strikethrough is ⇧⌘X).
const MARKS = [
  { cmd: 'bold', label: 'B', title: 'Bold ⌘B', key: 'b' },
  { cmd: 'italic', label: 'I', title: 'Italic ⌘I', key: 'i' },
  { cmd: 'underline', label: 'U', title: 'Underline ⌘U', key: 'u' },
  { cmd: 'strikeThrough', label: 'S', title: 'Strikethrough ⇧⌘X', key: 'x', shift: true },
];

const KEEP = new Set(['DIV', 'P', 'BR', 'H1', 'H2', 'H3', 'B', 'STRONG', 'I', 'EM', 'U', 'S', 'STRIKE', 'DEL', 'PRE', 'CODE', 'UL', 'OL', 'LI', 'A', 'BLOCKQUOTE', 'TABLE', 'THEAD', 'TBODY', 'TR', 'TD', 'TH']);
const DROP = /^(SCRIPT|STYLE|IFRAME|OBJECT|EMBED|IMG|SVG|MATH|VIDEO|AUDIO|CANVAS|FORM|INPUT|BUTTON|TEXTAREA|SELECT|LINK|META|HEAD|TITLE|TEMPLATE|NOSCRIPT)$/;
const CLASSES = { UL: ['checklist', 'dashed'], LI: ['done'] };

// clean keeps only the editor's own tags and classes, and http(s)/mailto links; other tags give up
// their text, and scripts, styles, images, frames and event handlers are dropped. Parsed in an inert
// document, so nothing loads or runs while it's cleaned.
export function clean(dirty) {
  const doc = new DOMParser().parseFromString('<body>' + dirty + '</body>', 'text/html');
  const walk = (el) => {
    for (const n of [...el.childNodes]) {
      if (n.nodeType === 3) continue;
      if (n.nodeType !== 1 || DROP.test(n.tagName)) { n.remove(); continue; }
      walk(n);
      if (!KEEP.has(n.tagName)) { n.replaceWith(...n.childNodes); continue; }
      const cls = (CLASSES[n.tagName] || []).filter((c) => n.classList.contains(c));
      const href = n.tagName === 'A' && /^(https?:|mailto:)/i.test(n.getAttribute('href') || '') ? n.getAttribute('href') : null;
      for (const a of [...n.attributes]) n.removeAttribute(a.name);
      if (cls.length) n.className = cls.join(' ');
      if (href) n.setAttribute('href', href);
    }
  };
  walk(doc.body);
  return doc.body.innerHTML;
}

// blockAt is the element of the editor the caret is in that matches sel (closest), or null.
function blockAt(ed, sel) {
  const s = getSelection();
  if (!s.rangeCount) return null;
  let n = s.anchorNode;
  if (n && n.nodeType === 3) n = n.parentNode;
  const b = n && n.closest && n.closest(sel);
  return b && ed.contains(b) ? b : null;
}

// gone are the notes you deleted: the editor leaving one mustn't save it back.
export const gone = new Set();

// Editor is the open note. It loads it (a Markdown note comes back as HTML at a new path: moved),
// saves 600ms after you stop typing (⌘S at once) and when you leave it, takes Claude's changes when
// you haven't typed since, and throws an empty note away when you leave it, as Apple Notes does.
export function Editor({ path, note, starred, star, moveTo, trash, moved, reloadKey, onEmptyLeave, create }) {
  const ed = useRef();
  const [loaded, setLoaded] = useState(false);
  const [state, setState] = useState(''); // '', 'saving', 'saved', or an error
  const [words, setWords] = useState(0);
  const [, bump] = useState(0); // the toolbar shows the caret's styles
  const dirty = useRef(false), timer = useRef();
  const count = () => setWords((ed.current.innerText.match(/\S+/g) || []).length);
  const load = () => api('GET', '/notes/note?path=' + encodeURIComponent(path)).then((r) => {
    if (r.path !== path) return moved(r.path);
    if (dirty.current || !ed.current) return;
    ed.current.innerHTML = clean(r.text) || '<h1><br></h1>'; // a new note starts on its title
    setLoaded(true); count();
  }, (e) => setState(e.message));
  useEffect(() => { load(); }, [path]);
  useEffect(() => { if (reloadKey) load(); }, [reloadKey]);
  useEffect(() => { if (loaded && ed.current.innerText.trim() === '') { ed.current.focus(); } }, [loaded]);
  const save = async () => {
    clearTimeout(timer.current);
    setState('saving');
    if (gone.has(path) || !ed.current) return;
    try { await api('PUT', '/notes/note', { Path: path, Text: ed.current.innerHTML }); dirty.current = false; setState('saved'); } catch (e) { setState(e.message); }
  };
  const changed = () => {
    dirty.current = true; setState(''); count();
    clearTimeout(timer.current);
    timer.current = setTimeout(save, 600);
  };
  useEffect(() => () => { // leaving: keep what you typed, or drop the note if it's empty
    clearTimeout(timer.current);
    const el = ed.current;
    if (!el || !loaded || gone.has(path)) return;
    if (el.innerText.trim() === '') onEmptyLeave(path);
    else if (dirty.current) api('PUT', '/notes/note', { Path: path, Text: el.innerHTML });
  }, [path, loaded]);

  const exec = (cmd, arg) => { ed.current.focus(); document.execCommand(cmd, false, arg); changed(); bump((n) => n + 1); };
  // list turns the caret's lines into a list of kind ('' bullets, 'dashed', 'checklist', 'ol'),
  // into another kind of list, or back into lines when they already are that kind.
  const list = (kind) => {
    ed.current.focus();
    const tag = kind === 'ol' ? 'OL' : 'UL';
    const cur = blockAt(ed.current, 'ul,ol');
    const same = cur && cur.tagName === tag && (cur.className || '') === (kind === 'ol' ? '' : kind);
    if (!cur || cur.tagName !== tag || same) document.execCommand(tag === 'OL' ? 'insertOrderedList' : 'insertUnorderedList');
    const now = !same && blockAt(ed.current, 'ul,ol');
    if (now) now.className = kind === 'ol' ? '' : kind;
    changed(); bump((n) => n + 1);
  };
  const style = (s) => {
    if (s.list !== undefined) return list(s.list);
    const cur = blockAt(ed.current, 'ul,ol');
    if (cur) document.execCommand(cur.tagName === 'OL' ? 'insertOrderedList' : 'insertUnorderedList'); // a style takes a line out of its list
    exec('formatBlock', s.block);
  };
  // table puts a 3×2 table at the caret, the caret in its first cell.
  const table = () => {
    ed.current.focus();
    const row = '<tr>' + '<td><br></td>'.repeat(3) + '</tr>';
    document.execCommand('insertHTML', false, '<table><tbody>' + row + row + '</tbody></table><div><br></div>');
    const after = blockAt(ed.current, 'div'), t = after && after.previousElementSibling; // the caret lands on the line after it
    if (t && t.tagName === 'TABLE') { const r = document.createRange(); r.setStart(t.querySelector('td'), 0); r.collapse(true); getSelection().removeAllRanges(); getSelection().addRange(r); }
    changed(); bump((n) => n + 1);
  };
  // cell changes the table the caret is in: 'row' or 'col' adds one after the caret's, 'delrow',
  // 'delcol' and 'deltable' take them out.
  const cell = (op) => {
    const td = blockAt(ed.current, 'td,th');
    if (!td) return;
    const tr = td.parentNode, tbl = td.closest('table'), i = [...tr.children].indexOf(td);
    const blank = () => Object.assign(document.createElement('td'), { innerHTML: '<br>' });
    if (op === 'row') { const n = document.createElement('tr'); for (let j = 0; j < tr.children.length; j++) n.append(blank()); tr.after(n); }
    if (op === 'col') for (const r of tbl.rows) r.children[i] ? r.children[i].after(blank()) : r.append(blank());
    if (op === 'delrow') tr.remove();
    if (op === 'delcol') for (const r of [...tbl.rows]) r.children[i] && r.children[i].remove();
    if (op === 'deltable' || !tbl.querySelector('td,th')) tbl.remove();
    changed(); bump((n) => n + 1);
  };
  // moveCell puts the caret in the next (or previous) cell; tab past the last adds a row.
  const moveCell = (td, back) => {
    const cells = [...td.closest('table').querySelectorAll('td,th')];
    let to = cells[cells.indexOf(td) + (back ? -1 : 1)];
    if (!to && !back) { cell('row'); to = td.closest('table').rows[td.closest('table').rows.length - 1].children[0]; }
    if (!to) return;
    const r = document.createRange();
    r.selectNodeContents(to); r.collapse(false);
    getSelection().removeAllRanges(); getSelection().addRange(r);
  };
  const tick = () => { const li = blockAt(ed.current, 'ul.checklist > li'); if (li) { li.classList.toggle('done'); changed(); } };

  const key = (e) => {
    if (e.key === 'Tab' && !e.metaKey) { // tab moves between cells, indents a list item, or is a tab
      e.preventDefault();
      const td = blockAt(ed.current, 'td,th');
      if (td) return moveCell(td, e.shiftKey);
      if (blockAt(ed.current, 'li')) return exec(e.shiftKey ? 'outdent' : 'indent');
      return exec('insertText', '\t');
    }
    if (!e.metaKey) {
      // Enter on a ticked item: the new item starts unticked.
      if (e.key === 'Enter' && blockAt(ed.current, 'ul.checklist > li')) setTimeout(() => { const li = blockAt(ed.current, 'li'); li && li.classList.remove('done'); });
      return;
    }
    const k = e.key.toLowerCase();
    if (k === 's' && !e.shiftKey) { e.preventDefault(); return save(); }
    if (e.shiftKey) {
      const s = STYLES.find((x) => x.code === e.code);
      if (s) { e.preventDefault(); return style(s); }
      if (e.code === 'KeyL') { e.preventDefault(); return list('checklist'); }
      if (e.code === 'KeyU') { e.preventDefault(); return tick(); }
    }
    const m = MARKS.find((x) => x.key === k && !!x.shift === e.shiftKey);
    if (m) { e.preventDefault(); return exec(m.cmd); }
    if (e.key === ']') { e.preventDefault(); return exec('indent'); }
    if (e.key === '[') { e.preventDefault(); return exec('outdent'); }
  };
  // A click in a checklist item's box (its left edge) ticks it; ⌘-click opens a link.
  const down = (e) => {
    const li = e.target.closest && e.target.closest('ul.checklist > li');
    if (li && e.clientX - li.getBoundingClientRect().left < 24) { e.preventDefault(); li.classList.toggle('done'); changed(); }
  };
  const click = (e) => {
    const a = e.target.closest && e.target.closest('a[href]');
    if (a && e.metaKey) { e.preventDefault(); openLink(a.getAttribute('href')); }
  };
  const paste = (e) => {
    e.preventDefault();
    const h = e.clipboardData.getData('text/html');
    if (h) document.execCommand('insertHTML', false, clean(h));
    else document.execCommand('insertText', false, e.clipboardData.getData('text/plain'));
    changed();
  };

  const inBlock = loaded && ed.current && blockAt(ed.current, 'h1,h2,h3,pre,ul,ol');
  const curStyle = !inBlock ? 'Body' : inBlock.tagName === 'UL' ? (inBlock.className === 'checklist' ? 'Checklist' : inBlock.className === 'dashed' ? 'Dashed list' : 'Bulleted list')
    : ({ H1: 'Title', H2: 'Heading', H3: 'Subheading', PRE: 'Monostyled', OL: 'Numbered list' })[inBlock.tagName];
  const inTable = loaded && ed.current && !!blockAt(ed.current, 'td,th');
  const on = (cmd) => { try { return loaded && document.queryCommandState(cmd); } catch { return false; } };
  return html`<main class="page">
    <header class="bar"><div class="bar-title"><h1>${note ? note.title : 'New note'}</h1><span class="meta">${note && note.folder ? note.folder + ' · ' : ''}${words} words</span></div>
      <div class="bar-actions">
        <span class=${'meta nt-state' + (state && state !== 'saving' && state !== 'saved' ? ' err' : '')}>${state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved ✓' : state}</span>
        <button class=${'small' + (starred ? ' nt-starred' : '')} title=${starred ? 'Unstar' : 'Star: keep it on top'} onClick=${star}>${starred ? '★' : '☆'}</button>
        <${Menu} label="⋯" title="More" items=${[{ label: 'Move to folder', run: moveTo }, { label: 'Copy text', run: () => navigator.clipboard.writeText(ed.current ? ed.current.innerText : '') }]} />
      </div></header>
    <div class="nt-tools" onMouseDown=${(e) => e.target.closest('button') && e.preventDefault()}>
      <button class="nt-icon" title="New note (⌘N)" onClick=${create}>${ICONS.compose}</button>
      <${Format} cur=${curStyle} style=${style} exec=${exec} on=${on} list=${list} />
      <button class=${'nt-icon' + (curStyle === 'Checklist' ? ' on' : '')} title="Checklist (⇧⌘L) · tick an item (⇧⌘U)" onClick=${() => list('checklist')}>${ICONS.checklist}</button>
      <button class=${'nt-icon' + (inTable ? ' on' : '')} title="Table" onClick=${table}>${ICONS.table}</button>
      <button class="nt-icon nt-trash" title="Delete this note (⌘⌫) · Undo brings it back" onClick=${trash}>${ICONS.trash}</button>
      ${inTable && html`<span class="nt-sep"></span><span class="nt-table-acts">
        <button title="Add a row below (tab in the last cell does too)" onClick=${() => cell('row')}>+ Row</button>
        <button title="Add a column to the right" onClick=${() => cell('col')}>+ Column</button>
        <button title="Delete this row" onClick=${() => cell('delrow')}>− Row</button>
        <button title="Delete this column" onClick=${() => cell('delcol')}>− Column</button>
        <button class="danger" title="Delete the table" onClick=${() => cell('deltable')}>Delete table</button></span>`}
    </div>
    ${state && !loaded && html`<div class="empty">${state}</div>`}
    <div class="nt-scroll"><div ref=${ed} class="nt-doc" contenteditable="true" spellcheck="true"
      onInput=${changed} onKeyDown=${key} onMouseDown=${down} onClick=${click} onPaste=${paste} onKeyUp=${() => bump((n) => n + 1)} onMouseUp=${() => bump((n) => n + 1)}></div></div>
  </main>`;
}

// ICONS are the toolbar's, drawn like Apple Notes' (stroke icons in the text colour).
const svg = (d) => html`<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML=${{ __html: d }}></svg>`;
const ICONS = {
  compose: svg('<path d="M12 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-6"/><path d="M18.4 2.6a2 2 0 0 1 2.9 2.9L12 14.8 8.5 15.5l.7-3.5z"/>'),
  checklist: svg('<circle cx="5.5" cy="7" r="2.5"/><path d="m4.3 7 .9.9 1.6-1.8"/><circle cx="5.5" cy="17" r="2.5"/><path d="M11 7h10M11 17h10"/>'),
  trash: svg('<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>'),
  table: svg('<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9.5h18M3 15h18M9 4v16M15 4v16"/>'),
};

// Format is the Aa button: a panel with the paragraph styles, the text styles and the lists, each
// with its shortcut, as Apple Notes' Aa shows them.
function Format({ cur, style, exec, on, list }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return;
    const close = (e) => { if (!e.target.closest('.nt-format')) setOpen(false); };
    const esc = (e) => e.key === 'Escape' && setOpen(false);
    addEventListener('mousedown', close); addEventListener('keydown', esc);
    return () => { removeEventListener('mousedown', close); removeEventListener('keydown', esc); };
  }, [open]);
  const pick = (f) => { f(); setOpen(false); };
  return html`<span class="nt-format">
    <button class=${'nt-icon nt-aa' + (open ? ' on' : '')} title="Format: styles, bold, lists" onClick=${() => setOpen(!open)}>Aa</button>
    ${open && html`<div class="nt-pop">
      ${STYLES.filter((s) => s.block).map((s) => html`<button key=${s.key} class=${'nt-style nt-s-' + s.key + (cur === s.label ? ' on' : '')} onClick=${() => pick(() => style(s))}>
        <span>${s.label}</span><span class="meta">${s.keys}</span></button>`)}
      <div class="nt-pop-row">${MARKS.map((m) => html`<button key=${m.cmd} class=${'nt-mark nt-' + m.cmd + (on(m.cmd) ? ' on' : '')} title=${m.title} onClick=${() => exec(m.cmd)}>${m.label}</button>`)}</div>
      <div class="nt-pop-row">
        <button class=${cur === 'Bulleted list' ? 'on' : ''} title="Bulleted list ⇧⌘7" onClick=${() => pick(() => list(''))}>•</button>
        <button class=${cur === 'Dashed list' ? 'on' : ''} title="Dashed list ⇧⌘8" onClick=${() => pick(() => list('dashed'))}>–</button>
        <button class=${cur === 'Numbered list' ? 'on' : ''} title="Numbered list ⇧⌘9" onClick=${() => pick(() => list('ol'))}>1.</button>
        <button title="Outdent ⌘[" onClick=${() => exec('outdent')}>⇤</button>
        <button title="Indent ⌘]" onClick=${() => exec('indent')}>⇥</button>
      </div>
    </div>`}
  </span>`;
}
