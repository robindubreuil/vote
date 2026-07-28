# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

---

## Session 1 — Critical one-liners

Small, independent, high-impact correctness bugs. Each verifiable in isolation.

- [x] **C1** `report_game_score` sends cumulative localStorage high score instead of per-round `game.score` — `frontend/src/stagiaire/handlers.js:292`. Competitive leaderboard inflates by lifetime high score on every won round.
- [~] **C2** _Audited; false positive._ Server intentionally splits the total (`hub.go:476` sends `TotalScore - gameScore`); the frontend's `+ (state.gameScore || 0)` correctly recomposes. No change.
- [x] **C4** `reset_vote` handler hardcodes `nil` for labels (vs `handleStartVote` which passes `msg.Labels`) — `backend/internal/hub/client.go:428`. Silent label loss after every reset/round.
- [x] **FH3** `isConnecting = false` runs before the `connectionId` guard in `onclose` — `frontend/shared/websocket-client.js:70-75`. Reconnect race leaks sockets.
- [x] **S10** `handleVote` has no client-type gate (trainer can call `vote`) — `backend/internal/hub/client.go:370`.
- [x] **S5** Backoff integer overflow: `1 << (Count-3)` overflows int64 around `Count=56`, collapses cap to 100 ms — `backend/internal/security/security.go:120-124`.
- [x] **BL4** Name length check runs before `TrimSpace` — `backend/internal/vote/validation.go:43-52`.
- [x] Add tests for each fix; run `go test -race ./...` and `npm test`.

## Session 2 — Frontend listener tracker + reconnect hygiene

Two reinforcing themes: the listener tracker is reused across app and per-render
lifecycles (causes silent UX regressions), and the WS client reconnect logic has
no offline awareness, no jitter, no max attempts.

- [x] **C3** `attachAppKeyboardShortcuts` is registered via the per-render listener tracker; `cleanupAllListeners()` after `session_created` silently kills the Escape-to-leave shortcut every session — `frontend/src/formateur/renderers.js:884-898`, `frontend/src/formateur/websocket.js:263`. **Fix:** split into three trackers (app / session / render). The Escape shortcut lives on the app tracker, which `cleanupAllListeners()` intentionally preserves so it survives both intra-session renders and session leave/rejoin.
- [x] **M1** Multiple `attachConfigListeners(client)` calls without cleanup grow the module-level Set unboundedly — `frontend/src/formateur/handlers.js`. **Fix:** `attachConfigListeners` and `attachVoteListeners` now self-clean the render tracker on entry, so direct calls from preset handlers no longer accumulate stale entries.
- [x] **FH1** Reconnect keeps retrying while `navigator.onLine === false`; no fast-retry on `online` event — `frontend/shared/websocket-client.js:100-112`. **Fix:** the client subscribes to window `online`/`offline`. Offline cancels the pending timer; online triggers an immediate reconnect if the client is still disconnected and not explicitly/permanently closed.
- [x] **FH2** No jitter → 30 clients reconnect in lockstep on server restart (thundering herd against the per-IP rate limiter); no max-attempts ceiling — `frontend/shared/websocket-client.js:100-112`. **Fix:** jitter (50–100% of base delay) plus a configurable `maxReconnectAttempts` (default 50 ≈ 16h backoff). After the ceiling, the client flips to `isPermanentlyClosed` and stops. Application close codes 4xxx do the same immediately.
- [x] **M5** Game `scoreAnimId` rAF not cancelled on `teardownGame` / `handleQuitGame` — `frontend/src/stagiaire/handlers.js:260-275, 366-393`. **Fix:** added `cancelScoreAnimation()` helper invoked from both teardown paths.
- [x] Add `websocket-client.js` tests (reconnect backoff, jitter, max attempts, `connectionId` race, close-code branching, offline/online) — `frontend/shared/websocket-client.test.js` (18 tests). Added C3/M1 regression test in `frontend/src/formateur/renderers.test.js` (6 tests) and M5 regression test in `frontend/src/stagiaire/handlers.test.js` (3 tests). Installed `happy-dom` as a dev dependency so WebSocket/window/navigator tests run against a real DOM-ish environment.

