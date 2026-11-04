// NOTES: Markdown notes in ~/.crew/notes. The list on the left (search, starred on top, then by
// folder), the open note in an editor that saves as you type (Write, Split or Preview), and an Ask
// Claude panel: a chat locked down to the notes folder. Files and rules are crew's (internal/notes).
import { html, useState, useEffect, useRef, api, ago, useEvents, useLoad, Chat, Form, Empty, Markdown, Search, Menu, ask, css } from '../../lib.js';

css(import.meta.url);

const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);
const MODES = [['write', 'Write'], ['split', 'Split'], ['preview', 'Preview']];
let mode = 'split'; // kept while you move between notes and sections

// Editor is the open note: it loads it, saves 600ms after you stop typing (⌘S at once), and takes
// Claude's changes when you haven't typed since.
function Editor({ path, note, starred, star, rename, trash, reloadKey }) {
  const [text, setText] = useState(null);
  const [state, setState] = useState(''); // '', 'saving', 'saved', or an error
  const [view, setView] = useState(mode);
  const dirty = useRef(false), timer = useRef(), latest = useRef(null); // latest: the text as typed, for the save on leaving
  const load = () => api('GET', '/notes/note?path=' + encodeURIComponent(path)).then((r) => { if (!dirty.current) setText(r.text); }, (e) => setState(e.message));
  useEffect(() => { dirty.current = false; setText(null); setState(''); load(); }, [path]);
  useEffect(() => { if (reloadKey) load(); }, [reloadKey]);
  const save = async (t) => {
    clearTimeout(timer.current);
    setState('saving');
    try { await api('PUT', '/notes/note', { Path: path, Text: t }); dirty.current = false; setState('saved'); } catch (e) { setState(e.message); }
  };
  const edit = (e) => {
    const t = e.target.value;
    setText(t); latest.current = t; dirty.current = true; setState('');
    clearTimeout(timer.current);
    timer.current = setTimeout(() => save(t), 600);
  };
  useEffect(() => () => { if (dirty.current) api('PUT', '/notes/note', { Path: path, Text: latest.current }); clearTimeout(timer.current); }, [path]); // leaving a note keeps what you typed
  const key = (e) => {
    if (e.metaKey && e.key === 's') { e.preventDefault(); save(e.target.value); }
    if (e.key === 'Tab') { // tab indents instead of leaving the box
      e.preventDefault();
      const a = e.target, s = a.selectionStart;
      a.setRangeText('  ', s, a.selectionEnd, 'end');
      edit({ target: a });
    }
  };
  const pick = (m) => { mode = m; setView(m); };
  const words = text ? (text.match(/\S+/g) || []).length : 0;
  return html`<main class="page">
    <header class="bar"><div class="bar-title"><h1>${note ? note.title : path}</h1><span class="meta">${note && note.folder ? note.folder + ' · ' : ''}${words} words</span></div>
      <div class="bar-actions">
        <span class=${'meta nt-state' + (state && state !== 'saving' && state !== 'saved' ? ' err' : '')}>${state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved ✓' : state}</span>
        <div class="nt-seg">${MODES.map(([k, l]) => html`<button key=${k} class=${view === k ? 'on' : ''} onClick=${() => pick(k)}>${l}</button>`)}</div>
        <button class=${'small' + (starred ? ' nt-starred' : '')} title=${starred ? 'Unstar' : 'Star: keep it on top'} onClick=${star}>${starred ? '★' : '☆'}</button>
        <${Menu} label="⋯" title="More" items=${[{ label: 'Rename or move', run: rename }, { label: 'Copy text', run: () => navigator.clipboard.writeText(text || '') }, { label: 'Move to trash', danger: true, run: trash }]} />
      </div></header>
    ${text === null ? html`<div class="empty">${state || 'Loading…'}</div>` : html`<div class=${'nt-body ' + view}>
      ${view !== 'preview' && html`<textarea class="nt-text" spellcheck="true" autofocus value=${text} onInput=${edit} onKeyDown=${key}
        placeholder="Write in Markdown: # heading, - list, - [ ] checkbox, **bold**, \`code\`"></textarea>`}
      ${view !== 'write' && html`<div class="nt-preview" onDblClick=${() => view === 'preview' && pick('split')}><${Markdown} text=${text} /></div>`}
    </div>`}
  </main>`;
}

