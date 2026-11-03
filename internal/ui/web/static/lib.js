// What every section's page shares, in one import: the API and events, Markdown, the chat, small
// parts. Each lives in lib/; add a new shared part there and export it here.
// Pages draw and send; crew (internal/core and the services) decides everything.
export { html, useState, useEffect, useRef } from './vendor/preact-htm.js';
export * from './lib/api.js';
export * from './lib/markdown.js';
export * from './lib/chat.js';
export * from './lib/ui.js';
