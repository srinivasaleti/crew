// TODO: checklists. Lists on the left (Today and Upcoming gather every list's dated items), a
// quick-add line that reads "call the bank tomorrow !high #Home", items you tick, drag, edit in
// place, and turn into a crew task. Parsing and the rules are crew's (internal/todos); this draws and sends.
import { html, useState, useEffect, useRef, api, on, useEvents, useLoad, Form, Empty, Toolbar, ask, css } from '../../lib.js';

css(import.meta.url);

const TODAY = '★ Today', UPCOMING = '◷ Upcoming';
const PRIO = ['', 'Low', 'Medium', 'High'];
const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);

// watch counts what's due today or late for the rail's badge.
export function watch(setBadge) {
  const count = () => api('GET', '/todos').then(({ items, today }) => setBadge(items.filter((i) => !i.done && i.due && i.due <= today).length || ''));
  count();
  const t = setInterval(count, 5 * 60000);
  const off = on((ev) => ev.kind === 'todos' && count());
  return () => { clearInterval(t); off(); };
}

// dueLabel is how a due day reads next to today: Today, Tomorrow, Fri, Nov 20, and whether it's late.
function dueLabel(due, today) {
  const d = new Date(due + 'T00:00'), t = new Date(today + 'T00:00');
  const n = Math.round((d - t) / 864e5);
  const short = d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  if (n < 0) return { text: 'Late · ' + short, cls: 'late' };
  if (n === 0) return { text: 'Today', cls: 'today' };
  if (n === 1) return { text: 'Tomorrow', cls: '' };
  if (n < 7) return { text: d.toLocaleDateString([], { weekday: 'short' }), cls: '' };
  return { text: short, cls: '' };
}

