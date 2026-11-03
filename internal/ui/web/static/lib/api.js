// Talking to crew: the API, the one event stream, loading, and small formatters.
import { useState, useEffect, useRef } from '../vendor/preact-htm.js';

// api calls crew; a failure comes back as an Error with crew's own words.
export async function api(method, path, body) {
  const res = await fetch('/api' + path, { method, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// One event stream for the whole page; on(fn) listens until the returned function is called.
// fn({kind: 'open'}) runs on every (re)connect, so a page can reload what it missed.
const listeners = new Set();
let stream;
export function on(fn) {
  if (location.search.includes('noevents')) return () => {}; // a headless check of the page: no stream, so it settles
  if (!stream) {
    stream = new EventSource('/api/events');
    stream.onopen = () => listeners.forEach((l) => l({ kind: 'open' }));
    stream.onmessage = (e) => { const ev = JSON.parse(e.data); listeners.forEach((l) => l(ev)); };
  }
  listeners.add(fn);
  return () => listeners.delete(fn);
}

// useEvents runs fn for each event while the component is on screen.
export function useEvents(fn, deps = []) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => on((ev) => ref.current(ev)), deps);
}

// useLoad is what load() returns, reloaded when deps change or reload() is called.
export function useLoad(load, deps = []) {
  const [state, setState] = useState({ data: null, err: '', loading: true });
  const reload = () => load().then((data) => setState({ data, err: '', loading: false }), (e) => setState((s) => ({ ...s, err: e.message, loading: false })));
  useEffect(() => { setState((s) => ({ ...s, loading: true })); reload(); }, deps);
  return { ...state, reload };
}

export const clock = (at) => (at ? new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '');
export const base = (p) => (p || '').split('/').filter(Boolean).pop();
export const ago = (at) => {
  if (!at) return '';
  const s = (Date.now() - new Date(at)) / 1000;
  if (s < 60) return 'now';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  if (s < 86400) return Math.floor(s / 3600) + 'h';
  return Math.floor(s / 86400) + 'd';
};
export const openLink = (url) => api('POST', '/open', { URL: url });
