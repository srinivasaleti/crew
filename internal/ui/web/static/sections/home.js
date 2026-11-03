// Home: what crew needs and whether it's ready (setup's own checks, no tokens), the sections you
// turn on or off, and the tasks at a glance.
import { html, useState, api, useLoad, useEvents, Toolbar } from '../lib.js';
import { sections } from './index.js';

if (!document.querySelector('link[href="sections/home.css"]')) document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="sections/home.css">');

// Fix is the command that installs a tool or signs in, with a button that copies it.
function Fix({ cmd, hint }) {
  const [copied, setCopied] = useState(false);
  const copy = () => navigator.clipboard.writeText(cmd).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); });
  return html`<div class="fix"><code>${cmd}</code><button class="small" onClick=${copy}>${copied ? 'Copied' : 'Copy'}</button>${hint && html`<span class="meta">${hint}</span>`}</div>`;
}

export default function Home({ go }) {
  const setup = useLoad(() => api('GET', '/home/setup'));
  const secs = useLoad(() => api('GET', '/sections'));
  const tasks = useLoad(() => api('GET', '/tasks'));
  useEvents((ev) => { if (ev.kind === 'sections') secs.reload(); if (ev.kind === 'task') tasks.reload(); });
  const toggle = (k, on) => api('POST', '/home/sections', { Key: k, On: on }).then(secs.reload);
  const counts = {};
  (tasks.data || []).forEach((t) => (counts[t.label] = (counts[t.label] || 0) + 1));
  const working = (tasks.data || []).filter((t) => t.working).length;
  return html`<main class="page"><${Toolbar} title="crew" sub="home"><button title="Check again" onClick=${setup.reload}>↻ Check again</button><//>
    <div class="scroll"><div class="home">
      <section class="card"><h2>Setup</h2>
        ${setup.loading && !setup.data ? html`<div class="meta">Checking…</div>` : (setup.data || []).map((t) => {
          const ok = t.Installed && t.SignedIn;
          const state = !t.Installed ? 'not installed' : !t.SignedIn ? t.Off || 'signed out' : t.Account ? 'signed in · ' + t.Account : 'ready';
          return html`<div class="tool" key=${t.Name}>
            <span class=${'mark ' + (ok ? 'ok' : t.Optional ? 'opt' : 'bad')}>${ok ? '✓' : t.Optional ? '○' : '✗'}</span>
            <div class="tool-main"><div><b>${t.Name}</b> <span class=${ok ? 'meta' : 'warn'}>${state}</span>${t.Optional && !ok ? html` <span class="meta">(optional)</span>` : ''}</div>
              <div class="meta">${t.For}</div>
              ${!ok && (!t.Installed ? t.Install : t.SignIn) && html`<${Fix} cmd=${!t.Installed ? t.Install : t.SignIn} hint=${t.Installed ? t.Hint : ''} />`}</div></div>`;
        })}
        ${setup.err && html`<div class="err">${setup.err}</div>`}
        <p class="meta">Run a command in your terminal, then Check again.</p></section>

      <section class="card"><h2>Sections</h2><p class="meta">Turn on what you use. One that's off does no work in the background.</p>
        ${(secs.data || []).filter((s) => sections[s.key]).map((s) => html`<label class="sec" key=${s.key}>
          <input type="checkbox" checked=${s.on} onChange=${(e) => toggle(s.key, e.target.checked)} />
          <span class="sec-icon">${sections[s.key].icon}</span><b>${sections[s.key].title}</b><span class="meta">${sections[s.key].about}</span>
          ${s.on && html`<button class="small ghost" onClick=${(e) => { e.preventDefault(); go(s.key); }}>Open →</button>`}</label>`)}</section>

      <section class="card"><h2>Tasks</h2>
        <div class="stats">${Object.entries(counts).map(([l, n]) => html`<div class="stat" key=${l}><div class="n">${n}</div><div class="meta">${l}</div></div>`)}
          <div class="stat"><div class="n">${working}</div><div class="meta">agents working</div></div></div>
        <button class="primary" onClick=${() => go('tasks')}>Open Tasks</button></section>
    </div></div></main>`;
}
