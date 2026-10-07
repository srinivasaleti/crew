// The window's terminal: xterm.js wired to crew through the Go side (desktop/main.go). crew itself
// draws everything; this page only adds what a terminal can't: ⌘ keys, copy and paste, zoom.

const term = new Terminal({
  fontFamily: '"SF Mono", Menlo, monospace',
  fontSize: 13,
  cursorBlink: true,
  macOptionIsMeta: true,               // ⌥ works as alt, so crew's alt+ keys work
  macOptionClickForcesSelection: true, // ⌥-drag selects text even though crew uses the mouse
  theme: { background: '#16181e' },
});
const fit = new FitAddon.FitAddon();
term.loadAddon(fit);
term.open(document.getElementById('term'));
fit.fit();
term.focus();

const bytes = (b64) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

// ⌘ keys go to crew with their modifiers, as escape sequences: ESC [ code ; mods u, or ESC [ 1 ;
// mods A–D for the arrows. crew runs what they're bound to (its keys file, /keys), so ⌘⌥← moves
// between panels, ⌘K clears a shell, ⌘C copies a selection and so on. The app keeps ⌘V paste,
// ⌘Q/⌘H/⌘M for the window and ⌘ +/−/0 zoom.
const arrows = { ArrowUp: 'A', ArrowDown: 'B', ArrowRight: 'C', ArrowLeft: 'D' };
const named = { BracketLeft: 91, BracketRight: 93, Enter: 13, Comma: 44, Period: 46, Slash: 47, Semicolon: 59, Quote: 39, Backslash: 92, Backquote: 96 };

// keyCode is the key without shift (⌘⇧[ is [ with shift), as crew names it.
function keyCode(e) {
  if (e.code.startsWith('Key')) return e.code.slice(3).toLowerCase().charCodeAt(0);
  if (e.code.startsWith('Digit')) return e.code.charCodeAt(5);
  return named[e.code];
}

function cmdKey(app, e) {
  const k = e.key.toLowerCase();
  if (k === 'v' && !e.shiftKey && !e.altKey) {
    window.runtime.ClipboardGetText().then((t) => t && term.paste(t));
    return false;
  }
  if (k === 'c' && term.hasSelection()) { // an ⌥-drag selection across the window
    window.runtime.ClipboardSetText(term.getSelection());
    return false;
  }
  if (!e.shiftKey && !e.altKey && (k === 'q' || k === 'h' || k === 'm')) return true; // the app's menu: quit, hide, minimise
  if (k === '=' || k === '+' || k === '-' || k === '0') {
    term.options.fontSize = k === '0' ? 13 : Math.max(9, term.options.fontSize + (k === '-' ? -1 : 1));
    fit.fit();
    return false;
  }
  const mods = 1 + (e.shiftKey ? 1 : 0) + (e.altKey ? 2 : 0) + (e.ctrlKey ? 4 : 0) + 8;
  if (arrows[e.code]) {
    app.Write('\x1b[1;' + mods + arrows[e.code]);
  } else if (keyCode(e)) {
    app.Write('\x1b[' + keyCode(e) + ';' + mods + 'u');
  }
  e.preventDefault();
  return false;
}

// Shift+Enter adds a line in crew's message box; terminals can't tell it from Enter, so it's sent
// as its own sequence.
function shiftEnter(app, e) {
  app.Write('\x1b[13;2u');
  e.preventDefault();
  return false;
}

// Resize cursor: crew tells us which columns are draggable borders (OSC 7777 ; borders=x,x…), and
// the pointer becomes ↔ over them, and while dragging one.
let borders = [];
let dragging = false;
term.parser.registerOscHandler(7777, (data) => {
  if (data.startsWith('borders=')) borders = data.slice(8).split(',').filter(Boolean).map(Number);
  return true;
});
const screen = () => document.querySelector('#term .xterm-screen');
function column(e) {
  const el = screen();
  if (!el) return -1;
  const r = el.getBoundingClientRect();
  return Math.floor((e.clientX - r.left) / (r.width / term.cols));
}
function showCursor(on) {
  const el = screen();
  if (el) el.style.cursor = on ? 'col-resize' : '';
}
document.addEventListener('mousemove', (e) => showCursor(dragging || borders.includes(column(e))));
document.addEventListener('mousedown', (e) => { dragging = borders.includes(column(e)); });
document.addEventListener('mouseup', (e) => { dragging = false; showCursor(borders.includes(column(e))); });

// The Go bindings appear once Wails has loaded them.
function start() {
  const app = window.go && window.go.main && window.go.main.App;
  if (!app || !window.runtime) return setTimeout(start, 20);
  window.runtime.EventsOn('out', (b64) => term.write(bytes(b64)));
  term.onData((d) => app.Write(d));
  term.onBinary((d) => app.Write(d));
  term.onResize(({ cols, rows }) => app.Resize(cols, rows));
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true;
    if (e.metaKey) return cmdKey(app, e);
    if (e.key === 'Enter' && e.shiftKey) return shiftEnter(app, e);
    return true;
  });
  new ResizeObserver(() => fit.fit()).observe(document.getElementById('term'));
  app.Start(term.cols, term.rows);
}
start();
