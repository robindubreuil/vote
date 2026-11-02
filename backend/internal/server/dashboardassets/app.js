// Maintainer dashboard SPA. Served at /dashboard/assets/app.js (was an inline
// <script> in the page — moved to a file for the strict CSP: script-src
// 'self', no 'unsafe-inline'). CSP note: setting properties via el.style.x
// (CSSOM) is allowed under style-src 'self'; style="" attributes in HTML
// markup are not — hence the .value.sm class and the DOM-built histogram
// bars below.
const POLL_MS = 30000;
const MAX_SNAPSHOTS = 1440; // 12h at 30s — bounds localStorage growth
const STORE_KEY = 'vote:dashboard:snaps';

// --- Prometheus text parser ---------------------------------------------
// Parses the subset of the Prometheus exposition format the dashboard needs:
//   name value                          → name
//   name{labels} value                  → name{labels}
// Lines starting with '#' (HELP/TYPE/comments) and unparseable values are
// skipped. The map key preserves the (sorted-normalized) labels so callers
// can look up a specific series via getLabeled.
function parseMetrics(text) {
  const out = new Map();
  for (const line of text.split('\n')) {
    if (!line || line.startsWith('#')) continue;
    let name, rest, labels = '';
    const brace = line.indexOf('{');
    if (brace > -1) {
      const close = line.indexOf('}');
      if (close < 0) continue;
      name = line.slice(0, brace);
      labels = '{' + line.slice(brace + 1, close) + '}';
      rest = line.slice(close + 1).trim();
    } else {
      const sp = line.indexOf(' ');
      name = line.slice(0, sp);
      rest = line.slice(sp + 1);
    }
    const val = parseFloat(rest);
    if (Number.isNaN(val)) continue;
    out.set(name + labels, val);
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
    if (d === null) {
      deltaText = '—';
    } else {
      // Label must reflect the actual polling span: when the local history
      // is shorter than an hour, "/ dernière heure" would understate (or
      // overstate) the rate. Fall back to "depuis démarrage" if the
      // process itself is younger than the window, else advertise the
      // observed span in minutes.
      const histSpanMs = snaps.length >= 2 ? snaps[snaps.length - 1].t - snaps[0].t : 0;
      if (histSpanMs >= HOUR) deltaText = '+' + d + ' / dernière heure';
      else if (uptime * 1000 < HOUR) deltaText = '+' + d + ' depuis démarrage';
      else deltaText = '+' + d + ' / ' + Math.max(1, Math.round(histSpanMs / 60000)) + ' min';
    }
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
    '<div class="card"><div class="label">' + label + '</div><div class="value sm">' + value + '</div></div>'
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
  let infCount = 0;
  const re = new RegExp('^' + name + '_bucket\\{le="([^"]+)"\\}$');
  for (const [k, v] of m.entries()) {
    const match = k.match(re);
    if (!match) continue;
    if (match[1] === '+Inf') { infCount = v; continue; }
    buckets.push({ le: parseFloat(match[1]), count: v });
  }
  buckets.sort((a, b) => a.le - b.le);
  if (buckets.length === 0) {
    el.innerHTML = '<div class="legend">distribution indisponible</div>';
    return;
  }
  // Prometheus buckets are cumulative: bucket{le="x"} counts observations ≤ x.
  // Per-bar count = this bucket − previous bucket. The +Inf bucket captures
  // everything above the highest finite bound — render it as the rightmost
  // bar so outlier sessions (e.g. >50 votes) stay visible.
  const bars = [];
  let prevCount = 0;
  for (const b of buckets) {
    const n = b.count - prevCount;
    bars.push({ n, labelText: leToLabel(b.le), titleText: '≤ ' + leToLabel(b.le) + ' : ' + n });
    prevCount = b.count;
  }
  if (infCount > prevCount) {
    const topLE = buckets[buckets.length - 1].le;
    const n = infCount - prevCount;
    bars.push({ n, labelText: leToLabel(topLE) + '+', titleText: '> ' + leToLabel(topLE) + ' : ' + n });
  }
  const maxN = Math.max(1, ...bars.map(b => b.n));
  // Bars are built via DOM APIs and sized with bar.style.height (a CSSOM
  // assignment, not blocked by style-src 'self'); a style="" attribute in
  // markup would be rejected by the CSP.
  el.textContent = '';
  for (const b of bars) {
    const bar = document.createElement('div');
    bar.className = 'bar';
    bar.style.height = (b.n / maxN) * 100 + '%';
    bar.title = b.titleText;
    const le = document.createElement('span');
    le.className = 'le';
    le.textContent = b.labelText;
    bar.appendChild(le);
    el.appendChild(bar);
  }
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
// HISTORY_LIMIT mirrors dashboardHistoryDefaultLimit in auth.go (7 days
// of 5-min samples = 2016). Kept in sync deliberately: the server seeds
// exactly this many samples, and the local store keeps the same window
// in localStorage so a tab open across the weekly boundary doesn't lose
// resolution. Change both together.
const HISTORY_LIMIT = 2016;

async function seedFromServer() {
  try {
    const res = await fetch('/dashboard/history?limit=' + HISTORY_LIMIT, { cache: 'no-store' });
    if (!res.ok) return;
    const samples = await res.json();
    if (!Array.isArray(samples) || samples.length === 0) return;
    const seeded = samples.map(function (s) {
      return {
        t: new Date(s.ts).getTime(),
        sc: s.sc, vs: s.vs, vc: s.vc, tj: s.tj,
      };
    });
    // Merge with whatever the current tab has already accumulated. The
    // server samples at 5-min cadence; live polls are 30s — preferring local
    // samples for their span preserves the higher resolution instead of
    // discarding it on every reload (and stops a second tab from wiping the
    // first's history via shared localStorage).
    const local = loadSnapshots();
    if (local.length === 0) {
      saveSnapshots(seeded);
      return;
    }
    const earliestLocal = local[0].t;
    const older = seeded.filter(s => s.t < earliestLocal);
    saveSnapshots(older.concat(local));
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