## Session 3 — Trainer auth model (architectural)

The single most impactful security change. Closes the trainer-takeover chain.

- [x] **S1** Trainer role is self-asserted; any client that knows the public 3-char session code (shown in the QR to every stagiaire) can `trainer_join` and kick the legitimate trainer — `backend/internal/hub/client.go:175`, `backend/internal/hub/hub.go:197-219`. Add a per-session trainer token minted at session creation, validated before any takeover.
- [x] **S2** "Session introuvable" branch in `handleTrainerJoin` skips `RecordFailedJoin` → 12,167 codes enumerable in ~20 min — `backend/internal/hub/client.go:205-208`.
- [x] **S3** `CheckOrigin` returns `true` for empty Origin — `backend/internal/server/server.go:280-286`. Browsers always send Origin on cross-origin WS; reject absence.
- [x] **S4** Dashboard logout has no server-side revocation; exfiltrated cookie remains valid up to 7 days — `backend/internal/server/auth.go:160-168`.
- [x] **S9** `shouldUseSecureCookie` trusts the `Host` header (allows `Host: localhost` to flip `Secure=false`) — `backend/internal/server/auth.go:90-97`.
- [x] **S11** Error messages echo client input and internal error text — `backend/internal/vote/manager.go:203`, `backend/internal/hub/hub.go:297`.
- [x] Add token-minting + auth tests; verify integration test for takeover rejection.

## Session 4 — Concurrency cluster under reconnect storms

Three reinforcing issues that all trigger on a 30-student wifi flap and compound.

- [x] **CC2** TOCTOU name uniqueness — `IsNameInUse` runs in client goroutine; actual map write happens later in `Hub.Run` without re-checking. Two "jean"s both register — `backend/internal/hub/client.go:255-300`, `backend/internal/vote/manager.go:107`. **Fix:** `JoinStagiaire` now performs an authoritative normalised-name collision check inside `session.mu.Lock` and returns `ErrNameInUse`. The advisory checks in `handleStagiaireJoin` remain (fast-fail + backoff) but the session lock is the single source of truth.
- [x] **CC3** Global hub write-lock held across JSON marshal + map copies + channel send on every register/unregister — `backend/internal/hub/hub.go:174-176`, `notifyTrainerStagiaireListLocked:495-544`. O(N²) under reconnect storm. **Fix:** `registerClient`/`unregisterClient` collect messages into a `[]pendingSend` during the critical section (cheap pointer/map snapshots only) and flush (marshal + channel send) in a deferred loop that runs **after** `h.mu.Unlock()`. `notifyTrainerStagiaireListLocked` → `buildTrainerStagiaireListLocked` (pure builder, returns `*pendingSend`). The lock is now held only for data collection, not for per-message JSON work.
- [x] **CC1** Trainer send buffer overflows under burst (30 votes fan out ~60 trainer-bound msgs); `default` branch disconnects the trainer — `backend/internal/hub/client.go:158-164`. **Fix:** `ClientSendBufferSize` raised 256 → 512; the buffer-full `default` branch is now role-aware via `trySend` — a **trainer** gets a dropped message (logged warn) instead of a torn-down connection, since disconnecting the trainer disrupts the entire class. Stagiaires are still evicted (they reconnect idempotently).
- [x] **CM3** `BroadcastSession` slow-client `default` branch closes the conn but leaves it in the map → warning spam until `pongWait` evicts — `backend/internal/hub/hub.go:415-422`. **Fix:** `BroadcastSession` checks `closing.Load()` before attempting `trySend`; on eviction `markClosing()` is set immediately, so every subsequent broadcast in the same storm skips the client silently instead of re-attempting and warn-spamming.
- [x] **CL1** Old client's `Send` channel never drained after reconnect-by-ID takeover — `backend/internal/hub/hub.go:301-306`. **Fix:** `Client.closing atomic.Bool` added. Both takeover paths (trainer + stagiaire) call `old.markClosing()` before swapping the pointer. `SendJSON` and `trySend` short-circuit on `closing`, so stale references captured by an in-flight `BroadcastSession` target list (or the pending-sends flush of a concurrent register) drop silently instead of pushing onto a dead channel.
- [x] Stress test: 30-client reconnect storm under `-race` — `backend/internal/hub/stress_test.go` (4 tests: `TestStressReconnectStorm`, `TestStressConcurrentNameRegistration`, `TestTrainerBufferFullDropsNotDisconnects`, `TestClosingFlagSkipsBroadcasts`). Plus `TestJoinStagiaireNameUniquenessUnderLock` in `manager_test.go` for the CC2 unit case.

