// The board: a lane per status, cards you drag between them.
import { html, useState, api, on, useLoad } from '../../lib.js';
import { Row } from './row.js';

// Board is the tasks in a lane per status. Drag a card to another lane to change its status; a
// click opens the task.
// ponytail: no order within a lane (task files have none); add an order field if you want to rank cards.
export function Board({ tasks, open, del }) {
  const { data: statuses } = useLoad(() => api('GET', '/tasks/statuses'));
  const [over, setOver] = useState('');
  const drop = (e, key) => {
    e.preventDefault();
    setOver('');
    const id = e.dataTransfer.getData('text/plain'), t = tasks.find((x) => x.id === id);
    if (t && t.status !== key) api('POST', `/tasks/${id}/status`, { Status: key });
  };
  return html`<div class="board">${(statuses || []).map((s) => {
    const items = tasks.filter((t) => t.status === s.key);
    return html`<section key=${s.key} class=${'lane' + (over === s.key ? ' over' : '')}
        onDragOver=${(e) => { e.preventDefault(); setOver(s.key); }} onDragLeave=${(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(''); }} onDrop=${(e) => drop(e, s.key)}>
      <div class="lane-head"><span class=${'s-' + s.key}>${s.icon}</span> ${s.label} <span class="n">${items.length}</span></div>
      <div class="lane-list">${items.map((t) => html`<div key=${t.id} class="tcard" draggable="true" onDragStart=${(e) => e.dataTransfer.setData('text/plain', t.id)}>
        <${Row} t=${t} on=${false} open=${open} del=${del} /></div>`)}</div>
    </section>`;
  })}</div>`;
}
