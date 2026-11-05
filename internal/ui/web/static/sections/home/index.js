// Home: a greeting, your day at a glance (from the sections' own lists: no tokens), prompts to start
// from, and a chat about anything in crew and what you're connected to: tasks, to-dos, reminders and
// notes (crew's files), PRs (gh), Jira (acli), meetings and mail (MCP), and crew itself (crew:help).
// It only reads; a task it proposes is made when you click Create task (agents propose, you apply).
// Setup and sections are on Settings.
import { html, useState, api, useLoad, useEvents, Chat, Empty, css } from '../../lib.js';
import { sections } from '../index.js';

css(import.meta.url);

// PROMPTS start a conversation in one click; one ending in a space goes in the box for you to finish.
const PROMPTS = [
  ['📅', "What's on my calendar today?"],
  ['👀', 'Which PRs are waiting for my review?'],
  ['🎯', 'What Jira tickets are assigned to me?'],
  ['✅', "What's due on my to-do list this week?"],
  ['🧭', 'How do I get the most out of crew?'],
  ['✨', 'Create a task to '],
];

const greeting = () => { const h = new Date().getHours(); return h < 5 ? 'Up late' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'; };
const today = () => new Date().toLocaleDateString([], { weekday: 'long', day: 'numeric', month: 'long' });
const ymd = (d) => d.toLocaleDateString('en-CA'); // YYYY-MM-DD in your timezone

// useGlance is your day from each section that's on, read the way its page reads it (free).
function useGlance(on) {
  const get = (key, path) => (on.includes(key) ? api('GET', path).catch(() => null) : Promise.resolve(null));
  const tasks = useLoad(() => get('tasks', '/tasks'), [on.join()]);
  const prs = useLoad(() => get('prs', '/prs'), [on.join()]);
  const jira = useLoad(() => get('jira', '/jira'), [on.join()]);
  const todos = useLoad(() => get('todos', '/todos'), [on.join()]);
  const rems = useLoad(() => get('reminders', '/reminders'), [on.join()]);
  useEvents((ev) => { if (ev.kind === 'task' || (ev.kind === 'turn' && !ev.busy)) tasks.reload(); if (ev.kind === 'reminders') rems.reload(); });
  const cards = [];
  const t = (tasks.data || []).filter((x) => !x.archived);
  if (on.includes('tasks')) {
    const working = t.filter((x) => x.working).length, waiting = t.filter((x) => x.asks && !x.working).length;
    cards.push({ key: 'tasks', n: working, label: working === 1 ? 'agent working' : 'agents working', sub: waiting ? `${waiting} need${waiting === 1 ? 's' : ''} your answer` : `${t.length} task${t.length === 1 ? '' : 's'}`, hot: waiting > 0 });
  }
  if (prs.data) {
    const review = (prs.data.items || []).filter((p) => p.Review).length, mine = (prs.data.items || []).filter((p) => p.Mine).length;
    cards.push({ key: 'prs', n: review, label: 'PRs to review', sub: `${mine} of yours open`, hot: review > 0 });
  }
  if (jira.data && jira.data.ok !== false) {
    const issues = jira.data.issues || [];
    cards.push({ key: 'jira', n: issues.length, label: 'Jira open', sub: `${issues.filter((i) => i.Category === 'indeterminate').length} in progress` });
  }
  if (todos.data) {
    const d = todos.data.today || ymd(new Date());
    const open = (todos.data.items || []).filter((i) => !i.done);
    const late = open.filter((i) => i.due && i.due < d).length;
    cards.push({ key: 'todos', n: open.filter((i) => i.due && i.due <= d).length, label: 'to-dos due', sub: late ? `${late} overdue` : `${open.length} open`, hot: late > 0 });
  }
  if (rems.data) {
    const next = (rems.data.reminders || []).filter((r) => !r.done && !r.paused).sort((a, b) => new Date(a.at) - new Date(b.at))[0];
    cards.push({ key: 'reminders', n: next ? next.when.replace(/^Today /, '') : '–', label: next ? next.text : 'no reminders', sub: next ? 'next reminder' : '', text: true });
  }
  return { cards, tasks: tasks.data || [], reloadTasks: tasks.reload };
}

// TaskCard is a task the chat proposed (core.TaskProposal): Create task makes it, with crew's own rules.
function TaskCard({ p, tasks, created, go }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const made = tasks.find((t) => t.title === p.title);
  const open = (id) => { location.hash = 'tasks/' + id; go('tasks'); };
  const create = async () => {
    setBusy(true); setErr('');
    try { const t = await api('POST', '/tasks', { Title: p.title, Repo: p.repo, Body: p.body, Issue: p.issue && /^[A-Z][A-Z0-9]+-\d+$/.test(p.issue) ? p.issue : '' }); created(); open(t.id); }
    catch (x) { setErr(x.message); } finally { setBusy(false); }
  };
  return html`<div class="task-card">
    <div class="tc-head"><span class="tc-kind">New task</span>${p.repo && html`<span class="chip">${p.repo.split('/').pop()}</span>`}${p.issue && html`<span class="chip">${p.issue}</span>`}</div>
    <div class="tc-title">${p.title}</div>
    ${p.body && html`<div class="tc-body">${p.body}</div>`}
    <div class="tc-foot">${made ? html`<span class="meta">✓ Created</span><button class="small" onClick=${() => open(made.id)}>Open task →</button>`
      : html`<button class="primary small" disabled=${busy} onClick=${create}>${busy ? 'Creating…' : 'Create task'}</button><span class="meta">crew makes it; the agent starts when you send its first message</span>`}
      ${err && html`<span class="err">${err}</span>`}</div></div>`;
}

export default function Home({ go }) {
  const chat = useLoad(() => api('GET', '/home/chat'));
  const secs = useLoad(() => api('GET', '/sections'));
  useEvents((ev) => ev.kind === 'sections' && secs.reload());
  const on = (secs.data || []).filter((s) => s.on).map((s) => s.key);
  const { cards, tasks, reloadTasks } = useGlance(on);
  const [draft, setDraft] = useState(null); // {text}: what a prompt puts in the box
  const id = chat.data && chat.data.id;
  const ask = (q) => (q.endsWith(' ') ? setDraft({ text: q }) : api('POST', `/tasks/${id}/messages`, { Text: q }));
  // The chat's replies: a ```crew-task block becomes a card under the text.
  const proposals = (text) => {
    const found = [];
    const shown = text.replace(/```crew-task\s*\n([\s\S]*?)```/g, (_, j) => { try { found.push(JSON.parse(j)); return ''; } catch { return _; } });
    return [shown.trim(), found.length ? found.map((p, i) => html`<${TaskCard} key=${i} p=${p} tasks=${tasks} created=${reloadTasks} go=${go} />`) : null];
  };
  return html`<main class="page home">
    <header class="hero">
      <div class="hero-text"><h1>${greeting()}</h1><div class="meta">${today()} · ask about your work, or anything in crew</div></div>
      <button class="small ghost" title="Settings: setup and sections" onClick=${() => go('settings')}>⚙ Settings</button>
    </header>
    ${cards.length > 0 && html`<div class="glance">${cards.map((c) => html`<button key=${c.key} class=${'glance-card' + (c.hot ? ' hot' : '')} title=${c.label + (c.sub ? ' · ' + c.sub : '') + ' · open ' + sections[c.key].title} onClick=${() => go(c.key)}>
        <span class="g-icon" dangerouslySetInnerHTML=${{ __html: sections[c.key].icon }}></span>
        <span class=${'g-n' + (c.text ? ' small' : '')}>${c.n}</span><span class="g-label" title=${c.label}>${c.label}</span><span class="meta g-sub">${c.sub}</span></button>`)}</div>`}
    ${id && html`<div class="prompts">${PROMPTS.map(([icon, q]) => html`<button key=${q} class="prompt" onClick=${() => ask(q)}><span>${icon}</span>${q.trim()}${q.endsWith(' ') ? '…' : ''}</button>`)}</div>`}
    ${chat.err ? html`<${Empty} title="The chat didn't open">${chat.err}<//>`
      : !id ? html`<div class="empty">Loading…</div>`
      : html`<${Chat} id=${id} base=${'/tasks/' + id} agent="crew" cards=${proposals} draft=${draft}
          placeholder="Ask about your meetings, PRs, Jira, tasks, to-dos, or how crew works…"
          empty="Pick a prompt above or ask anything. It reads your tasks, to-dos, reminders and notes, your PRs and Jira, and your calendar and mail through MCP. It can also explain crew, and propose a task you create with one click." />`}
  </main>`;
}