## Session 5 — Business-logic correctness

- [x] **BL2** Rank semantic mismatch: ordinal at reveal vs competition on reconnect → same student sees rank 2 then rank 1 for tied scores — `backend/internal/vote/manager.go:342-344` vs `backend/internal/hub/hub.go:605-621`. **Fix:** `RevealAnswers` now assigns competition ranks (tied `TotalScore`s share a rank, next lower score skips) via `assignCompetitionRanks`, matching the on-the-fly `computeRank` used on reconnect.
- [x] **BL3** `RevealAnswers` scores regardless of `session.Competitive` flag (README scopes scoring to competitive mode) — `backend/internal/vote/manager.go:281-348`. **Fix:** cumulative `Scores`/`LastVoteScores`/`Revealed` mutation is gated on `session.Competitive`. Per-round `VoteScore` is still computed so the response carries correctness info.
- [x] **BM2** `start_vote` while Active silently discards in-progress votes — `backend/internal/vote/manager.go:122-152`. **Fix:** `StartVote` returns the new sentinel `ErrVoteAlreadyActive` ("un vote est déjà en cours") when state is already Active; Closed → StartVote (the legitimate next-round path) is unchanged.
- [x] **BM5** Labels validated against global `ValidColors`, not the selected palette — `backend/internal/hub/client.go:332-337`. **Fix:** `handleStartVote` and `handleResetVote` validate labels against `msg.Colors` (the ballot actually being configured).
- [x] **BM4** `VotesPerSession` histogram records `len(Votes)` at teardown (last round only), not lifetime — `backend/internal/vote/manager.go:397,411`. **Fix:** `Session.TotalVotes` accumulates every accepted `SubmitVote` call across rounds; `CleanupExpiredSessions` and `RemoveSession` observe that lifetime value instead of `len(Votes)`.
- [x] **BL6** Move duplicate-vote check inside `SubmitVote` (defense in depth) — `backend/internal/vote/manager.go:154-213`. **Fix:** `SubmitVote` rejects duplicates under the session lock; the handler-side check in `handleVote` stays as a fast-fail.
- [x] Tests for each: scoring under non-competitive, double start_vote, rank-tie invariants (`manager_test.go`: `TestStartVoteRejectsActive`, `TestSubmitVoteRejectsDuplicates`, `TestRevealAnswersCompetitionRankTies`, `TestRevealAnswersNonCompetitiveDoesNotScore`, `TestVotesPerSessionLifetime`; `client_test.go`: `TestStartVoteRejectsLabelsOutsidePalette`, `TestResetVoteRejectsLabelsOutsidePalette`). Plus a fix to `tests/e2e/ws-helper.ts` so the e2e WS suite passes the S3 `CheckOrigin` gate (the helper was sending no `Origin`, leaving the entire suite broken on main).

## Session 6 — Persistence correctness + graceful shutdown

