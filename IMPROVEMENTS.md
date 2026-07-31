# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

Sessions 1–30 (audit rounds 1, 2, 3, 4 & 5) are archived in
[`IMPROVEMENTS-DONE.md`](./IMPROVEMENTS-DONE.md). The findings below are
**audit round 6** — an end-to-end pass driven by three parallel code-audit
agents (concurrency, security, frontend/persistence), then distilled by manual
verification against source. Round 6 is deliberately small: the codebase is
already heavily hardened, so most findings are residuals, not regressions.

Item codes keep their category prefix (S = security, F = frontend, R = backend
reliability/correctness, A = accessibility, X = XSS, P = performance, D =
deploy/infra, CI = continuous integration, M = docs) and continue each series
(the archived log ends at S16, F28, R22, A3, X1, P1, D27, CI1, M3), so there is
no code collision with anything already done.

**Verification status.** The items in Sessions 31–33 were each confirmed
directly against the current source (file:line, behaviour) during this audit —
the reported bug is real, not a false positive. The items in Session 34 were
surfaced by an audit agent but **not yet manually verified**; confirm the
file:line and the described behaviour before fixing (a confirm pass is cheap
and is part of the session scope).

---

## Session 31 — Frontend error feedback & reconnect correctness

The two highest-impact frontend bugs in round 6. Both are user-facing failures
that surface silently, both were verified against source, and both are isolated
frontend edits with no backend coupling — they bundle cleanly into one
conversation.

