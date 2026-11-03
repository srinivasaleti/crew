// FOCUS: the timer as a ring, what's next, today and this week, its settings, and the break screen
// over the whole window when a break is on. The timer is crew's (internal/focus); this draws its state
// and counts down between crew's events.
import { html, useState, useEffect, api, on, useEvents, Toolbar, Form, css } from '../../lib.js';

css(import.meta.url);

const MIN = 6e10; // a minute, in the nanoseconds crew's durations come in
const ms = (ns) => ns / 1e6;
const at = (t) => (t && !t.startsWith('0001') ? new Date(t).getTime() : 0);
const pad = (n) => String(n).padStart(2, '0');
// countdown writes ms as "18:42".
const countdown = (d) => { const s = Math.max(Math.round(d / 1000), 0); return Math.floor(s / 60) + ':' + pad(s % 60); };
// dur writes ms the short way: "2h 05m", "6h", "25m", "40s".
const dur = (d) => {
  const s = Math.round(Math.max(d, 0) / 1000), h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60;
  if (h && !m) return h + 'h';
  if (h) return h + 'h ' + pad(m) + 'm';
  if (s >= 60) return Math.floor(s / 60) + 'm';
  return s + 's';
};
const left = (c, now) => Math.max(at(c.ends) - now, 0);
const flow = (c) => c.kind === 'focus' && !at(c.ends);

let last = null; // the last state crew sent, shared by the page and the badge
const listeners = new Set();
const take = (st) => { last = st; listeners.forEach((l) => l(st)); };
const load = () => api('GET', '/focus').then(take);
const act = (action, body) => api('POST', '/focus/' + action, body).then(take);
on((ev) => {
  if (ev.kind === 'focus') take(ev.data);
  if (ev.kind === 'open') load();
});

// useFocus is crew's FOCUS state, and the time now, ticking every second.
function useFocus() {
  const [st, setSt] = useState(last);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    listeners.add(setSt);
    load();
    const iv = setInterval(() => setNow(Date.now()), 1000);
    return () => { listeners.delete(setSt); clearInterval(iv); };
  }, []);
  return [st, now];
}

// watch shows the time left on the rail while a session or break runs.
export function watch(setBadge) {
  const show = () => {
    if (!last || !last.clock.kind) return setBadge('');
    const c = last.clock;
    setBadge(flow(c) ? '●' : Math.ceil(left(c, Date.now()) / 60000) + 'm');
  };
  listeners.add(show);
  load().catch(() => {});
  const iv = setInterval(show, 15000);
  return () => { listeners.delete(show); clearInterval(iv); };
}

// Ring is the timer: how far through the session or break, and the time.
function Ring({ st, now }) {
  const c = st.clock, s = st.settings;
  let frac = 0, big = '', label = '';
  if (flow(c)) { big = countdown(now - at(c.start)); label = 'Flow'; frac = ((now - at(c.start)) % (60 * 60000)) / (60 * 60000); }
  else if (c.kind) {
    const total = at(c.ends) - at(c.start);
    big = countdown(left(c, now));
    frac = total > 0 ? 1 - left(c, now) / total : 0;
    label = c.kind === 'break' ? c.label : `Focus · ${(c.done || 0) + 1} of ${s.long_every}`;
  } else { big = s.mode === 'flow' ? '0:00' : countdown(ms(s.focus)); label = s.mode === 'flow' ? 'Flow, when you start' : 'Ready'; }
  const kind = c.kind === 'break' ? 'brk' : c.kind ? 'on' : 'idle';
  return html`<div class=${'ring ' + kind} style=${'--p:' + (frac * 100).toFixed(2) + '%'}>
    <div class="ring-in"><div class="ring-time">${big}</div><div class="ring-label">${label}</div></div></div>`;
}

// Next is what comes after: a put-off break, the move and eye breaks, the end of the day.
function Next({ st, now }) {
  const s = st.settings, items = [];
  if (at(st.later.start)) items.push(st.later.label + ' in ' + dur(at(st.later.start) - now));
  if (s.move > 0) items.push(st.moveDue ? 'move now' : 'move in ' + dur(ms(st.moveIn)));
  if (s.eye > 0) items.push(now < at(st.eyeUntil) ? 'look 20 ft away · ' + Math.ceil((at(st.eyeUntil) - now) / 1000) + 's' : 'eyes in ' + dur(ms(st.eyeIn)));
  if (s.end_of_day) {
    const [h, m] = s.end_of_day.split(':').map(Number), end = new Date(); end.setHours(h, m, 0, 0);
    items.push(now < end ? `end of day ${s.end_of_day} (${dur(end - now)})` : `end of day ${s.end_of_day} · wrap up`);
  }
  if (!items.length) items.push('no breaks set · Settings turns them on');
  return html`<div class="next"><div class="next-h">Up next</div>${items.map((t) => html`<div key=${t} class=${'next-i' + (/now|wrap|look/.test(t) ? ' hot' : '')}>${t}</div>`)}</div>`;
}