export default function Notes() {
  const [q, setQ] = useState('');
  const { data, reload } = useLoad(() => api('GET', '/notes' + (q ? '?q=' + encodeURIComponent(q) : '')), [q]);
  const [path, setPath] = useState(decodeURIComponent(location.hash.split('/').slice(1).join('/') || ''));
  const [form, setForm] = useState(null); // 'new' or {move: note}
  const [chat, setChat] = useState(false);
  const [chatID, setChatID] = useState('');
  const [undo, setUndo] = useState('');
  const [folds, setFolds] = useState({});
  const [reloadKey, setReloadKey] = useState(0);
  useEvents((ev) => {
    if (ev.kind === 'notes' || ev.kind === 'open') reload();
    if (ev.kind === 'turn' && ev.id === chatID && !ev.busy) { reload(); setReloadKey((n) => n + 1); } // Claude may have changed notes
  }, [chatID, q]);
  useEffect(() => { if (chat && !chatID) api('GET', '/notes/chat').then((r) => setChatID(r.id), (e) => ask(e.message)); }, [chat]);
  const open = (p) => { setPath(p); location.hash = 'notes/' + p; };

  const notes = (data && data.notes) || [], starred = (data && data.starred) || [];
  const note = notes.find((n) => n.path === path);
  const groups = [];
  const star = notes.filter((n) => starred.includes(n.path));
  if (star.length) groups.push(['★ Starred', star]);
  const byFolder = {};
  for (const n of notes) if (!starred.includes(n.path)) (byFolder[n.folder] ||= []).push(n);
  for (const f of ['', ...((data && data.folders) || [])]) if (byFolder[f]) groups.push([f || 'Notes', byFolder[f]]);
  const order = groups.flatMap(([g, l]) => (folds[g] ? [] : l.map((n) => n.path)));

  const trash = async (p) => { await api('POST', '/notes/trash', { Path: p }); setUndo(p); if (p === path) open(''); reload(); };
  useEffect(() => {
    const k = (e) => {
      if (e.metaKey && e.key === 'j') { e.preventDefault(); setChat((c) => !c); return; }
      if (typing() || e.metaKey || form) return;
      const i = order.indexOf(path);
      if (e.key === 'n') { e.preventDefault(); setForm('new'); }
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); order.length && open(order[Math.min(i + 1, order.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); order.length && open(order[Math.max(i - 1, 0)]); }
      if (e.key === 'Enter' && path) { e.preventDefault(); document.querySelector('.nt-text') && document.querySelector('.nt-text').focus(); }
      if ((e.key === 'Backspace' || e.key === 'Delete') && path) { e.preventDefault(); trash(path); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });

  return html`<div class="split">
    <section class="col nt-col">
      <div class="col-head"><b>Notes</b><div class="head-btns">
        <button class=${'small' + (chat ? ' on' : '')} title="Ask Claude about your notes (⌘J)" onClick=${() => setChat(!chat)}>✦ Ask Claude</button>
        <button class="primary small" title="New note (n)" onClick=${() => setForm('new')}>+ New</button></div></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search notes" /></div>
      <div class="col-list">
        ${undo && html`<div class="nt-undo">Moved to trash <button class="small" onClick=${async () => { await api('POST', '/notes/restore', { Path: undo }); open(undo); setUndo(''); reload(); }}>Undo</button></div>`}
        ${!data ? html`<div class="col-empty">Loading…</div>` : !notes.length ? html`<div class="col-empty">${q ? 'No note has “' + q + '”.' : 'No notes yet. Press n to write one.'}</div>`
          : groups.map(([g, l]) => html`<div key=${g}>
            <button class="group fold" onClick=${() => setFolds({ ...folds, [g]: !folds[g] })}><span class="caret">${folds[g] ? '▸' : '▾'}</span> ${g} <span class="n">${l.length}</span></button>
            ${!folds[g] && l.map((n) => html`<div key=${n.path} class=${'item nt-item' + (n.path === path ? ' on' : '')} onClick=${() => open(n.path)}>
              <div class="item-main"><div class="item-title">${n.title}</div><div class="item-sub">${ago(n.mod) === 'now' ? 'just now' : ago(n.mod) + ' ago'}${n.snippet ? ' · ' + n.snippet : ''}</div></div></div>`)}</div>`)}
      </div>
      <div class="col-foot meta" title=${data ? data.dir : ''}>Markdown files in ~/.crew/notes</div>
    </section>
    ${path ? html`<${Editor} key=${path} path=${path} note=${note} reloadKey=${reloadKey} starred=${starred.includes(path)}
        star=${() => api('POST', '/notes/star', { Path: path, On: !starred.includes(path) }).then(reload)}
        rename=${() => setForm({ move: note || { path, folder: '' } })} trash=${() => trash(path)} />`
      : html`<main class="page"><${Empty} title=${notes.length ? 'Pick a note' : 'Write your first note'}>
          <p>Notes are Markdown files you can also open in any editor. Claude can search, summarise and tidy them for you.</p>
          <button class="primary" onClick=${() => setForm('new')}>New note (n)</button><//></main>`}
    ${chat && html`<aside class="nt-chat">
      <header><b>Ask Claude</b><span class="meta">reads and writes only your notes</span><button class="ghost" title="Close (⌘J)" onClick=${() => setChat(false)}>✕</button></header>
      ${chatID ? html`<${Chat} id=${chatID} base=${'/tasks/' + chatID} agent="Claude" placeholder="Ask about your notes"
          empty="“Summarise this week's notes”, “Turn my stand-up notes into a list of action items”, “Find where I wrote about the parser”." />`
        : html`<div class="empty">Opening the chat…</div>`}</aside>`}
    ${form === 'new' && html`<${Form} title="New note" action="Create" onClose=${() => setForm(null)} initial=${{ Folder: note ? note.folder : '' }}
      fields=${[{ key: 'Title', label: 'Title', placeholder: 'Stand-up notes' }, { key: 'Folder', label: 'Folder', type: 'combo', options: (data && data.folders) || [], placeholder: 'none', hint: 'optional: pick one or type a new one' }]}
      submit=${async (v) => { const r = await api('POST', '/notes', v); await reload(); open(r.path); mode = mode === 'preview' ? 'split' : mode; }} />`}
    ${form && form.move && html`<${Form} title="Rename or move" onClose=${() => setForm(null)}
      initial=${{ Name: form.move.path.split('/').pop().replace(/\.md$/, ''), Folder: form.move.folder }}
      fields=${[{ key: 'Name', label: 'File name', hint: '.md is added' }, { key: 'Folder', label: 'Folder', type: 'combo', options: (data && data.folders) || [], placeholder: 'none' }]}
      submit=${async (v) => {
        const to = (v.Folder ? v.Folder.replace(/\/+$/, '') + '/' : '') + v.Name.trim().replace(/\.md$/, '') + '.md';
        if (to !== form.move.path) { await api('POST', '/notes/move', { Path: form.move.path, To: to }); await reload(); open(to); }
      }} />`}
  </div>`;
}
