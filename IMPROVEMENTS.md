# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

Sessions 1–17 (audit rounds 1, 2 & 3-head) are archived in
[`IMPROVEMENTS-DONE.md`](./IMPROVEMENTS-DONE.md). The findings below are the
remainder of **audit round 3** — a fresh pass over areas where subtle residual
issues survived the prior sessions. Item codes are prefixed `R` (round 3) to
avoid colliding with the codes already used in the archived log.

---

## Session 18 — Graceful shutdown under load

One architectural fix plus the test coverage that would have caught it. Shutdown
is currently ordered backwards relative to Go's documented pattern, which leaves
a window for stuck sockets and (under `-race`) a `sync.WaitGroup` contract
violation.

- [ ] **R2** [High] The hub drains *before* the HTTP listener closes, so new
  WebSocket dials are accepted throughout the drain. `main.go:76` calls
  `h.Shutdown()` before `main.go:82` calls `srv.Shutdown()`, and
  `handleWebSocket` (`server.go:358-427`) never checks `hub.Context().Err()`
  before upgrading / calling `client.Start()` → `wg.Add(2)` (`client.go:123`).
  Two consequences: (1) Go forbids a positive-delta `Add` concurrent with `Wait`
  when the counter is zero ("sync: WaitGroup is reused before previous Wait has
  returned") — latent today only because no test loads `main.go`'s shutdown
  path; (2) hijacked WS conns aren't tracked by `http.Server.Shutdown` (Go docs),
  so a reconnect in the drain window upgrades successfully, its `writePump` exits
  on `ctx.Done`, but its `readPump` blocks in `ReadMessage` until `pongWait`
  (70 s) — a "connected" socket that delivers no state, freezing a 30-student
  class on every deploy. **Fix:** (a) in `main.go`, stop accepting (close the
  listener / `srv.Shutdown`) **before** `h.Shutdown()`; (b) add a drain guard at
  the top of `handleWebSocket` — `if s.hub.Context().Err() != nil { 503 }`
  *before* `AcquireIPSlot`/`Upgrade`; (c) add a SIGTERM-under-load integration
  test running under `-race` (the existing `shutdown_test.go` exercises
  `Hub.Shutdown()` in isolation, which is why this is latent).
- [ ] Tests: `TestShutdownRejectsNewUpgradesDuringDrain` (assert a dial during
  drain gets 503, not an upgrade), `TestShutdownJoinsAllGoroutinesUnderLoad`
  (assert no WaitGroup panic and all client goroutines exit under `-race`),
  `TestShutdownClosesListenerFirst` (ordering contract). Wire into
  `backend/integration/` so `main.go`'s real path is covered.

---

## Session 19 — Frontend reconnect & lifecycle edge cases

A cluster of state-desync bugs on reconnect / leave / page-reload. They share a
root cause: client-side lifecycle assumes the *first* event of a kind is the
*only* event, so second-order paths (leave→rejoin, reload-reconnect, async
rejection) reset or fail to re-bind state.

- [ ] **R3** [High] A stagiaire permanently loses offline/online reconnect
  awareness after leaving one session and joining another without a reload.
  `_bindOnlineEvents()` runs only in the `VoteClient` constructor
  (`shared/websocket-client.js:60-72`); `close()` calls `_unbindOnlineEvents()`
  (`:225`). Stagiaire `leaveSession` calls `client.close()` but never nulls the
  module-level `client` (`stagiaire/handlers.js:627-630`), and
  `connectToSession` reuses the existing instance
  (`stagiaire/websocket.js:73-78`); `connect()` (`:82`) does **not** re-bind the
  online/offline listeners. After a leave→rejoin, a classroom wifi drop burns
  reconnect attempts against a dead NIC with no fast-retry on `online` — exactly
  the regression FH1/FH2 were built to prevent. (Formateur avoids this by
  nulling + recreating the client in `closeClient`/`initClient`.) **Fix:** rebind
  in `connect()` (`this._bindOnlineEvents()` is idempotent), or expose a
  `resetClient()` from `stagiaire/websocket.js` that nulls `client` and call it
  from `leaveSession`.
- [ ] **R6** [Medium] The Escape-to-leave shortcut is never attached when a
  trainer creates a session from the landing page. `attachAppKeyboardShortcuts`
  is called only inside the `savedSessionCode` branch (`formateur/main.js:82`),
  i.e. only on reload with a persisted session; the `session_created` handler
  (`formateur/websocket.js:120-164`) never wires it. C3's fix (moving the
  shortcut to the app tracker so it survives `cleanupAllListeners`) is therefore
  only effective on the reload path — the *most common* flow (land → Créer) has
  no working Escape shortcut. The handler already self-guards on
  `state.sessionCode`, so attaching it is safe. **Fix:** call
  `attachAppKeyboardShortcuts` from the `session_created` handler behind a
  one-shot guard (or hoist the call to module top-level in `main.js` before the
  `if/else`).
