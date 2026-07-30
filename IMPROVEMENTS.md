# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

Sessions 1–21 (audit rounds 1, 2 & 3) are archived in
[`IMPROVEMENTS-DONE.md`](./IMPROVEMENTS-DONE.md). The findings below are
**audit round 4** — a fresh end-to-end pass over the now-hardened codebase. Round
4 surfaced a small number of residual issues that survived the prior sessions;
they cluster naturally into five conversation-sized sessions. Item codes keep
their category prefix (S = security, F = frontend, R = round-3 residual backend,
B = backend) and continue each series (the archived log ends at S12, F22, R14),
so there is no code collision with anything already done.

---

## Session 22 — Trainer auth-chain hardening  ✓

The headline of round 4. Two coupled bugs in the trainer-join path that together
collapse S1's takeover protection. S14 makes live-session discovery cheap; S13
is the payoff. Both touch `security.go` + `hub.go` + the join handlers, so fixing
them together keeps the auth-chain reasoning in one head.

- [x] **S13** [High] Post-disconnect trainer hijack using only the public session
  code. The S1 trainer-token gate (`hub.go:418-438`) runs *only* when
  `conns.Trainer != nil`. Once the trainer's `readPump` exits,
  `unregisterClient` sets `conns.Trainer = nil` (`hub.go:706`), opening an empty
  slot any client with the **public** 3-char code (printed in every stagiaire's
  QR) can claim with no token. Worse, the recovery path re-emits the original
  token to the claimant (`hub.go:479-483`), and `CreateSession` mints it once and
  never rotates it — so the attacker now holds the same token as the legitimate
  trainer and can take it back indefinitely. CLAUDE.md's "the legitimate trainer
  can always take it back" assurance is false against a malicious stagiaire
  during a classroom wifi flap (they gain full session control: see every
  individual vote, reveal/change answers, start/close votes). **Fix:** require
  the trainer token whenever one has been minted for the session (always true
  post-`CreateSession`), even on the empty-slot recovery path; stop re-emitting
  `trainerToken` to unauthenticated claimants (an empty slot + no token should
  behave like `ErrSessionNotFound`, not a free takeover).
- [x] **S14** [Medium] Per-message join attempts bypass the exponential backoff
  (S2 residual). `CheckJoinRateLimit` — the backoff *enforcer* — is called in
  exactly two places: the WS upgrade (`server.go:389`) and dashboard login
  (`auth.go:245`). It is **never** called inside `handleTrainerJoin` /
  `handleStagiaireJoin`; `SendErrorWithBackoff` only *accumulates* backoff state
  (`LastBackoffUntil`) that nothing on an established connection reads. The only
  in-connection bound is the per-client message rate (10/s, burst 20). A single
  upgraded WebSocket can probe `stagiaire_join` / `trainer_join` against
  arbitrary codes at 10/s — the ~12,167-code space is enumerable in ~20 min from
  one connection (the oracle: `"Session introuvable"` vs proceeds). Live codes
  feed S13. **Fix:** call `CheckJoinRateLimit(c.IP)` at the top of both join
  handlers (before the advisory lookups), backing off on rejection — or block
  re-join attempts on a connection whose IP is already in backoff.
- [x] Tests: `TestEmptyTrainerSlotRequiresToken` (S13 — drain the trainer via
  `readPump` exit, assert a tokenless `trainer_join` is rejected, assert the
  legit-token holder still recovers), `TestTrainerTokenNeverReEmittedToTokenlessClaim`
  (S13 — recovery path does not hand out the token to an unauthenticated
  claimant), `TestJoinHandlerEnforcesBackoff` (S14 — prime an IP into backoff,
  assert `stagiaire_join`/`trainer_join` messages on an established connection
  are rejected while the IP is in the backoff window). `go test -race ./...`
  green.

---

## Session 23 — Frontend trainer event-wiring  ✓

