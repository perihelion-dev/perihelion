package main

// pageTemplates holds every page. html/template escapes all interpolated
// values according to context, which matters here because everything shown —
// addresses, hashes, amounts — originates from the chain and is therefore
// supplied by strangers. Styling is inline because the Content-Security-Policy
// forbids loading anything external.
const pageTemplates = `
{{define "head"}}<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · Perihelion Explorer</title>
<style>
:root{--bg:#0b0e14;--card:#141a24;--border:#232b3a;--text:#e8ecf3;--dim:#8b94a7;--accent:#fbbf24;--green:#34d399}
*{box-sizing:border-box;margin:0}
body{background:var(--bg);color:var(--text);font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;padding:24px;max-width:1080px;margin:0 auto}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
header{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;margin-bottom:20px}
.logo{font-size:20px;font-weight:700}.logo a{color:var(--text)}.logo span{color:var(--accent)}
form{display:flex;gap:8px;flex:1;min-width:260px;max-width:520px}
input{flex:1;background:#0e131c;border:1px solid var(--border);color:var(--text);border-radius:8px;padding:9px 12px;font-size:14px}
button{background:var(--accent);border:0;color:#1a1204;font-weight:600;padding:9px 16px;border-radius:8px;cursor:pointer}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin-bottom:20px}
.card{background:var(--card);border:1px solid var(--border);border-radius:12px;padding:14px 16px}
.card h3{font-size:11px;text-transform:uppercase;letter-spacing:.07em;color:var(--dim);font-weight:600;margin-bottom:4px}
.big{font-size:22px;font-weight:700;font-variant-numeric:tabular-nums}
.sub{font-size:12px;color:var(--dim)}
table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}
th{font-size:11px;text-transform:uppercase;letter-spacing:.05em;color:var(--dim);text-align:left;padding:8px;border-bottom:1px solid var(--border)}
td{padding:9px 8px;font-size:13px;border-bottom:1px solid var(--border);word-break:break-all}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
section{background:var(--card);border:1px solid var(--border);border-radius:12px;padding:16px;margin-bottom:16px;overflow-x:auto}
h2{font-size:16px;margin-bottom:12px}
dl{display:grid;grid-template-columns:170px 1fr;gap:8px 16px;font-size:13px}
dt{color:var(--dim)}dd{word-break:break-all}
.tag{display:inline-block;background:rgba(52,211,153,.12);color:var(--green);border-radius:999px;padding:1px 8px;font-size:11px;font-weight:600}
footer{color:var(--dim);font-size:12px;margin-top:28px;line-height:1.7}
.warn{background:rgba(251,191,36,.08);border:1px solid rgba(251,191,36,.3);border-radius:10px;padding:12px 14px;font-size:13px;margin-bottom:16px}
</style></head><body>
<header>
  <div class="logo"><a href="/"><span>&#9728;</span> Perihelion</a> Explorer</div>
  <form action="/search" method="get">
    <input name="q" placeholder="Block height, hash, or per1… address" maxlength="128" autocomplete="off">
    <button type="submit">Search</button>
  </form>
</header>
{{if .Stats}}<div class="grid">
  <div class="card"><h3>Height</h3><div class="big">{{.Stats.Height}}</div><div class="sub">{{if .Synced}}synced{{else}}syncing{{end}} · {{.Peers}} peers</div></div>
  <div class="card"><h3>Circulating</h3><div class="big">{{.Stats.Supply}}</div><div class="sub">PER · <a href="/supply">{{.Stats.Burned}} burned</a></div></div>
  <div class="card"><h3>Difficulty</h3><div class="big">{{.Stats.Difficulty}}</div><div class="sub">retargets every block</div></div>
  <div class="card"><h3>Next reward</h3><div class="big">{{.Stats.NextSubsidy}}</div><div class="sub">PER · pool {{.Stats.Pool}}</div></div>
</div>{{end}}
{{end}}

{{define "foot"}}
<footer>
Read-only view of the Perihelion chain, served by a fully validating node.
This service holds no keys and cannot send transactions.<br>
Perihelion is experimental software with a small hashrate; treat confirmations accordingly.
</footer>
</body></html>
{{end}}

{{define "home"}}{{template "head" .}}
<section>
  <h2>Latest blocks</h2>
  <table>
    <thead><tr><th>Height</th><th>Time (UTC)</th><th>Txs</th><th>Reward</th><th>Mined by</th></tr></thead>
    <tbody>
    {{range .Blocks}}
      <tr>
        <td><a href="/block/{{.Height}}">{{.Height}}</a></td>
        <td>{{.Time}}</td>
        <td>{{.Txs}}</td>
        <td>{{.Reward}} PER</td>
        <td class="mono">{{if .Miner}}<a href="/address/{{.Miner}}">{{short .Miner}}</a>{{end}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
</section>
{{template "foot" .}}{{end}}

{{define "block"}}{{template "head" .}}
{{with .Block}}
<section>
  <h2>Block {{.Height}}</h2>
  <dl>
    <dt>Hash</dt><dd class="mono">{{.Hash}}</dd>
    <dt>Previous</dt><dd class="mono"><a href="/block/{{.Prev}}">{{.Prev}}</a></dd>
    <dt>Time</dt><dd>{{.Time}}</dd>
    <dt>Difficulty</dt><dd>{{.Difficulty}}</dd>
    <dt>Nonce</dt><dd>{{.Nonce}}</dd>
    <dt>Mined by</dt><dd class="mono">{{if .Miner}}<a href="/address/{{.Miner}}">{{.Miner}}</a>{{end}}</dd>
    <dt>Block reward</dt><dd>{{.Reward}} PER</dd>
  </dl>
</section>
<section>
  <h2>Transactions ({{len .Txs}})</h2>
  <table>
    <thead><tr><th>Transaction</th><th>In</th><th>Out</th><th>Value</th></tr></thead>
    <tbody>
    {{range .Txs}}
      <tr>
        <td class="mono"><a href="/tx/{{.ID}}">{{short .ID}}</a> {{if .Coinbase}}<span class="tag">coinbase</span>{{end}}</td>
        <td>{{.Inputs}}</td><td>{{.Outputs}}</td><td>{{.Value}} PER</td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{if .Truncated}}<p class="sub">Only the first transactions are shown.</p>{{end}}
</section>
{{end}}
{{template "foot" .}}{{end}}

{{define "tx"}}{{template "head" .}}
{{with .Tx}}
<section>
  <h2>Transaction {{if .Coinbase}}<span class="tag">coinbase</span>{{end}}</h2>
  <dl>
    <dt>Id</dt><dd class="mono">{{.ID}}</dd>
    <dt>In block</dt><dd><a href="/block/{{.Height}}">{{.Height}}</a></dd>
    <dt>Total out</dt><dd>{{.Total}} PER</dd>
  </dl>
</section>
<section>
  <h2>Inputs ({{len .Inputs}})</h2>
  {{if .Inputs}}
  <table><thead><tr><th>Previous transaction</th><th>Index</th></tr></thead><tbody>
  {{range .Inputs}}<tr><td class="mono"><a href="/tx/{{.PrevID}}">{{short .PrevID}}</a></td><td>{{.Index}}</td></tr>{{end}}
  </tbody></table>
  {{else}}<p class="sub">Newly created coins — this transaction has no inputs.</p>{{end}}
</section>
<section>
  <h2>Outputs ({{len .Outputs}})</h2>
  <table><thead><tr><th>Address</th><th>Value</th></tr></thead><tbody>
  {{range .Outputs}}<tr><td class="mono"><a href="/address/{{.Address}}">{{.Address}}</a></td><td>{{.Value}} PER</td></tr>{{end}}
  </tbody></table>
</section>
{{end}}
{{template "foot" .}}{{end}}

{{define "address"}}{{template "head" .}}
{{with .Address}}
<section>
  <h2>Address</h2>
  <dl>
    <dt>Address</dt><dd class="mono">{{.Address}}</dd>
    <dt>Spendable</dt><dd>{{.Spendable}} PER</dd>
    <dt>Immature</dt><dd>{{.Immature}} PER <span class="sub">(mining rewards still maturing)</span></dd>
    <dt>Total</dt><dd>{{.Total}} PER</dd>
  </dl>
</section>
{{end}}
{{template "foot" .}}{{end}}

{{define "supply"}}{{template "head" .}}
{{with .Supply}}
<section>
  <h2>Money supply at height {{.Height}}</h2>
  <dl>
    <dt>Ever emitted</dt><dd>{{.Emitted}} PER</dd>
    <dt>Destroyed by fee burn</dt><dd>{{.Burned}} PER</dd>
    <dt>Circulating</dt><dd><strong>{{.Circulating}} PER</strong></dd>
    <dt>Upper bound</dt><dd>{{.Bound}} PER <span class="sub">({{.PctOfBound}}% issued)</span></dd>
    <dt>Miner reward pool</dt><dd>{{.Pool}} PER <span class="sub">(fees awaiting payout)</span></dd>
  </dl>
</section>
<section>
  <h2>How the burn is verified</h2>
  <p style="font-size:13px;color:var(--dim);margin-bottom:12px">
    There is no burn address. Burned coins are never created in the first
    place: consensus fixes exactly what a block's coinbase may pay, and any
    block paying more is rejected by every node. So the burn is not something
    to take on trust — it is a rule each participant enforces independently.
  </p>
  <dl>
    <dt>Blocks re-checked</dt><dd>{{.AuditBlocks}} most recent</dd>
    <dt>Fees paid to miners</dt><dd>{{.AuditFees}} PER <span class="sub">(coinbase above the scheduled subsidy)</span></dd>
    <dt>Fees destroyed</dt><dd>{{.AuditBurn}} PER <span class="sub">(the other half of every fee)</span></dd>
    <dt>Every coinbase within its limit</dt>
    <dd>{{if .AuditOK}}<span class="tag">yes</span>{{else}}<strong>NO — report this</strong>{{end}}</dd>
  </dl>
  <p style="font-size:13px;color:var(--dim);margin-top:12px">
    Anyone can repeat this check without trusting this page: run a node and
    compare each block's coinbase against the emission formula in
    <span class="mono">core/emission.go</span>.
  </p>
</section>
{{end}}
{{template "foot" .}}{{end}}

{{define "error"}}{{template "head" .}}
<div class="warn">{{.Error}}</div>
<p><a href="/">Back to the latest blocks</a></p>
{{template "foot" .}}{{end}}
`
