// Small parts every page uses: dialogs, forms, the header, search and list keys.
import { html, render, useState, useEffect, useRef } from '../vendor/preact-htm.js';
import { on } from './api.js';

// ask shows a question with Cancel and an action button, and resolves true when you pick the action.
// It stands in for confirm(), which the app's web view doesn't show (it answers no at once).
export function ask(question, action = 'OK') {
  return new Promise((resolve) => {
    const host = document.body.appendChild(document.createElement('div'));
    const done = (yes) => { render(null, host); host.remove(); resolve(yes); };
    const key = (e) => { if (e.key === 'Enter') { e.preventDefault(); done(true); } };
    render(html`<${Modal} title=${question} onClose=${() => done(false)}>
      <div class="btns" onKeyDown=${key}><button onClick=${() => done(false)}>Cancel</button><button class="primary" autofocus onClick=${() => done(true)}>${action}</button></div><//>`, host);
  });
}

// Modal is a dialog over the page; esc or a click outside closes it.
export function Modal({ title, onClose, children, wide }) {
  useEffect(() => {
    const k = (e) => e.key === 'Escape' && onClose();
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <div class=${'modal' + (wide ? ' wide' : '')}><h2>${title}</h2>${children}</div></div>`;
}

// Form is a modal form: fields is [{key, label, placeholder?, type?: 'textarea'|'select', options?}],
// submit(values) returns a promise; its error shows in the form.
export function Form({ title, fields, initial = {}, submit, onClose, action = 'Save' }) {
  const [v, setV] = useState(initial);
  const [err, setErr] = useState('');
  const [saving, setSaving] = useState(false);
  const go = async (e) => {
    e.preventDefault();
    setSaving(true);
    try { await submit(v); onClose(); } catch (x) { setErr(x.message); setSaving(false); }
  };
  return html`<${Modal} title=${title} onClose=${onClose}><form class="form" onSubmit=${go}>
    ${fields.map((f, i) => html`<label key=${f.key}>${f.label}${f.hint ? html` <span class="meta">${f.hint}</span>` : ''}${
      f.type === 'textarea' ? html`<textarea rows=${f.rows || 4} placeholder=${f.placeholder} value=${v[f.key] || ''} onInput=${(e) => setV({ ...v, [f.key]: e.target.value })}></textarea>`
      : f.type === 'select' ? html`<select value=${v[f.key] || ''} onChange=${(e) => setV({ ...v, [f.key]: e.target.value })}>${f.options.map((o) => html`<option value=${o.value ?? o}>${o.label ?? o}</option>`)}</select>`
      : f.type === 'combo' ? html`<input list=${'dl-' + f.key} autofocus=${i === 0} placeholder=${f.placeholder} value=${v[f.key] || ''} onInput=${(e) => setV({ ...v, [f.key]: e.target.value })} /><datalist id=${'dl-' + f.key}>${(f.options || []).map((o) => html`<option value=${o} />`)}</datalist>`
      : html`<input autofocus=${i === 0} placeholder=${f.placeholder} type=${f.type || 'text'} value=${v[f.key] || ''} onInput=${(e) => setV({ ...v, [f.key]: e.target.value })} />`}</label>`)}
    ${err && html`<div class="err">${err}</div>`}
    <div class="btns"><button type="button" onClick=${onClose}>Cancel</button><button class="primary" disabled=${saving}>${saving ? '…' : action}</button></div>
  </form><//>`;
}

// Empty is a page's placeholder: what's missing and what to do.
export const Empty = ({ title, children }) => html`<div class="empty"><div class="big">${title}</div>${children}</div>`;

// Toolbar is a page's header: title, what it's about, and its actions on the right.
export const Toolbar = ({ title, sub, children }) => html`<header class="bar"><div class="bar-title"><h1>${title}</h1>${sub && html`<span class="meta">${sub}</span>`}</div><div class="bar-actions">${children}</div></header>`;

// Search is a filter box; ⌘F or / focuses it.
export function Search({ value, onInput, placeholder = 'Filter' }) {
  const ref = useRef();
  useEffect(() => {
    const k = (e) => {
      if ((e.metaKey && e.key === 'f') || (e.key === '/' && !/input|textarea|select/i.test(document.activeElement.tagName))) { e.preventDefault(); ref.current && ref.current.focus(); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<input ref=${ref} class="search" placeholder=${placeholder + ' (/)'} value=${value} onInput=${(e) => onInput(e.target.value)} />`;
}

// useListKeys moves a selection through items with ↑↓ (j/k) while focus isn't in a field.
export function useListKeys(items, sel, setSel) {
  useEffect(() => {
    const k = (e) => {
      if (/input|textarea|select/i.test(document.activeElement.tagName) || e.metaKey) return;
      const i = items.findIndex((x) => x === sel);
      if (e.key === 'ArrowDown' || e.key === 'j') { e.preventDefault(); setSel(items[Math.min(i + 1, items.length - 1)]); }
      if (e.key === 'ArrowUp' || e.key === 'k') { e.preventDefault(); setSel(items[Math.max(i - 1, 0)]); }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [items, sel]);
}

// css loads the stylesheet beside a section's module once: css(import.meta.url) for its style.css.
export function css(moduleURL, file = 'style.css') {
  const href = new URL(file, moduleURL).href;
  if (![...document.styleSheets].some((s) => s.href === href)) document.head.append(Object.assign(document.createElement('link'), { rel: 'stylesheet', href }));
}

// Menu is a button that opens a list of actions.
export function Menu({ label, title, items }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    setTimeout(() => addEventListener('click', close), 0);
    return () => removeEventListener('click', close);
  }, [open]);
  return html`<span class="menu-wrap"><button title=${title} onClick=${() => setOpen(!open)}>${label}</button>
    ${open && html`<div class="menu right">${items.map((it) => html`<div key=${it.label} class=${'menu-item' + (it.danger ? ' danger' : '')} onClick=${it.run}>${it.label}${it.key && html`<span class="meta">${it.key}</span>`}</div>`)}</div>`}</span>`;
}
