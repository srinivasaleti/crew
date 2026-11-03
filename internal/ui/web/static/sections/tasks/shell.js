// A task's shell panel (⌘T): xterm.js over /api/tasks/<id>/shell. The shell runs in crew, so closing
// the panel or switching tasks keeps it; × or `exit` ends it.
import { html, useEffect, useRef, api } from '../../lib.js';

// xterm loads once, the first time a panel opens (its scripts set window.Terminal and FitAddon).
let xterm;
const loadXterm = () => (xterm ||= new Promise((done, fail) => {
  document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="vendor/xterm.css">');
  const add = (src) => new Promise((ok) => { const s = document.createElement('script'); s.src = src; s.onload = ok; s.onerror = () => fail(new Error("couldn't load " + src)); document.head.append(s); });
  add('vendor/xterm.js').then(() => add('vendor/addon-fit.js')).then(done);
}));

const bytes = (b64) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

// ShellPane is the panel: the task's shell, started if it isn't running. onEnd runs when it exits.
export function ShellPane({ id, onEnd }) {
  const box = useRef();
  useEffect(() => {
    let term, es, ro, gone = false;
    // Keys go one request at a time, in order; what's typed meanwhile goes in the next.
    let pend = '', busy = false;
    const flush = async () => {
      busy = true;
      while (pend) { const d = pend; pend = ''; await api('POST', `/tasks/${id}/shell/in`, { Data: d }).catch(() => {}); }
      busy = false;
    };
    loadXterm().then(() => api('POST', `/tasks/${id}/shell`)).then(() => {
      if (gone) return;
      const css = getComputedStyle(document.body), v = (n) => css.getPropertyValue(n).trim();
      term = new window.Terminal({ fontFamily: v('--mono'), fontSize: 13, cursorBlink: true, allowProposedApi: true,
        theme: { background: v('--bg'), foreground: v('--text'), cursor: v('--accent'), selectionBackground: v('--accent-soft') } });
      const fit = new window.FitAddon.FitAddon();
      term.loadAddon(fit);
      term.open(box.current);
      const size = () => { try { fit.fit(); api('POST', `/tasks/${id}/shell/size`, { Cols: term.cols, Rows: term.rows }); } catch {} };
      ro = new ResizeObserver(size);
      ro.observe(box.current);
      size();
      term.onData((d) => { pend += d; if (!busy) flush(); });
      es = new EventSource(`/api/tasks/${id}/shell/out`);
      es.addEventListener('reset', (e) => { term.reset(); term.write(bytes(e.data)); });
      es.onmessage = (e) => term.write(bytes(e.data));
      es.addEventListener('exit', () => { es.close(); onEnd(); });
      term.focus();
    }).catch((e) => { if (!gone && box.current) box.current.textContent = e.message; });
    return () => { gone = true; ro && ro.disconnect(); es && es.close(); term && term.dispose(); };
  }, [id]);
  return html`<aside class="shell-pane">
    <header><span><b>Shell</b> <span class="meta">⌘T · ⌘⇧←/→ panels</span></span>
      <button class="ghost small" title="End the shell" onClick=${() => api('DELETE', `/tasks/${id}/shell`).then(onEnd)}>×</button></header>
    <div class="shell-box" ref=${box}></div>
  </aside>`;
}
