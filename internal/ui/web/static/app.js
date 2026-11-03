// crew's web UI: a rail of sections (like Slack's workspaces), each section's page beside it, and a
// ⌘K switcher. Each section is its own file (sections/<key>.js), loaded on its own, so one that breaks
// shows its error in its page and the rest keep working.
import { html, render, useState, useEffect, useErrorBoundary } from './vendor/preact-htm.js';
import { api, on, ask } from './lib.js';
import { sections } from './sections/index.js';

const loaded = {}; // key -> the section's module, or {error}
const load = (key) => (loaded[key] ||= import('./sections/' + key + '.js').catch((e) => ({ error: e })));

// Guard keeps a section's failure inside its page.
function Guard({ children, name }) {
  const [err, reset] = useErrorBoundary();
  if (err) return html`<main class="page"><div class="empty"><div class="big">${name} hit an error</div><pre class="err">${String(err.stack || err)}</pre><button onClick=${reset}>Try again</button></div></main>`;
  return children;
}

function Section({ name, setBadge, go }) {
  const [mod, setMod] = useState(null);
  useEffect(() => { setMod(null); load(name).then(setMod); }, [name]);
  if (!mod) return html`<main class="page"><div class="empty">Loading…</div></main>`;
  if (mod.error) return html`<main class="page"><div class="empty"><div class="big">${sections[name].title} isn't in the web UI yet</div><p>It's in the terminal UI (the rail's ⇄ switches).</p></div></main>`;
  const Page = mod.default;
  return html`<${Guard} name=${sections[name].title}><${Page} setBadge=${setBadge} go=${go} /><//>`;
}

// Palette is ⌘K: jump to a section, or switch to the terminal UI.
function Palette({ keys, go, onClose, toTerminal }) {
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const items = ['home', ...keys].map((k) => ({ label: sections[k].title, hint: k === 'home' ? '' : '⌘' + (keys.indexOf(k) + 1), run: () => go(k) }))
    .concat([{ label: 'Switch to the terminal UI', hint: '', run: toTerminal }])
    .filter((it) => it.label.toLowerCase().includes(q.toLowerCase()));
  const key = (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setI(Math.min(i + 1, items.length - 1)); }
    if (e.key === 'ArrowUp') { e.preventDefault(); setI(Math.max(i - 1, 0)); }
    if (e.key === 'Enter' && items[i]) { items[i].run(); onClose(); }
    if (e.key === 'Escape') onClose();
  };
  return html`<div class="scrim top" onClick=${(e) => e.target === e.currentTarget && onClose()}><div class="palette">
    <input autofocus placeholder="Go to…" value=${q} onInput=${(e) => { setQ(e.target.value); setI(0); }} onKeyDown=${key} />
    ${items.map((it, j) => html`<div key=${it.label} class=${'pal-item' + (j === i ? ' on' : '')} onMouseEnter=${() => setI(j)} onClick=${() => { it.run(); onClose(); }}>${it.label}<span class="meta">${it.hint}</span></div>`)}
  </div></div>`;
}

function App() {
  const [keys, setKeys] = useState([]);
  const [cur, setCur] = useState(location.hash.slice(1).split('/')[0] || 'home');
  const [badges, setBadges] = useState({});
  const [palette, setPalette] = useState(false);
  const watched = {};
  const readSections = () => api('GET', '/sections').then((l) => {
    const on = l.filter((s) => s.on && sections[s.key]).map((s) => s.key);
    setKeys(on);
    on.forEach((k) => !watched[k] && (watched[k] = true) && load(k).then((m) => m.watch && m.watch((b) => setBadges((x) => ({ ...x, [k]: b })))));
  });
  useEffect(() => { readSections(); return on((ev) => ev.kind === 'sections' && readSections()); }, []);
  const go = (k) => { setCur(k); if (location.hash.slice(1).split('/')[0] !== k) location.hash = k; };
  const toTerminal = async () => {
    if (!(await ask('Switch to the terminal UI? Agents working now are stopped.', 'Switch'))) return;
    await api('POST', '/ui', { Mode: 'terminal' }); // crew quits, and the app opens the terminal UI
    document.body.innerHTML = '<div class="empty">Switching to the terminal UI…</div>';
  };
  useEffect(() => {
    const k = (e) => {
      if (e.metaKey && e.key === 'k') { e.preventDefault(); setPalette((p) => !p); }
      if (e.metaKey && /^[1-9]$/.test(e.key) && keys[+e.key - 1]) { e.preventDefault(); go(keys[+e.key - 1]); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [keys]);
  return html`<nav class="rail">
      <button class=${'brand' + (cur === 'home' ? ' on' : '')} title="Home: setup and sections" onClick=${() => go('home')}>c</button>
      ${keys.map((k, i) => html`<button key=${k} class=${'rail-item' + (k === cur ? ' on' : '')} title=${sections[k].title + ' (⌘' + (i + 1) + ')'} onClick=${() => go(k)}>
        <span class="rail-icon" dangerouslySetInnerHTML=${{ __html: sections[k].icon }}></span><span class="rail-label">${sections[k].title}</span>
        ${badges[k] ? html`<span class="badge">${badges[k]}</span>` : ''}</button>`)}
      <div class="rail-fill"></div>
      <button class="rail-item" title="Go to… (⌘K)" onClick=${() => setPalette(true)}><span class="rail-icon" dangerouslySetInnerHTML=${{ __html: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>' }}></span><span class="rail-label">Go to</span></button>
      <button class="rail-item" title="Switch to the terminal UI" onClick=${toTerminal}><span class="rail-icon" dangerouslySetInnerHTML=${{ __html: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 15h4"/></svg>' }}></span><span class="rail-label">Terminal</span></button>
    </nav>
    ${keys.includes(cur) || cur === 'home' ? html`<${Section} key=${cur} name=${cur} go=${go} setBadge=${(b) => setBadges((x) => ({ ...x, [cur]: b }))} />` : html`<main class="page"><div class="empty">${keys.length ? '' : 'Loading…'}</div></main>`}
    ${palette && html`<${Palette} keys=${keys} go=${go} toTerminal=${toTerminal} onClose=${() => setPalette(false)} />`}`;
}

render(html`<${App} />`, document.getElementById('app'));
