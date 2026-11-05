// What TASKS' files share: which tasks have a reply you haven't read, money, the PR chip, deleting.
import { html, api, on, ask, openLink } from '../../lib.js';

export const unread = new Set(); // tasks with a reply you haven't opened, shared by the page and the badge
export const money = (n) => (n ? '$' + n.toFixed(2) : '');
// planUse is your Claude plan's use as the API reports it ({session, week}), "session 42% · week 18%";
// '' with none (an API key has no plan limits).
export const planUse = (l) => [['session', l && l.session], ['week', l && l.week]]
  .filter(([, w]) => w).map(([k, w]) => k + ' ' + Math.round(w.used * 100) + '%').join(' · ');
export const typing = () => /input|textarea|select/i.test(document.activeElement.tagName);

// PRChip is a task's PR. Small (a row): its number and ✓ ✗ ● for its checks. Big (the task's header):
// the same in words, with the review.
export function PRChip({ pr, big }) {
  if (!pr) return '';
  const tone = pr.state === 'MERGED' ? 'merged' : pr.state === 'CLOSED' ? '' : pr.checks === 'fail' ? 'bad' : pr.checks === 'pending' ? 'wait' : pr.checks === 'pass' ? 'ok' : '';
  const go = (e) => { e.stopPropagation(); openLink(pr.url); };
  if (!big) {
    const mark = pr.state === 'MERGED' ? 'merged' : pr.state === 'CLOSED' ? 'closed' : { fail: '✗', pending: '●', pass: '✓' }[pr.checks] || '';
    return html`<span class=${'chip pr ' + tone} title="Open on GitHub" onClick=${go}>${pr.draft ? 'draft ' : ''}#${pr.number}${mark ? ' ' + mark : ''}</span>`;
  }
  const parts = [(pr.draft ? 'Draft PR #' : 'PR #') + pr.number];
  if (pr.state !== 'OPEN') parts.push(pr.state.toLowerCase());
  else {
    if (pr.checks) parts.push({ fail: 'checks failed', pending: 'checks running', pass: 'checks pass' }[pr.checks]);
    if (pr.review === 'APPROVED') parts.push('approved');
    if (pr.review === 'CHANGES_REQUESTED') parts.push('changes requested');
  }
  const look = pr.state === 'OPEN' && pr.review === 'CHANGES_REQUESTED' && tone !== 'bad' ? 'wait' : tone; // asked changes outrank green checks
  return html`<button class=${'chip pr big ' + look} title="Open on GitHub" onClick=${go}>${parts.join(' · ')} ↗</button>`;
}

// deleteTask asks first, saying what the worktree would lose, then deletes the task. It gives the
// note to show, or null when you said no.
export async function deleteTask(task) {
  const { loss } = await api('GET', `/tasks/${task.id}/delete`);
  const what = task.started ? (loss ? `Its worktree has ${loss}, which will be lost. ` : 'Its worktree and branch go too. ') : '';
  if (!(await ask(`Delete “${task.title}”? ${what}This can't be undone.`, 'Delete'))) return null;
  return (await api('POST', `/tasks/${task.id}/delete`)).note || '';
}
