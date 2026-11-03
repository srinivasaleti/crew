// A task's terminals (⌘T, up to 4): a column on the right of the task, one pane per shell, stacked.
// Each is xterm.js over /api/tasks/<id>/shells/<n>. The shells run in crew, so switching tasks or
// reloading keeps them; ⌘W, × or `exit` ends one.
import { html, useEffect, useRef, api } from '../../lib.js';

// xterm loads once, the first time a terminal opens (its scripts set window.Terminal and FitAddon).
let xterm;
const loadXterm = () => (xterm ||= new Promise((done, fail) => {
  document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="vendor/xterm.css">');
  const add = (src) => new Promise((ok) => { const s = document.createElement('script'); s.src = src; s.onload = ok; s.onerror = () => fail(new Error("couldn't load " + src)); document.head.append(s); });
  add('vendor/xterm.js').then(() => add('vendor/addon-fit.js')).then(done);
}));

// colors are the theme's tokens as xterm wants them (read again when the theme changes).
function colors() {
  const css = getComputedStyle(document.documentElement), v = (k) => css.getPropertyValue(k).trim();
  return { background: v('--bg'), foreground: v('--text'), cursor: v('--accent'), selectionBackground: v('--accent-soft'), mono: v('--mono') };
}

const bytes = (b64) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

// width is the column's width in px, as you last dragged it (0: the stylesheet's).
let width = (() => { try { return +localStorage.getItem('crew.terms.width') || 0; } catch { return 0; } })();

// drag resizes the column from its left edge, keeping room for the task beside it.
function drag(e) {
  const col = e.currentTarget.parentElement, x = e.clientX, w = col.offsetWidth, room = col.parentElement.offsetWidth - 640;
  e.preventDefault();
  const move = (m) => { width = Math.max(280, Math.min(room, w - (m.clientX - x))); col.style.width = width + 'px'; };
  const up = () => { removeEventListener('mousemove', move); removeEventListener('mouseup', up); try { localStorage.setItem('crew.terms.width', width); } catch {} };
  addEventListener('mousemove', move);
  addEventListener('mouseup', up);
}

// Terminals is the column: a pane for each of the task's shells (ns: their numbers).
export function Terminals({ id, ns, onEnd }) {
  return html`<aside class="terms" style=${width ? { width: width + 'px' } : null}>
    <div class="terms-grip" title="Drag to resize" onMouseDown=${drag}></div>
    ${ns.map((n) => html`<${Term} key=${id + '#' + n} id=${id} n=${n} onEnd=${onEnd} />`)}
  </aside>`;
}

// Term is one shell's pane. onEnd runs when it exits.
function Term({ id, n, onEnd }) {
  const box = useRef();
  const base = `/tasks/${id}/shells/${n}`;
  // close is × and ⌘W: end the shell, then give the keys to the next terminal, or back to the task.
  const close = () => api('DELETE', base).then(onEnd).then(() => setTimeout(() => {
    const to = document.querySelector('.terms .shell-pane textarea') || document.querySelector('.task-page .compose textarea');
    to && to.focus();
  }, 100));
  // ⌘W is caught before xterm sees the key.
  const key = (e) => { if (e.metaKey && !e.shiftKey && !e.altKey && e.key === 'w') { e.preventDefault(); e.stopPropagation(); close(); } };
  useEffect(() => {
    let term, es, ro, gone = false;
    // Keys go one request at a time, in order; what's typed meanwhile goes in the next.
    let pend = '', busy = false;
    const flush = async () => {
      busy = true;
      while (pend) { const d = pend; pend = ''; await api('POST', base + '/in', { Data: d }).catch(() => {}); }
      busy = false;
    };
    loadXterm().then(() => {
      if (gone) return;
      term = new window.Terminal({ fontFamily: colors().mono, fontSize: 13, cursorBlink: true, theme: colors() });
      addEventListener('crew-theme', recolor);
      const fit = new window.FitAddon.FitAddon();
      term.loadAddon(fit);
      term.open(box.current);
      const size = () => { try { fit.fit(); api('POST', base + '/size', { Cols: term.cols, Rows: term.rows }); } catch {} };
      ro = new ResizeObserver(size);
      ro.observe(box.current);
      size();
      term.onData((d) => { pend += d; if (!busy) flush(); });
      es = new EventSource('/api' + base + '/out');
      es.addEventListener('reset', (e) => { term.reset(); term.write(bytes(e.data)); });
      es.onmessage = (e) => term.write(bytes(e.data));
      es.addEventListener('exit', () => { es.close(); onEnd(); });
      term.focus();
    }).catch((e) => { if (!gone && box.current) box.current.textContent = e.message; });
    const recolor = () => { if (term) term.options.theme = colors(); };
    return () => { gone = true; removeEventListener('crew-theme', recolor); ro && ro.disconnect(); es && es.close(); term && term.dispose(); };
  }, [id, n]);
  return html`<section class="shell-pane" onKeyDownCapture=${key}>
    <header><span><b>Terminal ${n}</b></span>
      <button class="ghost small" title="Close this terminal (⌘W)" onClick=${close}>×</button></header>
    <div class="shell-box" ref=${box}></div>
  </section>`;
}
