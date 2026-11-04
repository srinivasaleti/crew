// NOTES: notes like Apple Notes', in ~/.crew/notes. The list on the left (search, starred on top,
// then by folder), the open note in an editor where you write normally with styles and checklists
// (editor.js), and an Ask Claude panel: a chat locked down to the notes folder. Files and rules are
// crew's (internal/notes).
import { html, useState, useEffect, api, ago, useEvents, useLoad, Chat, Form, Empty, Search, ask, typing, css } from '../../lib.js';
import { Editor } from './editor.js';

css(import.meta.url);

export default function Notes() {
  const [q, setQ] = useState('');
  const { data, reload } = useLoad(() => api('GET', '/notes' + (q ? '?q=' + encodeURIComponent(q) : '')), [q]);
  const [path, setPath] = useState(decodeURIComponent(location.hash.split('/').slice(1).join('/') || ''));
  const [form, setForm] = useState(null); // {move: note}
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
  // create starts a new note in the open note's folder and puts you on its title.
  const create = async () => { const r = await api('POST', '/notes', { Folder: note ? note.folder : '' }); await reload(); open(r.path); };
  // dropEmpty throws away a note you left empty (no undo: there's nothing in it).
  const dropEmpty = (p) => api('POST', '/notes/trash', { Path: p }).then(reload, () => {});
  useEffect(() => {
    const k = (e) => {
      if (e.metaKey && !e.shiftKey && e.key === 'j') { e.preventDefault(); setChat((c) => !c); return; }
      if (e.metaKey && !e.shiftKey && e.key === 'n') { e.preventDefault(); create(); return; }
      if (typing() || e.metaKey || form) return;
      const i = order.indexOf(path);
      if (e.key === 'n') { e.preventDefault(); create(); }
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); order.length && open(order[Math.min(i + 1, order.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); order.length && open(order[Math.max(i - 1, 0)]); }
      if (e.key === 'Enter' && path) { e.preventDefault(); document.querySelector('.nt-doc') && document.querySelector('.nt-doc').focus(); }
      if ((e.key === 'Backspace' || e.key === 'Delete') && path) { e.preventDefault(); trash(path); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });

  return html`<div class="split">
    <section class="col nt-col">
      <div class="col-head"><b>Notes</b><div class="head-btns">
        <button class=${'small' + (chat ? ' on' : '')} title="Ask Claude about your notes (⌘J)" onClick=${() => setChat(!chat)}>✦ Ask Claude</button>
        <button class="primary small" title="New note (⌘N)" onClick=${create}>+ New</button></div></div>
      <div class="col-search"><${Search} value=${q} onInput=${setQ} placeholder="Search notes" /></div>
      <div class="col-list">
        ${undo && html`<div class="nt-undo">Moved to trash <button class="small" onClick=${async () => { await api('POST', '/notes/restore', { Path: undo }); open(undo); setUndo(''); reload(); }}>Undo</button></div>`}
        ${!data ? html`<div class="col-empty">Loading…</div>` : !notes.length ? html`<div class="col-empty">${q ? 'No note has “' + q + '”.' : 'No notes yet. ⌘N starts one.'}</div>`
          : groups.map(([g, l]) => html`<div key=${g}>
            <button class="group fold" onClick=${() => setFolds({ ...folds, [g]: !folds[g] })}><span class="caret">${folds[g] ? '▸' : '▾'}</span> ${g} <span class="n">${l.length}</span></button>
            ${!folds[g] && l.map((n) => html`<div key=${n.path} class=${'item nt-item' + (n.path === path ? ' on' : '')} onClick=${() => open(n.path)}>
              <div class="item-main"><div class="item-title">${n.title}</div><div class="item-sub">${ago(n.mod) === 'now' ? 'just now' : ago(n.mod) + ' ago'}${n.snippet ? ' · ' + n.snippet : ''}</div></div></div>`)}</div>`)}
      </div>
      <div class="col-foot meta" title=${data ? data.dir : ''}>⌘N new · ⌘J ask Claude · saved in ~/.crew/notes</div>
    </section>
    ${path ? html`<${Editor} key=${path} path=${path} note=${note} reloadKey=${reloadKey} starred=${starred.includes(path)}
        star=${() => api('POST', '/notes/star', { Path: path, On: !starred.includes(path) }).then(reload)}
        moveTo=${() => setForm({ move: note || { path, folder: '' } })} trash=${() => trash(path)}
        moved=${(p) => { open(p); reload(); }} onEmptyLeave=${dropEmpty} />`
      : html`<main class="page"><${Empty} title=${notes.length ? 'Pick a note' : 'Write your first note'}>
          <p>Write normally: the first line is the title. Checklists ⇧⌘L, headings ⇧⌘H, bold ⌘B. Claude can search, summarise and tidy your notes (⌘J).</p>
          <button class="primary" onClick=${create}>New note (⌘N)</button><//></main>`}
    ${chat && html`<aside class="nt-chat">
      <header><b>Ask Claude</b><span class="meta">reads and writes only your notes</span><button class="ghost" title="Close (⌘J)" onClick=${() => setChat(false)}>✕</button></header>
      ${chatID ? html`<${Chat} id=${chatID} base=${'/tasks/' + chatID} agent="Claude" placeholder="Ask about your notes"
          empty="“Summarise this week's notes”, “Turn my stand-up notes into a list of action items”, “Find where I wrote about the parser”." />`
        : html`<div class="empty">Opening the chat…</div>`}</aside>`}
    ${form && form.move && html`<${Form} title="Move to folder" action="Move" onClose=${() => setForm(null)} initial=${{ Folder: form.move.folder }}
      fields=${[{ key: 'Folder', label: 'Folder', type: 'combo', options: (data && data.folders) || [], placeholder: 'none (the top)', hint: 'pick one or type a new one' }]}
      submit=${async (v) => {
        const to = (v.Folder.trim() ? v.Folder.trim().replace(/\/+$/, '') + '/' : '') + form.move.path.split('/').pop();
        if (to !== form.move.path) { await api('POST', '/notes/move', { Path: form.move.path, To: to }); await reload(); open(to); }
      }} />`}
  </div>`;
}
