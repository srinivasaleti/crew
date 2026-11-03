// DOCKER: containers, images, volumes and networks, read with the docker CLI on a clock; a thing's
// Logs, Inspect and a chat locked down to crew ctr; changes on your click, after asking.
import { html, useState, api, Toolbar, Search } from '../../lib.js';
import { ago, useEvery, useKeys, St, Tabs, Chips, Grid, Confirm, Thing, Cleanup, Missing } from '../containers/ui.js';

const TABS = ['Containers', 'Images', 'Volumes', 'Networks'];
const KIND = ['Container', 'Image', 'Volume', 'Network'];
const short = (id) => (id || '').replace('sha256:', '').slice(0, 12);
const failed = (c) => !c.Running && c.ExitCode !== 0;

// watch counts the containers that stopped with an error, for the rail (like the terminal's ✗).
export function watch(setBadge) {
  const tick = () => api('GET', '/docker').then((d) => {
    const n = (d.containers || []).filter(failed).length;
    setBadge(d.error || !n ? '' : '✗' + n);
  }, () => {});
  tick();
  const t = setInterval(tick, 30000);
  return () => clearInterval(t);
}

const users = (r) => (r.Builtin ? html`<span class="meta">built in</span>` : !r.UsedBy.length ? html`<span class="meta">– unused</span>` : r.UsedBy.length === 1 ? r.UsedBy[0] : r.UsedBy.length + ' containers');

// tabs are each tab's chips, columns and rows.
const tabs = [
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'running', label: 'Running' }, { id: 'exited', label: 'Exited' }],
    rows: (d) => d.containers.map((c) => ({ ...c, key: c.ID, kind: 'Container', label: c.Name, tags: ['all', c.Running ? 'running' : 'exited'], find: c.Name + ' ' + c.Image + ' ' + short(c.ID) })),
    cols: [
      { label: 'Name', cell: (r) => html`<b>${r.Name}</b>${r.Project && html` <span class="sub">${r.Project}</span>`}`, sort: (r) => r.Name },
      { label: 'Image', cell: (r) => r.Image, sort: (r) => r.Image, cls: 'mono' },
      { label: 'State', cell: (r) => html`<${St} ok=${r.Running} bad=${failed(r)}>${r.State}<//>`, sort: (r) => (r.Running ? 0 : failed(r) ? 1 : 2) },
      { label: 'Ports', cell: (r) => r.Ports || '–', cls: 'mono' },
      { label: 'CPU', cell: (r) => r.CPU || '–', sort: (r) => parseFloat(r.CPU) || 0 },
      { label: 'Mem', cell: (r) => r.Mem || '–' },
      { label: 'Up', cell: (r) => html`<span class="meta">${r.Running ? ago(r.Since) : ago(r.Since) + ' ago'}</span>`, sort: (r) => r.Since },
    ],
    summary: (d) => { const n = d.containers.filter((c) => c.Running).length; return n + ' running · ' + (d.containers.length - n) + ' stopped'; },
    empty: 'No containers',
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'used', label: 'In use' }, { id: 'unused', label: 'Unused' }, { id: 'dangling', label: 'Dangling' }],
    rows: (d) => d.images.map((i) => ({ ...i, key: i.ID, kind: 'Image', label: i.Name, tags: ['all', i.UsedBy.length ? 'used' : 'unused', i.Dangling && !i.UsedBy.length ? 'dangling' : ''], find: i.Name + ' ' + short(i.ID) })),
    cols: [
      { label: 'Image', cell: (r) => (r.Dangling ? html`<span class="meta mono">${short(r.ID)}</span>` : html`<b>${r.Name.split(':').slice(0, -1).join(':') || r.Name}</b>`), sort: (r) => r.Name },
      { label: 'Tag', cell: (r) => r.Tag, cls: 'mono' },
      { label: 'Size', cell: (r) => r.Size, sort: (r) => size(r.Size) },
      { label: 'Used by', cell: (r) => (r.Dangling && !r.UsedBy.length ? html`<span class="meta">– dangling</span>` : users(r)) },
      { label: 'Created', cell: (r) => html`<span class="meta">${r.Created}</span>` },
    ],
    summary: (d) => d.images.length + ' images · ' + ((d.disk.Images || {}).Size || '?') + ' · ' + ((d.disk.Images || {}).Reclaimable || '?') + ' reclaimable',
    empty: 'No images',
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'used', label: 'In use' }, { id: 'unused', label: 'Unused' }],
    rows: (d) => d.volumes.map((v) => ({ ...v, key: v.ID, kind: 'Volume', label: v.Name, tags: ['all', v.UsedBy.length ? 'used' : 'unused'], find: v.Name + ' ' + v.Project })),
    cols: [
      { label: 'Volume', cell: (r) => (r.Anonymous ? html`<span class="mono">${short(r.Name)}</span> <span class="sub">anonymous</span>` : html`<b>${r.Name}</b>`), sort: (r) => r.Name },
      { label: 'Size', cell: (r) => r.Size || '–', sort: (r) => size(r.Size) },
      { label: 'Used by', cell: users },
      { label: 'Driver', cell: (r) => html`<span class="meta">${r.Driver}</span>` },
    ],
    summary: (d) => d.volumes.length + ' volumes · ' + ((d.disk['Local Volumes'] || {}).Size || '?') + ' · ' + d.volumes.filter((v) => !v.UsedBy.length).length + ' unused',
    empty: 'No volumes',
  },
  {
    chips: [],
    rows: (d) => d.networks.map((n) => ({ ...n, key: n.ID, kind: 'Network', label: n.Name, tags: ['all'], find: n.Name })),
    cols: [
      { label: 'Network', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name },
      { label: 'Driver', cell: (r) => r.Driver },
      { label: 'Scope', cell: (r) => html`<span class="meta">${r.Scope}</span>` },
      { label: 'Used by', cell: users },
    ],
    summary: (d) => d.networks.length + ' networks',
    empty: 'No networks',
  },
];