- [x] **BM1** Histogram Restore matches buckets by exact LE; future schema change leaves in-memory histogram non-monotonic → Prometheus rejects the scrape — `backend/internal/vote/stats.go:160-174`. Refuse-to-restore on mismatch.
- [x] **BM3** `Observe` mutates `count`/`sumBits`/buckets non-atomically; concurrent `Snapshot` can capture inconsistent state → checkpoint skipped — `backend/internal/vote/stats.go:41-72`. Single mutex around Observe+Snapshot, or read order so worst case is `bucket ≤ count`.
- [x] **CM1+CM2** `Hub.Shutdown` cancels contexts but doesn't wait for `Hub.Run` / `cleanupLoop` / `writePump` to return; hijacked WS conns aren't drained — `backend/cmd/server/main.go:71-80`, `backend/internal/hub/client.go:106-136`. Add `sync.WaitGroup` to Hub, `<-ctx.Done()` case in `writePump`, iterate+close `Connections` on shutdown.
- [x] **FH4** Trainer reconnect re-sends `trainer_join` but never requests fresh state; assumes local snapshot is canonical — `frontend/src/formateur/websocket.js:81-87`. Either verify server pushes full snapshot on trainer_join, or add explicit `request_sync`.
- [x] **FH5** Service worker registered at scope `/` but caches `/formateur/` as the offline shell; a stagiaire reloading `/stagiaire/` offline gets the formateur landing — `frontend/scripts/sw.template.js`, `frontend/shared/pwa.js`. Narrow scope or pre-cache `/stagiaire/` separately.

## Session 7 — Resource caps + dashboard cookie hardening

- [ ] **S7** No `MaxClientsPerSession`, no `MaxSessionsGlobal`, no `MaxConnectionsPerIP` — `backend/internal/hub/hub.go:174`, `backend/internal/vote/manager.go:107`.
- [ ] **S6** Name-based identity reclaim inherits prior stagiaire's `Scores`/`GameScores` after disconnect (names are common, ≤16 chars) — `backend/internal/hub/client.go:276-290`. Require explicit reclaim token or never inherit scores across name-only matches.
- [ ] **S12** Reclaim-by-`stagiaireID` grants identity solely on knowing the ID — `backend/internal/hub/client.go:253`. Issue per-stagiaire reconnect token at first join.
- [ ] Dashboard cookie: add server-side revocation set (token ID in payload) so logout actually invalidates — `backend/internal/server/auth.go:160-168`.
- [ ] Tests: per-session cap rejection, reclaim-token required.

## Session 8 — Test coverage backfill

Reliability-critical modules that are essentially untested today.

- [ ] `frontend/shared/websocket-client.js` — reconnect backoff, `connectionId` race, close-code branching, send-while-down.
- [ ] `frontend/src/formateur/websocket.js` — every server-message state transition; `attachListeners` / `cleanupAllListeners` interaction.
- [ ] `frontend/src/stagiaire/websocket.js` — every server-message state transition; reset-on-`session_joined`.
- [ ] `frontend/src/formateur/renderers.js` — idempotency snapshot tests for the 899-line DOM builders.
- [ ] `frontend/src/stagiaire/handlers.js` — game lifecycle (rAF cancel, teardown), vote submit failure path.
- [ ] `frontend/shared/pwa.js` — SW update flow, `controllerchange` loop guard, offline toast.
- [ ] `frontend/shared/ui.js` — `showConfirmDialog` focus trap; toast reuse via Map instead of DOM re-query.
- [ ] `frontend/shared/dom/listeners.js` — the tracker whose misuse causes C3/M1.

---

## Notes from the audit (kept for context)

Things checked and found correct — do not regress:

- HMAC dashboard cookie scheme (constant-time compare, expiry bound into payload, version tag).
- Per-IP session-creation cap and per-client message cap (sliding window, server-generated keys).
- Dashboard CSP (`frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `no-referrer`).
- Message read limit (`client.go:76`, 4096 bytes) bounds array/map allocations.
- Idempotent reveal reversal — subtracts exactly the prior `LastVoteScores[id]` that was added.
- Blank-vote scoring — blank is skipped in the score loop, matches README.
- Atomic `counters.json` rewrite — temp + rename in same dir, 0600 perms.
- Lock ordering — `h.mu → Manager.mu → Session.mu` everywhere; no cycles.
- `CloseVote` twice is idempotent; vote after close is rejected.
- `SetTrustedProxies` defaults to empty — `ClientIP()` uses `RemoteAddr`, X-Forwarded-For is not trusted by default.
