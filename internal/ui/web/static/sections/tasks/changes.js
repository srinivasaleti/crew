// The Changes tab: the task's diff (core.Changes), read again when the agent's turn ends, drawn with
// the shared DiffView (lib/diff.js).
import { html, api, useEvents, useLoad, DiffView } from '../../lib.js';

export function Changes({ id }) {
  const { data, err, reload } = useLoad(() => api('GET', `/tasks/${id}/changes`), [id]);
  useEvents((ev) => ev.kind === 'turn' && ev.id === id && !ev.busy && reload(), [id]);
  if (err) return html`<div class="scroll"><div class="err">${err}</div></div>`;
  if (!data) return html`<div class="scroll"></div>`;
  return html`<${DiffView} diff=${data.diff} summary=${data.summary} reload=${reload} />`;
}
