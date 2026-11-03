// KUBERNETES: a tab's objects in every namespace, read with kubectl in crew's own context (your
// kubeconfig never changes); an object's Logs, Describe and a chat locked down to crew ctr; changes on
// your click, after asking, with the context's name typed on production. It reads the cluster only
// once you open it, so starting crew never reaches a cluster (or its sign-in) you didn't ask for.
import { html, useState, api, Toolbar, Search } from '../lib.js';
import { ago, useEvery, useKeys, St, Tabs, Chips, Grid, Confirm, Thing, Cleanup, Missing } from './ctr-ui.js';

const TABS = ['Pods', 'Deployments', 'Services', 'Jobs', 'Config', 'Events'];
const age = (r) => html`<span class="meta">${ago(r.Created)}</span>`;
const nsCol = { label: 'Namespace', cell: (r) => html`<span class="meta">${r.NS}</span>`, sort: (r) => r.NS };

// tabs are each tab's chips, columns and how a row is tagged for them.
const tabs = [
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'problems', label: 'Problems' }, { id: 'running', label: 'Running' }, { id: 'done', label: 'Completed' }],
    tags: (r) => [r.Bad ? 'problems' : r.Status === 'Completed' ? 'done' : 'running'],
    cols: [
      { label: 'Pod', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name }, nsCol,
      { label: 'Ready', cell: (r) => r.Ready },
      { label: 'Status', cell: (r) => html`<${St} ok=${r.Healthy && r.Status !== 'Completed'} bad=${r.Bad}>${r.Status}<//>`, sort: (r) => (r.Bad ? 0 : 1) },
      { label: 'Restarts', cell: (r) => (r.Restarts ? html`<span class="warn-text">${r.Restarts}</span>` : '0'), sort: (r) => -r.Restarts },
      { label: 'Node', cell: (r) => html`<span class="meta">${r.Node}</span>` },
      { label: 'Age', cell: age, sort: (r) => r.Created },
    ],
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'notready', label: 'Not ready' }],
    tags: (r) => (r.Bad ? ['notready'] : []),
    cols: [
      { label: 'Deployment', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name }, nsCol,
      { label: 'Ready', cell: (r) => (r.Bad ? html`<span class="st bad">${r.Ready}</span>` : r.Ready) },
      { label: 'Rollout', cell: (r) => html`<span class="meta">${r.Status}</span>` },
      { label: 'Age', cell: age, sort: (r) => r.Created },
    ],
  },
  {
    chips: [],
    tags: () => [],
    cols: [
      { label: 'Service', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name }, nsCol,
      { label: 'Type', cell: (r) => r.Type },
      { label: 'Cluster IP', cell: (r) => r.IP, cls: 'mono' },
      { label: 'Ports', cell: (r) => r.Ports, cls: 'mono' },
      { label: 'Age', cell: age, sort: (r) => r.Created },
    ],
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'Running', label: 'Running' }, { id: 'Failed', label: 'Failed' }, { id: 'Complete', label: 'Complete' }],
    tags: (r) => [r.Status],
    cols: [
      { label: 'Job', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name }, nsCol,
      { label: 'Completions', cell: (r) => r.Ready },
      { label: 'Status', cell: (r) => html`<${St} ok=${r.Healthy} bad=${r.Bad}>${r.Status}<//>` },
      { label: 'From', cell: (r) => html`<span class="meta">${r.From || '–'}</span>` },
      { label: 'Age', cell: age, sort: (r) => r.Created },
    ],
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'ConfigMap', label: 'ConfigMaps' }, { id: 'Secret', label: 'Secrets' }],
    tags: (r) => [r.Kind],
    cols: [
      { label: 'Name', cell: (r) => html`<b>${r.Name}</b>`, sort: (r) => r.Name }, nsCol,
      { label: 'Kind', cell: (r) => r.Kind, sort: (r) => r.Kind },
      { label: 'Type', cell: (r) => html`<span class="meta">${r.Type || '–'}</span>` },
      { label: 'Keys', cell: (r) => html`<span class="meta">${(r.Keys || []).join(', ')}</span>` },
      { label: 'Age', cell: age, sort: (r) => r.Created },
    ],
  },
  {
    chips: [{ id: 'all', label: 'All' }, { id: 'Warning', label: 'Warnings' }],
    tags: (r) => (r.Warning ? ['Warning'] : []),
    cols: [
      { label: 'Reason', cell: (r) => (r.Warning ? html`<span class="warn-text">${r.Reason}</span>` : html`<span class="meta">${r.Reason}</span>`), sort: (r) => r.Reason },
      { label: 'Object', cell: (r) => r.Object, cls: 'mono' }, nsCol,
      { label: 'Message', cell: (r) => r.Message },
      { label: 'Count', cell: (r) => r.Count, sort: (r) => -r.Count },
      { label: 'Last', cell: (r) => html`<span class="meta">${ago(r.Last)}</span>`, sort: (r) => -new Date(r.Last) },
    ],
  },
];