- [x] **F29** [Critical] `showError` silently no-ops during any active session.
  `frontend/shared/ui.js:45-64` does `document.querySelector('.error-message')`
  and returns early (no mutation) when the element is absent. That element
  exists only in the landing/join/edit-name views — `renderLandingPage`
  (`frontend/src/formateur/renderers.js:123`), `renderJoinHTML` and
  `renderEditNameHTML` (`frontend/src/stagiaire/renderers.js:252, 310`) — and is
  **never** present in the WAITING / VOTING / VOTED / CLOSED layouts. The
  stagiaire test suite itself admits this
  (`frontend/src/stagiaire/handlers.test.js:216`: "no .error-message rendered
  in WAITING"). Consequence: `submitVote` failures
  (`frontend/src/stagiaire/handlers.js`) show the button flickering to "Envoi…"
  and back with zero error feedback; server `error` messages mid-session
  (`frontend/src/formateur/websocket.js:285-300`,
  `frontend/src/stagiaire/websocket.js:50, 225, 229`) are invisible; and the F25
  "Connexion perdue — rechargez la page" sentinel (`stagiaire/websocket.js:50`)
  never renders during a session. Users get no signal that a vote didn't send or
  that the connection is dead. **Fix:** route the mid-session call sites through
  `showToast(..., { type: 'error' })` (works in every view), or hoist a single
  global `.error-message` slot into both `renderLayout` / `renderFullLayout`
  outside the per-view container. The toast route is the smaller, safer change
  (one line per call site) and reuses the existing dedup/auto-hide machinery.
- [x] **F30** [High] Reconnect backoff gives up after ~18 min, not the claimed
  ~16 h. `frontend/shared/websocket-client.js:24` sets
  `maxReconnectAttempts: 50` with `initialReconnectDelay: 2000` /
  `maxReconnectDelay: 30000` (lines 18–19). The cap clamps at attempt 4; with
  the jitter (avg factor 0.75) the summed backoff over 50 attempts is ≈ 17.6
  min. The inline comments (lines 11 and 21) and CLAUDE.md's F25 note both claim
  "≈ 16 h backoff" — that would need ~2500 attempts at these defaults. A wifi
  flap, school-IT router reboot, or DNS hiccup lasting > 18 min therefore flips
  every connected client to the F25 "Connexion perdue" dead state, forcing a
  manual reload mid-class even once the server has recovered. The reconnect
  *state machine* itself is correct (connectionId race guard on close, jitter,
  online/offline awareness, R3 re-arm) — only the attempts-vs-duration math is
  wrong. **Fix:** bump `maxReconnectAttempts` to ~2500 (≈ 16 h with jitter), and
  correct the two comments + the CLAUDE.md F25 wording. Per-client cost is one
  pending `setTimeout` and ≤ 1 attempt per 30 s — negligible. Optionally accept
  `Infinity` and rely on the auto-reset of `reconnectAttempts` on any successful
  `onopen`.
- [x] Tests: `shared/ui.test.js` (+showToast surfaces a mid-session error when
  no `.error-message` exists, F29), `stagiaire/handlers.test.js`
  (+submitVote-failure path now surfaces feedback in VOTING state, F29),
  `websocket-client.test.js` (+summed backoff over `maxReconnectAttempts`
  reaches ≥ 15 h at defaults; +comment claims 16 h, F30). `npm test` green;
  `npm run lint` clean; `npm run build` clean.

---

## Session 32 — Backend reliability: persistence recovery & goroutine lifecycle

Two verified backend reliability bugs. Both are real today (not theoretical),
both live in the `internal/hub` + `internal/store` cluster, and they keep the
crash/graceful-shutdown and reconnect reasoning in one head.

- [x] **R23** [High] `ReadSamples` stops unrecoverably on a > 1 MiB line, so one
  corrupt line poisons the rest of the history file. `backend/internal/store/
  store.go:369-370` uses `scanner.Buffer(make([]byte, 0, 64*1024),
  scannerBufferSize)` then `for scanner.Scan()`. Per the `bufio.Scanner` docs, a
  token too large to fit the buffer yields `bufio.ErrTooLong` and **Scan stops
  unrecoverably** — every valid sample after the oversized line is dropped until
  the file rotates to `.1`. The package doc (lines 328–331) claims "Malformed
  lines are skipped … a torn write never poisons the whole history", and the
  `scannerBufferSize` block (lines 321–326) claims oversized lines "are skipped,
  … the same recovery behaviour as the prior bytes.Split path." Neither is true
  with `bufio.Scanner`. Trigger: any disk corruption or external write that
  produces > 1 MiB without an embedded `\n` (a torn multi-MB write, a stray
  `dd`, log injection). The existing regression test
  `TestReadSamplesScannerBufferSkipsPathologicallyLongLine`
  (`store_test.go:215`) only writes (valid, huge) and asserts `len == 1` — it
  never appends a valid sample *after* the huge line, so the regression is
  unguarded. **Fix:** switch to `bufio.Reader.ReadSlice('\n')` and skip lines
  longer than `scannerBufferSize` (bounded discard loop, no unbounded
  allocation), so recovery is genuine. Added a regression test writing
  (valid, huge, valid) and asserting both valid samples return.
- [x] **R24** [Medium] `writePump` goroutines linger up to `PingInterval` (30 s)
  after every disconnect/eviction. `backend/internal/hub/client.go`: `c.Send` is
  **never closed** (verified — no `close(c.Send)` outside tests), so writePump's
  `case message, ok := <-c.Send; if !ok { return }` branch (lines 240–243) is
  dead code. writePump only exits on a write/ping error or `ctx.Done`. On a
  stagiaire eviction in `trySend` (lines 313–317) or a trainer takeover
  (`hub.go:475-479`, `time.AfterFunc(trainerTakeoverCloseDelay)`), only
  `markClosing()` + `Conn.Close()` run — writePump is parked on its select with
  an empty Send buffer and only wakes on the next `pingTick.C` where the ping
  write fails (up to 30 s later). Under a reconnect storm at the resource caps
  (`MaxClientsPerSession=200` × `MaxSessionsGlobal=1000`) this is a real
  transient goroutine/512-entry-buffer spike. `c.Send` isn't closed from
  readPump's defer because that would race `pendingSend.flush`
  (`hub.go:82-86`), which deliberately bypasses the `closing` flag and sends
  after the hub lock is released — a send-to-closed-channel panic. **Fix:**
  introduced a separate `done chan struct{}` on `Client`, closed in readPump's
  defer (LIFO: after `Conn.Close`, before `wg.Done`). writePump now `select`s on
  `<-c.done` to exit promptly. `pendingSend.flush` uses a check-then-send
  pattern (non-blocking `<-done` check first, then send-or-drop select) so
  post-unregister flushes become clean no-ops instead of buffering into a dead
  channel. This recovers the prompt-exit property without regressing the CC3
  flush-safety argument.
- [x] Tests: `store/read_streaming_test.go` (enhanced
  `TestReadSamplesScannerBufferSkipsPathologicallyLongLine` now writes
  valid+huge+valid and asserts BOTH valid samples return, R23),
  `hub/client_test.go` (+`TestWritePumpExitsPromptlyOnEviction` using real WS +
  `goroutinesIn` stack-frame counter to assert writePump exits within 5s of
  eviction with PingInterval=1h, R24; +`TestPendingSendFlushNoOpAfterDone` and
  `TestPendingSendFlushDeliversWhenDoneOpen` for the flush invariant, R24).
  `go test -race ./...` green; `make fmt-check` / `go vet` clean.

---

## Session 33 — Auth hardening residuals

Two security findings, both in `internal/server/auth.go`, both verified. S17 is
a real silent failure of a documented control in the recommended deploy; S18 is
a constant-time discipline slip.

- [x] **S17** [Medium] Dashboard cookie loses its `Secure` flag behind the
  documented Caddy deploy. `backend/internal/server/auth.go:189-197`
  `shouldUseSecureCookie` derives its decision from `r.TLS` and `r.RemoteAddr`:
  loopback `RemoteAddr` → `Secure=false` (to let local dev persist the cookie
  over plain HTTP). The recommended production topology (`README`,
  `debian/Caddyfile.example`) is Caddy terminating TLS and `reverse_proxy …
  localhost:8080` — on that path `r.TLS` is `nil` (Caddy terminates TLS) and
  `r.RemoteAddr` is `127.0.0.1`, so the function returns **`false`** and the
  Set-Cookie carries no `Secure` attribute for *every* production login. The
  code comment at `auth.go:182` ("production behind TLS always sets it") is
  wrong for this topology. Impact: the browser transmits the `vote_admin`
  cookie over plaintext HTTP; a MITM who induces any `http://…` request (link,
  typed URL, non-HSTS first visit) captures the cookie before Caddy's
  HTTP→HTTPS redirect. **Fix:** when the peer is loopback, honour
  `X-Forwarded-Proto`/`Forwarded` (Caddy sets it) —
  `if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") { return
  true }` inside the loopback branch. Safe to trust that header because the
  loopback case already implies the dialer is in `TrustedProxies`. (Pair with
  HSTS set at Caddy.)
- [x] **S18** [Low] `subtle.ConstantTimeCompare` on the dashboard password leaks
  the secret length. `backend/internal/server/auth.go:252`:
  `subtle.ConstantTimeCompare([]byte(password), s.auth.secret)`.
  `ConstantTimeCompare` returns `0` **immediately** when `len(x) != len(y)` — it
  is only constant-time for equal lengths. Each probe whose submitted length
  differs from the secret's length short-circuits measurably faster, letting an
  attacker recover `len(secret)` by timing. Low impact alone (length isn't the
  secret; `CheckJoinRateLimit("dash:"+IP)` at `auth.go:245` throttles probing to
  ~3 per 10 min per IP), but it's the one place the codebase's otherwise-solid
  constant-time discipline slips. **Fix:** hash both sides and compare the fixed
  32-byte digests — `got := sha256.Sum256([]byte(password)); want :=
  sha256.Sum256(s.auth.secret); subtle.ConstantTimeCompare(got[:], want[:])`.
- [x] Tests: `server/dashboard_test.go` (extended `TestShouldUseSecureCookie`
  with loopback + `X-Forwarded-Proto`/`Forwarded` cases — true for `https`
  behind a proxy, false for plain-HTTP dev; S17), (+`TestSecretMatches`
  behavioural table + `TestSecretMatchesNoLengthOracle` timing-tolerant
  property test verifying the at-length probe is not privileged by the length
  short-circuit, asserted by temporarily reverting to the buggy direct compare
  and confirming it fails; S18). `go test -race ./...` green; `go vet` /
  `make fmt-check` clean.

---

## Session 34 — Verify-first residuals (frontend & backend polish)

Surfaced by an audit agent but **not yet manually verified** in this round.
Confirm the file:line and described behaviour before fixing; a confirm pass is
part of the session. These are independent, low-to-medium severity, and each is
a contained edit — they bundle into one conversation because none is large
enough to anchor a session alone.

- [ ] **F31** [Medium, verify-first] Confirm-dialog Escape leaks the in-flight
  promise. `frontend/shared/ui.js:114-172` (dialog key handler) is registered
  lazily on first open; `attachAppKeyboardShortcuts`
  (`frontend/src/formateur/renderers.js:1061-1081`, registered at init via
  `main.js:95`) registers earlier, so event dispatch hits the app handler first.
  On Escape-while-confirm-open, the app handler synchronously calls
  `showConfirmDialog`, whose Promise constructor overwrites `confirmResolve`
  (`ui.js:218`) — dropping the original click handler's resolver. The dialog
  handler then resolves the *app* handler's promise; the click handler's `await`
  never resolves and its frame is permanently retained. The comment at
  `ui.js:147-148` ("stopImmediatePropagation prevents other app-level Escape
  handlers") is incorrect — it only affects handlers registered *after* this
  one. **Verify:** open a confirm (Quitter), press Escape, assert the click
  handler's promise resolved. **Fix:** the app handler checks a `dialogOpen`
  flag (or `confirmResolve`) before invoking `showConfirmDialog`, or register
  the dialog handler in capture phase.
- [ ] **F32** [Medium, verify-first] `saveHighScore` / `setLastConfig` swallow
  localStorage quota failures → silent data loss framed as success.
  `frontend/shared/game-storage.js:37-48` `saveHighScore` returns `false` on a
  quota throw (private-mode Safari, full storage), so `Mastermind.submit`
  (`frontend/src/stagiaire/game.js:201-202`) reports `isRecord = false` for a
  real record-breaking score and the "Nouveau record !" badge is suppressed; the
  HUD keeps showing the old best. Symmetric in `frontend/shared/presets.js:95-103`
  `setLastConfig` (called from `formateur/handlers.js:198-206`): a quota failure
  silently drops the trainer's just-committed config, so next-session autoload
  restores the *previous* config. `savePreset` already returns null and its
  caller toasts `presetSaveFailed`; the autoload path doesn't. **Verify:**
  force a quota exception, win a game / start a vote, observe the silent
  no-record / stale-autoload. **Fix:** have both return a `{ isRecord,
  persisted }` (or boolean) and surface an info-level toast on failure.
- [ ] **R25** [Low, verify-first] Data race on `Client.ID` (read by `logAttrs`
  from writePump/broadcast goroutines, written in readPump). `backend/internal/
  hub/client.go:151` reads `c.ID` inside `logAttrs()`; `c.ID` is written in
  `handleStagiaireJoin` (`:444` → `c.OriginalID`, `:479` → `msg.StagiaireID`).
  `logAttrs` is invoked from other goroutines — writePump on write errors
  (`:246`), `trySend` slow-buffer paths (`:311, 313`, reached from
  `BroadcastSession` running off a *trainer*'s readPump iterating *stagiaire*
  clients). Go's string assignment is a two-word write, not atomic. **Verify:**
  run `go test -race ./internal/hub/...` under a reconnect/ID-cycling scenario.
  **Fix:** store the logging identity in an `atomic.Pointer[clientIdentity]`
  updated on each join, read atomically in `logAttrs`; or guard `ID`/`Name` with
  a small `sync.RWMutex`.
- [ ] **R26** [Low, verify-first] TOCTOU window in `cleanupLoop` on the
  `protected` snapshot. `backend/internal/hub/hub.go:980-1003` snapshots
  `protected` (sessions with a live trainer or any stagiaire) under `h.mu.RLock`,
  releases the lock, then calls `VoteManager.CleanupExpiredSessions(...,
  protected)`. A session published in the window between snapshot and reap is
  not in `protected`; if its `LastActivity` reads as older than
  `SessionTimeout` (clock skew, a long GC/debugger pause around `NewSession`) it
  is reaped out from under the trainer. Low probability (a freshly-published
  session sets `LastActivity = time.Now()` under the session lock before
  publication, so it is normally fresh), but the snapshot-then-act pattern is
  fragile. **Verify:** reason about the publish ordering; confirm there is no
  path where `LastActivity` can be stale at first publication. **Fix:** compute
  `protected` *inside* `CleanupExpiredSessions` via an `isProtected func(id
  string) bool` callback that takes `h.mu.RLock` itself, so protection is
  atomic with the reap decision; or hold `h.mu.RLock` across the call (cleanup
  runs at `CleanupInterval`, not on the hot path).
- [ ] Tests: as appropriate per item once verified (e.g. `ui.test.js` for F31
  resolve-on-Escape, `game-storage.test.js`/`presets.test.js` for F32 quota,
  a `-race` harness for R25, a `hub_test.go` reap-ordering test for R26).
  `go test -race ./...` green; `npm test` green; `npm run lint` / `npm run build`
  clean.

---

## Verification checklist (per session, before marking `[x]`)

- `make fmt-check` clean; `go vet ./...` clean; `go test -race ./...` green (for
  sessions touching Go).
- `npm run lint` clean; `npm test` green; `npm run build` clean (for sessions
  touching JS).
- No regression against the archived "things checked and found correct" list in
  `IMPROVEMENTS-DONE.md` (lock ordering `h.mu → Manager.mu → Session.mu`,
  idempotent reveal reversal, atomic counter writes, constant-time token
  compares, `SetTrustedProxies` defaulting to empty, `escapeHtml` on every
  user-supplied name, `sanitizeColor` at every `style="background-color:…"`
  site, `safeLocalGet`/`safeSessionGet` wrappers used everywhere).

---

## Things checked and found correct this round (do not regress)

Round 6 re-confirmed these — no action needed, listed so future audits don't
re-flag them:

- Lock ordering is consistent everywhere (`h.mu → m.mu → session.mu`); no path
  acquires them in a conflicting order. `Stagiaires`, `ReclaimTokens`, `Votes`,
  `Scores`, `GameScores`, `LastVoteScores` are never read without the session
  lock; the `GetX()` snapshot methods deep-copy under `RLock`.
- Auth gates are complete and correct: trainer-token gate on every join to an
  existing Manager session including the empty-slot recovery path (S13);
  reclaim-token gate under the session lock as the sole authority (S6/S12); a
  `c.Type` role check on every mutating handler (`start_vote`, `vote`,
  `close_vote`, `reset_vote`, `reveal_answers`, `report_game_score`,
  `update_name`).
- Input bounds: `maxMessageBytes = 4096` bounds every slice/map at unmarshal;
  `GameScore` clamped `[0, 100000]` and re-checked under lock (R17);
  `computeBackoffMs` caps the exponent before `1<<n` (no overflow).
- Rate limiting is not bypassable: per-client limiter keyed by immutable
  `OriginalID` (R22); per-IP failed-join backoff applied at WS upgrade *and*
  both join handlers (S14); per-IP connection slot acquired before the upgrade.
- WS origin rejects absent Origin then exact-match allowlists; `ALLOWED_ORIGINS=*`
  disables credentials. Dashboard CSRF: `SameSite=Strict` cookie, POST-only
  mutations, `connect-src 'self'` CSP.
- Persistence is otherwise sound: R21 fsync ordering (temp fsync → rename → dir
  fsync), B9 self-heal on dir perms, R20 NaN/Inf guards on load, atomic rename
  in the owned dir, ring-buffer tail reads bounding memory to O(limit).
- Resource caps enforced before allocation: `MaxSessionsGlobal` before code
  generation, `MaxClientsPerSession` before `JoinStagiaire`,
  `MaxConnectionsPerIP` before the WS upgrade; `ipConns` deletes zero-count
  keys (no unbounded growth).
