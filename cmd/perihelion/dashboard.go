package main

// dashboardHTML is the built-in local dashboard served at / on the RPC
// listener. Self-contained (no external resources), German UI. The __TOKEN__
// placeholder is replaced with the local RPC token at serve time.
const dashboardHTML = `<!doctype html>
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Perihelion</title>
<style>
:root{--bg:#0b0e14;--card:#141a24;--border:#232b3a;--text:#e8ecf3;--dim:#8b94a7;--accent:#fbbf24;--green:#34d399;--red:#f87171}
*{margin:0;box-sizing:border-box}
body{background:var(--bg);color:var(--text);font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;padding:24px;max-width:1000px;margin:0 auto}
header{display:flex;align-items:center;justify-content:space-between;margin-bottom:20px;flex-wrap:wrap;gap:10px}
.logo{font-size:22px;font-weight:700}
.logo span{color:var(--accent)}
.badge{padding:6px 12px;border-radius:999px;font-size:13px;font-weight:600;background:#1f2937;color:var(--dim)}
.badge.on{background:rgba(52,211,153,.12);color:var(--green)}
.addr{display:flex;align-items:center;gap:10px;background:var(--card);border:1px solid var(--border);border-radius:12px;padding:12px 16px;margin-bottom:16px;flex-wrap:wrap}
.addr .lbl{color:var(--dim);font-size:13px}
.addr code{font-size:12px;word-break:break-all;flex:1;min-width:200px}
button{background:var(--accent);border:0;color:#1a1204;font-weight:600;padding:8px 14px;border-radius:8px;cursor:pointer;font-size:13px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:12px;margin-bottom:16px}
.card{background:var(--card);border:1px solid var(--border);border-radius:12px;padding:16px}
.card h3{font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:var(--dim);margin-bottom:6px;font-weight:600}
.big{font-size:26px;font-weight:700;font-variant-numeric:tabular-nums}
.big small{font-size:14px;color:var(--dim);font-weight:600}
.sub{font-size:12px;color:var(--dim);margin-top:2px}
table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}
th{font-size:11px;text-transform:uppercase;letter-spacing:.05em;color:var(--dim);text-align:left;padding:6px 8px;border-bottom:1px solid var(--border)}
td{padding:7px 8px;font-size:13px;border-bottom:1px solid var(--border)}
tr.mine td{color:var(--accent)}
form{display:grid;gap:10px;grid-template-columns:1fr;margin-top:8px}
input{background:#0e131c;border:1px solid var(--border);color:var(--text);border-radius:8px;padding:10px 12px;font-size:14px;width:100%}
input:focus{outline:none;border-color:var(--accent)}
.row2{display:grid;grid-template-columns:1fr 1fr;gap:10px}
#sendresult{margin-top:8px;font-size:13px;word-break:break-all}
#sendresult.ok{color:var(--green)}#sendresult.err{color:var(--red)}
h2{font-size:15px;margin-bottom:8px}
section.card{margin-bottom:16px}
@media(max-width:600px){body{padding:12px}}
</style>
</head>
<body>
<header>
  <div class="logo"><span>&#9728;</span> Perihelion</div>
  <div id="minebadge" class="badge">Verbinde&hellip;</div>
</header>
<div class="addr"><span class="lbl">Deine Adresse</span><code id="address">&ndash;</code><button onclick="copyAddr()">Kopieren</button></div>
<div class="grid">
  <div class="card"><h3>Verf&uuml;gbar</h3><div class="big" id="spendable">&ndash;</div><div class="sub" id="immature"></div></div>
  <div class="card"><h3>Blockh&ouml;he</h3><div class="big" id="height">&ndash;</div><div class="sub" id="difficulty"></div></div>
  <div class="card"><h3>Umlaufmenge</h3><div class="big" id="supply">&ndash;</div><div class="sub" id="burned"></div></div>
  <div class="card"><h3>Netzwerk</h3><div class="big" id="peers">&ndash;</div><div class="sub" id="mempool"></div></div>
</div>
<section class="card">
  <h2>Senden</h2>
  <form id="sendform">
    <input id="to" placeholder="Empf&auml;nger (per1&hellip;)" required>
    <div class="row2">
      <input id="amount" placeholder="Betrag in PER" required>
      <input id="fee" value="0.001">
    </div>
    <button type="submit" id="sendbtn">Senden</button>
  </form>
  <div id="sendresult"></div>
</section>
<section class="card">
  <h2>Letzte Bl&ouml;cke</h2>
  <table><thead><tr><th>H&ouml;he</th><th>Zeit</th><th>Txs</th><th>Belohnung</th><th></th></tr></thead><tbody id="blocks"></tbody></table>
</section>
<script>
var TOKEN="__TOKEN__";
function el(id){return document.getElementById(id)}
function j(path,opts){opts=opts||{};opts.headers=opts.headers||{};opts.headers["X-Auth"]=TOKEN;
 return fetch(path,opts).then(function(r){if(!r.ok){return r.text().then(function(t){throw new Error(t)})}return r.json()})}
function copyAddr(){navigator.clipboard.writeText(el("address").textContent)}
function refresh(){
 j("/status").then(function(s){
  el("height").textContent=s.height;
  el("difficulty").textContent="Difficulty "+s.difficulty;
  el("supply").innerHTML=s.supply+" <small>PER</small>";
  el("burned").textContent=s.burned+" PER verbrannt · Pool "+s.pool;
  el("peers").textContent=s.peers;
  el("mempool").textContent=(s.peers==1?"Peer":"Peers")+" · "+s.mempool+" Tx im Mempool";
  var b=el("minebadge");
  if(s.mining){b.className="badge on";b.textContent="⛏ Mining aktiv · "+s.threads+" Threads"}
  else{b.className="badge";b.textContent="Mining aus"}
 }).catch(function(){el("minebadge").className="badge";el("minebadge").textContent="Node nicht erreichbar"});
 j("/balance").then(function(b){
  el("address").textContent=b.address;
  el("spendable").innerHTML=b.spendable+" <small>PER</small>";
  el("immature").textContent=b.immature!=="0"?("+ "+b.immature+" PER reifen noch (20 Blöcke)"):"";
 }).catch(function(){});
 j("/recent").then(function(r){
  var rows="";
  (r.blocks||[]).forEach(function(x){
   var d=new Date(x.time*1000);
   rows+="<tr"+(x.mine?" class='mine'":"")+"><td>"+x.height+"</td><td>"+d.toLocaleTimeString("de-DE")+"</td><td>"+x.txs+"</td><td>"+x.reward+" PER</td><td>"+(x.mine?"⛏ von dir":"")+"</td></tr>"
  });
  el("blocks").innerHTML=rows;
 }).catch(function(){});
}
el("sendform").addEventListener("submit",function(e){
 e.preventDefault();
 var res=el("sendresult");res.className="";res.textContent="Sende…";
 el("sendbtn").disabled=true;
 j("/send",{method:"POST",body:JSON.stringify({to:el("to").value.trim(),amount:el("amount").value.trim(),fee:el("fee").value.trim()})})
 .then(function(r){res.className="ok";res.textContent="✓ Gesendet! Tx: "+r.txid+" — bestätigt mit dem nächsten Block.";el("to").value="";el("amount").value=""})
 .catch(function(err){res.className="err";res.textContent="✗ "+err.message})
 .finally(function(){el("sendbtn").disabled=false});
});
refresh();setInterval(refresh,4000);
</script>
</body>
</html>`
