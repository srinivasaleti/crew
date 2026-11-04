// The Plan tab: the plan as crew reads it (core.ReadPlan), laid out to scan. On top, how far along it
// is and Rewrite; then Claude's questions as choices you pick and send; what the plan does; the
// steps as rows you can mark (the step being worked on opens, the rest show their first line); the
// notes; Claude's last reply, folded; and a box to change the plan. Claude writes and marks it with
// crew's plan skill (core.PlanTurn); this draws and sends.
import { html, useState, useEffect, api, useEvents, useLoad, ask, openLink, Empty, Markdown } from '../../lib.js';

const ICON = { todo: '○', doing: '◐', done: '✓', blocked: '!' };
const NEXT = { todo: 'doing', doing: 'done', done: 'blocked', blocked: 'todo' };
const SAY = { todo: 'To do', doing: 'In progress', done: 'Done', blocked: 'Blocked' };

// Questions are Claude's questions in the plan: pick a choice (the recommended one is marked) or
// write your own, then send them all at once; Claude updates the plan.
function Questions({ id, qs, busy }) {
  const [picked, setPicked] = useState({});
  const [own, setOwn] = useState({});
  const [err, setErr] = useState('');
  useEffect(() => { setPicked({}); setOwn({}); }, [qs.map((q) => q.text).join()]);
  const answer = (i) => (own[i] && own[i].trim()) || (picked[i] !== undefined ? qs[i].options[picked[i]].replace(/\s*\(recommended[^)]*\)/i, '') : '');
  const n = qs.filter((_, i) => answer(i)).length;
  const useRecommended = () => setPicked(Object.fromEntries(qs.map((q, i) => [i, q.recommended]).filter(([, r]) => r >= 0)));
  const send = () => { setErr(''); api('POST', `/tasks/${id}/plan/answers`, { Answers: qs.map((_, i) => answer(i)) }).catch((e) => setErr(e.message)); };
  return html`<section class="pl-qs">
    <div class="pl-qs-head"><b>Claude needs your call</b><span class="meta">${qs.length} question${qs.length === 1 ? '' : 's'}</span>
      ${qs.some((q) => q.recommended >= 0) && html`<button class="small ghost" onClick=${useRecommended}>Use the recommended</button>`}</div>
    ${qs.map((q, i) => html`<div class="pl-q" key=${i}>
      <div class="pl-q-text"><span class="pl-q-n">${i + 1}</span><${Markdown} text=${q.text} /></div>
      <div class="pl-opts">${q.options.map((o, j) => html`<button key=${j} class=${'pl-opt' + (picked[i] === j && !own[i] ? ' on' : '')}
          onClick=${() => { setPicked({ ...picked, [i]: picked[i] === j ? undefined : j }); setOwn({ ...own, [i]: '' }); }}>
          <span class="pl-radio"></span><${Markdown} text=${o.replace(/\s*\(recommended[^)]*\)/i, '')} />${j === q.recommended && html`<span class="pl-rec" title=${(o.match(/\(recommended:?\s*([^)]*)\)/i) || [])[1] || ''}>recommended</span>`}</button>`)}
        <input class="pl-own" placeholder=${q.options.length ? 'or your own answer' : 'your answer'} value=${own[i] || ''} onInput=${(e) => setOwn({ ...own, [i]: e.target.value })} />
      </div></div>`)}
    <div class="pl-qs-foot">${err ? html`<span class="err">${err}</span>` : html`<span class="meta">${n} of ${qs.length} answered · Claude updates the plan</span>`}
      <button class="primary small" disabled=${!n || busy} onClick=${send}>Send answers</button></div>
  </section>`;
}