// QuickAdd adds an item to the list on screen, showing what the line makes as you type.
function QuickAdd({ list, lists, today, onAdded, box }) {
  const [text, setText] = useState('');
  const [p, setP] = useState(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    if (!text.trim()) { setP(null); return; }
    const t = setTimeout(() => api('POST', '/todos/preview', { Quick: text }).then(setP, () => setP(null)), 150);
    return () => clearTimeout(t);
  }, [text]);
  const add = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    try { await api('POST', '/todos', { Quick: text, List: lists.includes(list) ? list : '', Due: list === TODAY ? today : '' }); setText(''); setErr(''); onAdded(); } catch (x) { setErr(x.message); }
  };
  const chips = p && [(p.due || list === TODAY) && '📅 ' + dueLabel(p.due || today, today).text, p.priority && '⚑ ' + PRIO[p.priority], p.list && '# ' + p.list].filter(Boolean);
  return html`<form class="td-add" onSubmit=${add}>
    <span class="td-plus">+</span>
    <input ref=${box} placeholder=${'Add to ' + (lists.includes(list) ? list : list === TODAY ? 'Today' : 'Inbox') + '… (n)'} value=${text} onInput=${(e) => setText(e.target.value)}
      onKeyDown=${(e) => e.key === 'Escape' && (setText(''), e.target.blur())} />
    <div class="td-hint">${err ? html`<span class="err">${err}</span>` : chips && chips.length ? html`${chips.map((c) => html`<span class="chip">${c}</span> `)}<span class="meta">⏎ adds it</span>`
      : html`<span class="meta">Try: call the bank tomorrow !high #Home · write report fri !! · pay rent 2026-12-01</span>`}</div>
  </form>`;
}

// Row is one item: its box, its text (double-click edits), its due day, priority and list, and its
// actions on hover. Open, it shows its details to change.
function Row({ it, today, lists, showList, sel, open, select, toggleOpen, save, del, makeTask, drag }) {
  const due = it.due && dueLabel(it.due, today);
  return html`<div class=${'td-row' + (sel ? ' on' : '') + (it.done ? ' done' : '') + (open ? ' open' : '') + (it.priority ? ' p' + it.priority : '')}
      draggable=${!open} onDragStart=${(e) => { e.dataTransfer.setData('text/todo', String(it.id)); e.dataTransfer.effectAllowed = 'move'; }}
      onDragOver=${(e) => { e.preventDefault(); e.currentTarget.classList.add('drop'); }} onDragLeave=${(e) => e.currentTarget.classList.remove('drop')}
      onDrop=${(e) => { e.preventDefault(); e.currentTarget.classList.remove('drop'); drag(+e.dataTransfer.getData('text/todo'), it); }}
      onClick=${select}>
    <div class="td-line">
      <input type="checkbox" class="td-check" checked=${it.done} title="Done (space)" onClick=${(e) => e.stopPropagation()} onChange=${(e) => save({ Done: e.target.checked })} />
      <div class="td-text" onDblClick=${toggleOpen}>${it.text}
        ${it.note && !open && html`<span class="meta td-has-note" title=${it.note}> ✎</span>`}</div>
      ${it.task && html`<a class="chip td-task" href=${'#tasks/' + it.task} title="Open its crew task" onClick=${(e) => e.stopPropagation()}>Task ↗</a>`}
      ${showList && html`<span class="chip td-list">${it.list}</span>`}
      ${it.priority > 0 && html`<span class=${'td-flag p' + it.priority} title=${PRIO[it.priority] + ' priority'}>⚑</span>`}
      ${due && html`<span class=${'td-due ' + due.cls}>${due.text}</span>`}
      <div class="td-acts">
        ${!it.task && html`<button class="small ghost" title="Make it a crew task (t)" onClick=${(e) => { e.stopPropagation(); makeTask(); }}>→ Task</button>`}
        <button class="small ghost" title="Details (e)" onClick=${(e) => { e.stopPropagation(); toggleOpen(); }}>${open ? 'Close' : 'Edit'}</button>
        <button class="small ghost danger" title="Delete (⌫)" onClick=${(e) => { e.stopPropagation(); del(); }}>✕</button>
      </div>
    </div>
    ${open && html`<${Details} it=${it} lists=${lists} save=${save} close=${toggleOpen} />`}
  </div>`;
}

// Details edit an item in place; each field saves as you leave it.
function Details({ it, lists, save, close }) {
  const [text, setText] = useState(it.text);
  const [note, setNote] = useState(it.note || '');
  return html`<div class="td-details" onClick=${(e) => e.stopPropagation()}>
    <input class="td-edit-text" value=${text} autofocus onInput=${(e) => setText(e.target.value)} onBlur=${() => text !== it.text && save({ Text: text })}
      onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); e.target.blur(); close(); } if (e.key === 'Escape') close(); }} />
    <textarea rows="3" placeholder="Notes" value=${note} onInput=${(e) => setNote(e.target.value)} onBlur=${() => note !== (it.note || '') && save({ Note: note })}
      onKeyDown=${(e) => e.key === 'Escape' && close()}></textarea>
    <div class="td-fields">
      <label>Due <input type="date" value=${it.due || ''} onChange=${(e) => save({ Due: e.target.value })} />
        ${it.due && html`<button class="small ghost" onClick=${() => save({ Due: '' })}>clear</button>`}</label>
      <label>Priority <select value=${it.priority || 0} onChange=${(e) => save({ Priority: +e.target.value })}>
        ${PRIO.map((p, i) => html`<option value=${i}>${p || 'None'}</option>`)}</select></label>
      <label>List <select value=${it.list} onChange=${(e) => save({ List: e.target.value })}>${lists.map((l) => html`<option>${l}</option>`)}</select></label>
      <button class="small" onClick=${close}>Done</button>
    </div>
  </div>`;
}

export default function Todos({ go }) {
  const { data, reload } = useLoad(() => api('GET', '/todos'));
  const [list, setList] = useState(decodeURIComponent(location.hash.split('/')[1] || '') || TODAY);
  const [sel, setSel] = useState(null);
  const [open, setOpen] = useState(null);
  const [showDone, setShowDone] = useState(false);
  const [undo, setUndo] = useState(null);
  const [form, setForm] = useState(null); // 'list', {rename}, or {task: item}
  const box = useRef();
  useEvents((ev) => { if (ev.kind === 'todos' || ev.kind === 'open') reload(); }, []);
  const pick = (l) => { setList(l); setSel(null); setOpen(null); location.hash = 'todos/' + encodeURIComponent(l); };

  const items = (data && data.items) || [], lists = (data && data.lists) || ['Inbox'], today = data ? data.today : '';
  const smart = list === TODAY || list === UPCOMING;
  const inView = (i) => list === TODAY ? i.due && i.due <= today : list === UPCOMING ? i.due && i.due > today : i.list === list;
  let shown = items.filter((i) => inView(i) && !i.done);
  if (smart) shown = [...shown].sort((a, b) => a.due.localeCompare(b.due) || b.priority - a.priority);
  const done = items.filter((i) => inView(i) && i.done).sort((a, b) => (b.doneAt || '').localeCompare(a.doneAt || ''));
  const order = [...shown, ...(showDone ? done : [])];
  const cur = items.find((i) => i.id === sel);
  const openCount = (l) => items.filter((i) => !i.done && (l === TODAY ? i.due && i.due <= today : l === UPCOMING ? i.due && i.due > today : i.list === l)).length;

  const save = (it, change) => api('PUT', '/todos/' + it.id, change).then(reload, (e) => ask(e.message));
  const del = async (it) => {
    const i = items.indexOf(it), next = items.slice(i + 1).find((x) => x.list === it.list);
    await api('DELETE', '/todos/' + it.id);
    setUndo({ it, before: next ? next.id : 0 });
    if (sel === it.id) setSel(null);
    reload();
  };
  const restore = async () => { await api('POST', '/todos/restore', { Item: undo.it, Before: undo.before }); setUndo(null); reload(); };
  // drag drops item id before item onto (in onto's list); dropping on a list moves it to that list's end.
  const drag = async (id, onto) => { if (id && id !== onto.id) { await api('POST', `/todos/${id}/move`, { Before: onto.id, List: onto.list }); reload(); } };
  const dropOnList = async (e, l) => {
    e.preventDefault(); e.currentTarget.classList.remove('drop');
    const id = +e.dataTransfer.getData('text/todo');
    if (id && !l.startsWith('★') && !l.startsWith('◷')) { await api('POST', `/todos/${id}/move`, { Before: 0, List: l }); reload(); }
  };

  useEffect(() => {
    const k = (e) => {
      if (typing() || e.metaKey || form) return;
      const ids = order.map((i) => i.id), i = ids.indexOf(sel);
      if (e.key === 'n') { e.preventDefault(); box.current && box.current.focus(); }
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); setSel(ids[Math.min(i + 1, ids.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); setSel(ids[Math.max(i - 1, 0)]); }
      if (!cur) return;
      if (e.key === ' ' || e.key === 'x') { e.preventDefault(); save(cur, { Done: !cur.done }); }
      if (e.key === 'e' || e.key === 'Enter') { e.preventDefault(); setOpen(open === cur.id ? null : cur.id); }
      if (e.key === 'Backspace' || e.key === 'Delete') { e.preventDefault(); del(cur); }
      if (/^[0-3]$/.test(e.key)) save(cur, { Priority: +e.key });
      if (e.key === 't' && !cur.task) setForm({ task: cur });
      if (e.key === 'Escape') setOpen(null);
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  });

  const row = (it) => html`<${Row} key=${it.id} it=${it} today=${today} lists=${lists} showList=${smart} sel=${it.id === sel} open=${it.id === open}
    select=${() => setSel(it.id)} toggleOpen=${() => setOpen(open === it.id ? null : it.id)} save=${(c) => save(it, c)} del=${() => del(it)}
    makeTask=${() => setForm({ task: it })} drag=${drag} />`;
  const navItem = (l, icon) => html`<div key=${l} class=${'item td-nav' + (l === list ? ' on' : '')} onClick=${() => pick(l)}
      onDragOver=${(e) => { e.preventDefault(); e.currentTarget.classList.add('drop'); }} onDragLeave=${(e) => e.currentTarget.classList.remove('drop')} onDrop=${(e) => dropOnList(e, l)}>
    <span class="td-icon">${icon}</span><div class="item-main item-title">${l.replace(/^[★◷] /, '')}</div><span class="meta">${openCount(l) || ''}</span></div>`;

  return html`<div class="split">
    <section class="col td-col">
      <div class="col-head"><b>To do</b><button class="small" title="New list" onClick=${() => setForm('list')}>+ List</button></div>
      <div class="col-list">
        ${navItem(TODAY, '★')}${navItem(UPCOMING, '◷')}
        <div class="group">Lists</div>
        ${lists.map((l) => navItem(l, l === 'Inbox' ? '⌂' : '≡'))}
      </div>
      <div class="col-foot meta">drag an item onto a list to move it</div>
    </section>
    <main class="page">
      <${Toolbar} title=${list.replace(/^[★◷] /, '')} sub=${list === TODAY ? 'due today and late, from every list' : list === UPCOMING ? 'what has a day coming up' : shown.length + ' to do'}>
        ${!smart && list !== 'Inbox' && html`<button class="small" onClick=${() => setForm({ rename: list })}>Rename</button>
          <button class="small danger" onClick=${async () => { if (await ask(`Delete the list ${list} and its ${items.filter((i) => i.list === list).length} items?`, 'Delete')) { await api('DELETE', '/todos/lists/' + encodeURIComponent(list)); pick('Inbox'); reload(); } }}>Delete list</button>`}
      <//>
      <div class="scroll td">
        <${QuickAdd} list=${list} lists=${lists} today=${today} onAdded=${reload} box=${box} />
        ${undo && html`<div class="td-undo">Deleted “${undo.it.text}” <button class="small" onClick=${restore}>Undo</button><button class="ghost small" onClick=${() => setUndo(null)}>✕</button></div>`}
        ${!data ? html`<div class="meta">Loading…</div>`
          : !shown.length && !done.length ? html`<${Empty} title=${list === TODAY ? 'Nothing due today' : 'Nothing here yet'}>${list === TODAY ? 'Add “… today” or “… tomorrow” to give an item a day.' : 'Type above and press ⏎.'}<//>`
          : html`${shown.length ? shown.map(row) : html`<div class="meta td-clear">All done here. ✓</div>`}
            ${done.length > 0 && html`<div class="td-done-head"><button class="group fold" onClick=${() => setShowDone(!showDone)}><span class="caret">${showDone ? '▾' : '▸'}</span> Completed <span class="n">${done.length}</span></button>
              ${!smart && html`<button class="small ghost" onClick=${() => api('POST', '/todos/clear', { List: list }).then(reload)}>Clear completed</button>`}</div>
              ${showDone && done.map(row)}`}`}
        <div class="hint">n add · ↑↓ choose · space done · e edit · 0–3 priority · t make a task · ⌫ delete · drag to reorder</div>
      </div>
    </main>
    ${form === 'list' && html`<${Form} title="New list" action="Add" onClose=${() => setForm(null)} fields=${[{ key: 'Name', label: 'Name', placeholder: 'Work, Home, Groceries' }]}
      submit=${async (v) => { await api('POST', '/todos/lists', v); await reload(); pick(v.Name.trim()); }} />`}
    ${form && form.rename && html`<${Form} title="Rename list" onClose=${() => setForm(null)} initial=${{ Name: form.rename }} fields=${[{ key: 'Name', label: 'Name' }]}
      submit=${async (v) => { await api('PUT', '/todos/lists/' + encodeURIComponent(form.rename), v); await reload(); pick(v.Name.trim()); }} />`}
    ${form && form.task && html`<${TaskForm} it=${form.task} onClose=${() => setForm(null)} done=${async (t) => { await save(form.task, { Task: t.id }); location.hash = 'tasks/' + t.id; go('tasks'); }} />`}
  </div>`;
}

// TaskForm makes a crew task from an item: its text is the name, its notes the description.
function TaskForm({ it, onClose, done }) {
  const repos = useLoad(() => api('GET', '/tasks/repos'));
  if (!repos.data) return null;
  return html`<${Form} title="Make it a crew task" action="Create task" onClose=${onClose} initial=${{ Title: it.text, Body: it.note || '', Repo: repos.data[0] || '', Agent: 'claude' }}
    fields=${[{ key: 'Title', label: 'Name' }, { key: 'Repo', label: 'Repo folder', type: 'combo', options: repos.data, placeholder: '~/code/project' },
      { key: 'Body', label: 'Description', type: 'textarea', placeholder: 'What the agent should know' }]}
    submit=${async (v) => done(await api('POST', '/tasks', v))} />`;
}