- [ ] **R7** [Medium] A trainee's game high score and streak are silently wiped
  on a page reload mid-session. `session_joined` resets them when
  `state.appState === AppState.JOINING` (`stagiaire/websocket.js:110-113`); a
  reload rebuilds state with `appState = JOINING` (`stagiaire/state.js`). F15
  (archived) made reload auto-reconnect seamlessly via sessionStorage, so the
  reset now fires on a reconnect, contradicting CLAUDE.md's "Persists across
  sessions and reconnects". A PWA update or accidental reload erases progress and
  drops difficulty to level 1. **Fix:** skip `resetHighScore()`/`saveStreak(0)`
  when an existing `vote_stagiaire_id` in sessionStorage signals a
  reconnect-to-same-session rather than a first join (or move the reset to fire
  only on an explicit join-form submit).
- [ ] **R8** [Medium] A server-rejected rename has no UI rollback.
  `handleEditName` commits `state.prenom`, persists it, closes the modal, and
  renders — all before the server responds (`stagiaire/handlers.js:476-518`); the
  `name_updated` handler ignores `msg.name` (`stagiaire/websocket.js:228-232`).
  `UpdateStagiaireName` performs an authoritative normalised-name collision check
  under the session lock (`vote/manager.go:528-532`) and can return
  `ErrNameInUse` in the same TOCTOU window CC2 identified for joins. On rejection
  the trainee's UI shows the rejected name while the trainer still sees the old
  one, the modal is already closed, and the only signal is a 5 s toast.
  **Fix:** don't set `state.prenom` optimistically — set it from `msg.name` in
  the `name_updated` handler; on `error`, keep `prenomEdit = true` so the modal
  stays open with the invalid input for correction.
- [ ] Tests: `websocket-client.test.js` (+rebind-on-reconnect after close, R3),
  `formateur/websocket.test.js` / a `main.js` test (+Escape attached on
  `session_created`, R6), `stagiaire/websocket.test.js` (+high score preserved
  on reload-reconnect when `vote_stagiaire_id` present, R7), `stagiaire/
  handlers.test.js` (+rename keeps modal open / leaves `prenom` untouched on
  server error, R8). `npm test` green; `npm run build` clean.

---

## Session 20 — Backend robustness & security cluster

Four small, independent, hardening fixes. Each is isolated and verifiable on its
own; each was missed by a prior session that touched neighbouring code.

- [ ] **R9** [Medium] `Server.Shutdown` nil-pointer panics when startup failed
  at `net.Listen`. If listen fails, `s.srv` stays nil, `Run` returns to `errCh`,
  and `main`'s shutdown block calls `s.srv.Shutdown(ctx)`
  (`server.go:291-293`) → panic, which masks the real listen error in the logs
  and turns a clean "exit 1 with a clear message" into a stack trace. **Fix:**
  guard `Server.Shutdown` — `if s.srv == nil { return nil }` (or return the
  stored startup error).
- [ ] **R10** [Medium] The dashboard cookie mints an all-zero nonce on CSPRNG
  failure, contradicting B14's "fail loud" policy. `auth.go:85-96` only `slog`
  the `rand.Read(nonce)` error and continues with `nonce` left zeroed; B14
  (archived) deliberately removed time-derived CSPRNG fallbacks and panics in
  `security.GenerateID`/`GenerateToken` because predictable secrets collapse S1
  (trainer takeover) and S6/S12 (reclaim) — the auth nonce was missed. With a
  zero nonce the HMAC payload `v1.<nonce>.<exp>` is deterministic over
  `(secret, exp-second)`, so two logins within the same second produce
  byte-identical cookies, defeating per-token revocation granularity (S4).
  **Fix:** return the error (or panic via `security.failCSPRNG`) and refuse to
  mint — one-line consistency with the rest of the secret-minting surface.