// size reads "412MB" for sorting.
function size(s) {
  const m = /^([\d.]+)\s*([kKMGT]?i?B)?/.exec(s || '');
  if (!m) return 0;
  return parseFloat(m[1]) * ({ B: 1, kB: 1e3, KB: 1e3, MB: 1e6, MiB: 1 << 20, GB: 1e9, GiB: 1 << 30, TB: 1e12 }[m[2]] || 1);
}

export default function Docker() {
  const [d, setD] = useState(null);
  const [tab, setTab] = useState(0);
  const [chip, setChip] = useState('all');
  const [q, setQ] = useState('');
  const [sel, setSel] = useState('');
  const [open, setOpen] = useState(null);
  const [ask, setAsk] = useState(null);
  const [cleanup, setCleanup] = useState(false);
  const [note, setNote] = useState('');
  const load = () => api('GET', '/docker' + (tab === 0 ? '?stats=1' : '')).then(setD, (e) => setD({ error: e.message }));
  useEvery(() => !open && load(), 5000, [tab, !!open]);

  // act asks, then does a change on a row; the list reads again after.
  const act = (r, action) => {
    const name = r.label;
    const texts = {
      restart: ['Restart ' + name + '?', '', 'Restart'],
      stop: ['Stop ' + name + '?', '', 'Stop'],
      start: ['Start ' + name + '?', '', 'Start'],
      remove: r.kind === 'Volume' ? ['Remove volume ' + name + ' and the data in it?', 'A volume usually holds a database or other data; this can\'t be undone.', 'Remove']
        : r.kind === 'Container' && r.Running ? ['Remove container ' + name + '?', "It's running; it's stopped first.", 'Remove']
        : ['Remove ' + r.kind.toLowerCase() + ' ' + name + (r.Size ? ' (' + r.Size + ')' : '') + '?', '', 'Remove'],
    }[action];
    setAsk({ title: texts[0], warn: texts[1], action: texts[2], danger: action === 'remove',
      yes: async () => { await api('POST', '/docker/act', { Kind: r.kind, ID: r.ID, Action: action }); setNote({ restart: 'Restarted', stop: 'Stopped', start: 'Started', remove: 'Removed' }[action] + ' ' + name); load(); } });
  };
  const acts = (r) => {
    const blocked = r.kind !== 'Container' && (r.Builtin ? "Docker's own network can't be removed" : r.UsedBy && r.UsedBy.length ? 'used by ' + r.UsedBy.join(', ') + ': remove those first' : '');
    const out = [];
    if (r.kind === 'Container') {
      out.push({ label: 'Restart', title: 'Restart (R)', run: () => act(r, 'restart') });
      out.push(r.Running ? { label: 'Stop', title: 'Stop (s)', run: () => act(r, 'stop') } : { label: 'Start', title: 'Start (S)', run: () => act(r, 'start') });
    }
    out.push({ label: 'Remove', title: blocked || 'Remove (x)', danger: true, disabled: !!blocked, run: () => act(r, 'remove') });
    return out;
  };

  const t = tabs[tab];
  const all = d && !d.error ? t.rows(d) : [];
  const rows = all.filter((r) => (t.chips.length ? r.tags.includes(chip) : true) && (!q || r.find.toLowerCase().includes(q.toLowerCase())));
  const cur = all.find((r) => r.key === sel);

  useKeys((k) => {
    if (open) return;
    if (k === 'C') return setCleanup(true);
    if (!cur) return;
    const a = { R: 'restart', s: 'stop', S: 'start', x: 'remove' }[k];
    const b = acts(cur).find((b) => b.label.toLowerCase() === a || (a === 'remove' && b.label === 'Remove'));
    if (a && b && !b.disabled && (cur.kind === 'Container' || a === 'remove')) act(cur, a);
    if (k === 'a') setOpen({ ...cur, pane: 'Chat' });
  }, [cur, open]);

  if (d && d.error && !d.containers) return html`<${Missing} title=${d.missing ? 'Docker isn\'t installed' : "Can't reach Docker"} err=${d.error} retry=${load} />`;
  if (open) {
    const panes = open.kind === 'Container' ? ['Logs', 'Inspect', 'Chat'] : ['Inspect', 'Chat'];
    return html`<${Thing} key=${open.key} start=${open.pane} title=${open.label} sub=${open.kind.toLowerCase() + (open.Image ? ' · ' + open.Image : '')} panes=${panes}
      read=${(logs) => api('GET', `/docker/thing?kind=${open.kind}&id=${encodeURIComponent(open.ID)}${logs ? '&logs=1' : ''}`).then((x) => x.out)}
      chat=${() => api('POST', '/docker/chat', { Kind: open.kind, ID: open.ID, Name: open.label }).then((x) => x.id)}
      onBack=${() => setOpen(null)}
      actions=${open.kind === 'Container' ? acts(open).map((a) => html`<button key=${a.label} class=${a.danger ? 'danger' : ''} onClick=${a.run}>${a.label}</button>`) : ''} />
      ${ask && html`<${Confirm} ...${ask} onClose=${() => setAsk(null)} />`}`;
  }

  return html`<main class="page">
    <${Toolbar} title="Docker" sub=${d ? (d.error ? d.error : t.summary(d)) : 'Reading…'}>
      ${note && html`<span class="meta">${note}</span>`}
      <button onClick=${load} title="Read again">↻</button>
      <button onClick=${() => setCleanup(true)} title="Cleanup (C)">Cleanup</button><//>
    <${Tabs} cur=${tab} set=${(i) => { setTab(i); setChip('all'); setSel(''); }}
      tabs=${TABS.map((label, i) => ({ label, count: d && !d.error ? tabs[i].rows(d).length : null, bad: i === 0 && d && d.containers && d.containers.some(failed) }))} />
    <div class="ctr-head"><${Chips} chips=${t.chips} cur=${chip} set=${setChip} /><span class="grow"></span>
      <${Search} value=${q} onInput=${setQ} /></div>
    ${!d ? html`<div class="empty">Reading Docker…</div>` : html`<${Grid} cols=${t.cols} rows=${rows} sel=${sel} setSel=${setSel} open=${(r) => setOpen(r)} acts=${acts}
      empty=${q || chip !== 'all' ? 'Nothing matches' : t.empty} />`}
    <div class="hint" style="padding:6px 20px">⏎ opens · ${tab === 0 ? 'R restart · s stop · S start · ' : ''}x remove · C cleanup · a ask Claude · / filter</div>
    ${ask && html`<${Confirm} ...${ask} onClose=${() => setAsk(null)} />`}
    ${cleanup && html`<${Cleanup} load=${() => api('GET', '/docker/cleanup')} run=${(labels) => api('POST', '/docker/cleanup', { Labels: labels })}
      done=${() => { setNote('Cleaned up'); load(); }} onClose=${() => setCleanup(false)} />`}
  </main>`;
}
