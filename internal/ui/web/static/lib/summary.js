// The Summary tab of a task and of a PR: Write summary / Update, the summary as a document with
// chips to its sections, Copy, and when it was written.
import { html, useState, useRef } from '../vendor/preact-htm.js';
import { api, ago, useEvents, useLoad } from './api.js';
import { Markdown } from './markdown.js';
import { Empty } from './ui.js';

// Summary is a task (or a PR) explained for someone catching up: what it is, how it behaves, where it
// stands, how it works and what changed, with diagrams. Claude writes it with crew's summary skill
// (core.SummaryTurn) in the chat id, from the diff, the plan and the chat; Update writes it again.
// working: that chat's agent is busy.
export function Summary({ id, working }) {
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/summary`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  const [err, setErr] = useState('');
  const [copied, setCopied] = useState(false);
  const doc = useRef();
  const has = !!(data && data.summary);
  const write = () => { setErr(''); api('POST', `/tasks/${id}/summary`).catch((e) => setErr(e.message)); };
  const copy = () => navigator.clipboard.writeText(data.summary).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); });
  // The sections, for the chips: the intro (what it is), then each ## heading, in order.
  const sections = has ? ['What it is', ...[...data.summary.matchAll(/^## (.+)$/gm)].map((m) => m[1].trim())] : [];
  const jump = (i) => {
    const el = i === 0 ? doc.current : doc.current && doc.current.querySelectorAll('.h2')[i - 1];
    el && el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };
  return html`<div class="plan-tab">
    <div class="plan-head">
      <button class=${has ? 'small' : 'primary small'} disabled=${working} onClick=${write}
        title="Claude reads the diff, the plan and the chat, then writes the summary; it changes no code">${has ? '↻ Update' : 'Write summary'}</button>
      ${working ? html`<span class="meta"><span class="dot work"></span> Claude is working…</span>`
        : err ? html`<span class="err">${err}</span>`
        : html`<span class="meta">${has ? (data.at ? 'Written ' + (ago(data.at) === 'now' ? 'just now' : ago(data.at) + ' ago') : '') : 'What it is, how it behaves, where it stands, what changed'}</span>`}
      ${has && html`<span class="grow"></span><button class="small" title="Copy as Markdown, for a PR or a message" onClick=${copy}>${copied ? '✓ Copied' : 'Copy'}</button>`}
    </div>
    ${sections.length > 1 && html`<nav class="sum-nav">${sections.map((s, i) => html`<button key=${i} class="fchip" onClick=${() => jump(i)}>${s}</button>`)}</nav>`}
    <div class="scroll">${!data ? '' : has ? html`<div class="doc summary" ref=${doc}><${Markdown} text=${data.summary} /></div>`
      : html`<${Empty} title="No summary yet">${html`<p>Write summary has Claude read the task's diff, plan and chat and explain it: what it's for, where it stands, what changed, and how it works, drawn as diagrams.</p>`}<//>`}</div>
  </div>`;
}

