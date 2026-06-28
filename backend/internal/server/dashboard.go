package server

// dashboardHTML is a self-contained maintainer dashboard. It has no build
// step and no external dependencies: it polls /metrics, parses the Prometheus
// text format in-browser, keeps a compact ring buffer of counter snapshots in
// localStorage (the server's counters reset on restart, so the client owns
// the trend), and renders SVG sparklines + histogram bars.
//
// Auth is enforced by the backend (HMAC cookie); this page only renders if
// the request already passed requireAuth.
const dashboardHTML = `<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Tableau de bord — vote</title>
<style>
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin:0; background:#0f1115; color:#e6e6e6;
  font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif; }
header { display:flex; align-items:center; justify-content:space-between;
  padding:1rem 1.5rem; border-bottom:1px solid #1f2430; }
header h1 { margin:0; font-size:1.1rem; font-weight:600; }
header .meta { font-size:.78rem; color:#8a93a6; display:flex; gap:1rem; align-items:center; }
.dot { width:8px; height:8px; border-radius:50%; background:#3a3; display:inline-block; }
.dot.stale { background:#a33; }
header a { color:#8a93a6; text-decoration:none; font-size:.78rem; }
header a:hover { color:#e6e6e6; }
header form.logout { display:inline; margin:0; }
header form.logout button { background:none; border:0; color:#8a93a6;
  font-size:.78rem; font-family:inherit; padding:0; cursor:pointer; text-decoration:none; }
header form.logout button:hover { color:#e6e6e6; }
main { max-width:1100px; margin:0 auto; padding:1.5rem; }
.grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:1rem; margin-bottom:1.5rem; }
.card { background:#1a1d24; border:1px solid #242a35; border-radius:12px; padding:1.1rem 1.2rem; }
.card .label { font-size:.72rem; text-transform:uppercase; letter-spacing:.05em; color:#8a93a6; }
.card .value { font-size:1.9rem; font-weight:700; margin-top:.3rem; font-variant-numeric:tabular-nums; }
.card .delta { font-size:.78rem; color:#5fae5f; margin-top:.15rem; font-variant-numeric:tabular-nums; }
.card .delta.muted { color:#8a93a6; }
section h2 { font-size:.95rem; font-weight:600; margin:1.5rem 0 .75rem; color:#c4cad6; }
.spark { width:100%; height:48px; display:block; }
.hist { display:flex; align-items:flex-end; gap:6px; height:90px; padding-top:.5rem; }
.hist .bar { flex:1; background:#3a6cef; border-radius:4px 4px 0 0; min-height:2px; transition:height .3s; position:relative; }
.hist .bar .le { position:absolute; bottom:-1.3rem; left:50%; transform:translateX(-50%); font-size:.65rem; color:#8a93a6; white-space:nowrap; }
.hist-wrap { padding-bottom:1.4rem; }
.legend { font-size:.75rem; color:#8a93a6; margin-top:.4rem; }
.mini { width:100%; height:26px; margin-top:.5rem; display:block; }
footer { text-align:center; color:#5a6173; font-size:.72rem; padding:2rem; }
</style>
</head>
<body>
<header>
  <h1>📈 Tableau de bord</h1>
  <div class="meta">
    <span><span class="dot" id="live"></span> <span id="liveText">en direct</span></span>
    <span id="uptime"></span>
    <form class="logout" method="POST" action="/dashboard/logout"><button type="submit">Déconnexion</button></form>
  </div>
</header>
<main>
  <div class="grid" id="counters"></div>
  <section>
    <h2>Activité des votes (en direct, dernière heure)</h2>
    <div class="card"><svg class="spark" id="sparkVotes"></svg></div>
  </section>
  <section>
    <h2>Répartition par session (depuis le démarrage)</h2>
    <div class="grid">
      <div class="card hist-wrap"><div class="label">Votes / session</div><div class="hist" id="histVotes"></div></div>
      <div class="card hist-wrap"><div class="label">Stagiaires / session</div><div class="hist" id="histTrainees"></div></div>
      <div class="card hist-wrap"><div class="label">Durée session (min)</div><div class="hist" id="histDuration"></div></div>
    </div>
  </section>
  <section>
    <h2>Instantané live</h2>
    <div class="grid" id="liveGrid"></div>
  </section>
</main>
<footer>Tendances persistées côté serveur (VOTE_DATA_DIR) — les compteurs survivent aux redémarrages. Échantillonnage périodique.</footer>

<script>
const POLL_MS = 30000;
const MAX_SNAPSHOTS = 1440; // 12h at 30s — bounds localStorage growth
const STORE_KEY = 'vote:dashboard:snaps';

// --- Prometheus text parser ---------------------------------------------
function parseMetrics(text) {
  const out = new Map();
  for (const line of text.split('\n')) {
    if (!line || line.startsWith('#')) continue;
    // name{labels} value  |  name value
    let name, rest;
    const brace = line.indexOf('{');
    if (brace > -1) {
      name = line.slice(0, brace);
      const close = line.indexOf('}');
      rest = line.slice(brace + 1, close) + ' ' + line.slice(close + 2);
    } else {
      const sp = line.indexOf(' ');
      name = line.slice(0, sp);
      rest = line.slice(sp + 1);
    }
    const val = parseFloat(rest);
    if (Number.isNaN(val)) continue;
    out.set(name + (brace > -1 ? '{' + line.slice(brace + 1, line.indexOf('}')) + '}' : ''), val);
  }
  return out;
}
function get(m, name) { return m.has(name) ? m.get(name) : 0; }
function getLabeled(m, name, labelKey, labelVal) {
  return get(m, name + '{' + labelKey + '="' + labelVal + '"}');
}

// --- Snapshot store ------------------------------------------------------
function loadSnapshots() {
  try { return JSON.parse(localStorage.getItem(STORE_KEY) || '[]'); }
  catch { return []; }
}
function saveSnapshots(snaps) {
  try { localStorage.setItem(STORE_KEY, JSON.stringify(snaps)); } catch {}
}
function pushSnapshot(m) {
  let snaps = loadSnapshots();
  // Restart detection: cumulative counters must never decrease. If the new
  // snapshot is below the last stored one, the server restarted (counters
  // reset to 0) and the old history belongs to a previous run — drop it so
  // derived rates/sparklines stay correct.
  if (snaps.length > 0) {
    const last = snaps[snaps.length - 1];
    const cur = {
      sc: get(m, 'vote_sessions_created_total'),
      vs: get(m, 'vote_votes_started_total'),
      vc: get(m, 'vote_votes_cast_total'),
      tj: get(m, 'vote_trainees_joined_total'),
    };
    if (cur.sc < last.sc || cur.vs < last.vs || cur.vc < last.vc || cur.tj < last.tj) {
      snaps = [];
    }
  }
  snaps.push({
    t: Date.now(),
    sc: get(m, 'vote_sessions_created_total'),
    vs: get(m, 'vote_votes_started_total'),
    vc: get(m, 'vote_votes_cast_total'),
    tj: get(m, 'vote_trainees_joined_total'),
  });
  while (snaps.length > MAX_SNAPSHOTS) snaps.shift();
  saveSnapshots(snaps);
}

// --- Renderers -----------------------------------------------------------
function fmt(n) {
  if (n >= 1000) return (n/1000).toFixed(n >= 10000 ? 0 : 1) + 'k';
  return String(Math.round(n));
}
// deltaWindow returns how much the given key grew over the last ms window,
// using the persisted history. Falls back to the full run span when history
// is shorter.
function deltaWindow(snaps, key, ms) {
  if (snaps.length < 2) return null;
  const last = snaps[snaps.length - 1];
  const cutoff = last.t - ms;
  let base = snaps[0][key];
  for (const s of snaps) {
    if (s.t >= cutoff) { base = s[key]; break; }
  }
  return last[key] - base;
}
// miniSpark renders a small inline SVG line of a cumulative series. Monotonic
// values produce a rising line — a quick visual of growth since process start.
function miniSpark(values, color) {
  const W = 120, H = 26;
  if (!values || values.length < 2) {
    return '<svg class="mini" viewBox="0 0 ' + W + ' ' + H + '"></svg>';
  }
  const min = Math.min.apply(null, values);
  const max = Math.max.apply(null, values);
  const range = (max - min) || 1;
  const step = W / (values.length - 1);
  let d = '';
  for (let i = 0; i < values.length; i++) {
    const x = i * step;
    const y = H - ((values[i] - min) / range) * (H - 4) - 2;
    d += (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1) + ' ';
  }
  return '<svg class="mini" viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none">' +
    '<path d="' + d + '" fill="none" stroke="' + color + '" stroke-width="1.5"/></svg>';
}
function seriesForKey(snaps, key) {
  return snaps.map(function (s) { return s[key]; });
}
function renderCounters(m) {
  const uptime = get(m, 'vote_uptime_seconds');
  const sc = get(m, 'vote_sessions_created_total');
  const vs = get(m, 'vote_votes_started_total');
  const vc = get(m, 'vote_votes_cast_total');
  const tj = get(m, 'vote_trainees_joined_total');
  const game = get(m, 'vote_game_enabled_votes_total');
  const mc = get(m, 'vote_multiple_choice_votes_total');
  const snaps = loadSnapshots();
  const HOUR = 3600 * 1000;
  // Per-card definition: label, cumulative value, history key, sparkline color.
  const cards = [
    { label: 'Sessions créées', value: sc, key: 'sc', color: '#4a7cff' },
    { label: 'Votes lancés', value: vs, key: 'vs', color: '#a855f7' },
    { label: 'Votes exprimés', value: vc, key: 'vc', color: '#22c55e' },
    { label: 'Stagiaires accueillis', value: tj, key: 'tj', color: '#f97316' },
  ];
  document.getElementById('counters').innerHTML = cards.map(function (c) {
    const d = deltaWindow(snaps, c.key, HOUR);
    let deltaText;
    if (d === null) deltaText = '—';
    else if (uptime * 1000 < HOUR) deltaText = '+' + d + ' depuis démarrage';
    else deltaText = '+' + d + ' / dernière heure';
    return '<div class="card"><div class="label">' + c.label + '</div>' +
      '<div class="value">' + c.value + '</div>' +
      '<div class="delta' + (d === 0 ? ' muted' : '') + '">' + deltaText + '</div>' +
      miniSpark(seriesForKey(snaps, c.key), c.color) + '</div>';
  }).join('');
  // Feature-adoption cards (percentages, no sparkline).
  const gamePct = vs > 0 ? Math.round(game / vs * 100) : 0;
  const mcPct = vs > 0 ? Math.round(mc / vs * 100) : 0;
  document.getElementById('counters').insertAdjacentHTML('beforeend',
    '<div class="card"><div class="label">Mini-jeu activé</div>' +
    '<div class="value">' + gamePct + '%</div>' +
    '<div class="delta muted">' + game + ' / ' + vs + ' votes</div></div>' +
    '<div class="card"><div class="label">Choix multiple</div>' +
    '<div class="value">' + mcPct + '%</div>' +
    '<div class="delta muted">' + mc + ' / ' + vs + ' votes</div></div>'
  );
  document.getElementById('uptime').textContent = 'uptime ' + fmtDuration(uptime);
}
function renderLive(m) {
  const items = [
    ['Sessions actives', get(m, 'vote_sessions_active')],
    ['Formateurs connectés', get(m, 'vote_trainers_connected')],
    ['Stagiaires connectés', get(m, 'vote_stagiaires_connected')],
    ['Vote idle', getLabeled(m, 'vote_sessions_by_state', 'state', 'idle')],
    ['Vote actif', getLabeled(m, 'vote_sessions_by_state', 'state', 'active')],
    ['Vote clos', getLabeled(m, 'vote_sessions_by_state', 'state', 'closed')],
    ['Goroutines', get(m, 'go_goroutines')],
    ['Mémoire heap', fmtBytes(get(m, 'go_mem_alloc_bytes'))],
  ];
  document.getElementById('liveGrid').innerHTML = items.map(([label, value]) =>
    '<div class="card"><div class="label">' + label + '</div><div class="value" style="font-size:1.3rem">' + value + '</div></div>'
  ).join('');
}
function renderSparkline() {
  const snaps = loadSnapshots();
  const svg = document.getElementById('sparkVotes');
  const W = svg.clientWidth || 600, H = 48;
  if (snaps.length < 2) { svg.innerHTML = '<text x="4" y="28" fill="#5a6173" font-size="12">collecte en cours…</text>'; return; }
  const last = snaps[snaps.length - 1];
  const oneHourAgo = Date.now() - 3600 * 1000;
  const recent = snaps.filter(s => s.t >= oneHourAgo);
  const pts = recent.length >= 2 ? recent : snaps.slice(-120);
  const base = pts[0].vc;
  const series = pts.map(s => s.vc - base);
  const max = Math.max(1, ...series);
  const stepX = W / (pts.length - 1);
  let d = '';
  pts.forEach((s, i) => {
    const x = i * stepX;
    const y = H - (series[i] / max) * (H - 4) - 2;
    d += (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1) + ' ';
  });
  const delta = last.vc - base;
  svg.innerHTML =
    '<path d="' + d + '" fill="none" stroke="#4a7cff" stroke-width="2"/>' +
    '<text x="' + (W - 4) + '" y="16" text-anchor="end" fill="#8a93a6" font-size="11">+' + delta + ' votes / ' + pts.length + ' échantillons</text>';
}
function renderHistogram(elId, m, name, leToLabel) {
  const count = get(m, name + '_count');
  const el = document.getElementById(elId);
  if (count === 0) { el.innerHTML = '<div class="legend">aucune session terminée</div>'; return; }
  const buckets = [];
  const re = new RegExp('^' + name + '_bucket\\{le="([^"]+)"\\}$');
  for (const [k, v] of m.entries()) {
    const match = k.match(re);
    if (match && match[1] !== '+Inf') buckets.push({ le: parseFloat(match[1]), count: v });
  }
  buckets.sort((a, b) => a.le - b.le);
  if (buckets.length === 0) { el.innerHTML = ''; return; }
  // non-cumulative per-bucket delta
  const bars = buckets.map((b, i) => {
    const prev = i > 0 ? buckets[i - 1].count : 0;
    return { le: b.le, n: b.count - prev };
  });
  const maxN = Math.max(1, ...bars.map(b => b.n));
  el.innerHTML = bars.map(b => {
    const h = (b.n / maxN) * 100;
    return '<div class="bar" style="height:' + h + '%" title="≤ ' + leToLabel(b.le) + ' : ' + b.n + '"><span class="le">' + leToLabel(b.le) + '</span></div>';
  }).join('');
}
function fmtDuration(sec) {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), mn = Math.floor((sec % 3600) / 60);
  if (d > 0) return d + 'j ' + h + 'h';
  if (h > 0) return h + 'h ' + mn + 'min';
  return mn + 'min';
}
function fmtBytes(b) {
  if (b > 1e9) return (b/1e9).toFixed(1) + ' GB';
  if (b > 1e6) return (b/1e6).toFixed(1) + ' MB';
  if (b > 1e3) return (b/1e3).toFixed(0) + ' KB';
  return b + ' B';
}

// --- Main loop -----------------------------------------------------------
let lastFetchOk = null;
// seedFromServer pulls the persisted time-series (collected 24/7 by the server
// regardless of whether anyone had the dashboard open) and seeds the local
// snapshot store, so trends reflect all usage since the data dir was created —
// not just the periods this browser tab was open.
async function seedFromServer() {
  try {
    const res = await fetch('/dashboard/history?limit=2016', { cache: 'no-store' });
    if (!res.ok) return;
    const samples = await res.json();
    if (!Array.isArray(samples) || samples.length === 0) return;
    saveSnapshots(samples.map(function (s) {
      return {
        t: new Date(s.ts).getTime(),
        sc: s.sc, vs: s.vs, vc: s.vc, tj: s.tj,
      };
    }));
  } catch (e) { /* network/auth hiccup — fall back to live polling */ }
}
async function tick() {
  try {
    const res = await fetch('/metrics', { cache: 'no-store' });
    if (!res.ok) throw new Error(res.status);
    const text = await res.text();
    const m = parseMetrics(text);
    pushSnapshot(m);
    renderCounters(m);
    renderLive(m);
    renderSparkline();
    renderHistogram('histVotes', m, 'vote_votes_per_session', v => String(v));
    renderHistogram('histTrainees', m, 'vote_trainees_per_session', v => String(v));
    renderHistogram('histDuration', m, 'vote_session_duration_seconds', v => Math.round(v/60));
    lastFetchOk = true;
  } catch (e) {
    lastFetchOk = false;
  }
  const dot = document.getElementById('live');
  const txt = document.getElementById('liveText');
  if (lastFetchOk === true) { dot.classList.remove('stale'); txt.textContent = 'en direct'; }
  else if (lastFetchOk === false) { dot.classList.add('stale'); txt.textContent = 'hors-ligne'; }
}
seedFromServer().then(tick);
setInterval(tick, POLL_MS);
</script>
</body>
</html>`