Two independent bugs where formateur buttons don't behave, both invisible to the
existing tests because they mock out the piece that fails in production. Same
file cluster (`formateur/websocket.js`, `handlers.js`, `renderers.js`), same fix
shape (re-check the element after it's in the DOM / check the send return value).

- [x] **F23** [High] Formateur header buttons (`#leaveSessionBtn`,
  `#openConnectionAidBtn`) never receive click listeners in production. In the
  `session_created` handler, `renderFullLayout` emits an **empty**
  `<header id="app-header">` (`renderers.js:157`), then `attachHeaderListeners`
  runs (`websocket.js:133`) querying both buttons — they're `null`, so nothing
  binds (`renderers.js:220,232` are `if`-guarded). Only afterward does
  `updateHeader` (`websocket.js:155` → `renderers.js:198-210`) inject the
  buttons. On the reload-with-saved-session path it's worse: `app-content`
  already exists so the `if (!app-content)` block is skipped and
  `attachHeaderListeners` is **never called at all** (`main.js:78-81`). Result:
  the "Quitter" button and, critically, the QR / "Aide à la connexion"
  classroom-display button are dead for the entire session — a documented
  headline feature is unreachable via UI, and the aid button has no keyboard
  alternative. Tests missed it: `websocket.test.js` mocks `updateHeader`
  (`:52`), `renderers.test.js` pre-populates the header via `buildAppShell`
  (`:48-58`), inverting the production ordering. **Fix:** `updateHeader` now
  calls `attachHeaderListeners` after every fresh markup injection (the
  className fast-path returns early so there's no double-bind); the leave
  handler is registered once at module level (`registerHeaderLeaveHandler` from
  `main.js`) so it doesn't need to be threaded through every call site;
  `attachHeaderListeners` is self-cleaning (wipes `sessionTracker` first) so
  repeated injections can't accumulate stale references.
- [x] **F24** [Medium] Formateur action handlers silently swallow clicks when the
  WS is down. All four trainer actions (`startVote` / `closeVote` /
  `revealAnswers` / `resetVote`) gate only on `if (!client)` then call
  `client.send(...)` and **ignore its return value** (`handlers.js:188-256`).
  Contrast the stagiaire side (`stagiaire/handlers.js:630-644`) which checks
  `const success = client.send(...)` and restores the button + `showError` on
  false. The buttons are disabled at render via `${!isConnected ? 'disabled' :
  ''}`, but `onStatusChange` only calls `updateHeader` + `updateConnectionBanner`
  + `publishState` — it never re-renders the config/vote card. During a
  mid-session wifi flap the buttons keep their pre-drop enabled state; a click
  silently drops, no error shows, and the trainer proceeds believing the vote is
  closed/started/revealed while the message was never sent. **Fix:** all four
  handlers now capture `const ok = client.send(...)` and call `showError` on
  false (mirrors `submitVote`); additionally `onStatusChange` calls the new
  `updateActionButtonsState()` which cheaply toggles `disabled` on the action
  buttons so their state tracks `state.connected` live (non-disruptive —
  preserves in-progress label edits / preset form).
- [x] Tests: `formateur/websocket.test.js` (+`updateHeader` runs on the reload
  path so buttons get bound, +`updateActionButtonsState` fires on status change,
  F23/F24), `formateur/handlers.test.js` (new — each action handler shows
  `showError` and sends exactly once when `client.send` returns false, F24),
  `formateur/renderers.test.js` (+`updateHeader` re-attaches on fresh inject +
  idempotent fast-path + no double-bind across injections, +`updateActionButtonsState`
  live disabled tracking, F23/F24). `npm test` green (572); `npm run build` clean.

---

## Session 24 — Identity & registration invariants

Two state-invariant violations in the join/register path, both missed by prior
sessions that touched neighbouring code. They share the join→`registerClient`
area, so fixing them together keeps the invariant reasoning local. Both are
correctness bugs that degrade ranking/leaderboard integrity or leak resource-cap
slots.

- [ ] **S15** [Medium] The reclaim-rename path skips the authoritative
  name-collision check (CC2 residual). CC2 added an under-lock normalised-name
  collision check, but only on the **fresh-join** branch (`manager.go:206-213`).
  The reclaim branch (existing `stagiaireID` + valid token) overwrites
  `session.Stagiaires[stagiaireID] = name` directly (`manager.go:198`) with no
  re-check; the only guard is the advisory `IsNameInUse` check in the client
  goroutine (`client.go:439-446`), exactly the TOCTOU pattern CC2 was built to
  eliminate. Two stagiaires can end up sharing a normalised name, breaking the
  uniqueness invariant that gates ranking / leaderboard tie-breakers (`Sort by
  Name ASC` + `AssignCompetitionRanks`) and showing two indistinguishable rows in
  the trainer view. **Fix:** extract the `NormalizeName` collision loop into a
  shared helper and run it on both branches — including before the reclaim-path
  rename, excluding the reclaimer's own `stagiaireID`, returning `ErrNameInUse`
  on collision.
- [ ] **R16** [Medium] Stale `conns.Stagiaires` entry when a connection re-joins
  under a different ID. `Client.ID` is mutable across `stagiaire_join` messages
  on the same connection (`client.go:400-434`: reset to `OriginalID`, then
  overwritten to a presented `stagiaireId`). `registerClient` only cleans the
  *current* ID slot (`hub.go:595-604`); `unregisterClient` only deletes the
  *current* ID (`hub.go:710-711`). A client that registers as ID B after joining
  as ID A leaves `conns.Stagiaires["A"]` → same `*Client` forever — inflating
  `connected_count` (a phantom connected stagiaire the trainer can never clear)
  and leaking a `MaxClientsPerSession` slot per cycle. A malicious client
  cycling stolen `(id, token)` pairs can exhaust the session cap and block
  legitimate joins with `"Session complète"`. **Fix:** in `registerClient`,
  before assigning the new slot, scan for any prior registration of `client`
  under a different ID and delete it (O(N) per join is fine — N is bounded by the
  cap and the common case has zero matches).
- [ ] Tests: `TestJoinStagiaireReclaimRenameRejectsCollision` (S15 — two clients
  racing a reclaim-rename to the same normalised name; the second is rejected
  under the lock), `TestRegisterClientRemovesPriorIDSlot` (R16 — same `*Client`
  re-registered under a new ID; assert the old slot is gone and
  `connected_count` doesn't double-count; assert the cap isn't exhausted by
  cycling). `go test -race ./...` green.

---

## Session 25 — Persistence & metrics robustness

Three low-risk integrity fixes in `stats.go` / `store.go`, all about
histogram/counter correctness across clock jumps, corrupted state files, and
crash durability. Coherent cluster, isolated from the rest of the codebase.

- [ ] **R19** [Medium] Negative durations poison the session-duration histogram
  persistently. `observeEndedSession` computes lifetime as
  `time.Since(time.Unix(createdAt, 0))` (`stats.go:239-244`); `time.Unix(...)`
  returns a `Time` with **no monotonic component**, so `time.Since` falls back to
  wall-clock arithmetic. A backward NTP step / VM suspend-resume clock snap /
  manual `date -s` yields a negative value; `Observe` then adds it to every
  finite bucket and to `Sum`, `validHistogram` still passes, the poisoned state
  flushes to `counters.json`, restores on reboot, and never self-heals short of
  deleting the file. **Fix:** bound-check `d >= 0` before observing; optionally
  also reject `v < 0` inside `Observe` to defend the invariant for any caller.
- [ ] **R20** [Low] `validHistogram` does not validate `Sum`; NaN/Inf propagates
  to `/metrics`. `validHistogram` (`store.go:429-441`) enforces count/bucket
  invariants but never inspects `Sum`. A `counters.json` whose `sum` field is
  `NaN`/`+Inf`/`-Inf` (disk corruption, manual editing, or accumulated R19
  damage) unmarshals cleanly, passes validation, restores via `addLocked`
  (`stats.go:124-130` does `h.sum += snap.Sum` → `x + NaN = NaN`), and surfaces in
  `/metrics` as `..._sum NaN`, which breaks the in-tree dashboard's SVG sparkline
  renderer (`dashboard.go:174` assumes finite values) and most alerting rules.
  **Fix:** `if math.IsNaN(h.Sum) || math.IsInf(h.Sum, 0) { return false }` in
  `validHistogram` — `LoadCounters` already rejects the whole file on validation
  failure, which is the right recovery (start fresh).
- [ ] **R21** [Low] `SaveCounters` / `AppendSample` perform no `fsync`; the
  durability contract is overstated. `os.WriteFile` + `os.Rename` (counters) and
  `O_APPEND` Write (stats log) never `Sync()` (`store.go:134-150, 200-261`).
  Atomicity is at the **directory-entry** level only: a successful `Rename`
  doesn't mean the data blocks are durable, so a power loss in the window between
  `Rename` returning and the kernel flushing can leave `counters.json` empty or
  referring to unwritten extents. CLAUDE.md and `store.go:14-17` claim "atomic,
  no half-writes" — true at the rename level but overstated as durability. **Fix:
  (a)** correct the comments to distinguish "atomic visibility" from
  "crash-durability" (README's "worst-case crash loses at most one interval" is
  the real guarantee), or **(b)** add `f.Sync()` between WriteFile and Rename for
  `counters.json` only if true durability is wanted (accepting the per-flush I/O
  cost — keep `stats.jsonl` un-fsynced since it's an append-only lossy history).
- [ ] Tests: `TestObserveEndedSessionIgnoresNegativeDuration` (R19 — fake a
  `createdAt` in the future; assert no bucket/sum mutation), `TestObserveRejectsNegative`
  (R19 — if `Observe` gets the guard), `TestValidHistogramRejectsNaNSum` /
  `TestValidHistogramRejectsInfSum` (R20 — assert `LoadCounters` discards the
  file and starts fresh), a doc/comment assertion or no-op for R21. `go test
  -race ./...` green.

---

## Session 26 — Lower-priority cleanup

The remaining round-4 findings bundled into one tidy-up session. Each is small,
isolated, and independently verifiable.

- [ ] **R15** [Low/Medium] `handleWebSocket` post-upgrade ctx-race during
  shutdown (narrows R2). R2's drain guard (`server.go:380`) only catches requests
  that arrive *after* `h.ctx` cancels. Once a request passes the guard and
  reaches `upgrader.Upgrade` (`server.go:426`), the conn is hijacked;
  `http.Server.Shutdown` doesn't track hijacked conns, so `srv.Shutdown` can
  return while `handleWebSocket`'s post-upgrade tail (`NewClient`,
  `client.Start()` → `wg.Add(2)`) is still running. If that `wg.Add(2)` races
  `h.wg.Wait()` at counter zero, Go panics (`sync: WaitGroup is reused before
  previous Wait has returned`); the new client's `readPump` also isn't in the
  `h.Connections` snapshot `Hub.Shutdown` iterates, so it blocks in
  `ReadMessage` until `pongWait` (70 s). **Fix:** add a second guard immediately
  before `client.Start()` — `if s.hub.Context().Err() != nil { conn.Close();
  ReleaseIPSlot(clientIP); return }` — narrowing the race to a few instructions.
- [ ] **R17** [Low] `UpdateGameScore` skips the `GameEnabled` recheck.
  `handleReportGameScore` does an advisory `GetGameEnabled()` check, then calls
  `UpdateGameScore` (`client.go:687-698`); the manager method takes
  `session.mu.Lock` but only checks `Stagiaires[id]` existence, not `GameEnabled`
  (`manager.go:643-664`). A concurrent `ResetVote` (sets `GameEnabled=false`) in
  the race window leaves a stale score recorded against a disabled game for the
  session lifetime, feeding the competitive leaderboard. Every other
  stagiaire-state mutation re-validates under the session lock; this one doesn't.
  **Fix:** add `if !session.GameEnabled { return ErrGameDisabled }` inside
  `UpdateGameScore` after `session.mu.Lock`, mirroring `SubmitVote` /
  `RevealAnswers`.
- [ ] **S16** [Medium/Low] All per-IP protections collapse to a single shared
  bucket behind the documented reverse proxy. `TrustedProxies` defaults to empty
  (deliberate anti-spoofing), so `SetTrustedProxies([])` makes `c.ClientIP()`
  return `RemoteAddr`, which behind the recommended Caddy deploy
  (`reverse_proxy … localhost:8080`, `Caddyfile.example:15`) is `127.0.0.1` for
  **every** client. With it unset, `VOTE_MAX_CONNECTIONS_PER_IP=50` becomes a
  global 50-connection ceiling, the per-hour session cap is shared across all
  trainers, and the S2 failed-join backoff is shared — so one attacker's
  brute-force trips backoff that locks out every legitimate student (amplification
  DoS). `.env.example:12` documents `TRUSTED_PROXIES=127.0.0.1` but leaves it
  commented out, and `Caddyfile.example` never tells the operator to set it.
  **Fix:** make `Caddyfile.example` explicitly require
  `VOTE_TRUSTED_PROXIES=127.0.0.1` in the vote-server env (and uncomment it in
  `.env.example` for the proxy case); optionally emit a startup `slog.Warn` if
  `TrustedProxies` is empty while a high fraction of `ClientIP()` values are
  loopback.
- [ ] **F25** [Low] `isPermanentlyClosed` is never surfaced to the app. After
  `maxReconnectAttempts` (default 50 ≈ 16 h backoff), `scheduleReconnect` flips
  `isPermanentlyClosed = true` and logs to console (`websocket-client.js:200-204`)
  — but no callback fires. The last `onStatusChange(false)` is all the app sees,
  so the formateur reconnect banner (`updateConnectionBanner`, gated on
  `state.everConnected && !state.connected`) shows "Reconnexion…" **forever**
  after the client has internally given up. Practically unreachable, but if it
  ever triggers (server permanently moved / IP blocked) the trainer stares at a
  "reconnecting" banner with no signal to reload. **Fix:** add an
  `onPermanentClose` callback to the `VoteClient` options, invoke it where the
  flag is set, and wire the formateur/stagiaire banners to swap to a
  recoverable "Connexion perdue — rechargez la page" state.
- [ ] Tests: `TestShutdownRejectsUpgradeBetweenGuardAndStart` (R15 — under
  `-race`, force the context to cancel after the guard but before `Start`),
  `TestUpdateGameScoreRejectsWhenGameDisabled` (R17), a config/docs change for
  S16 (+ optional `TestWarnsOnLoopbackWithoutTrustedProxies`),
  `websocket-client.test.js` (+`onPermanentClose` fires once at the ceiling,
  F25). `go test -race ./...` green; `npm test` green.

---

## Verification checklist (per session, before marking `[x]`)

- `make fmt-check` clean; `go vet ./...` clean; `go test -race ./...` green.
- `npm run lint` clean; `npm test` green; `npm run build` clean.
- No regression against the archived "things checked and found correct" list in
  `IMPROVEMENTS-DONE.md` (lock ordering `h.mu → Manager.mu → Session.mu`,
  idempotent reveal reversal, atomic counter writes, constant-time token
  compares, `SetTrustedProxies` defaulting to empty).
