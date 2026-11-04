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

const KEEP = new Set(['DIV', 'P', 'BR', 'H1', 'H2', 'H3', 'B', 'STRONG', 'I', 'EM', 'U', 'S', 'STRIKE', 'DEL', 'PRE', 'CODE', 'UL', 'OL', 'LI', 'A', 'BLOCKQUOTE']);
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

// Editor is the open note. It loads it (a Markdown note comes back as HTML at a new path: moved),
// saves 600ms after you stop typing (⌘S at once) and when you leave it, takes Claude's changes when
// you haven't typed since, and throws an empty note away when you leave it, as Apple Notes does.
export function Editor({ path, note, starred, star, moveTo, trash, moved, reloadKey, onEmptyLeave }) {
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
    if (!el || !loaded) return;
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
  const tick = () => { const li = blockAt(ed.current, 'ul.checklist > li'); if (li) { li.classList.toggle('done'); changed(); } };

  const key = (e) => {
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
  const on = (cmd) => { try { return loaded && document.queryCommandState(cmd); } catch { return false; } };
  return html`<main class="page">
    <header class="bar"><div class="bar-title"><h1>${note ? note.title : 'New note'}</h1><span class="meta">${note && note.folder ? note.folder + ' · ' : ''}${words} words</span></div>
      <div class="bar-actions">
        <span class=${'meta nt-state' + (state && state !== 'saving' && state !== 'saved' ? ' err' : '')}>${state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved ✓' : state}</span>
        <button class=${'small' + (starred ? ' nt-starred' : '')} title=${starred ? 'Unstar' : 'Star: keep it on top'} onClick=${star}>${starred ? '★' : '☆'}</button>
        <${Menu} label="⋯" title="More" items=${[{ label: 'Move to folder', run: moveTo }, { label: 'Copy text', run: () => navigator.clipboard.writeText(ed.current ? ed.current.innerText : '') }, { label: 'Delete note', danger: true, run: trash }]} />
      </div></header>
    <div class="nt-tools" onMouseDown=${(e) => e.preventDefault()}>
      <${Menu} label=${'Aa ' + curStyle} title="Paragraph style" items=${STYLES.map((s) => ({ label: s.label, key: s.keys, run: () => style(s) }))} />
      <span class="nt-sep"></span>
      ${MARKS.map((m) => html`<button key=${m.cmd} class=${'nt-mark nt-' + m.cmd + (on(m.cmd) ? ' on' : '')} title=${m.title} onClick=${() => exec(m.cmd)}>${m.label}</button>`)}
      <span class="nt-sep"></span>
      <button class=${curStyle === 'Checklist' ? 'on' : ''} title="Checklist ⇧⌘L · tick ⇧⌘U" onClick=${() => list('checklist')}>☑ Checklist</button>
      <button class=${curStyle === 'Bulleted list' ? 'on' : ''} title="Bulleted list ⇧⌘7" onClick=${() => list('')}>• List</button>
      <button title="Indent ⌘] · outdent ⌘[" onClick=${() => exec('indent')}>⇥</button>
    </div>
    ${state && !loaded && html`<div class="empty">${state}</div>`}
    <div class="nt-scroll"><div ref=${ed} class="nt-doc" contenteditable="true" spellcheck="true"
      onInput=${changed} onKeyDown=${key} onMouseDown=${down} onClick=${click} onPaste=${paste} onKeyUp=${() => bump((n) => n + 1)} onMouseUp=${() => bump((n) => n + 1)}></div></div>
  </main>`;
}
