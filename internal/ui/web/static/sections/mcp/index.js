// MCP: the MCP servers Claude Code has (claude.ai connectors like Jira, Outlook, GitHub and Slack,
// plugins' servers, and your own), whether each is connected and how to sign in, adding and
// removing your own, and a Chat that uses them. Reading goes ahead; a call that changes something
// waits for your OK in the chat (crew's gate, in every chat). Listing is `claude mcp list`: no tokens.
import { html, useState, api, useLoad, openLink, Chat, Form, Empty, Toolbar, ask, css } from '../../lib.js';

css(import.meta.url);

// KEY are the servers most people want, shown on top: a title to look for, and what it's for.
const KEY = [
  { match: /atlassian|jira/i, name: 'Jira', what: 'issues, comments, Confluence' },
  { match: /microsoft|outlook/i, name: 'Outlook', what: 'mail, calendar, Teams' },
  { match: /github/i, name: 'GitHub', what: 'repos, PRs, issues' },
  { match: /slack/i, name: 'Slack', what: 'channels, messages' },
];
const GROUPS = [['connected', 'Connected'], ['signin', 'Needs sign-in'], ['failed', "Couldn't connect"], ['off', 'Not set up']];
const SAY = { connected: 'Connected', signin: 'Needs sign-in', failed: "Couldn't connect", off: 'Not set up' };

// Connect is how to sign in to a server: a link for a claude.ai connector, else what to do in Claude Code.
function Connect({ s }) {
  const [shown, setShown] = useState(false);
  if (s.status === 'connected') return '';
  if (s.connectUrl) return html`<button class="small primary" title=${s.connectHow} onClick=${() => openLink(s.connectUrl)}>Connect ↗</button>`;
  return html`<span class="mcp-how"><button class="small" onClick=${() => setShown(!shown)}>How to connect</button>
    ${shown && html`<div class="mcp-how-box">${s.connectHow}<button class="small" onClick=${() => navigator.clipboard.writeText('claude')}>Copy “claude”</button></div>`}</span>`;
}

function Servers() {
  const [fresh, setFresh] = useState(0);
  const { data, err } = useLoad(() => api('GET', '/mcp' + (fresh ? '?fresh=1' : '')), [fresh]);
  const [adding, setAdding] = useState(false);
  const list = (data && data.servers) || [];
  const remove = async (s) => { if (await ask(`Remove ${s.name}? Claude Code stops using it everywhere.`, 'Remove')) { await api('DELETE', '/mcp/' + encodeURIComponent(s.name)); setFresh((n) => n + 1); } };
  const key = KEY.map((k) => ({ ...k, s: list.filter((s) => k.match.test(s.name)).sort((a, b) => (a.status === 'connected' ? -1 : 0) - (b.status === 'connected' ? -1 : 0))[0] }));
  return html`<div class="scroll mcp">
    <div class="mcp-bar"><span class="meta">${data ? `${list.filter((s) => s.status === 'connected').length} of ${list.length} connected · checked ${new Date(data.checked).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}` : err ? '' : 'Checking every server… (a few seconds)'}</span>
      <button class="small" disabled=${!data} onClick=${() => setFresh((n) => n + 1)}>Check again</button>
      <button class="small primary" onClick=${() => setAdding(true)}>+ Add server</button></div>
    ${err && html`<${Empty} title="Couldn't read the MCP servers">${err}<//>`}
    ${data && html`<div class="mcp-keys">${key.map((k) => html`<div key=${k.name} class=${'mcp-key ' + (k.s ? k.s.status : 'off')}>
        <div class="mcp-key-head"><span class=${'mcp-dot ' + (k.s ? k.s.status : 'off')}></span><b>${k.name}</b></div>
        <div class="meta">${k.what}</div>
        <div class="mcp-key-foot">${k.s ? html`<span class="meta">${SAY[k.s.status]}</span><${Connect} s=${k.s} />` : html`<span class="meta">No server yet · add one, or connect it in claude.ai</span>`}</div>
      </div>`)}</div>
      ${GROUPS.map(([st, title]) => {
        const l = list.filter((s) => s.status === st);
        return l.length > 0 && html`<div key=${st}><div class="group">${title} <span class="n">${l.length}</span></div>
          ${l.map((s) => html`<div class="mcp-row" key=${s.name}>
            <span class=${'mcp-dot ' + s.status}></span>
            <div class="mcp-main"><div><b>${s.title}</b> <span class="chip">${s.source}</span></div>
              <div class="meta mcp-target" title=${s.detail}>${s.target || s.detail}</div></div>
            <${Connect} s=${s} />
            ${s.source === 'yours' && html`<button class="small ghost danger" onClick=${() => remove(s)}>Remove</button>`}
          </div>`)}</div>`;
      })}
      <div class="hint">Claude reads through these freely. Anything that changes something (sending mail, posting to Slack, commenting on Jira or GitHub) waits for your Allow in the chat, in every chat.</div>`}
    ${adding && html`<${Form} title="Add an MCP server" action="Add" onClose=${() => setAdding(false)}
      fields=${[{ key: 'Name', label: 'Name', placeholder: 'my-server' }, { key: 'Target', label: 'URL or command', placeholder: 'https://mcp.example.com/mcp  or  npx some-mcp --flag', hint: 'for every project (user scope)' }]}
      submit=${async (v) => { await api('POST', '/mcp', v); setFresh((n) => n + 1); }} />`}
  </div>`;
}

// ChatTab is the MCP chat: Claude with your MCP servers; writes wait for your OK.
function ChatTab() {
  const { data, err } = useLoad(() => api('GET', '/mcp/chat'));
  if (err) return html`<${Empty} title="The chat didn't open">${err}<//>`;
  if (!data) return html`<div class="empty">Loading…</div>`;
  return html`<${Chat} id=${data.id} base=${'/tasks/' + data.id} agent="Claude" placeholder="Ask across Jira, Outlook, GitHub, Slack…"
    empty="“What did I get assigned in Jira this week?”, “Summarise my unread Outlook mail”, “Any GitHub PRs waiting on me?”. Reading goes ahead; sending or posting asks you first." />`;
}

export default function Mcp() {
  const [tab, setTab] = useState(location.hash.endsWith('/chat') ? 1 : 0);
  const go = (i) => { setTab(i); location.hash = 'mcp' + (i ? '/chat' : ''); };
  return html`<main class="page">
    <${Toolbar} title="MCP" sub="the servers Claude uses · reads go ahead, writes ask you" />
    <div class="tabs">${['Servers', 'Chat'].map((n, i) => html`<button key=${n} class=${tab === i ? 'on' : ''} onClick=${() => go(i)}>${n}</button>`)}</div>
    ${tab === 0 ? html`<${Servers} />` : html`<${ChatTab} />`}
  </main>`;
}
