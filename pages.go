package main

const pagesHTML = `
{{define "head"}}<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.}} · LinkedIn MCP by Servloci</title>
<style>
:root{--bg:#f7f7f5;--card:#fff;--fg:#1d1d1f;--mut:#6b6b70;--line:#e4e4e0;--acc:#0a66c2;--ok:#127c3a;--warn:#b25e00;--bad:#b3261e;--code:#f1f1ee}
@media (prefers-color-scheme:dark){:root{--bg:#141416;--card:#1e1e21;--fg:#ececee;--mut:#9a9aa1;--line:#2e2e33;--acc:#5aa5f0;--ok:#4cc27a;--warn:#f0a040;--bad:#f06a60;--code:#2a2a2e}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.55 system-ui,-apple-system,Segoe UI,sans-serif}
main{max-width:760px;margin:0 auto;padding:32px 16px 64px}
h1{font-size:30px;line-height:1.2;margin:8px 0 12px}h2{font-size:19px;margin:0 0 10px}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:20px;margin:16px 0}
.mut{color:var(--mut)}.ok{color:var(--ok)}.warn{color:var(--warn)}.bad{color:var(--bad)}
a{color:var(--acc)}code,pre{background:var(--code);border-radius:6px;font:13.5px/1.5 ui-monospace,Menlo,monospace}
code{padding:2px 5px}pre{padding:12px;overflow-x:auto;white-space:pre-wrap;word-break:break-all;margin:8px 0}
button,.btn{display:inline-block;background:var(--acc);color:#fff;border:0;border-radius:8px;padding:10px 16px;font:inherit;font-weight:600;cursor:pointer;text-decoration:none}
button.sec{background:transparent;color:var(--fg);border:1px solid var(--line)}button.danger{background:transparent;color:var(--bad);border:1px solid var(--bad)}
form{display:inline}.row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
.top{display:flex;justify-content:space-between;align-items:center;gap:12px}.brand{font-weight:700;text-decoration:none;color:var(--fg)}
ul{padding-left:20px}li{margin:4px 0}.price{font-size:34px;font-weight:800}
</style></head><body><main>
<div class="top"><a class="brand" href="/">LinkedIn MCP <span class="mut">· Servloci</span></a></div>
{{end}}
{{define "foot"}}<p class="mut" style="margin-top:40px;font-size:14px">Open source: <a href="https://github.com/ivikasavnish/linkedin-mcp">github.com/ivikasavnish/linkedin-mcp</a> · <a href="https://servloci.in/privacy.html">Privacy</a> · Not affiliated with LinkedIn.</p></main></body></html>{{end}}

{{define "home"}}{{template "head" "Post to LinkedIn from your AI"}}
<h1>Post to LinkedIn straight from Claude, Cursor, or any MCP client.</h1>
<p class="mut">Your own personal LinkedIn MCP server, hosted. Sign in with LinkedIn, connect your AI client, and say "post this to LinkedIn".</p>
<div class="card">
<div class="price">$1<span class="mut" style="font-size:16px;font-weight:400"> / month</span></div>
<ul>
<li>Create posts with images, link cards, <b>bold</b> and <i>italic</i>, real #hashtags</li>
<li>Comment on posts, delete posts, save drafts</li>
<li>Connect from claude.ai (OAuth, no key copying) or Claude Code, Cursor, etc. (personal API key)</li>
<li>Your LinkedIn token is encrypted at rest. We only post when your AI asks. Cancel or delete any time.</li>
</ul>
<a class="btn" href="/login">Sign in with LinkedIn</a>
</div>
<p class="mut">Prefer to self-host? It's open source and free to run on your own machine.</p>
{{template "foot"}}{{end}}

{{define "dashboard"}}{{template "head" "Dashboard"}}
<div class="row" style="justify-content:space-between"><h1>Hi {{.U.Name}}</h1>
<form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="sec">Sign out</button></form></div>

<div class="card"><h2>1. Subscription</h2>
{{if .Free}}<p class="ok">Free access enabled for your account.</p>
{{else if .Paid}}<p class="ok">Active until {{date .U.PaidUntil}}.</p>
  {{if .Paying}}<form method="post" action="/cancel"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="sec">Cancel at end of period</button></form>{{end}}
  {{if eq .U.SubStatus "cancel_at_cycle_end"}}<p class="mut">Cancelled, no further charges.</p>{{end}}
{{else if .Billing}}<p class="warn">Not active. Posting needs a subscription: <b>$1 / month</b>.</p>
  <form method="post" action="/subscribe"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Subscribe for $1/month</button></form>
  {{if eq .U.SubStatus "created"}}<p class="mut">Paid just now? It can take a minute to show. Reload this page.</p>{{end}}
{{else}}<p class="warn">Subscriptions open soon.</p>{{end}}
</div>

<div class="card"><h2>2. LinkedIn connection</h2>
{{if .LiOK}}<p class="ok">Connected, valid until {{date .U.LiExpires}}.</p><p class="mut">LinkedIn tokens last 60 days. Sign in again before then to renew.</p>
{{else}}<p class="bad">Expired on {{date .U.LiExpires}}.</p>{{end}}
<a class="btn" href="/login" style="background:transparent;color:var(--fg);border:1px solid var(--line)">Reconnect LinkedIn</a>
</div>

<div class="card"><h2>3. Connect your AI client</h2>
<p><b>claude.ai / Claude Desktop</b>: Settings → Connectors → Add custom connector, URL:</p>
<pre>{{.Base}}/mcp</pre>
<p class="mut">You'll be asked to approve it here. No key needed.</p>
<p><b>Claude Code, Cursor, others</b>: use a personal API key.</p>
{{if .NewKey}}<p class="warn">Copy your key now, it won't be shown again:</p>
<pre>{{.NewKey}}</pre>
<p>Claude Code:</p>
<pre>claude mcp add --transport http --scope user linkedin {{.Base}}/mcp --header "Authorization: Bearer {{.NewKey}}"</pre>
<p>Other clients (JSON config):</p>
<pre>{"mcpServers": {"linkedin": {"type": "http", "url": "{{.Base}}/mcp",
  "headers": {"Authorization": "Bearer {{.NewKey}}"}}}}</pre>
{{else if .U.KeyPrefix}}<p class="mut">Current key starts with <code>{{.U.KeyPrefix}}…</code></p>{{end}}
<div class="row">
<form method="post" action="/key"><input type="hidden" name="csrf" value="{{.CSRF}}"><button{{if .U.KeyPrefix}} class="sec"{{end}}>{{if .U.KeyPrefix}}Replace API key{{else}}Create API key{{end}}</button></form>
<form method="post" action="/revoke-apps"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="sec">Disconnect OAuth apps</button></form>
</div>
{{if .U.KeyPrefix}}<p class="mut">Replacing the key disables the old one immediately.</p>{{end}}
</div>

<div class="card"><h2>Try it</h2>
<p>Ask your AI: <i>"Draft a LinkedIn post about what I shipped this week, show me, then post it."</i></p>
<p class="mut">Tools: whoami, create_post (text, images as base64, link card, draft), comment_post, delete_post. Limit: 60 calls/hour.</p>
</div>

<div class="card"><h2>Delete account</h2>
<p class="mut">Cancels billing and deletes your stored LinkedIn token, keys and app connections.</p>
<form method="post" action="/delete" onsubmit="return confirm('Delete your account and cancel billing?')"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="danger">Delete account</button></form>
</div>
{{template "foot"}}{{end}}

{{define "consent"}}{{template "head" "Approve app"}}
<h1>Allow <b>{{.Client}}</b> to post on your LinkedIn?</h1>
<div class="card">
<p>Signed in as <b>{{.U.Name}}</b>.</p>
<p>The app will be able to create, comment on and delete LinkedIn posts as you, through this server.</p>
<p class="mut">You'll return to: <code>{{.RedirectHost}}</code><br>Only approve apps you added yourself just now.</p>
<form method="post" action="/authorize"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="q" value="{{.Query}}">
<div class="row"><button name="action" value="allow">Allow</button><button class="sec" name="action" value="deny">Deny</button></div></form>
</div>
{{template "foot"}}{{end}}

{{define "msg"}}{{template "head" .Title}}
<h1>{{.Title}}</h1><div class="card"><p>{{.Body}}</p><a href="/">Home</a></div>
{{template "foot"}}{{end}}
`
