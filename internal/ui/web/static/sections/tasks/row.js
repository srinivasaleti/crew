// A task in the list or on the board: status, title, the chat's last word, where it runs, its PR.
import { html, on, base, ago, Markdown } from '../../lib.js';
import { unread, money, PRChip } from './common.js';

// preview is a message as one line: who said it, and its start without Markdown.
function preview(m, agent) {
  if (!m) return '';
  const text = m.text.replace(/```[\s\S]*?```/g, ' ').replace(/[#*`>|_]/g, '').replace(/\s+/g, ' ').trim();
  return (m.role === 'you' ? 'You: ' : m.role === 'error' ? '⚠ ' : agent + ': ') + text;
}

const trash = html`<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 4h11M6 4V2.5h4V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5"/></svg>`;

// Row is one task in the list: its status (or a pulse while it works), its title and when it last
// moved, the chat's last word, then a quiet line of where it runs and what it cost, with chips only
// for what wants a look (its PR, a question for you).
export function Row({ t, on, open, del }) {
  const agent = t.agent === 'gemini' ? 'Gemini' : 'Claude';
  const needs = t.asks && !t.working;
  const last = t.working ? '' : t.last ? preview(t.last, agent) : '';
  const where = [base(t.repo) + (t.branch ? ' · ' + t.branch : ''), t.plan && 'plan ' + t.plan, money(t.cost)].filter(Boolean).join(' · ');
  const cls = 'trow' + (on ? ' on' : '') + (unread.has(t.id) ? ' unread' : '') + (t.archived ? ' dim' : '') + (t.status === 'done' ? ' done' : '');
  return html`<div class=${cls} onClick=${() => open(t.id)}>
    <span class=${'status s-' + t.status}>${t.working ? html`<span class="dot work"></span>` : t.icon}</span>
    <div class="trow-main">
      <div class="trow-top"><span class="trow-title" title=${t.title}>${t.parent ? html`<span class="meta">↳ </span>` : ''}${t.title}</span>
        ${unread.has(t.id) && html`<span class="dot" title="new reply"></span>`}<span class="meta trow-ago">${ago(t.updated)}</span>
        <button class="ghost small trow-del" title="Delete task" onClick=${(e) => { e.stopPropagation(); del(t); }}>${trash}</button></div>
      <div class="trow-last" title=${last}>${t.working ? html`<i>${agent} is working…</i>` : last || html`<i>not started</i>`}</div>
      <div class="trow-foot"><span class="trow-where">${where}</span><${PRChip} pr=${t.pr} />${needs && html`<span class="chip wait">needs you</span>`}</div>
    </div>
  </div>`;
}