// Today is the day so far: screen time against the limit, focus, sessions and breaks.
function Today({ st }) {
  const d = st.today, lim = ms(st.settings.limit), active = ms(d.crew);
  const pct = lim ? Math.min(active / lim, 1) * 100 : 0;
  return html`<div class="cards">
    <div class="card wide"><div class="card-k">${st.whole ? 'Mac today' : 'In crew today'}</div>
      <div class="card-v">${dur(active)}${lim ? html`<span class="meta"> of ${dur(lim)}</span>` : ''}</div>
      ${lim ? html`<div class=${'meter' + (active >= lim ? ' over' : active >= lim * 0.8 ? ' warn' : '')}><div style=${'width:' + pct + '%'}></div></div>` : ''}
      ${!st.whole && html`<div class="meta">counts while crew is in front (the Mac's idle time isn't readable)</div>`}</div>
    <div class="card"><div class="card-k">Focus</div><div class="card-v">${dur(ms(d.focus))}</div></div>
    <div class="card"><div class="card-k">Sessions</div><div class="card-v">${d.sessions}</div></div>
    <div class="card"><div class="card-k">Breaks</div><div class="card-v">${d.breaks || 0}<span class="meta">${d.skipped ? ' · ' + d.skipped + ' skipped' : ''}</span></div></div>
  </div>`;
}

// Week is the last seven days' screen time as bars, with the limit as a line.
function Week({ st }) {
  const days = st.week || [], lim = ms(st.settings.limit);
  const most = Math.max(lim, ...days.map((d) => ms(d.crew)), 1);
  return html`<div class="week">
    ${lim ? html`<div class="limit-area"><div class="limit" style=${'bottom:' + (lim / most) * 100 + '%'}><span>${dur(lim)} a day</span></div></div>` : ''}
    ${days.map((d) => {
      const v = ms(d.crew), day = new Date(d.date + 'T12:00');
      return html`<div class="wk" key=${d.date} title=${dur(v) + ' · ' + d.sessions + ' sessions'}>
        <div class="wk-bar"><div class=${lim && v > lim ? 'over' : ''} style=${'height:' + (v / most) * 100 + '%'}></div></div>
        <div class="wk-v">${v ? dur(v) : ''}</div><div class="meta">${day.toLocaleDateString([], { weekday: 'short' })}</div></div>`;
    })}</div>`;
}