export default function Kube() {
  const [d, setD] = useState(null);
  const [tab, setTab] = useState(0);
  const [chip, setChip] = useState('all');
  const [ns, setNs] = useState('');
  const [q, setQ] = useState('');
  const [sel, setSel] = useState('');
  const [open, setOpen] = useState(null);
  const [ask, setAsk] = useState(null);
  const [cleanup, setCleanup] = useState(false);
  const [note, setNote] = useState('');
  const [want, setWant] = useState(''); // a context you picked, sent on the next read
  const load = (kctx = want) => api('GET', `/kube?tab=${tab}${kctx ? '&context=' + encodeURIComponent(kctx) : ''}`).then((x) => { setD(x); setWant(''); }, (e) => setD({ error: e.message }));
  useEvery(() => !open && load(), 10000, [tab, !!open]);
  const kctx = d && d.context;
  const prod = d && d.prod;

  // act asks, then does a change; on production the context's name must be typed (crew checks it too).
  const act = (r, action) => {
    const what = r.Kind === 'Deployment' ? 'Rollout restart deployment ' + r.Name : 'Delete ' + r.Kind.toLowerCase() + ' ' + r.Name;
    setAsk({ title: what + ' in ' + r.NS + '?', action: action === 'restart' ? 'Restart' : 'Delete', danger: action === 'remove', typed: prod ? kctx : '',
      warn: action === 'restart' ? "Its pods restart one by one." : r.Kind === 'Pod' ? 'Whatever made it (a ReplicaSet, a Job) makes a new one.' : '',
      yes: async (typed) => {
        await api('POST', '/kube/act', { Context: kctx, Kind: r.Kind, NS: r.NS, Name: r.Name, Action: action, Confirm: typed });
        setNote((action === 'restart' ? 'Restarting ' : 'Deleted ') + r.Name);
        load();
      } });
  };
  const acts = (r) => r.Kind === 'Deployment' ? [{ label: 'Restart', title: 'Rollout restart (R)', run: () => act(r, 'restart') }]
    : r.Kind === 'Pod' || r.Kind === 'Job' ? [{ label: 'Delete', title: 'Delete (x)', danger: true, run: () => act(r, 'remove') }] : [];

  const t = tabs[tab];
  const objs = d && d.objs && d.tab === tab ? d.objs : [];
  const all = objs.map((o) => ({ ...o, key: o.Kind + '/' + o.NS + '/' + o.Name, tags: ['all', ...t.tags(o)] }));
  const nss = [...new Set(all.map((r) => r.NS))].sort();
  const rows = all.filter((r) => (t.chips.length ? r.tags.includes(chip) : true) && (!ns || r.NS === ns)
    && (!q || (r.Name + ' ' + r.NS + ' ' + (r.Message || '')).toLowerCase().includes(q.toLowerCase())));
  const cur = all.find((r) => r.key === sel);
  // openRow opens an object; an event opens what it's about.
  const openRow = (r, pane) => setOpen(r.Kind === 'Event' ? { Kind: r.Type, Name: r.Object.split('/').slice(1).join('/'), NS: r.NS, key: 'ev' + r.key, Logs: ['Pod', 'Deployment', 'Job'].includes(r.Type), pane } : { ...r, pane });

  useKeys((k) => {
    if (open) return;
    if (k === 'C') return setCleanup(true);
    if (!cur) return;
    if (k === 'R' && cur.Kind === 'Deployment') act(cur, 'restart');
    if (k === 'x' && (cur.Kind === 'Pod' || cur.Kind === 'Job')) act(cur, 'remove');
    if (k === 'a') openRow(cur, 'Chat');
  }, [cur, open, d]);

  if (d && d.error && !d.context) return html`<${Missing} title=${d.missing ? 'kubectl isn\'t installed' : "Can't reach Kubernetes"} err=${d.error} retry=${() => load()} />`;
  if (open) {
    const panes = open.Logs ? ['Logs', 'Describe', 'Chat'] : ['Describe', 'Chat'];
    const okind = open.Kind;
    return html`<${Thing} key=${open.key} start=${open.pane} title=${open.Name} sub=${okind.toLowerCase() + ' · ' + open.NS + ' · ' + kctx} prod=${prod} panes=${panes}
      read=${(logs) => api('GET', `/kube/thing?context=${encodeURIComponent(kctx)}&kind=${okind}&name=${encodeURIComponent(open.Name)}&ns=${encodeURIComponent(open.NS)}${logs ? '&logs=1' : ''}`).then((x) => x.out)}
      chat=${() => api('POST', '/kube/chat', { Context: kctx, Kind: okind, NS: open.NS, Name: open.Name }).then((x) => x.id)}
      onBack=${() => setOpen(null)}
      actions=${acts(open).map((a) => html`<button key=${a.label} class=${a.danger ? 'danger' : ''} onClick=${a.run}>${a.label}</button>`)} />
      ${ask && html`<${Confirm} ...${ask} onClose=${() => setAsk(null)} />`}`;
  }

  return html`<main class="page">
    <${Toolbar} title="Kubernetes" sub=${!d ? 'Reading…' : html`${d.pods} pods${d.problems ? html` · <span class="st bad">${d.problems} not healthy</span>` : ''}${d.error ? html` · <span class="err">${d.error}</span>` : ''}`}>
      <span class="ctx-pick">${prod && html`<span class="prod" title="Every change asks, with the name typed; Cleanup is off">PRODUCTION</span>`}
        <select title="crew's own context: your kubeconfig doesn't change" value=${want || kctx || ''} onChange=${(e) => { setWant(e.target.value); setSel(''); setNs(''); load(e.target.value); }}>
          ${((d && d.contexts) || []).map((c) => html`<option key=${c} value=${c}>${c}</option>`)}</select>
        <select title="Namespace" value=${ns} onChange=${(e) => setNs(e.target.value)}>
          <option value="">All namespaces</option>${nss.map((n) => html`<option key=${n} value=${n}>${n}</option>`)}</select></span>
      ${note && html`<span class="meta">${note}</span>`}
      <button onClick=${() => load()} title="Read again">↻</button>
      <button onClick=${() => setCleanup(true)} title=${prod ? 'Cleanup is off on production' : 'Cleanup (C)'}>Cleanup</button><//>
    <${Tabs} cur=${tab} set=${(i) => { setTab(i); setChip('all'); setSel(''); setD((x) => x && { ...x, objs: null }); }}
      tabs=${TABS.map((label, i) => ({ label, count: i === 0 && d && !d.error ? d.pods : i === tab && d && d.objs && d.tab === tab ? d.objs.length : null, bad: i === 0 && d && d.problems > 0 }))} />
    <div class="ctr-head"><${Chips} chips=${t.chips} cur=${chip} set=${setChip} /><span class="grow"></span><${Search} value=${q} onInput=${setQ} /></div>
    ${!d || (d.tab !== tab && !d.error) ? html`<div class="empty">Reading ${kctx || 'the cluster'}…</div>`
      : html`<${Grid} cols=${t.cols} rows=${rows} sel=${sel} setSel=${setSel} open=${(r) => openRow(r)} acts=${acts}
        empty=${q || chip !== 'all' || ns ? 'Nothing matches' : 'Nothing here in ' + (kctx || 'this context')} />`}
    <div class="hint" style="padding:6px 20px">⏎ opens · ${tab === 0 ? 'x deletes the pod · ' : tab === 1 ? 'R rollout restart · ' : tab === 3 ? 'x deletes the job · ' : ''}C cleanup · a ask Claude · / filter</div>
    ${ask && html`<${Confirm} ...${ask} onClose=${() => setAsk(null)} />`}
    ${cleanup && html`<${Cleanup} load=${(n) => api('GET', `/kube/cleanup?context=${encodeURIComponent(kctx || '')}${n ? '&ns=' + encodeURIComponent(n) : ''}`)}
      run=${(labels) => api('POST', '/kube/cleanup', { Context: kctx, Labels: labels })}
      done=${() => { setNote('Cleaned up'); load(); }} onClose=${() => setCleanup(false)} />`}
  </main>`;
}