- [ ] **R11** [Low] The backoff cap is applied *before* the +25% jitter, so the
  real maximum is `MaxBackoffMs × 1.25 = 375 s` (6.25 min), not the documented 5
  min. `security.go:137-153` caps then adds `jitterRange` (up to 75 s) on top.
  **Fix:** apply the cap after jitter (and keep the existing 100 ms floor).
- [ ] **R12** [Low] `runtime.ReadMemStats` forces a stop-the-world on every
  `/metrics` scrape and every 30 s dashboard poll (`metrics.go:93`). The
  observability endpoint periodically stalls the very real-time path it
  monitors, and the stall is invisible to `/metrics` itself. **Fix:** switch the
  `go_mem_*` gauges to the non-STW `runtime/metrics` API
  (`/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`,
  `/sched/goroutines:goroutines`), or drop `runtime.ReadMemStats` entirely.
- [ ] Tests: `TestServerShutdownNoPanicOnFailedListen` (R9), `TestSignCookieFailsOnCSPRNGFailure`
  / extend the existing `randRead` seam to auth (R10), `TestBackoffRespectsMaxAfterJitter`
  (R11 — table test at the cap), `TestMetricsGaugesDoNotSTW` or a benchmark
  comparing `ReadMemStats` vs `runtime/metrics` (R12). `go test -race ./...` green.

---

## Session 21 — Frontend polish

Two small, low-risk cleanups that close a drift vector and unlock a
backend-supported feature the UI currently blocks.

- [ ] **R13** [Low] The live `connected_count` update duplicates the pluralization
  logic F16 (archived) extracted into `formatConnectedCount` precisely so the
  two surfaces couldn't drift. `formateur/websocket.js:176-177` still hardcodes
  `stagiaire${s} connecté${s}` inline; the initial render in `renderConfigHTML`
  (`formateur/renderers.js:476`) uses the helper. They agree today, but the whole
  point of F16 was to make drift impossible. **Fix:** replace the inline string
  with `formatConnectedCount(state.connectedCount)`.
- [ ] **R14** [Low/Medium] Re-reveal (correcting the answer key) is unreachable
  from the UI despite full backend support. `RevealAnswers` is explicitly
  idempotent (BL2/BL3, archived): re-clicking "Révéler" reverses the previously
  applied scores and applies the new ones so the trainer can change
  `correctColors` between reveals. But `renderVoteHTML`'s button matrix removes
  the reveal button entirely once `state.revealed === true`
  (`formateur/renderers.js:688-700`), and the reveal checkbox section
  (`renderCompetitiveSectionHTML`, `:709`) is gated on `!state.revealed`. A
  trainer who marks the wrong correct colors and reveals cannot fix it — the
  scoreboard shows wrong scoring for the whole class, and the only recovery is
  "New vote" (which resets the round). **Fix:** after reveal, keep the reveal
  section visible (with the current `correctColors` pre-checked) and keep the
  "Révéler" button alongside "New vote". The backend already handles re-reveal
  correctly.
- [ ] Tests: `formateur/renderers-snapshot.test.js` (+reveal section/button
  persist after `state.revealed`, R14), `formateur/websocket.test.js`
  (+`connected_count` update uses `formatConnectedCount`, R13). `npm test` green;
  `npm run build` clean.

---

## Verification checklist (per session, before marking `[x]`)

- `make fmt-check` clean; `go vet ./...` clean; `go test -race ./...` green.
- `npm run lint` clean; `npm test` green; `npm run build` clean.
- No regression against the archived "things checked and found correct" list in
  `IMPROVEMENTS-DONE.md` (lock ordering `h.mu → Manager.mu → Session.mu`,
  idempotent reveal reversal, atomic counter writes, constant-time token
  compares, `SetTrustedProxies` defaulting to empty).