// BreakScreen covers the window while a break is offered, on, or just over.
function BreakScreen({ st, now }) {
  const [sure, setSure] = useState(false);
  const c = st.clock;
  useEffect(() => {
    const k = (e) => {
      if (st.screen === 'ask' && e.key === 'Enter') act('take');
      if (st.screen === 'ask' && /^[123]$/.test(e.key) && st.extrasLeft > 0) act('extend', { Index: +e.key - 1 });
      if (st.screen === 'back' && e.key === 'Enter') act('start', { Minutes: 0 });
      if (st.screen === 'back' && e.key === 'Escape') act('notyet');
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [st.screen, st.extrasLeft]);
  let body;
  if (st.screen === 'ask') {
    const d = at(st.due.ends) - at(st.due.start);
    body = html`<div class="bs-dot">●</div><h2>${st.why}</h2>
      <p class="bs-say">Time for a ${dur(d)} ${st.due.label.toLowerCase()}. ${st.advice}.</p>
      <button class="primary big" onClick=${() => act('take')}>Start the break <kbd>⏎</kbd></button>
      ${st.extrasLeft > 0 ? html`<div class="bs-more">Keep going: ${st.extras.map((m, i) => html`<button onClick=${() => act('extend', { Index: i })}>+${m} min <kbd>${i + 1}</kbd></button>`)}</div>`
        : html`<div class="meta">no more extensions</div>`}
      <div class="meta">${st.extrasLeft > 0 ? `you can keep going ${st.extrasLeft} more time${st.extrasLeft === 1 ? '' : 's'}` : ''}${st.settings.auto ? ' · the break starts on its own in ' + countdown(ms(st.autoBreak) - (now - at(st.asked))) : ''}</div>`;
  } else if (st.screen === 'break') {
    const total = at(c.ends) - at(c.start), l = left(c, now);
    body = html`<div class="bs-dot brk">◐</div><h2>${c.label}</h2><div class="bs-time">${countdown(l)}</div>
      <p class="bs-say">${st.advice}</p>
      <div class="bs-bar"><div style=${'width:' + (total ? (1 - l / total) * 100 : 0) + '%'}></div></div>
      ${sure ? html`<div class="bs-more">End the break early? <button class="danger" onClick=${() => act('endearly')}>End it</button><button onClick=${() => setSure(false)}>Keep it</button></div>`
        : html`<button class="ghost" onClick=${() => setSure(true)}>End early</button>`}`;
  } else {
    body = html`<div class="bs-dot ok">✓</div><h2>Break done</h2><p class="bs-say">Ready for the next one?</p>
      <div class="bs-more"><button class="primary big" onClick=${() => act('start', { Minutes: 0 })}>Start the next focus <kbd>⏎</kbd></button>
        <button onClick=${() => act('notyet')}>Not yet <kbd>esc</kbd></button></div>
      ${st.settings.auto && html`<div class="meta">the next focus starts on its own ${st.here ? 'in ' + countdown(ms(st.autoFocus) - (now - at(st.backAt))) : "once you're back at the Mac"}</div>`}`;
  }
  return html`<div class=${'breakscreen ' + st.screen}><div class="bs-box">${body}</div></div>`;
}

// settingsFields are the settings form's fields; minutes (and hours for the limit) stand for crew's
// durations, and 0 turns a break or the limit off.
const yesNo = [{ value: 'yes', label: 'Yes' }, { value: 'no', label: 'No' }];
const settingsFields = [
  { key: 'mode', label: 'Mode', type: 'select', options: [{ value: 'pomodoro', label: 'Pomodoro (counts down)' }, { value: 'flow', label: 'Flow (counts up)' }] },
  { key: 'focus', label: 'Focus session (minutes)', type: 'number' },
  { key: 'break', label: 'Short break (minutes)', type: 'number' },
  { key: 'long', label: 'Long break (minutes)', type: 'number' },
  { key: 'long_every', label: 'A long break after this many sessions', type: 'number' },
  { key: 'move', label: 'Move break every (minutes at the Mac, 0 off)', type: 'number' },
  { key: 'move_for', label: 'Move break length (minutes)', type: 'number' },
  { key: 'eye', label: 'Eye break every (minutes, 0 off)', type: 'number' },
  { key: 'break_screen', label: 'Break screen', type: 'select', options: [{ value: 'full', label: "Full screen, over every app" }, { value: 'crew', label: "In crew's window" }, { value: 'off', label: 'Off (notifications only)' }] },
  { key: 'auto', label: 'Run the cycle on its own', type: 'select', options: yesNo },
  { key: 'on_open', label: 'Opening crew starts a session', type: 'select', options: yesNo },
  { key: 'limit', label: 'Daily screen time limit (hours, 0 off)', type: 'number' },
  { key: 'end_of_day', label: 'End of day (empty for none)', type: 'time' },
  { key: 'dnd_on', label: 'Shortcut to run when a session starts (Do Not Disturb)' },
  { key: 'dnd_off', label: 'Shortcut to run when it ends' },
];
const mins = ['focus', 'break', 'long', 'move', 'move_for', 'eye'];
const toForm = (s) => {
  const v = { ...s, auto: s.auto ? 'yes' : 'no', on_open: s.on_open ? 'yes' : 'no', limit: String(s.limit / (60 * MIN)) };
  mins.forEach((k) => (v[k] = String(Math.round(s[k] / MIN))));
  v.long_every = String(s.long_every);
  return v;
};
const fromForm = (v) => {
  const s = { ...v, auto: v.auto === 'yes', on_open: v.on_open === 'yes', limit: Math.round(+v.limit * 60) * MIN, long_every: +v.long_every || 1 };
  mins.forEach((k) => (s[k] = Math.round(+v[k] || 0) * MIN));
  return s;
};

export default function Focus() {
  const [st, now] = useFocus();
  const [editing, setEditing] = useState(false);
  useEffect(() => {
    const k = (e) => {
      if (!st || st.screen || editing || e.metaKey || /input|textarea|select/i.test(document.activeElement.tagName)) return;
      const running = !!st.clock.kind;
      if (e.key === 's') act(running ? 'stop' : 'start', running ? undefined : { Minutes: 0 });
      if (e.key === 'b') act('break');
      if (e.key === 'e') setEditing(true);
      if ((e.key === '+' || e.key === '=') && running) act('nudge', { Minutes: 5 });
      if (e.key === '-' && running) act('nudge', { Minutes: -5 });
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [st, editing]);
  if (!st) return html`<main class="page"><div class="empty">Loading…</div></main>`;
  const c = st.clock, s = st.settings, running = !!c.kind;
  const sub = `${st.whole ? 'Mac' : 'In crew'} today ${dur(ms(st.today.crew))}${s.limit ? ' of ' + dur(ms(s.limit)) : ''}`;
  const state = c.kind === 'break' ? 'On a break' : flow(c) ? 'In flow' : c.kind ? 'Focusing' : 'Ready to focus';
  const say = c.kind === 'break' ? st.advice || 'Step away from the screen.' : c.kind ? (flow(c) ? 'Counting up until you stop.' : 'Ends at ' + new Date(at(c.ends)).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) + '.')
    : s.mode === 'flow' ? 'Flow counts up until you stop.' : 'Pick a length, or start your usual ' + dur(ms(s.focus)) + '.';
  return html`<main class="page focus">
    <${Toolbar} title="Focus" sub=${sub}>
      <button class="small" title="Settings (e)" onClick=${() => setEditing(true)}>Settings</button><//>
    <div class="scroll">
      ${!st.on && html`<div class="notice">FOCUS is turned off on home, so nothing counts.</div>`}
      <section class=${'hero ' + (c.kind === 'break' ? 'brk' : c.kind ? 'on' : 'idle')}>
        <${Ring} st=${st} now=${now} />
        <div class="hero-side">
          <div class="hero-state">${state}</div><div class="meta hero-say">${say}</div>
          ${!running && html`<div class="seg">${['pomodoro', 'flow'].map((m) => html`<button class=${s.mode === m ? 'on' : ''} onClick=${() => act('settings', { Settings: { ...s, mode: m } })}>${m === 'flow' ? 'Flow' : 'Pomodoro'}</button>`)}</div>`}
          <div class="controls">
            ${running ? html`<button class="big" onClick=${() => act('stop')}>${c.kind === 'break' ? 'Skip break' : 'Stop'} <kbd>s</kbd></button>`
              : html`<button class="primary big" onClick=${() => act('start', { Minutes: 0 })}>Start ${s.mode === 'flow' ? 'flow' : dur(ms(s.focus))} <kbd>s</kbd></button>`}
            ${c.kind !== 'break' && html`<button onClick=${() => act('break')}>Break now <kbd>b</kbd></button>`}
          </div>
          ${!running && s.mode !== 'flow' && html`<div class="presets"><span class="meta">or</span>${[15, 25, 45, 60].filter((m) => m * MIN !== s.focus).map((m) => html`<button key=${m} class="small" onClick=${() => act('start', { Minutes: m })}>${m} min</button>`)}</div>`}
          ${running && !flow(c) && html`<div class="presets">
            <button class="small" title="5 minutes less (-)" onClick=${() => act('nudge', { Minutes: -5 })}>−5 min</button><button class="small" title="5 minutes more (+)" onClick=${() => act('nudge', { Minutes: 5 })}>+5 min</button>
            ${c.kind === 'focus' && (c.extra || 0) < 2 && html`<button class="small" title="Add 15 minutes (twice at most)" onClick=${() => act('extend', { Index: 1 })}>Keep going +15</button>`}</div>`}
          ${st.notice && html`<div class="meta">${st.notice}</div>`}
          <${Next} st=${st} now=${now} />
        </div>
      </section>
      <h3>Today</h3><${Today} st=${st} />
      <h3>This week</h3><${Week} st=${st} />
    </div>
    ${st.screen && html`<${BreakScreen} st=${st} now=${now} />`}
    ${editing && html`<${Form} title="FOCUS settings" fields=${settingsFields} initial=${toForm(s)} onClose=${() => setEditing(false)}
      submit=${(v) => act('settings', { Settings: fromForm(v) })} />`}
  </main>`;
}