// RunBar runs the plan and opens its PRs. You pick how the PRs go, one for the whole plan or one per
// step stacked on each other, and whether the agent stops after each step; Run sends the plan to the
// agent (a commit per step). Once steps have commits, Open PR(s) runs the checks and pushes, on your
// click only.
function RunBar({ task, v, setErr }) {
  const [stacked, setStacked] = useState(v.stacked);
  const [oneStep, setOneStep] = useState(false);
  useEffect(() => setStacked(v.stacked), [task.id, v.stacked]);
  const left = v.steps.filter((s) => s.status !== 'done').length;
  const busy = task.working || v.pushing;
  const run = () => api('POST', `/tasks/${task.id}/plan/run`, { Stacked: stacked, OneStep: oneStep }).then(() => setErr(''), (e) => setErr(e.message));
  const open = async () => {
    const what = stacked ? `open ${v.committed} stacked PR${v.committed === 1 ? '' : 's'} (one per committed step)` : task.pr ? 'push the new commits to its PR' : 'push the branch and open its PR';
    if (!(await ask(`Run the checks, then ${what}?`, stacked ? 'Open PRs' : task.pr ? 'Push' : 'Open PR'))) return;
    api('POST', `/tasks/${task.id}/${stacked ? 'stack' : 'pr'}`).then(() => setErr(''), (e) => setErr(e.message));
  };
  const canOpen = stacked ? v.committed > 0 && !v.problem : task.started;
  return html`<section class="pl-run">
    <div class="pl-run-opts">
      <div class="pl-seg" role="radiogroup" title="How this plan's work goes up for review">
        <button class=${!stacked ? 'on' : ''} onClick=${() => setStacked(false)}><b>One PR</b><span>the whole plan together</span></button>
        <button class=${stacked ? 'on' : ''} onClick=${() => setStacked(true)}><b>A PR per step</b><span>small PRs, each on the one before</span></button>
      </div>
      <label class="pl-pause"><input type="checkbox" checked=${oneStep} onChange=${(e) => setOneStep(e.target.checked)} /> Stop after each step so I can check it</label>
    </div>
    <div class="pl-run-acts">
      ${left > 0 && html`<button class="primary" disabled=${busy} onClick=${run}>▶ ${oneStep ? 'Run the next step' : v.done ? 'Run the rest' : 'Run plan'}</button>`}
      <button disabled=${busy || !canOpen} title=${v.problem || (stacked ? 'Each committed step gets its own PR, stacked' : 'The task branch as one PR')} onClick=${open}>
        ${v.pushing ? 'Checking and pushing…' : stacked ? `Open ${v.committed || ''} stacked PR${v.committed === 1 ? '' : 's'}` : task.pr ? 'Push to PR' : 'Open PR'}</button>
      <span class="meta">${v.problem ? html`<span class="err">${v.problem}</span>`
        : `${v.committed} of ${v.steps.length} steps committed · the checks run before anything is pushed`}</span>
    </div>
  </section>`;
}

