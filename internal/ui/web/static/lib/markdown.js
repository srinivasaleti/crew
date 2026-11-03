// Markdown for agent replies and GitHub/Jira comments, and links that open in your browser.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js';
import { on, openLink } from './api.js';
import { Modal } from './ui.js';

// Link opens in your browser (the window stays on crew).
export function Link({ href, children }) {
  return html`<a href=${href} onClick=${(e) => { e.preventDefault(); openLink(href); }}>${children || href}</a>`;
}

// inline is a line's Markdown: `code`, **bold**, *em*, [text](url) and bare links.
function inline(text) {
  const out = [];
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(\[[^\]]+\]\([^)\s]+\))|(https?:\/\/[^\s)]+)/g;
  let at = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > at) out.push(text.slice(at, m.index));
    const t = m[0];
    if (m[1]) out.push(html`<code>${t.slice(1, -1)}</code>`);
    else if (m[2]) out.push(html`<b>${t.slice(2, -2)}</b>`);
    else if (m[3]) out.push(html`<i>${t.slice(1, -1)}</i>`);
    else if (m[4]) { const [, label, url] = t.match(/\[([^\]]+)\]\(([^)]+)\)/); out.push(html`<${Link} href=${url}>${label}<//>`); }
    else out.push(html`<${Link} href=${t} />`);
    at = m.index + t.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
}

// Markdown draws an agent's reply or a GitHub/Jira comment: code blocks, headings, lists, quotes,
// tables kept as text, paragraphs.
export function Markdown({ text, live }) {
  const blocks = [];
  const lines = (text || '').split('\n');
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (l.startsWith('```')) {
      const code = [];
      for (i++; i < lines.length && !lines[i].startsWith('```'); i++) code.push(lines[i]);
      blocks.push(l.slice(3).trim() === 'mermaid' && !live ? html`<${Diagram} src=${code.join('\n')} />` : html`<pre>${code.join('\n')}</pre>`);
    } else if (/^#{1,6} /.test(l)) {
      const n = l.match(/^#+/)[0].length;
      blocks.push(html`<div class=${'h h' + Math.min(n, 3)}>${inline(l.slice(n + 1))}</div>`);
    } else if (/^\s*([-*]|\d+\.) /.test(l)) {
      const items = [];
      const ordered = /^\s*\d+\./.test(l);
      for (; i < lines.length && /^\s*([-*]|\d+\.) /.test(lines[i]); i++) items.push(html`<li>${inline(lines[i].replace(/^\s*([-*]|\d+\.) /, ''))}</li>`);
      i--;
      blocks.push(ordered ? html`<ol>${items}</ol>` : html`<ul>${items}</ul>`);
    } else if (l.startsWith('>')) {
      blocks.push(html`<blockquote>${inline(l.replace(/^>\s?/, ''))}</blockquote>`);
    } else if (l.startsWith('|')) {
      const rows = [];
      for (; i < lines.length && lines[i].startsWith('|'); i++) rows.push(lines[i]);
      i--;
      blocks.push(html`<pre class="table">${rows.join('\n')}</pre>`);
    } else if (l.trim()) {
      const para = [l];
      for (; i + 1 < lines.length && lines[i + 1].trim() && !/^(```|#{1,6} |\s*([-*]|\d+\.) |>|\|)/.test(lines[i + 1]); i++) para.push(lines[i + 1]);
      blocks.push(html`<p>${inline(para.join('\n'))}</p>`);
    }
  }
  return html`<div class=${'md' + (live ? ' live' : '')}>${blocks}</div>`;
}

// mermaid loads once, the first time a diagram shows (its script sets window.mermaid).
let mermaid;
const loadMermaid = () => (mermaid ||= new Promise((ok, fail) => {
  const s = Object.assign(document.createElement('script'), { src: 'vendor/mermaid.min.js' });
  s.onload = () => ok(window.mermaid);
  s.onerror = () => fail(new Error("couldn't load the diagram library"));
  document.head.append(s);
}));
let drawn = 0;

// Diagram draws a ```mermaid block in the page's theme, again when the theme changes. Strict mode:
// the text is an agent's, so mermaid sanitises it and allows no scripts or clicks. A diagram that
// doesn't parse shows its source and why. A click opens it large.
function Diagram({ src }) {
  const box = useRef();
  const [err, setErr] = useState('');
  const [svg, setSvg] = useState('');
  const [big, setBig] = useState(false);
  useEffect(() => {
    let gone = false;
    const draw = () => loadMermaid().then((m) => {
      m.initialize({ startOnLoad: false, securityLevel: 'strict', theme: document.documentElement.dataset.theme === 'light' ? 'default' : 'dark', fontFamily: 'Lato, sans-serif' });
      return m.render('diagram-' + ++drawn, src);
    }).then(({ svg }) => { if (!gone && box.current) { box.current.innerHTML = svg; setSvg(svg); setErr(''); } }, (e) => !gone && setErr(String(e.message || e)));
    draw();
    addEventListener('crew-theme', draw);
    return () => { gone = true; removeEventListener('crew-theme', draw); };
  }, [src]);
  if (err) return html`<div class="diagram-err"><pre>${src}</pre><div class="meta">Couldn't draw this diagram: ${err}</div></div>`;
  return html`<div class="diagram" ref=${box} title="Click to open it large" onClick=${() => svg && setBig(true)}></div>
    ${big && html`<${Modal} title="Diagram" wide=${true} onClose=${() => setBig(false)}><div class="diagram big" dangerouslySetInnerHTML=${{ __html: svg }}></div><//>`}`;
}