// Step is one step: its mark (a click moves it on), its line, and what's under it, folded unless open.
function Step({ s, i, open, toggle, mark }) {
  const first = s.notes.split('\n')[0];
  return html`<div class=${'pl-step ' + s.status + (open ? ' open' : '')}>
    <button class=${'pl-mark ' + s.status} title=${SAY[s.status] + ' · click: ' + SAY[NEXT[s.status]]} onClick=${() => mark(i, NEXT[s.status])}>${ICON[s.status]}</button>
    <div class="pl-step-main" onClick=${() => s.notes && toggle(i)}>
      <div class="pl-step-title"><${Markdown} text=${s.text} />
        ${s.commit && html`<code class="pl-commit" title="its commit">${s.commit}</code>`}
        ${s.pr && html`<button class="chip pl-pr" title=${s.pr} onClick=${(e) => { e.stopPropagation(); openLink(s.pr); }}>PR #${s.pr.split('/').pop()} ↗</button>`}
        ${s.notes && html`<span class="pl-caret">${open ? '▾' : '▸'}</span>`}</div>
      ${s.notes && (open ? html`<div class="pl-notes"><${Markdown} text=${s.notes} /></div>` : html`<div class="pl-peek"><${Markdown} text=${first} /></div>`)}
    </div></div>`;
}

export function Plan({ task }) {
  const id = task.id;
  const { data, reload } = useLoad(() => api('GET', `/tasks/${id}/plan`), [id]);
  useEvents((ev) => (ev.kind === 'turn' || ev.kind === 'task') && ev.id === id && reload(), [id]);
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [opened, setOpened] = useState({}); // step index -> open, as you set it
  const [more, setMore] = useState(false);
  const [showReply, setShowReply] = useState(false);
  useEffect(() => { setOpened({}); setMore(false); setShowReply(false); }, [id]);
  const v = data && data.view;
  const send = (a) => { setErr(''); api('POST', `/tasks/${id}/plan`, { Ask: a }).then(() => setText(''), (e) => setErr(e.message)); };
  const key = (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(text); } };
  const mark = (i, status) => api('POST', `/tasks/${id}/plan/step`, { Index: i, Status: status }).then(reload, (e) => setErr(e.message));
  const reply = task.last && task.last.role === 'agent' ? task.last.text : '';
  // The step in hand (in progress, else the first not done) opens; you can open or fold any.
  const doing = v ? v.steps.findIndex((s) => s.status === 'doing') : -1;
  const now = doing >= 0 ? doing : v ? v.steps.findIndex((s) => s.status !== 'done') : -1;
  const isOpen = (i) => opened[i] ?? i === now;
  const allOpen = v && v.steps.every((s, i) => !s.notes || isOpen(i));
  const pct = v && v.steps.length ? Math.round((v.done / v.steps.length) * 100) : 0;

  return html`<div class="plan-tab">
    <div class="plan-head">
      ${v ? html`<div class="pl-progress" title=${v.done + ' of ' + v.steps.length + ' steps done'}>
          <div class="pl-bar"><div style=${'width:' + pct + '%'}></div></div><span>${v.done}/${v.steps.length} done</span></div>`
        : html`<span class="meta">${task.working ? '' : 'Read first, then plan: no code changes'}</span>`}
      ${task.working && html`<span class="meta"><span class="dot work"></span> Claude is working…</span>`}
      <span class="pl-fill"></span>
      ${v && v.steps.some((s) => s.notes) && html`<button class="small ghost" onClick=${() => setOpened(Object.fromEntries(v.steps.map((_, i) => [i, !allOpen])))}>${allOpen ? 'Fold all' : 'Open all'}</button>`}
      <button class=${v ? 'small' : 'primary small'} disabled=${task.working} title=${v ? 'Claude writes the plan again from the task' : 'Claude reads the code and writes a plan; it changes no code'}
        onClick=${async () => { if (!v || (await ask('Write the plan again from the task? Claude replaces the current plan.', 'Rewrite'))) send(v ? '' : text); }}>${v ? 'Rewrite plan' : 'Write plan'}</button>
    </div>
    <div class="scroll pl">${!data ? '' : !v
      ? html`<${Empty} title="No plan yet">${html`<p>Write plan has Claude read the code and write a short checklist for this task. Say what to plan in the box below, or leave it empty to plan the whole task.</p>`}<//>`
      : html`
        ${v.title && html`<h2 class="pl-title">${v.title}</h2>`}
        ${v.approach && html`<div class="pl-approach"><${Markdown} text=${v.approach} /></div>`}
        ${v.background && html`<button class="ghost small pl-more" onClick=${() => setMore(!more)}>${more ? '▾ Less' : '▸ More context'}</button>${more && html`<div class="pl-bg"><${Markdown} text=${v.background} /></div>`}`}
        ${v.questions.length > 0 && html`<${Questions} id=${id} qs=${v.questions} busy=${task.working} />`}
        <${RunBar} task=${task} v=${v} setErr=${setErr} />
        <div class="pl-steps">${v.steps.map((s, i) => html`<${Step} key=${i} s=${s} i=${i} open=${isOpen(i)} toggle=${(j) => setOpened({ ...opened, [j]: !isOpen(j) })} mark=${mark} />`)}</div>
        ${v.rest && html`<details class="pl-rest"><summary>Notes</summary><${Markdown} text=${v.rest.replace(/^##\s*Notes\s*\n/i, '')} /></details>`}`}</div>
    ${reply && html`<div class=${'plan-reply' + (showReply ? ' open' : '')} onClick=${() => !showReply && setShowReply(true)}>
      <div class="pl-reply-head"><span class="meta">Claude's last reply</span><button class="ghost small" onClick=${(e) => { e.stopPropagation(); setShowReply(!showReply); }}>${showReply ? 'Fold' : 'Show'}</button></div>
      ${showReply ? html`<${Markdown} text=${reply} />` : html`<div class="pl-reply-peek">${reply.replace(/[#*`>|]/g, '').split('\n').find((l) => l.trim())}</div>`}</div>`}
    <div class="compose"><form onSubmit=${(e) => { e.preventDefault(); send(text); }}>
      <textarea rows="1" value=${text} onInput=${(e) => setText(e.target.value)} onKeyDown=${key}
        placeholder=${v ? 'Ask Claude to change the plan…' : 'What to plan (optional: empty plans the whole task)'} />
      <button class="primary">${v ? 'Send' : 'Plan'}</button></form>
      <div class="hint">${err ? html`<span class="err">${err}</span>` : "⏎ sends · ⇧⏎ new line · goes to the task's chat too · click a step's mark to move it on"}</div></div>
  </div>`;
}
