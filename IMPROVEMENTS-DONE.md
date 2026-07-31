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

- [x] **S7** No `MaxClientsPerSession`, no `MaxSessionsGlobal`, no `MaxConnectionsPerIP` — `backend/internal/hub/hub.go:174`, `backend/internal/vote/manager.go:107`. **Fix:** three configurable caps added (`VOTE_MAX_SESSIONS`, `VOTE_MAX_CLIENTS_PER_SESSION`, `VOTE_MAX_CONNECTIONS_PER_IP`). Per-session cap fires in `registerClient` before `JoinStagiaire`. Global cap fires in `handleTrainerJoin` before `GenerateSessionCode` via `Hub.AtSessionsCap`. Per-IP cap fires in `handleWebSocket` *before* the WS upgrade via `Hub.AcquireIPSlot` — a rejected dial never consumes a goroutine or fd. Slots are released by `unregisterClient` (deferred last so it runs after `h.mu.Unlock()`). A zero or negative config disables each cap independently (used by tests).
- [x] **S6** Name-based identity reclaim inherits prior stagiaire's `Scores`/`GameScores` after disconnect (names are common, ≤16 chars) — `backend/internal/hub/client.go:276-290`. **Fix:** `GetStagiaireIDByName` removed; `handleStagiaireJoin` no longer falls back to name matching. Two clients presenting the same name no longer collapse onto one identity — the second is rejected with `ErrNameInUse`. Name is reserved until the trainer resets the vote (the prior entry persists in `session.Stagiaires`).
- [x] **S12** Reclaim-by-`stagiaireID` grants identity solely on knowing the ID — `backend/internal/hub/client.go:253`. **Fix:** `Session.ReclaimTokens map[string]string` added; `JoinStagiaire` mints a 256-bit token (via `security.GenerateToken`) on every fresh join, returns it in `session_joined`, and constant-time compares it (via `crypto/subtle`) against the stored value on every reclaim-by-ID. Mismatch → `ErrReclaimUnauthorized` ("Session expirée — veuillez recréer votre identité"). Frontend persists it in `sessionStorage` (`vote_stagiaire_reclaim_token`) alongside the ID and auto-retries as a fresh identity on rejection.
- [x] Dashboard cookie: server-side revocation set with token ID in payload. **Done in Session 3 (S4)** — the cookie already carries a 16-byte nonce (`dashboardNonceBytes`) that makes each minting unique, and the revocation set is keyed by the full cookie value so per-token revocation is precise. No additional work.
- [x] Tests: `TestStagiaireReclaimRequiresToken`, `TestStagiaireNameReclaimRemoved`, `TestMaxClientsPerSessionCap`, `TestMaxSessionsGlobalCap`, `TestIPConnectionCap` (5 new hub tests). `TestJoinStagiaireReclaimTokenRequired` (manager unit test). Integration tests updated to capture + resend the reclaim token. E2E helper `connectStagiaire` extended with `reclaimToken` param.

## Session 8 — Test coverage backfill

Reliability-critical modules that were essentially untested on main. The
session backfills focused tests around state transitions, idempotency, and
the listener lifecycle (whose misuse caused C3/M1 in Session 2). Frontend
line coverage went 48.43% → 82.13%; 213 new tests across 7 files.

- [x] `frontend/shared/websocket-client.js` — done in Session 2 (18 tests; reconnect backoff, `connectionId` race, close-code branching, send-while-down, offline/online awareness).
- [x] `frontend/src/formateur/websocket.js` — `websocket.test.js` (32 tests). Every server-message state transition (session_created, connected_count, vote_started, vote_received, vote_closed, answers_revealed, vote_reset, config_updated, error → landing fallback) plus reconnect-toast `everConnected` guard, trainer-token persistence across `initClient`, publisher lifecycle, leaderboard computation in competitive mode, and the `attachListeners` idle→config / active→vote routing. FH4 (server-pushed sync on reconnect) covered by the `config_updated` suite.
- [x] `frontend/src/stagiaire/websocket.js` — `websocket.test.js` (28 tests). Every server message (session_joined with reclaim-token persistence + stale-identity drop + reset-on-join; vote_started with existingVote resume; vote_accepted; vote_closed; answers_revealed with totalScore recomposition `server.totalScore + local.gameScore`; vote_reset; name_updated). The S6/S12 reclaim-rejection retry-as-fresh-identity path is covered including the "no retry when name/code missing" guard.
- [x] `frontend/src/formateur/renderers.js` — original 6 tests (C3/M1) in `renderers.test.js` + 39 new snapshot/idempotency tests in `renderers-snapshot.test.js`. Covers `renderLandingPage` / `renderFullLayout` / `renderMainContent` idempotency, `updateLandingPageLoadingState` button-swap, `updateConnectionBanner` `everConnected` guard, `updateHeader` in-place class swap, `renderConfigHTML` color selection + start-button gating + competitive/blank-toggle visibility, `renderVoteHTML` active/closed/revealed button matrix, `renderColorBarsHTML` descending-count sort, `renderCombinationsHTML` empty state, `renderStagiairesVotesHTML` waiting/voted/blank/disconnected/anonymous variants.
- [x] `frontend/src/stagiaire/handlers.js` — extended from 3 (M5) to 26 tests. New coverage: `submitVote` failure path (button restored when `client.send` returns false), success path (loader inserted + button stuck disabled until `vote_accepted`), `handleSubmitVote` empty-selection no-op, `handleSingleChoiceVote` / `handleBlankVote` selection replacement, `handleCheckboxChange` state mutation + submit-button enable/disable, `handleJoin` validation (empty name, invalid code, lowercase normalization, `connectToSession` invocation), `handleEditName` validation + client `update_name` send, `handleKeyPress` Escape exits prenomEdit, `leaveSession` confirm-cancel vs confirm-ok full state reset.
- [x] `frontend/src/stagiaire/renderers.js` — `renderers.test.js` (36 tests). Every AppState (JOINING, WAITING, VOTING single+multiple, VOTED, CLOSED) plus the prenomEdit modal, `renderPlayGameButtonHTML` `gameEnabled` gating, single-choice selected/aria-pressed, multiple-choice submit-button gating + `allowBlank` button, score feedback competitive+revealed gating with `1er` vs `2e` rank suffix and negative-score rendering, idempotency (re-render produces identical HTML), inline-handler wiring (changeVoteBtn/editNameBtn/cancelEditName), focus preservation across re-renders.
- [x] `frontend/shared/pwa.js` — `pwa.test.js` (15 tests). ServiceWorker update flow (`updatefound` → `installed` → toast; initial `waiting` worker; first-install suppression when no controller), Recharger button posts `SKIP_WAITING` to the waiting worker or hard-reloads when none, close button removes the toast, toast idempotency, **`controllerchange` loop guard** (only one reload even if the event fires twice), offline/online toast visibility + idempotency, 60-minute `reg.update()` polling cadence + rejection swallowing, dev-mode no-op, no-SW no-op.
- [x] `frontend/shared/ui.js` — `ui.test.js` (30 tests). `showConfirmDialog` focus trap (Tab wraps last→first, Shift+Tab wraps first→last), Escape cancels with `preventDefault` + `stopImmediatePropagation` (so app-level Escape shortcuts don't double-fire), Enter confirms, backdrop click cancels, focus restoration to the trigger on resolve, `danger:false` swaps OK to `btn-primary`, no-title hides the title element, overlay reuse across calls (no DOM accumulation). `showError` 5s auto-hide timer + reset-on-recall + `showError(null)` immediate hide + `hideError` cancel. `showToast` reuse via the `activeToasts` Map (same message → no duplicate node), fade-out sequence (1000ms visibility + 200ms removal), `info`/`error` type classes, container reuse across toasts, empty-message no-op. Plus `renderFooterHTML` + `renderSessionCodeButton` snapshot coverage.
- [x] `frontend/shared/dom/listeners.js` — `listeners.test.js` (10 tests). The tracker whose misuse caused C3/M1: `track` selector-string vs Element resolution, missing-element no-op, `trackAll` multi-element binding + empty-match no-op, `cleanup` removes listeners + idempotent, post-cleanup tracking works, two trackers are independent, and the same `(target, event, handler)` triple tracked twice removes both on cleanup.

**Coverage result:** frontend 48.43% → 82.13% lines. Per-file: `shared/ui.js` 5% → 97%, `shared/pwa.js` 0% → 95%, `src/stagiaire/websocket.js` 1.78% → 98%, `src/stagiaire/renderers.js` 3.14% → 89%, `src/formateur/websocket.js` 0% → 83%, `src/stagiaire/handlers.js` 46% → 77%. Added `@vitest/coverage-v8` as a dev dependency so `npm run test -- --coverage` works out of the box.

---

## Sessions 9+ — Audit round 2

Fresh audit pass over areas untouched by sessions 1–8 (DevOps/packaging, accessibility, observability, ops hygiene, defensive validation at trust boundaries). Findings grouped into conversation-sized sessions.

### Session 9 — Critical frontend crashes + UX bugs

Isolated, high-impact, fast. Each verifiable on its own.

- [x] **F1** `localStorage.getItem` is unwrapped in `loadHighScore`, `hasSeenRules`, `loadStreak` — `frontend/shared/game-storage.js:11,18,55`. Writes are wrapped in try/catch, reads are not. Firefox "never remember history" mode and embedded WebViews throw `SecurityError` on `getItem`; the stagiaire app reads `loadHighScore` synchronously on join → crash. **Fix:** reads route through `safeLocalGet`; `_resetForTests` through `safeLocalRemove`.
- [x] **F2** Same pattern in `getLastConfig`, `listPresets`, `_resetForTests` — `frontend/shared/presets.js:88,107,257`. Crashes the formateur's first render (last-config autoload on `session_created`). **Fix:** route through `safeLocalGet` / `safeLocalRemove`.
- [x] **F3** `qrcode` (~116 KB pre-gzip) statically imported by the formateur entry chunk but used only in the `?aide=` branch — `frontend/src/formateur/main.js:14`. Every formateur page load pays for a dependency most trainers never open. **Fix:** dynamic `import('./connection-aid.js')` inside the aid branch. Verified: build now emits `connection-aid-*.js` as a separate 29 KB chunk (10.9 KB gzip); formateur entry dropped from ~70 KB to 41 KB.
- [x] **F4** Preset-name `<input>` keydown handler called `preventDefault` but not `stopImmediatePropagation` on Escape → event bubbled to the app-level keydown handler → "Quitter la session?" confirm opened on top of the just-closed preset form — `frontend/src/formateur/renderers.js:384-392`. **Fix:** `stopImmediatePropagation` on Escape (and Enter for symmetry).
- [x] **F5** `stagiaire/handlers.js:569-599` `leaveSession` manually reset state but omitted 7 fields (`prenomEdit`, `voteScore`, `totalScore`, `gameScore`, `rank`, `totalStagiaires`, `revealed`) plus `competitive`/`allowBlank`/`colorLabels`/`multipleChoice`/`gameEnabled`/`gamePlaying`. Formateur has `resetTrainerState()`; stagiaire had no symmetric helper. **Fix:** extracted `resetStagiaireState()` in `state.js` (preserves `prenom`), called from `leaveSession`.
- [x] Tests: `game-storage.test.js` +4 (read-throws for loadHighScore/hasSeenRules/loadStreak + removeItem-throws), `presets.test.js` +3 (read-throws for getLastConfig/listPresets + removeItem-throws), `renderers.test.js` +2 (Escape + Enter isolation in preset input), `stagiaire/handlers.test.js` +1 (leaveSession resets all 13 fields). Frontend 403 → 413 tests, all green; `npm run lint` clean; `npm run build` emits the split chunk; backend `go test -race ./...` unchanged green.

### Session 10 — Backend robustness cluster

Sentinel error mapping, metrics-text safety, atomic counters, store self-heal.

- [x] **B1** `Counter` guards a single int64 with `sync.RWMutex`; `Inc` takes a write lock on every vote and every join — `backend/internal/vote/stats.go:12-33`. Wrong primitive. **Fix:** `atomic.Int64` backed Counter. The hot path (Inc on every vote / Add on every boot restore) is now lock-free; readers (`Value()`) are wait-free. API is unchanged (pointer-receiver methods) so existing call sites compile untouched. Added contention benchmarks (`BenchmarkCounterIncContended`, `BenchmarkCounterReadHeavy`); the existing `TestCounterConcurrentAdd` keeps `-race` honest.
- [x] **B6** `writeInfoMetric` interpolates `version`/`buildTime` into Prometheus label values with no escaping — `backend/internal/server/metrics.go:113-116`. A `"`, `\`, or `\n` in the version (operator-settable via ldflags) produces a malformed scrape → Prometheus rejects the **entire** scrape. **Fix:** `escapeLabelValue()` helper applied at the label site (escapes `\`, `"`, `\n` to their two-character Prometheus forms). Table test (`TestWriteInfoMetricEscapesLabelValues`) covers plain ASCII, each special individually, and the all-three combined case; asserts each metric stays on exactly one physical line.
- [x] **B3** Many leaf errors still flow raw to French clients: `"vote is not active"`, `"stagiaire not found in session"` (leaks internal entity name), `"only one color allowed..."`, lowercase `"unauthorized"`. Inconsistent with the capitalized French sentinels added in S11. **Fix:** French sentinel errors (`ErrVoteNotActive`, `ErrSingleChoiceOnly`, `ErrDuplicateColors`, `ErrBlankNotAllowed`, `ErrBlankWithColors`, `ErrAtLeastOneColor`, `ErrInvalidColor`, `ErrVoteNotClosed`, `ErrStagiaireNotFound`, `ErrNotAuthorized`) replace every leaf `errors.New(...)` in `manager.go`; handler-side `"unauthorized"` literals in `client.go` and the remaining English handler strings (`"Invalid session code"`, `"Invalid color(s)"`, etc.) are now French; the central `vote.UserFacingError(err)` helper is the single client-boundary mapper, used by every hub handler that forwards manager errors. `registerClient`'s join-error switch collapses to `vote.UserFacingError(err)` so the internal sentinels (`ErrSessionNotFound`, `ErrInvalidInput`) map to safe French strings rather than leaking. Table test (`TestUserFacingErrorMapping`) covers every sentinel + the unknown-error fallback; `TestUserFacingErrorRejectsEnglishSentinels` guards against regressions of the specific English leaves called out in the audit.
- [x] **B9** Store rotation: if reopen fails after rename, `logFile` points at the closed/renamed-away handle → every subsequent `AppendSample` fails silently until restart — `backend/internal/store/store.go:193-216`. **Fix:** factored `reopenLocked()`; rotation always nils the handle (success or failure), reopen failure leaves `logFile == nil`, and `AppendSample` self-heals at the top via `if s.logFile == nil { reopenLocked() }` so the sampling goroutine recovers from a transient FS fault (close, rename failure, perms revoked, external truncation) instead of poisoning every subsequent write. The post-`Write` failure path retries once via the same reopen helper. Tests: `TestAppendSampleReopensAfterClose` (Close + AppendSample self-heal), `TestRotationReopenFailureLeavesNilHandle` (forced nil handle, failed reopen, restored perms → recovery), `TestAppendSampleRecoversFromExternalLogTruncation` (write-retry path after the fd is closed out-of-band).
- [x] **B11** `BroadcastSession` marshals the payload before checking the session exists — `backend/internal/hub/hub.go:678-690`. Broadcasts to dead sessions pay full marshal cost. **Fix:** existence check + target snapshot run under `RLock` first; if the session doesn't exist OR the target list is empty (e.g. `excludeID` matches everyone), the function returns before marshalling. Tests: `TestBroadcastSessionSkipsDeadSessionWithoutMarshal` (uses a `MarshalJSON` counter to assert zero marshal calls for unknown sessions and zero-target broadcasts; asserts exactly one marshal for a live session) and `TestBroadcastSessionExcludedClientNotDelivered` (excludeID contract still honoured when other clients ARE present).
- [x] **B10** `Client.LastActivity` is written but never read — dead field — `backend/internal/hub/client.go:41,60,109`. **Fix:** removed the field and its two write sites (constructor + PongHandler). An idle-evictor was the alternative offered by the audit, but the pong-deadline already reaps dead connections (no idle-evictor is currently wired), so the field was pure dead weight; reintroducing it later with a real reader is straightforward.

### Session 11 — Backend perf + observability

- [x] **B2** `store.ReadSamples` reads both `stats.jsonl*` (~100 MB) fully into memory, then applies `limit`, while holding `s.mu` — `backend/internal/store/store.go:223-253`. `/dashboard/history` OOM risk on small VMs; sampler stalled by slow read. **Fix:** paths snapshotted under `s.mu` then released; files streamed via `bufio.Scanner` with a 1 MiB buffer cap (oversized lines recover gracefully); a fixed-size `ringSample` ring buffer holds only the tail when `limit > 0`, bounding memory to O(limit) regardless of file size. `TestReadSamplesDoesNotBlockConcurrentAppend`, `TestReadSamplesReleasesLockBeforeRead`, `TestReadSamplesRingBufferTail`, `TestReadSamplesRingBufferWrapOrder`, `TestReadSamplesMergesBackupAndLive`, `TestReadSamplesScannerBufferSkipsPathologicallyLongLine`, `TestStoreStreamingReadLargeLog` (7 new tests).
- [x] **B8** `Hub.GetMetrics` holds `h.mu.RLock` across up to `MaxSessionsGlobal`=1000 `session.GetState()` calls — `backend/internal/hub/hub.go:937-966`. Blocks all register/unregister during a scrape. **Fix:** snapshot Connections-derived counters and release `h.mu` before iterating the session slice (which `VoteManager.GetAllSessions` already returns lock-free). `TestGetMetricsDoesNotHoldHubLockDuringSessionIteration` (races a scrape against `SessionExists` probes; 50ms-per-probe budget catches the old contention), `TestGetMetricsCorrectnessAfterLockRelease`.
- [x] **B7** No HTTP access log / request-ID middleware; hub/client error logs lack session/IP/client correlation — only `gin.Recovery()` is wired. **Fix:** `internal/server/middleware.go` adds `requestIDMiddleware` (8-byte hex ID, preserved from inbound `X-Request-ID` up to 64 chars; regenerated otherwise) + `accessLogMiddleware` (slog structured line per request, level-routed by status: 2xx→INFO, 4xx→WARN, 5xx→ERROR, /health & /metrics→DEBUG so the polling cadences don't drown the log). Hub side: new `Client.RequestID` captured at the WS handshake, plus `Client.logAttrs()` helper that emits `client_id`, `session`, `ip`, `request_id`, `role`. Applied to every `slog.Error`/`slog.Warn` in `hub/client.go` (readPump, writePump, handleMessage, SendJSON, trySend, rate-limit) and `hub/hub.go` (registerClient, BroadcastSession, pendingSend.flush). 11 middleware tests + `TestWebSocketPropagatesRequestIDToClient` (2 subtests).
- [x] **B4** `GenerateID` calls `rand.Int` 12 times per connection (one syscall + big.Int per byte) vs `GenerateToken`'s single `rand.Read` — `backend/internal/security/security.go:344-357`. Hot path under reconnect flaps. **Fix:** single batched `rand.Read` of `clientIDLength+4` bytes (headroom for the ~1.6% rejection rate), rejection-sampled against the 252-bound (256 − 256%36) so the modulo mapping stays unbiased; top-up read only on the vanishingly-rare exhaustion. Benchmark dropped from 12 syscalls/big.Ints per ID to ~92 ns/op, 1 alloc/op. `TestGenerateIDProducesValidChars`, `TestGenerateIDCharsetUniformity` (±15% per bucket over 60k chars), `TestGenerateIDFallbackProducesValidID`, `BenchmarkGenerateID`, `BenchmarkGenerateIDParallel`.
- [x] **B5** `getEnvDuration`/`getEnvInt` silently return the default on parse error — `backend/internal/config/config.go:135-151`. Ops typo invisible. **Fix:** `slog.Warn` on parse failure (key, value, default, error); new `splitList` helper filters empty/whitespace elements so `"a,,b,"` no longer yields phantom `""` list entries (applied to ALLOWED_ORIGINS, TRUSTED_PROXIES, VALID_COLORS). `TestGetEnvDurationWarnsOnParseError`, `TestGetEnvDurationNoWarnOnValid`, `TestGetEnvDurationNoWarnOnMissing`, `TestGetEnvIntWarnsOnParseError`, `TestGetEnvIntNoWarnOnValid`, `TestSplitListFiltersEmptyElements`, `TestLoadConfigFiltersEmptyListElements`, `TestLoadConfigWarnsOnBadDuration` (8 new tests; `internal/config` coverage to 100%).
- [x] Tests for each. `go test -race ./...` green; `make test-integration` green; `gofmt` clean.

### Session 12 — Frontend accessibility + safety nets

The biggest UX gap — a11y was never audited.

- [x] **F6** Stagiaire layout has no `<main>` landmark and emits `<header>` per-state (missing entirely from `renderJoinHTML`/`renderEditNameHTML`) — `frontend/src/stagiaire/renderers.js:17-89`. Formateur has stable `<header>` + `<main>`. **Fix:** `renderLayout` now emits a single stable `<header id="app-header">` (hidden when empty) and wraps the container in `<main id="main-container">`. A new `renderStagiaireHeaderHTML()` helper, called from `updateView`, populates the header for WAITING/VOTING/VOTED/CLOSED and leaves it hidden for JOINING and the edit-name modal. Every per-state renderer was stripped of its inline `<header>` block, so exactly one banner landmark is in the a11y tree at all times.
- [x] **F7** Vote grid lacks grouping semantics — single-choice is a `<div>` of buttons, multiple-choice is loose checkboxes — `frontend/src/stagiaire/renderers.js:293,322`. SR users hear "Rouge, Vert…" with no context. **Fix:** single-choice wrapper exposes `role="radiogroup"` + `aria-label`; each button switched from the toggle pattern (`aria-pressed`) to `role="radio"` + `aria-checked` so the semantics match the single-select behaviour (picking one clears the others). Multiple-choice options are now wrapped in a `<fieldset>` with a visually-hidden `<legend>` (CSS reset strips fieldset defaults so the visual layout is unchanged).
- [x] **F8** Form-validation errors add `.error` class but never `aria-invalid` / `aria-describedby` — `frontend/src/stagiaire/handlers.js:418,425,454`. SR users get no field-level signal. **Fix:** join form's `.error-message` gets `id="join-error"`; both inputs reference it via `aria-describedby` and start with `aria-invalid="false"`. Edit-name modal gets its own `#edit-name-error` slot (previously errors were silent in that state — `showError()` couldn't find a `.error-message` element). A shared `setFieldInvalid(input, bool)` helper in `handlers.js` toggles `aria-invalid` and `.error` symmetrically on every validation path. `aria-invalid="false"` (rather than removing the attribute) keeps the DOM state symmetric with the freshly-rendered default.
- [x] **F10** 10 sites interpolate `color.color` raw into `style="background-color:..."` with no `^#[0-9a-f]{3,8}$` guard — `frontend/src/formateur/renderers.js:481,556,709,738,781,785,823,887`; `utils.js:142,146`. Safe today (COLORS is constant), tripwire if custom palettes land. **Fix:** new `sanitizeColor()` helper in `shared/colors.js` enforces a strict hex regex (3/4/6/8 digits — the only CSS-valid hex lengths; rejects named colours, `rgb()/hsl()`, `url(...)`, broken lengths, JS expressions, style-attribute breakout payloads). Applied at all 10 sites in `renderers.js` and `utils.js`. The fallback is itself validated so a future caller can't poison the helper with a bad default.
- [x] **F11** No `unhandledrejection` / `error` handlers in either `main.js`. Dynamic `import('./renderers.js')` in `utils.js:207` is fire-and-forget — failed import = stale vote panel, no UI signal. **Fix:** new `shared/error-boundary.js` exports `installGlobalErrorHandlers()` (registers window-level `error` + `unhandledrejection` listeners; both log to console and surface a single rate-limited toast; opaque cross-origin `"Script error."` reports and `AbortError` rejections are swallowed; idempotent — a stable no-op is returned on repeat installs). Both `main.js` files call it at startup. `guardDynamicImport()` wraps the dynamic import in `utils.js` so chunk-load failures surface a toast instead of stalling the panel. Two new i18n keys (`unexpectedError`, `sessionError`).
- [x] **F12** `connection-aid.js` starts `setInterval(checkStale, 2000)` and registers an anonymous `keydown` on `document`, neither cleared on `beforeunload` — `frontend/src/formateur/connection-aid.js:219,255,266`. **Fix:** `attachListeners` returns a teardown function that removes the now-named `keydown` handler. `initConnectionAid` captures both the stale-check interval id and the teardown, then registers a single idempotent `teardown()` on both `beforeunload` (desktop) and `pagehide` (mobile Safari, where `beforeunload` is unreliable) with `{ once: true }`. `subscriber.close()` is called inside teardown and is itself idempotent.
- [x] Tests / snapshot updates for each. **52 new frontend tests** across 6 files: `shared/colors.test.js` (new, 12 tests — strict-hex regex coverage incl. CSS-function rejection, style-attribute breakout payloads, non-string inputs, fallback validation), `shared/error-boundary.test.js` (new, 11 tests — error/rejection surfacing, opaque-script-error filter, AbortError filter, idempotency, rate-limiting, teardown, `guardDynamicImport` success/failure/context-label), `src/stagiaire/renderers.test.js` (+13 tests — F6 landmark structure, F7 radiogroup/fieldset semantics, F8 aria-describedby wiring; updated the existing `aria-pressed` assertion to `aria-checked`), `src/stagiaire/handlers.test.js` (+5 tests — F8 aria-invalid toggle on `handleJoin`/`handleEditName` for both failure and success paths), `src/formateur/renderers-snapshot.test.js` (+4 tests — F10 sanitization tripwire on `renderColorBarsHTML`/`renderCombinationsHTML`/`renderStagiairesVotesHTML` with poisoned values), `src/formateur/connection-aid-lifecycle.test.js` (new, 5 tests — F12 named keydown listener, interval id tracked, beforeunload/pagehide teardown, idempotent teardown). Frontend 413 → 465 tests, all green; `npm run lint` clean; `npm run build` produces the same chunks (the dynamic import in `utils.js` was already ineffective — F14 in Session 13 will hoist it); `go test -race ./...` unchanged green.

### Session 13 — Frontend polish + i18n consolidation

- [x] **F9** Hardcoded French strings bypass `t` in ~15 sites (`pwa.js:66-91`, all game aria-labels, `'Anonyme'`, `'Session expirée...'`). Even for a French-only app, `t` is the single point for typo fixes. **Fix:** new `t.common` keys (`anonymous`, `pwaUpdateAvailable`, `pwaReload`, `pwaClose`, `pwaOffline`); new `t.stagiaire` keys for the game board aria-labels (`gameBoardAriaLabel`, `gamePaletteAriaLabel`, `gameSlotEmpty/Filled`, `gamePegBlack/White`, `gamePegBlackDesc/WhiteDesc`, `gameRulesOk`), level-progress title helper (`gameLevelProgressTitle(toNext, level)`), `gameLevelMax`, and `sessionExpired`. All four pwa.js toast strings now route through `t.common.*`. The 14 `t.stagiaire?.X || 'literal'` defensive patterns in `stagiaire/renderers.js` collapsed to direct `t.stagiaire.X` accesses (every key is defined). `'Anonyme'` literals across `formateur/{utils,vote-data,websocket,connection-aid,renderers}.js` and `formateur/websocket.js` use `t.common.anonymous` (the duplicate `t.formateur.anonymous` was removed). The fragile `errorMessage === 'Session expirée…'` literal compare in `stagiaire/websocket.js` now compares against `t.stagiaire.sessionExpired`, with a docstring pointing at the server's matching sentinel in `backend/internal/vote/errors.go`.
- [x] **F13** `vite.config.js` has no explicit `build.target`. SW uses `Promise.allSettled` + optional chaining — breaks old Chromium silently. **Fix:** `build.target: 'es2019'` (covers ~Chrome 80+ / Firefox 74+ / Safari 13.1, every classroom browser we've seen in the wild). The SW template was rewritten to use `Promise.all` + per-promise `.catch(() => {})` instead of `Promise.allSettled` (the SW is shipped untranspiled by `gen-sw.js`, so it must avoid ES2020 primitives natively). Verified: production bundle contains zero `?.` and zero `Promise.allSettled`.
- [x] **F14** Dynamic `import('./renderers.js')` on every `vote_received` — `frontend/src/formateur/utils.js:207`. Smell (presumably breaks a circular dep), and a failed import silently stalls the vote panel. **Fix:** extracted the pure data helpers (`getColorCounts`, `getCombinations`, `sortStagiaires`) into a new `src/formateur/vote-data.js` module. `renderers.js` now imports them from there (not from `utils.js`), so `utils.js` can statically import `renderCombinationsHTML` / `renderStagiairesVotesHTML` and the dynamic import + `guardDynamicImport` wrapper are gone. Vite no longer emits the `INEFFECTIVE_DYNAMIC_IMPORT` warning for the cycle.
- [x] **F15** `stagiaire/main.js:52-55` persists `prenom` in `localStorage` (device-scoped) while S6/S12 carefully scoped identity to `sessionStorage`. Shared-tablet leak. **Fix:** `prenom` now lives in `sessionStorage` (`vote_stagiaire_prenom`). Closing the tab clears it; a different student on a shared tablet starts fresh instead of auto-joining under the previous user's name. `leaveSession` keeps the prenom (so an explicit leave/rejoin within the same tab still works), matching how `resetStagiaireState` already preserves it.
- [x] **F16** Duplicated pluralization logic in `connection-aid.js:141` and `renderers.js:464`. **Fix:** extracted `formatConnectedCount(n)` into `shared/utils/format.js`. The helper enforces the French agreement rule (`n > 1 ? 's' : ''`) so the two UI surfaces ("N stagiaire(s) connecté(s)" in the trainer config card and in the connection-aid view) cannot drift again.
- [x] **F17** `_resetForTests`, `_constants` exported from production modules. Bundle bloat + console-callable surface. **Fix:** test surface in `game-storage.js`, `presets.js`, and `stagiaire/game.js` is now gated behind `import.meta.env.DEV`. In dev (vitest, dev server) `_test` exposes `{ resetForTests, constants }` (or the game internals); in production Vite statically replaces `DEV` with `false` and Rollup's DCE collapses the export to `null`. Verified: production bundle contains zero `resetForTests` references. `pwa.js`'s unused `_test` export and the dead `updateAvailable` flag were deleted outright.
- [x] **F18** `validateName` rejects periods/commas; doesn't trim before length check — `frontend/shared/validation.js:19-29`. **Fix:** trim-first, mirroring the backend's `IsValidName` (BL4) so "  aaaaaaaaaaaaaaaa  " (16 letters padded to 20) is now valid. The character restriction (letters, digits, spaces, hyphens, apostrophes only) is documented in the JSDoc and pinned by tests — periods/commas stay rejected because classroom first-names don't need them and the backend's `hasValidCharacters` enforces the same set.
- [x] Tests for each: 465 → 525 tests, all green. New files: `shared/utils/format.test.js` (7), `shared/validation.test.js` (15), `shared/i18n.test.js` (10, including F17 DEV-gating contract), `src/formateur/vote-data.test.js` (9), `scripts/vite-config.test.js` (1), `src/stagiaire/main-prenom-storage.test.js` (3), `frontend/f14-cycle.test.js` (6), `frontend/pwa-i18n.test.js` (5). Extended: `scripts/sw.template.test.js` (+4 for the ES2020-primitive guard), `src/stagiaire/handlers.test.js` (+2 for F15 prenom storage boundary). `npm run lint` clean; `npm run build` clean (no `INEFFECTIVE_DYNAMIC_IMPORT` warning, zero `?.` in shipped JS, zero `resetForTests` references); `go test -race ./...` unchanged green.

### Session 14 — CI / release hardening

- [x] **D1** `release.yml` builds Docker image but never pushes; no GHCR, no Trivy scan, no SBOM, no cosign. **Fix:** new `docker` job (release.yml) builds + pushes to `ghcr.io/<owner>/<repo>` (lowercased), Trivy image scan (fail on `CRITICAL`, `ignore-unfixed`), cosign **keyless** sign of the pushed digest (`id-token: write`), SPDX SBOM via `anchore/sbom-action` attached to the GitHub Release, buildx `provenance: true` + `sbom: true` attestations on the registry. The `release` job now `needs: [build-deb, docker]` and attaches both `vote_*.deb` and `sbom.spdx.json`. Trivy scans the **built image** (base-image + artifact CVEs) where it adds unique value; repo-level vuln detection is delegated to govulncheck + npm audit in the CI security job (D6) to avoid duplicating findings and false-positiving on dev-tool advisories.
- [x] **D3** `ci.yml:69-73` frontend job runs lint+test but never `npm run build`. SW template / version / compress-assets regressions merge to main, fail only at release. **Fix:** added `npm run build` step to the frontend job (after lint+test).
- [x] **D4** `debian/rules:31` runs plain `npm ci` (Dockerfile uses `--ignore-scripts`). Supply-chain gap. **Fix:** `npm ci --ignore-scripts`. Verified: `dpkg-buildpackage -b` succeeds end-to-end; the `.deb` contains all 20 built/compressed frontend assets, so install scripts were never needed.
- [x] **D5** No tag/changelog version consistency check on release. A mistagged `v1.4.0` against a `1.3.1-1` changelog ships a mislabeled `.deb`. **Fix:** `build-deb` job asserts `GITHUB_REF_NAME` (minus leading `v`) equals the upstream portion of `debian/changelog` head (`<version>-<revision>` → `<version>`); fails fast with a `::error::` before any build step.
- [x] **D6** No `govulncheck` / `npm audit` / Trivy in CI. CVEs undetected. **Fix:** new `security` job (ci.yml) runs **govulncheck** on the backend (precise — only called-code vulns; `work-dir: backend`) and **npm audit** on frontend **production deps** (`--omit=dev --audit-level=high`) — the only deps that ship, since the app is a bundled SPA where everything else is a build-time devDependency (the high dev-tool advisories — postcss/brace-expansion/esbuild — are handled by the existing weekly Dependabot `npm` config, not by gating every PR). Trivy runs at release on the built image (D1). All three tools are now present in the pipeline.
- [x] **D7** `release.yml:30-34` `setup-node` cache missing `cache-dependency-path` (package-lock is in `frontend/`) → cache never hits. **Audited; already present** — both `setup-go` and `setup-node` in release.yml already carry `cache-dependency-path` (backend/go.sum, frontend/package-lock.json); ci.yml likewise. No change.
- **SHA-pinning + automation:** every third-party action across both workflows is now pinned to an immutable commit SHA with a trailing `# vN` version comment (Dependabot `github-actions` ecosystem, already configured, bumps them on a weekly cadence, so the pins never go stale). All workflow YAML validated with `actionlint` (release.yml exit 0; ci.yml's only warnings are two pre-existing shellcheck notes in the untouched e2e job). `npm run build`, `npm run lint`, `npm test` (525 tests), and `go test -race ./...` all green; `dpkg-buildpackage` green.

### Session 15 — Infra / deployment hardening

- [x] **D2** No `/version` HTTP endpoint. **Fix:** `GET /version` returns `{version, build_time, git_commit}` JSON. `buildInfo` gained a `GitCommit` field; `SetBuildInfo` arity went 2→3 and `main.gitCommit` is injected via `-ldflags` in the Makefile, Dockerfile (`GIT_COMMIT` build-arg, wired through `release.yml`), and `debian/rules`. Missing values surface as the literal `"unknown"` (never empty) so the JSON shape is stable for parse-once monitors. Public — the version is already exposed via the `vote_build_info` metric.
- [x] **D8** `debian/vote.service:12` `Restart=on-failure` weak for unattended classroom hardware (OOM-kill via SIGKILL, clean exits). **Fix:** `Restart=always` + `StartLimitIntervalSec=60` + `StartLimitBurst=5` (in `[Unit]`, the canonical location) + `RestartPreventExitStatus=143`. `always` recovers from OOM-kill (exit 137) and clean-but-unexpected exits; `systemctl stop` is still honoured (systemd's internal "manually stopped" flag suppresses Restart regardless of policy). 143 = 128+SIGTERM(15) — an external SIGTERM-initiated shutdown is treated as "stay down" so a supervisor script's intentional stop is respected. The rate limiter bounds a crash-loop so a wedged server goes to `failed` for inspection instead of burning CPU forever.
- [x] **D9** Single `/health` did both liveness and readiness. **Fix:** split `/livez` (liveness — 200 whenever the HTTP server answers, intentionally cheap with no locks/stats reads, stays 200 during drain so a graceful shutdown isn't killed by a liveness probe) and `/readyz` (readiness — 503 while the hub context is cancelled/draining, 200 otherwise). `/health` is kept as a backward-compatible alias of `/readyz` (same drain semantics, enriched payload) so the CI wait-loop, the e2e `webServer` probe, and any external monitor already wired to it keep working.
- [x] **D10** Runtime image was alpine. **Fix:** switched to `gcr.io/distroless/static-debian12:nonroot` (no shell, no package manager, no libc — the server binary is built `CGO_ENABLED=0` so it's fully static). Image dropped to ~18 MB. The `HEALTHCHECK` can't use `wget`/`curl` (absent), so `vote-server --health` does an in-process HTTP GET to `/livez` on loopback (`127.0.0.1:PORT`, honouring the same `PORT` env var the server listens on, 3s timeout) and exits 0/1. The FHS data dir is pre-created in the builder stage (`mkdir -m 0700 /build/varlib`) and `COPY --chown=nonroot:nonroot`'d into the runtime image because distroless has no shell to `mkdir`; the `VOLUME` directive preserves that ownership into anonymous volumes. Verified end-to-end with podman: binary serves, persistence writes, and `vote-server --health` returns 0 inside the container.
- [x] **D11** `.dockerignore` missing `data/`, `**/playwright-report/`, `**/test-results/`, `.gocache/`, `.gomodcache/`, `*.log`. **Fix:** all added (the `.gocache`/`.gomodcache` dirs are created by `debian/rules`'s `GOCACHE`/`GOMODCACHE` exports and would otherwise bloat the build context; `data/` holds local-dev runtime stats).
- [x] **D12** `debian/Caddyfile.example` missing `Cache-Control immutable` for `/assets/*`, `encode gzip zstd`, HSTS, `X-Content-Type-Options`, precompressed-asset handling. **Fix:** `encode gzip zstd` for on-the-fly compression of proxied responses; `file_server { precompressed gzip br }` serves the build-time `.gz`/`.br` sidecars (emitted by `frontend/scripts/compress-assets.js`) with zero request-time CPU; `Cache-Control: no-cache` default + `public, max-age=31536000, immutable` override for content-hashed `/assets/*` (the HTML entry points, `sw.js`, and manifest stay `no-cache` so new deploys are picked up); HSTS + `X-Content-Type-Options: nosniff` + `Referrer-Policy` + `X-Frame-Options: DENY` via a consolidated `header` block. The proxy matcher gained `/livez /readyz /version` alongside `/health`. `caddy validate` reports `Valid configuration`.
- [x] Tests: `TestLivezIsCheapLiveness`, `TestReadyzReflectsDrain`, `TestHealthAliasStillDrains`, `TestVersionEndpoint` (populated + defaults-to-unknown) in `server_test.go`; `TestRunHealthCheckGreen`/`Non200`/`DialFailure`/`HonoursPortEnv` for the `--health` CLI in `cmd/server/main_test.go`. Updated `writeInfoMetric` and `SetBuildInfo` call sites + `vote_build_info` expectations in `metrics_test.go`/`dashboard_test.go` for the new `git_commit` label. `go test -race ./...` green; `go vet` clean; image built + served + self-probed under podman; `caddy validate` green.

### Session 16 — Low-priority cleanup

- [x] **B12** Promote magic numbers to named consts with docs (`pongWait` vs `PingInterval`, trainer-takeover 50 ms delay, `ClientSendBufferSize`, dashboard caps). **Fix:** `pongWait` (70 s) now documents the 2× PingInterval + slack relationship and why tightening it below PingInterval would evict healthy clients; `maxMessageBytes` (4 KiB read limit) and `MaxGameScore` (100 k clamp) promoted to named consts with docs explaining the allocation/ranking bounds; trainer-takeover `time.AfterFunc(50*ms, ...)` → `trainerTakeoverCloseDelay` const documenting why the displaced trainer needs a scheduling slice to flush the takeover warning before close; dashboard caps (`2016` default history tail, `20000` max, `60` Retry-After) promoted to named consts (`dashboardHistoryDefaultLimit`, `dashboardHistoryMaxLimit`, `dashboardLoginRetryAfter`) with the server↔client `HISTORY_LIMIT` constant in `dashboard.go` cross-referenced so the two stay in sync.
- [x] **B13** Test-gap backfill: `auth.purgeRevoked` lazy eviction, store rotation failed-reopen (B9), `formatLE` non-integer branch. **B9's failed-reopen path** was already covered by Session 10 (`TestRotationReopenFailureLeavesNilHandle`, `TestAppendSampleReopensAfterClose`, `TestAppendSampleRecoversFromExternalLogTruncation`) — no backfill needed. **Added:** `TestPurgeRevokedDropsExpiredEntries` / `TestPurgeRevokedEmptyAndIdempotent` / `TestPurgeRevokedRespectsMaxAgeChange` (3 tests pinning the S4 lazy-eviction contract: backdated entries past `maxAge` are dropped, fresh ones retained, the cutoff tracks `a.maxAge` so a config change propagates, and the no-op paths are safe). `TestFormatLE` (table test, 10 cases) + `TestFormatLEExpositionInContext` cover the previously-untouched non-integer branch of the Prometheus bucket-bound formatter (`0.5` → `"0.5"`, not `"5e-01"` or `"0.50"`), including a full `writeHistogram` integration that asserts the fractional `le="0.5"` line renders cleanly and the `1.0` integer collapses to `le="1"`.
- [x] **B14** CSPRNG-failure fallback in `generateTimestampID`/`GenerateToken` is predictable. If `/dev/urandom` is unavailable, S1/S12 security collapses. **Fix:** `GenerateToken` and `GenerateID` now panic on `rand.Read` failure via a shared `failCSPRNG` helper (distinct greppable message, slog before panic). The time-derived fallbacks are removed entirely — a server that cannot draw secrets from the kernel CSPRNG is in a degenerate state that should not be papered over; `/dev/urandom` failures are effectively unreachable on every supported target. A `randRead` package-level seam (defaulting to `crypto/rand.Read`) makes the panic path testable without a kernel-entropy fault. Tests: `TestGenerateIDPanicsOnCSPRNGFailure`, `TestGenerateTokenPanicsOnCSPRNGFailure` (assert the panic carries the context name), `TestGenerateTokenProducesURLEncodedSecret` (happy-path entropy budget). The obsolete `TestGenerateTimestampID` and `TestGenerateIDFallbackProducesValidID` are removed (the helper they tested no longer exists); `generateTimestampID` is gone.
- [x] **F19** `i18n.js` is misnamed (no locale system). **Fix:** renamed `frontend/shared/i18n.js` → `strings.js` (and the test file). Updated all 14 import sites (8 `@shared/strings.js`, 4 `./strings.js`, the error-boundary `vi.mock` target, the F9 pwa contract test). The CLAUDE.md cross-reference and the F9/F19 docstrings now describe the module as the single French string catalog (no implied locale machinery). A new `pwa-strings.test.js` assertion guards against regressions to the old name.
- [x] **F20** `eslint.config.js` doesn't add Vitest globals for co-located `*.test.js` → false `no-undef` risk. **Fix:** added a `files: ['**/*.test.js', '**/*.spec.js']` config block declaring the Vitest globals (`describe`, `it`, `test`, `expect`, `vi`, `beforeAll/Each`, `afterAll/Each`, `assert`, `suite`, `onTestFinished`) plus `globals.node`. Existing tests still explicitly import what they use, so the block is purely defensive — a future test that drops the import no longer false-positives. No new dependency (an explicit list vs `@vitest/eslint-plugin` keeps the dep footprint flat).
- [x] **F21** No defense-in-depth CSP `<meta>` in `formateur/index.html` / `stagiaire/index.html`. **Fix:** a `cspMeta()` Vite plugin injects the CSP at **build time only** (dev stays unconstrained so HMR/Vite client don't fight a tight policy). Policy: `default-src 'self'`; `script-src 'self'` (verified — built HTML has zero inline `<script>` bodies); `style-src 'self' 'unsafe-inline'` (dynamic `background-color`/`animation-delay`/SVG `stop-color` from data, guarded by F10's `sanitizeColor`); `img-src 'self' data:` (inline-SVG favicon); `connect-src 'self' ws: wss:` (live vote transport); `manifest-src 'self'`; `object-src 'none'`; `base-uri 'self'`; `form-action 'self'`. `frame-ancestors` omitted (not enforced by `<meta>`; the Caddyfile's `X-Frame-Options: DENY` covers anti-clickjacking). Tests: 7 new in `scripts/vite-config.test.js` pin the policy shape (lock `script-src 'self'`, guard against `script-src` widening, assert `object-src 'none'`/`form-action 'self'`/`base-uri 'self'`) plus an end-to-end check on the built `dist/**/index.html` when present.
- [x] **F22** `requestFullscreen()` promise ignored — `connection-aid.js:191`. **Fix:** `toggleFullscreen` now handles the returned promise's rejection (`p.catch`) and the synchronous throw path. Rejections (user-gesture rule, iframe `allowfullscreen` absent, permissions policy) flip the fullscreen button into an `.error` state for 1.5 s (mirrors the copy button's error affordance) so the operator sees the browser blocked the toggle rather than a silent no-op. A matching `.aid-fullscreen-btn.error` CSS rule was added. The console warns the underlying rejection so the global error boundary (F11) doesn't surface a misleading "unexpected error" toast.
- [x] **D13** Add `make fmt-check` / `vuln` / `check` targets. **Fix:** `make fmt-check` runs `gofmt -l` and fails if any file needs formatting (no modification); `make vuln` runs `govulncheck ./...` (backend, precise — only called-code vulns) + `npm audit --omit=dev --audit-level=high` (frontend prod deps), mirroring the CI security job (D6); `make check` is a one-shot pre-push gate combining `fmt-check` + `vet` + backend `go test -race` + frontend `npm run lint` + `npm test`. Fixed a pre-existing gofmt drift in `stats_bench_test.go` along the way.
- [x] **D14** Single-arch deb; no arm64. **Fix:** `build-deb` is now a matrix over `[amd64, arm64]`. New `Dockerfile.deb` (golang:1.24-bookworm + NodeSource node 22 + debhelper/fakeroot/brotli) is the build environment; buildx builds it per-platform with `--load`, then `dpkg-buildpackage` runs inside it under QEMU for arm64. Reusing `dpkg-buildpackage` unchanged means `debian/rules` is the single source of truth for both arches and the resulting `.deb` is correctly tagged `Architecture: arm64` by `dpkg --print-architecture` under emulation. Artifacts are uploaded per-arch (`debian-package-amd64`, `debian-package-arm64`); the release job downloads both via `pattern: debian-package-*` + `merge-multiple: true` and verifies each arch is present. Verified end-to-end locally: amd64 deb builds and `dpkg-deb -I` reports the right arch; arm64 deb builds under QEMU and `file usr/bin/vote-server` reports `ELF 64-bit LSB executable, ARM aarch64, statically linked`. buildx GHA cache is keyed per-arch so the two legs don't evict each other's layers.
- [x] **D15** `.env.example` missing `VOTE_MAX_SESSIONS_PER_HOUR`. **Fix:** added a documented entry under the resource-caps section (20/h default, matching `config.go` and the S7 anti-flood rationale; ≤ 0 falls back to the hardcoded default).
- [x] **D16** Base images tag-floated, not digest-pinned. **Fix:** all three `FROM` lines in `Dockerfile` pinned to immutable manifest-list digests (`golang:1.24-alpine@sha256:…`, `node:22-alpine@sha256:…`, `gcr.io/distroless/static-debian12:nonroot@sha256:…`). The digests are manifest-list (multi-arch) so buildx multi-platform builds still resolve to the right variant per platform. Dependabot's `docker` ecosystem bumps the tag+digest pair together weekly. Verified: `podman build --platform linux/amd64` and `--platform linux/arm64` both resolve the digests, and the built image serves + self-probes via `vote-server --health`.
- [x] **D17** `getFreePort` TOCTOU — `backend/integration/integration_test.go:126-141`. **Fix:** removed `getFreePort` (which bound `:0`, read the port, closed the listener, returned the port string — leaving a window where another process could grab the port before `ListenAndServe` rebound it). `NewTestServer` and the two other callers now bind the listener once via `newBoundListener(t)` and pass it to a new `Server.Serve(l net.Listener)`. `Server.Run()` was refactored to bind its own listener and delegate to `Serve`, so the race is closed at every entry point: the listener that owns the port is the same one the server accepts on. Tests: `TestServeAcceptsPreBoundListener` pins the contract (a pre-bound listener answers real HTTP on its advertised address); the integration helpers and two standalone integration tests were migrated.
- [x] **D18** Release uses broad `contents: write`; no OIDC/cosign signing, no SLSA provenance. **OIDC + SLSA provenance were already added in Session 14 (D1)** (cosign keyless signing via `id-token: write`, buildx `provenance: true` + `sbom: true`, SPDX SBOM). The remaining gap was the workflow-level `permissions: contents: write` that every job inherited. **Fix:** the workflow default is now `contents: read` (least privilege); only the `release` job (which creates the GitHub Release) is granted `contents: write` explicitly. `build-deb` and the reusable `ci` call now run with read-only repo access, so a compromised or buggy job in either can't push tags, rewrite releases, or tamper with repo contents — the blast radius is bounded to its own artifact. `actionlint` clean (exit 0; only pre-existing info-level shellcheck notes in the untouched e2e job).

**Verification:** `go test -race ./...` green; `gofmt -l` clean; `go vet ./...` clean; `npm test` 525 → 536 tests, all green; `npm run lint` clean; `npm run build` clean and both `dist/**/index.html` carry the CSP meta; `make fmt-check` clean; `actionlint` exit 0 on both workflows; amd64 + arm64 debs built and verified via the new `Dockerfile.deb` pipeline under podman; distroless runtime image built + self-probes `/livez` with digest-pinned bases.

---

## Session 17 — Reclaim retry + trainer-scoreboard state correctness

Three correctness bugs in the hub/vote state machine. The first is the headline
of audit round 3 (a stuck-user loop); the other two are quick wins uncovered
while verifying it. All three live in the join/reveal/reconnect paths.

- [x] **R1** [Critical] Reclaim-retry loops forever — the stagiaire is silently
  trapped. `Client.ID` is minted server-side then overwritten when a known
  `stagiaireId` is presented; on reclaim failure the frontend retries with an
  empty `stagiaireId`, but the server's `c.ID` is never reset (the
  `msg.StagiaireID != ""` guard is skipped), so `JoinStagiaire` re-takes the
  reclaim path with the stale ID + empty token and fails again. The frontend
  retry handler has no retry-count guard, so it re-sends on every matching
  error → a tight message loop (throttled only by the 10/s per-client rate cap)
  with the toast suppressed. Triggered by any partial sessionStorage failure
  (private browsing, quota exhaustion) that leaves a known `stagiaireId` paired
  with a missing token. **Fix:** (a) added `Client.OriginalID` (the immutable
  server-generated identity minted at the WS handshake) and reset
  `c.ID = c.OriginalID` at the top of `handleStagiaireJoin` so each attempt
  starts clean (`server.go` sets `OriginalID = clientID` alongside `client.ID`;
  the reset is guarded on `OriginalID != ""` so test fixtures that bypass the
  handshake keep working); (b) added a one-shot `reclaimRetried` guard in
  `stagiaire/websocket.js` that retries at most once per WS connection — a
  second rejection surfaces the error to the user instead of looping silently.
  The guard resets on `connectToSession()` so a real reconnect can retry once.
  A `_test` DEV-gated helper exposes the guard for unit tests.
- [x] **R4** [Medium] `buildScoreboard` assigned ordinal ranks, inconsistent
  with the competition ranks `RevealAnswers` assigns. The ordinal loop
  (`entries[i].Rank = i + 1`) made a tied class flip between "1er, 1er, 3e" at
  reveal and "1er, 2e, 3e" on trainer reconnect (buildScoreboard feeds trainer
  reconnect). **Fix:** exported `vote.AssignCompetitionRanks` (was
  package-private `assignCompetitionRanks`) and replaced the ordinal loop with a
  call to it. The slice is already sorted by `TotalScore DESC, Name ASC`, which
  is the precondition the helper expects.
- [x] **R5** [Medium] `UpdateTrainer` failure left `conns.Trainer` set with no
  Manager session. `registerClient` set `conns.Trainer = client` unconditionally;
  the `CreateSession` failure path rolled it back, but the `UpdateTrainer`
  failure path did not. If the session was reaped between `GetSession` and
  `UpdateTrainer`, the trainer was connected but every later op returned
  `ErrSessionNotFound`, and `cleanupLoop` couldn't reap the entry because
  `conns.Trainer != nil`. **Fix:** on `UpdateTrainer` failure, fall back to
  `CreateSession` (matching the `!ok` branch) so the trainer keeps a working
  session; if `CreateSession` also fails, reset `conns.Trainer = nil` so
  `cleanupLoop` can reap. Added two package-level test seams (`updateTrainerFn`,
  `createSessionFn`, mirroring the existing `randRead` seam pattern) so the race
  window can be exercised deterministically under `-race` without a real
  concurrency hazard.
- [x] Tests: `TestJoinStagiaireRetryResetsClientID` (R1 — bootstraps a real
  prior identity, asserts the first reclaim attempt is rejected, then the retry
  with an empty `stagiaireId` joins as `OriginalID`; also asserts `c.ID ==
  OriginalID` post-retry), 3 new frontend tests in
  `stagiaire/websocket.test.js` (R1 — retry at most once per connection, guard
  resets on `connectToSession`, used-up guard surfaces the error to the user),
  `TestBuildScoreboardUsesCompetitionRanks` (R4 — tied totals share a rank,
  cross-checks against `vote.AssignCompetitionRanks`),
  `TestRegisterClientUpdateTrainerFailureRollsBack` (R5 — `conns.Trainer ==
  reconnect` + Manager session exists after the forced-UpdateTrainer-failure
  fallback), `TestRegisterClientUpdateTrainerAndCreateBothFailRollsBack` (R5 —
  defensive `conns.Trainer = nil` rollback when both stubs fail, so cleanupLoop
  can reap). `go test -race ./...` green; `npm test` 536 → 539, all green;
  `npm run lint` clean; `npm run build` clean; `make fmt-check` clean; `go vet`
  clean.

---

## Session 18 — Graceful shutdown under load

One architectural fix plus the test coverage that would have caught it. The
shutdown sequence in `main.go` was ordered backwards relative to Go's
documented graceful-shutdown pattern: the hub drained (cancelling its context
and calling `wg.Wait`) *before* the HTTP listener closed, so new WebSocket
dials were accepted throughout the drain. Two consequences: (1) a reconnect
upgrading during drain called `client.Start()` → `wg.Add(2)` concurrent with
the Hub's `wg.Wait`, which Go forbids when the counter is zero (`sync:
WaitGroup is reused before previous Wait has returned`); (2) hijacked WS conns
aren't tracked by `http.Server.Shutdown`, so such a reconnect's `writePump`
exited on `ctx.Done` but its `readPump` blocked in `ReadMessage` until
`pongWait` (70 s) — a "connected" socket that delivered no state, freezing a
30-student class on every deploy. Both were latent only because no test loaded
`main.go`'s shutdown path (the existing `shutdown_test.go` exercised
`Hub.Shutdown()` in isolation).

- [x] **R2** [High] **Fix (a):** reordered `main.go`'s shutdown so
  `srv.Shutdown` (closes the HTTP listener, drains HTTP) runs **before**
  `h.Shutdown` (cancels the hub context, closes every live WebSocket, waits
  for all Hub-owned goroutines). Once `srv.Shutdown` returns, no new TCP dial
  can reach `handleWebSocket`, so no new `wg.Add(2)` can race with the Hub's
  `wg.Wait`. The shutdown sequence was extracted into a `gracefulShutdown`
  helper so the ordering contract is testable. `http.Server.Shutdown` does not
  track hijacked WebSocket connections, so the guard in (b) is defense in depth
  for any request that slipped past the listener before close. **Fix (b):**
  added a drain guard at the top of `handleWebSocket` — `if
  s.hub.Context().Err() != nil { 503 }` *before* `AcquireIPSlot` / `Upgrade` /
  `client.Start()`. With the new ordering the listener is already closed by
  the time the context cancels, but the guard catches any in-flight request
  and makes the drain observable as a clean 503 rather than a stuck socket.
  **Fix (c):** mirrored the corrected order in every test helper that shuts
  down a server (`TestServer.Close`, `cleanup_test.go`, `scenario_test.go`) so
  the test infrastructure can't drift from production.
- [x] Tests: `TestShutdownRejectsNewUpgradesDuringDrain`
  (`internal/server/server_test.go` — cancels the hub context via `h.Shutdown`
  with no registered clients, which returns immediately leaving the HTTP
  listener open; asserts a fresh `GET /ws` gets 503, not a 400 from the
  upgrader or an upgrade; verified to fail with `[400]` when the guard is
  removed). `TestShutdownClosesListenerFirst` (`cmd/server/main_test.go` —
  wraps the listener in a `trackingListener` whose `Close()` records an event,
  spawns a goroutine recording when the hub context cancels, runs
  `gracefulShutdown`, asserts the event order is `[listener, hub]`; verified
  to fail with `[hub listener]` when the order is reverted). `TestShutdown-
  JoinsAllGoroutinesUnderLoad` (`integration/shutdown_test.go` — loads the
  real `server.Server` + `handleWebSocket` path with 12 registered trainers
  plus a 4-goroutine reconnect storm continuously re-dialing throughout
  shutdown, triggers drain via `TestServer.Close` which mirrors `main.go`'s
  order, asserts under `-race`: no WaitGroup panic, every client read pump
  observes the close, goroutine count returns to baseline; stable across 5
  consecutive runs). `go test -race ./...` green; `make fmt-check` clean;
  `go vet ./...` clean.

---

## Session 19 — Frontend reconnect & lifecycle edge cases

A cluster of state-desync bugs on reconnect / leave / page-reload. They share a
root cause: client-side lifecycle assumes the *first* event of a kind is the
*only* event, so second-order paths (leave→rejoin, reload-reconnect, async
rejection) reset or fail to re-bind state.

- [x] **R3** [High] A stagiaire permanently loses offline/online reconnect
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
- [x] **R6** [Medium] The Escape-to-leave shortcut is never attached when a
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
- [x] **R7** [Medium] A trainee's game high score and streak are silently wiped
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
- [x] **R8** [Medium] A server-rejected rename has no UI rollback.
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
- [x] Tests: `websocket-client.test.js` (+rebind-on-reconnect after close, R3),
  `formateur/websocket.test.js` / a `main.js` test (+Escape attached on
  `session_created`, R6), `stagiaire/websocket.test.js` (+high score preserved
  on reload-reconnect when `vote_stagiaire_id` present, R7), `stagiaire/
  handlers.test.js` (+rename keeps modal open / leaves `prenom` untouched on
  server error, R8). `npm test` green; `npm run build` clean.

---

## Session 20 — Backend robustness & security cluster

Four small, independent, hardening fixes. Each is isolated and verifiable on its
own; each was missed by a prior session that touched neighbouring code.

- [x] **R9** [Medium] `Server.Shutdown` nil-pointer panics when startup failed
  at `net.Listen`. If listen fails, `s.srv` stays nil, `Run` returns to `errCh`,
  and `main`'s shutdown block calls `s.srv.Shutdown(ctx)`
  (`server.go:291-293`) → panic, which masks the real listen error in the logs
  and turns a clean "exit 1 with a clear message" into a stack trace. **Fix:**
  guard `Server.Shutdown` — `if s.srv == nil { return nil }` (or return the
  stored startup error).
- [x] **R10** [Medium] The dashboard cookie mints an all-zero nonce on CSPRNG
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
- [x] **R11** [Low] The backoff cap is applied *before* the +25% jitter, so the
  real maximum is `MaxBackoffMs × 1.25 = 375 s` (6.25 min), not the documented 5
  min. `security.go:137-153` caps then adds `jitterRange` (up to 75 s) on top.
  **Fix:** apply the cap after jitter (and keep the existing 100 ms floor).
- [x] **R12** [Low] `runtime.ReadMemStats` forces a stop-the-world on every
  `/metrics` scrape and every 30 s dashboard poll (`metrics.go:93`). The
  observability endpoint periodically stalls the very real-time path it
  monitors, and the stall is invisible to `/metrics` itself. **Fix:** switch the
  `go_mem_*` gauges to the non-STW `runtime/metrics` API
  (`/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`,
  `/sched/goroutines:goroutines`), or drop `runtime.ReadMemStats` entirely.
- [x] Tests: `TestServerShutdownNoPanicOnFailedListen` (R9), `TestSignCookieFailsOnCSPRNGFailure`
  / extend the existing `randRead` seam to auth (R10), `TestBackoffRespectsMaxAfterJitter`
  (R11 — table test at the cap), `TestMetricsGaugesDoNotSTW` or a benchmark
  comparing `ReadMemStats` vs `runtime/metrics` (R12). `go test -race ./...` green.

---

## Session 21 — Frontend polish

Two small, low-risk cleanups that close a drift vector and unlock a
backend-supported feature the UI currently blocks.

- [x] **R13** [Low] The live `connected_count` update duplicates the pluralization
  logic F16 (archived) extracted into `formatConnectedCount` precisely so the
  two surfaces couldn't drift. `formateur/websocket.js:176-177` still hardcodes
  `stagiaire${s} connecté${s}` inline; the initial render in `renderConfigHTML`
  (`formateur/renderers.js:476`) uses the helper. They agree today, but the whole
  point of F16 was to make drift impossible. **Fix:** replace the inline string
  with `formatConnectedCount(state.connectedCount)`.
- [x] **R14** [Low/Medium] Re-reveal (correcting the answer key) is unreachable
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
- [x] Tests: `formateur/renderers-snapshot.test.js` (+reveal section/button
  persist after `state.revealed`, R14), `formateur/websocket.test.js`
  (+`connected_count` update uses `formatConnectedCount`, R13). `npm test` green;
  `npm run build` clean.

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

## Session 24 — Identity & registration invariants  ✓

Two state-invariant violations in the join/register path, both missed by prior
sessions that touched neighbouring code. They share the join→`registerClient`
area, so fixing them together keeps the invariant reasoning local. Both are
correctness bugs that degrade ranking/leaderboard integrity or leak resource-cap
slots.

- [x] **S15** [Medium] The reclaim-rename path skips the authoritative
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
- [x] **R16** [Medium] Stale `conns.Stagiaires` entry when a connection re-joins
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
- [x] Tests: `TestJoinStagiaireReclaimRenameRejectsCollision` (S15 — two clients
  racing a reclaim-rename to the same normalised name; the second is rejected
  under the lock), `TestRegisterClientRemovesPriorIDSlot` (R16 — same `*Client`
  re-registered under a new ID; assert the old slot is gone and
  `connected_count` doesn't double-count; assert the cap isn't exhausted by
  cycling). `go test -race ./...` green.

---

## Session 25 — Persistence & metrics robustness  ✓

Three low-risk integrity fixes in `stats.go` / `store.go`, all about
histogram/counter correctness across clock jumps, corrupted state files, and
crash durability. Coherent cluster, isolated from the rest of the codebase.

- [x] **R19** [Medium] Negative durations poison the session-duration histogram
  persistently. `observeEndedSession` computes lifetime as
  `time.Since(time.Unix(createdAt, 0))` (`stats.go:239-244`); `time.Unix(...)`
  returns a `Time` with **no monotonic component**, so `time.Since` falls back to
  wall-clock arithmetic. A backward NTP step / VM suspend-resume clock snap /
  manual `date -s` yields a negative value; `Observe` then adds it to every
  finite bucket and to `Sum`, `validHistogram` still passes, the poisoned state
  flushes to `counters.json`, restores on reboot, and never self-heals short of
  deleting the file. **Fix:** bound-check `d >= 0` before observing in
  `observeEndedSession` (so a future-negative lifetime doesn't even bump the
  count); also reject `v < 0` (and NaN/Inf) inside `Observe` to defend the
  invariant for any caller and to keep the count/sum consistent if a future
  code path bypasses the `observeEndedSession` short-circuit.
- [x] **R20** [Low] `validHistogram` does not validate `Sum`; NaN/Inf propagates
  to `/metrics`. `validHistogram` (`store.go:429-441`) enforces count/bucket
  invariants but never inspects `Sum`. A `counters.json` whose `sum` field is
  `NaN`/`+Inf`/`-Inf` (disk corruption, manual editing, or accumulated R19
  damage) restores via `addLocked` (`stats.go:124-130` does `h.sum += snap.Sum`
  → `x + NaN = NaN`) and surfaces in `/metrics` as `..._sum NaN`, which breaks
  the in-tree dashboard's SVG sparkline renderer (`dashboard.go:174` assumes
  finite values) and most alerting rules. (Go's stdlib `encoding/json` rejects
  NaN/Inf at both marshal and unmarshal time, so the JSON path can't deliver
  them today — but `validHistogram` is also the pre-write gate in `SaveCounters`
  and the documented last-line guard should the decoder ever change.) **Fix:**
  `if math.IsNaN(h.Sum) || math.IsInf(h.Sum, 0) { return false }` in
  `validHistogram` — `LoadCounters` and `SaveCounters` both already react to
  validation failure correctly (start fresh / refuse to write).
- [x] **R21** [Low] `SaveCounters` / `AppendSample` perform no `fsync`; the
  durability contract is overstated. `os.WriteFile` + `os.Rename` (counters) and
  `O_APPEND` Write (stats log) never `Sync()` (`store.go:134-150, 200-261`).
  Atomicity is at the **directory-entry** level only: a successful `Rename`
  doesn't mean the data blocks are durable, so a power loss in the window between
  `Rename` returning and the kernel flushing can leave `counters.json` empty or
  referring to unwritten extents. CLAUDE.md and `store.go:14-17` claim "atomic,
  no half-writes" — true at the rename level but overstated as durability.
  **Fix (b), full portable durability recipe:** `SaveCounters` now does
  temp-write → `f.Sync()` (commit the bytes) → rename → `syncDir` (commit the
  rename metadata). Cost is two extra fsyncs per flush; counters.json is
  rewritten once per `VOTE_STATS_INTERVAL` (default 5m) and on graceful
  shutdown, so steady-state cost is ~12 fsyncs/hour — negligible. `stats.jsonl`
  stays un-fsynced (append-only lossy history; "worst-case crash loses at most
  one interval" is the documented contract). Package doc and `SaveCounters` doc
  corrected to distinguish "atomic visibility" from "crash-durability".
- [x] Tests: `TestObserveRejectsNegative` / `TestObserveRejectsNonFinite`
  (R19 — `Observe` guard), `TestObserveEndedSessionIgnoresNegativeDuration`
  (R19 — caller short-circuit, asserts the histogram stays untouched while
  sibling histograms still record) + `TestObserveEndedSessionRecordsPositiveDuration`
  (R19 happy-path guard against over-suppression),
  `TestValidHistogramRejectsNaNSum` / `TestValidHistogramRejectsInfSum`
  (R20 — direct predicate unit tests, ±Inf subtests) +
  `TestValidHistogramAcceptsFiniteSum` (R20 — no over-suppression) +
  `TestSaveCountersRejectsNaNSumBeforeWrite` (R20 — pre-write gate refuses to
  write and leaves no file), `TestSaveCountersRemovesTempFile` +
  `TestSyncDirAndSyncPathHelpersRun` + `TestSaveCountersDocAssertsDurabilityRecipe`
  (R21 — temp file cleaned, helpers wired, doc contract guards the recipe).
  `go test -race ./...` green; `npm test` green (572); `npm run lint` clean.

---

## Session 26 — Lower-priority cleanup  ✓

The remaining round-4 findings bundled into one tidy-up session. Each is small,
isolated, and independently verifiable.

- [x] **R15** [Low/Medium] `handleWebSocket` post-upgrade ctx-race during
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
- [x] **R17** [Low] `UpdateGameScore` skips the `GameEnabled` recheck.
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
- [x] **S16** [Medium/Low] All per-IP protections collapse to a single shared
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
- [x] **F25** [Low] `isPermanentlyClosed` is never surfaced to the app. After
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
- [x] Tests: `TestShutdownRejectsUpgradeBetweenGuardAndStart` (R15 — under
  `-race`, force the context to cancel after the guard but before `Start`),
  `TestUpdateGameScoreRejectsWhenGameDisabled` +
  `TestUpdateGameScoreRejectsConcurrentResetVote` (R17), config/docs change for
  S16 + `TestLoopbackMonitorWarnsOnLoopbackWithoutTrustedProxies` /
  `TestLoopbackMonitorDoesNotWarnWithTrustedProxies` /
  `TestLoopbackMonitorRespectsMinObservations` /
  `TestLoopbackMonitorIgnoresNonLoopbackTraffic` /
  `TestLoopbackMonitorRearmsAfterConditionClears` /
  `TestLoopbackMonitorIgnoresUnparsableIP` (S16), `websocket-client.test.js`
  (+`onPermanentClose` fires once at the ceiling and on a 4xxx permanent
  close, F25), `formateur/websocket.test.js` (+`onPermanentClose` flips
  `permanentlyClosed` + banner re-render + clears on reconnect, F25),
  `formateur/renderers-snapshot.test.js` (+banner swaps to the `lost` state
  + clears the class when hidden again, F25). `go test -race ./...` green;
  `npm test` green (578); `npm run lint` clean; `npm run build` clean.

---

## Sessions 27–30 — Audit round 5

Round 5 was a fresh end-to-end pass over the now-hardened codebase. It
surfaced a small number of residual issues, clustering into four
conversation-sized sessions. Item codes keep their category prefix (S =
security, F = frontend, R = backend reliability/correctness, A =
accessibility, X = XSS, P = performance, D = deploy/infra, CI = continuous
integration, M = docs) and continue each series (rounds 1–4 ended at S16,
F25, R21, D18), so there is no code collision with anything already done.
All items below are `[x]` done.

---

## Session 27 — Trainer competitive display & preset completeness

The headline of round 5. Two coupled frontend gaps that together make the
trainer's own view of competitive mode *less* informative than the classroom
projector — and break the round-trip of the answer key through presets. Both
touch the formateur handlers/renderers/presets cluster, so fixing them together
keeps the competitive-display reasoning in one head.

- [x] **F26** [High] Trainer scoreboard shows only per-round `voteScore`, hides
  `totalScore` / `rank` / `gameScore`. `renderScoreboardHTML`
  (`frontend/src/formateur/renderers.js:802-834`) sorts by `b.voteScore -
  a.voteScore` and renders only `<span class="scoreboard-votescore">…</span>`.
  The backend's `ScoreEntry` (`backend/internal/vote/manager.go:421-428`) ships
  `voteScore`, `totalScore`, `gameScore`, `rank` per row; the renderer ignores
  three of the four fields. Dead CSS (`.scoreboard-row.rank-1`,
  `.scoreboard-rank` at `frontend/src/formateur/style.css:1544-1555`) confirms
  the rank column was intended but never wired. Competitive mode is cumulative
  across rounds (CLAUDE.md), yet the trainer's screen shows a per-round number
  that resets every round, while the classroom aid view
  (`frontend/src/formateur/connection-aid.js:156-166`, fed by the publisher at
  `frontend/src/formateur/websocket.js:50`) correctly sums `(score || 0) +
  (gameScore || 0)`. R4 (DONE) fixed the backend's rank assignment; the
  frontend display was never updated to consume it. **Fix:** sort by
  `entry.totalScore + (entry.gameScore || 0)` (or just `entry.totalScore` — it
  already includes `gameScore`), emit `<li class="scoreboard-row
  rank-${entry.rank}">` + `<span class="scoreboard-rank">${entry.rank}</span>`,
  and surface `totalScore` alongside (or instead of) `voteScore`.
- [x] **F27** [Medium] `correctColors` never persisted in presets / last-config
  (v3 schema is half-wired). CLAUDE.md documents preset schema `_v: 3` as
  "added competitive, allowBlank, **correctColors**". `shared/presets.js:66-68`
  `sanitizeConfig` reads and validates `correctColors`. But every save/restore
  site omits it: `confirmSavePreset` (`frontend/src/formateur/handlers.js:53-60`)
  passes only `selectedColors / colorLabels / multipleChoice / gameEnabled /
  competitive / allowBlank`; `setLastConfig` (the `startVote` handler at
  `:194-202`) is the same; `applyPreset` (`:72-85`) and
  `applyLastConfigIfAvailable` (`:172-186`) don't restore it into
  `state.correctColors`. The v3 migration is incomplete — schema supports a field
  the UI never writes or reads. A trainer who saves a preset expects the answer
  key to round-trip; instead they must re-select the correct colors every time
  they apply the preset. R14 (DONE) made re-reveal work *within* a session; this
  finding extends it *across* sessions. **Fix:** plumb
  `correctColors: Array.from(state.correctColors)` through `savePreset` /
  `setLastConfig`, and `state.correctColors = new Set(preset.config.correctColors
  || [])` through both restore sites.
- [ ] Tests: `formateur/renderers.test.js` (+scoreboard sorts by totalScore,
  +renders `rank-N` row class, +renders `totalScore` column, F26);
  `formateur/handlers.test.js` + `shared/presets.test.js` (+savePreset +
  setLastConfig carry `correctColors`, +applyPreset +
  applyLastConfigIfAvailable restore `state.correctColors`, +round-trip via
  sanitizeConfig, F27). `npm test` green; `npm run lint` clean; `npm run build`
  clean.

---

## Session 28 — Frontend send-result guards & accessibility cluster

Two reinforcing themes: a handful of `client.send(...)` call sites still discard
the return value (F24's pattern, missed in the stagiaire rename path), and the
vote UI carries ARIA contracts it doesn't fully honour. All findings are
isolated frontend edits with no backend coupling, so they bundle cleanly into
one polish session.

- [x] **F28** [Medium] Stagiaire `handleEditName` ignores `client.send` return
  value (F24 leftover). `frontend/src/stagiaire/handlers.js:549-553` sets
  `state.pendingRename = newPrenom` BEFORE `client.send({ type: 'update_name',
  ... })` and discards the return value. If send returns false (WS dropped in
  the click-to-send window), `pendingRename` stays set, no `name_updated` will
  ever arrive to clear it, and the modal gives no signal that the rename failed
  — the user sits on a frozen form. F24 (DONE) fixed exactly this pattern in the
  formateur action handlers by capturing `const ok = client.send(...)` and
  calling `showError` on false. The stagiaire rename path was missed. R8 (DONE)
  added the `pendingRename` routing but didn't add the send-result guard.
  **Fix:** capture the return; on false, clear `state.pendingRename`, surface a
  connection message in the inline error slot (`#edit-name-error`), keep the
  modal open with the user's input preserved (mirror the R8 rejection path).
- [x] **A1** [Medium] `showConfirmDialog` declares `aria-modal="true"` but never
  inerts/hides the background. `frontend/shared/ui.js:90-100`; CSS at
  `frontend/shared/styles/base.css:551-616`. The overlay covers the viewport and
  `body.confirm-lock` scroll-locks the body, but background content is not
  marked `inert` or `aria-hidden`. The Tab handler (`ui.js:135-147`) only wraps
  between the dialog's own buttons. `aria-modal="true"` is supposed to make AT
  treat other content as non-existent, but NVDA/JAWS/VoiceOver in browse mode
  widely ignore it without explicit sibling `aria-hidden` or `inert`. SR users
  can virtual-cursor into the background app and trigger confusing
  announcements mid-dialog. Session 8 (DONE) tested the focus-trap Tab wrap but
  not background semantic isolation. **Fix:** on open, set `inert` (or
  `aria-hidden="true"`) on `<main id="app-content">` and the header; remove on
  `resolveConfirm`. `inert` is supported in all current evergreen browsers
  (Chrome 102+, Firefox 112+, Safari 15.5+) — well within the F13 baseline.
- [x] **A2** [Medium] Multi-choice vote: duplicate tab stops (native checkbox +
  tabbable label sibling). `frontend/src/stagiaire/renderers.js:386-411` +
  `:556-572`. Each color renders a native `<input type="checkbox">` (focusable
  by default) AND a sibling `<label for="color-X" tabindex="0">` with its own
  keydown handler. Both are independently tabbable, so keyboard users press Tab
  twice per color (16 stops for an 8-color palette). The label's manual keydown
  handler duplicates the native Space/Enter behavior already provided by the
  checkbox itself. F7 (DONE) wrapped the group in `<fieldset>`/`<legend>` but
  didn't address the double tab stop. **Fix:** drop `tabindex="0"` from the
  label (the native checkbox is the tab target), one line.
- [x] **A3** [Medium] Single-choice vote grid uses `role="radio"` but provides
  no arrow-key navigation. `frontend/src/stagiaire/renderers.js:347-373`. Wrapper
  has `role="radiogroup"`, each button has `role="radio"` + `aria-checked`.
  WAI-ARIA Authoring Practices prescribe `Arrow Up/Down/Left/Right` to move
  selection between radios within a radiogroup (Tab leaves the group). Current
  implementation only supports Tab between buttons. The `role="radio"` semantic
  carries an implicit contract that keyboard users will recognize from native
  radios. F7 (DONE) switched from `aria-pressed` to `aria-checked` for the
  correct semantics but left the keyboard interaction at odds with the role.
  **Fix:** add an arrow-key handler on the radiogroup that moves
  `state.selectedColors` to the next/previous color and re-renders (or focus the
  new button). Alternatively, drop `role="radio"` and use a plain button group
  with `aria-pressed` if Tab-only is the intended UX.
- [x] **X1** [Low] Unescaped `colorId` fallback in three `title="..."`
  interpolations. `frontend/src/formateur/renderers.js:812`
  (`renderScoreboardHTML`), `:908` (`renderCombinationsHTML`), `:972`
  (`renderStagiairesVotesHTML`). Pattern `title="${color?.name || colorId}"`
  interpolates `colorId` raw when `COLORS.find(...)` returns undefined. Safe
  today — `colorId` comes from server-validated votes against `ValidColors`. But
  F10 (DONE) hardened the parallel `style="background-color: ..."` path with
  `sanitizeColor`; the `title="..."` path was missed. A future custom-palette
  feature (or a tampered BroadcastChannel message in the connection-aid path)
  could let an unsanitized ID reach the attribute and break out via
  `"><script>`. **Fix:** `title="${escapeHtml(color?.name || colorId)}"` at all
  three sites.
- [x] **P1** [Low] `WebSocket.send()` not wrapped in try/catch.
  `frontend/shared/websocket-client.js:242-249`. `send()` checks
  `readyState === OPEN` then calls `this.ws.send(JSON.stringify(data))`
  unguarded. `JSON.stringify` can throw on circular references; `ws.send` can
  throw `InvalidStateError` if the socket transitioned states between the check
  and the call. All four stagiaire callers (handleEditName, handleSubmit,
  report_game_score, reclaim retry) and four formateur callers assume `send`
  returns true/false, not throws. A thrown error escapes to the click handler
  and surfaces via the global error boundary as a misleading "unexpected error"
  toast. **Fix:** wrap in `try { ...; return true } catch (e) {
  console.warn(e); return false }`.
- [x] Tests: `stagiaire/handlers.test.js` (+handleEditName: when `client.send`
  returns false, `pendingRename` is cleared, inline error populated, modal stays
  open, F28), `shared/ui.test.js` (+showConfirmDialog sets `inert` on
  `<main>`/header while open, removes on close, A1), `stagiaire/renderers.test.js`
  (+multi-choice: single Tab stop per color after dropping `tabindex="0"`, A2; +
  single-choice: arrow keys move selection within radiogroup, A3),
  `formateur/renderers.test.js` (+`title=` attributes are escaped, X1),
  `websocket-client.test.js` (+send returns false and does not throw on
  circular-ref payload or post-check state transition, P1). `npm test` green;
  `npm run lint` clean; `npm run build` clean.

---

## Session 29 — Backend handler & invariant hardening residuals

Four small backend fixes clustering around the hub/vote mutation handlers. All
are defense-in-depth or invariant-polish; none is an exploitable bug today, but
each closes a single-point inconsistency that a future refactor could turn into
one. Same package cluster (`internal/hub/`, `internal/vote/`,
`internal/security/`), so the lock-ordering and identity-invariant reasoning
stays in one head.

- [x] **B1** [Medium] `handleUpdateName` is the only mutation handler without a
  role gate. `backend/internal/hub/client.go:725`. All six peer handlers gate
  (`handleStartVote:497`, `handleVote:559`, `handleCloseVote:592`,
  `handleResetVote:605`, `handleRevealAnswers:648`, `handleReportGameScore:705`);
  this one doesn't. Today the fallthrough is harmless
  (`UpdateStagiaireName` returns `ErrStagiaireNotFound` for a trainer ID), but
  it's a single-point inconsistency: a future refactor adding a code path that
  doesn't fail on missing stagiaire would let a trainer (or pre-join anonymous
  client) mutate stagiaire state. No test exercises the trainer-sends-
  `update_name` case. **Fix:** add `if c.Type != "stagiaire" {
  c.SendError(vote.ErrNotAuthorized.Error()); return }` as the first line.
  One-line change, mirrors every other mutation handler.
- [x] **C1** [Low/Medium] `computeRank` returns `rank > totalStagiaires` for
  post-reveal joiners. `backend/internal/hub/hub.go:1037-1053` (called from
  `:732-740`). A stagiaire joining a Competitive session during `Closed+Revealed`
  is not in `session.Scores` (populated only inside `RevealAnswers`'s Stagiaires
  iteration). `computeRank` iterates `voteScores` (the Scores snapshot), so
  `total = len(voteScores)` = N (pre-reveal class size, excludes joiner),
  `rank = 1 + (scorers with score > 0)` = M+1. When all existing stagiaires
  scored positive (common), `rank = N+1`, `total = N` → UI shows impossible
  "rank N+1 of N". The joiner also receives `totalScore = scores[client.ID] = 0`
  (zero-value for missing key). UX inconsistency visible in the classroom; a
  late arrival sees a nonsensical rank until the next reveal normalizes it. No
  test covers post-reveal join. **Fix:** derive `total` from
  `len(session.Stagiaires)` (the live identity set, not the at-reveal snapshot).
  For rank, either include all current Stagiaires in the iteration (read
  `session.GetStagiaires()` once, look up each score), or simpler: if `id` not
  in `voteScores`, return `(len(stagiaires), len(stagiaires))` — they're ranked
  last, total reflects current class size.
- [x] **C3** [Low] `c.Name = msg.Name` stores untrimmed input.
  `backend/internal/hub/client.go:731` (also
  `backend/internal/vote/manager.go:221,237,561`). `IsValidName` (and the
  manager's guard) trims before checking length/charset, but the original
  (untrimmed) string is what's stored in `session.Stagiaires[id]` and `c.Name`.
  A client sending `"  Jean  "` passes validation and is stored with whitespace.
  Cosmetic in the UI (HTML collapses whitespace), but the stored form is the
  canonical identity string used in `stagiaireName` broadcasts and the trainer's
  stagiaire list. Worst case is weird whitespace in toasts/log lines. **Fix:**
  trim once at the validation boundary (`name = strings.TrimSpace(name)` before
  storing), at all four sites. Or have `IsValidName` return the trimmed form.
- [x] **R22** [Low] Rate-limit entries leak when a client cycles `stagiaireId`.
  `backend/internal/hub/client.go:166`. `readPump`'s defer calls
  `c.Hub.Security.RemoveMessageRate(c.ID)` — but `c.ID` is mutable across
  `stagiaire_join` messages (R1 in DONE log resets it to `OriginalID`, then a
  presented ID overwrites it). Only the LAST ID's entry is removed; earlier IDs
  leave orphaned `MessageRateLimiter` entries in `Security.messageRates` until
  the 5-min `cleanup()` GCs them. Worse: each new ID gets a fresh
  `MaxBurstMessages=20` budget via `CheckMessageRate`, so a client scripting
  `stagiaire_join` between message bursts effectively refreshes its quota per
  cycle (bounded by `CheckJoinRateLimit` per IP, but the message cap is supposed
  to be the inner defense). Memory bloat under malicious cycling (bounded by
  per-IP cap × rate × cleanup interval — single attacker ≤ ~3 MB, not
  catastrophic) + weakened inner rate limit. Defense-in-depth gap alongside the
  documented R1 fix. **Fix:** either (a) remove by `OriginalID` in addition to
  final `c.ID`, or (b) key `CheckMessageRate` by a stable per-connection
  identifier (e.g., `OriginalID`) instead of the mutable `c.ID`.
- [x] Tests: `client_test.go` (+handleUpdateName rejects a trainer client with
  `ErrNotAuthorized` and does not call `UpdateStagiaireName`, B1),
  `hub_test.go` (+computeRank for a post-reveal joiner: total reflects current
  Stagiaires count, rank is `total` for a scoreless joiner, C1),
  `client_test.go`/`manager_test.go` (+name stored trimmed at all four sites,
  C3), `client_test.go` (+cycling `stagiaireId` on one connection removes all
  prior `messageRates` entries on disconnect; per-cycle quota no longer
  refreshes, R22). `go test -race ./...` green.

---

## Session 30 — Infra, CI & docs alignment

Round-5 infra findings. The deploy stack is already heavily hardened (sessions
9/14/15/16 did D1–D18), so nothing here is High severity — these are residuals
and post-hardening drift. All are config/docs/CI changes with no application
code coupling, so the whole session runs without touching the Go/JS test
surfaces and ships in one PR.

- [x] **CI1** [Medium] No CI concurrency control — wasted runner minutes, slow
  feedback. `.github/workflows/ci.yml`, `.github/workflows/release.yml` (no
  `concurrency:` key anywhere; verified via grep). 5 jobs run per push
  (backend/frontend/e2e/docker/security). Pushing 3 commits to a PR launches 15
  jobs; superseded runs are not cancelled. docker + govulncheck + e2e legs are
  heavy; duplicate runs block the queue and burn minutes. **Fix:** add to
  `ci.yml` (and the `ci` workflow_call) —
  ```yaml
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref || github.event.pull_request.number }}
    cancel-in-progress: true
  ```
  Omit `cancel-in-progress` (or the whole key) in `release.yml` — tags are
  immutable, you don't want a release run killed mid-publish.
- [x] **D19** [Medium] Debian maintainer scripts double-manage systemd; "don't
  autostart" intent is false. `debian/vote.postinst:16-20` (manual `systemctl
  enable`), `debian/vote.prerm:6-10` (manual `systemctl stop`),
  `debian/vote.postrm:15-17` (manual `daemon-reload`) — all alongside
  `#DEBHELPER#` (`:32`,`:22`,`:29`). With debhelper-compat 13 and a shipped
  `.service`, `dh_installsystemd` auto-runs and injects enable/start/stop
  snippets at `#DEBHELPER#`. The manual calls are redundant (idempotent, so
  harmless) BUT — critically — there is no `override_dh_installsystemd
  --no-start`. So debhelper **starts the service on `apt install`**,
  contradicting the postinst comment "don't start it (admin will start it)".
  Behaviour ↔ doc mismatch; the operator's first install auto-starts under the
  default env (dev CORS origins). **Fix:** drop the manual `systemctl` calls
  from all three scripts and let debhelper own the lifecycle. If no-autostart is
  truly wanted, add to `rules`:
  ```makefile
  override_dh_installsystemd:
  	dh_installsystemd --no-start
  ```
- [x] **D20** [Medium] `ci.yml` `docker` job rebuilds cold every push + never
  warms the release cache. `.github/workflows/ci.yml:140-141` (`docker build -t
  vote:ci .`, legacy builder, no cache). D1 (DONE) added `cache-from/to:
  type=gha` to `release.yml`'s buildx push, but the PR-gate docker job uses the
  legacy builder with no cache. It pays the full multi-stage rebuild on every
  PR, AND its layers never populate the GHA cache that `release.yml` reads — so
  the first tag build after a code change also starts cold. **Fix:** switch to
  `docker/setup-buildx-action` + `docker/build-push-action` with `load:true`,
  `cache-from: type=gha`, `cache-to: type=gha,mode=max` (mirrors `release.yml`).
  Bonus: also run the HEALTHCHECK (`docker run --rm -d …; docker inspect
  --format=…`) instead of just `--help` (line 144) — `vote-server --health` is
  the documented probe and is never exercised in CI.
- [x] **M1** [Low] README drift: runtime image documented as "Alpine", is
  actually distroless. `README.md:17` — `**Docker**: multi-stage build (Go +
  Node → Alpine)`. D10 (DONE) switched the runtime stage to
  `gcr.io/distroless/static-debian12:nonroot` (~18 MB, no shell). Alpine is only
  a transient *builder* base now. Misleading for anyone threat-modelling the
  runtime surface or expecting `wget`/shell in the container. **Fix:** `multi-
  stage build (Go + Node → distroless static runtime)`. The `Production Build`
  block (line 119) and `Health & version` note (line 111) are already correct —
  only line 17 drifted.
- [x] **M2** [Low] README env table missing `VOTE_MAX_SESSIONS_PER_HOUR`.
  `README.md:72-84`. D15 (DONE) added it to `.env.example:51`, but the README
  env reference table (which presents itself as the canonical list) omits it —
  it lists the other three S7 caps. Inconsistent source-of-truth. **Fix:** add
  the row (default `20`, sliding 1h window, `≤0` → hardcoded default).
- [x] **M3** [Low] CLAUDE.md claims `ResetVote` clears `session.Stagiaires` —
  it doesn't (docs/code contract violation). `internal/vote/manager.go:385-419`
  vs CLAUDE.md (two locations: the S15 note and the Stagiaire Reclaim Token
  section). The code clears `Votes`, `LastVoteScores`, `CorrectColors`,
  `Revealed` — but leaves `Stagiaires`, `ReclaimTokens`, `Scores`, `GameScores`
  untouched. This is intentional (the "Cumulative: scores accumulate across
  votes within a session" feature depends on it), so the code is correct and the
  docs are wrong. A developer reading CLAUDE.md as the source of truth will
  write code assuming reset = fresh identity sheet. The two CLAUDE.md claims are
  also internally inconsistent with the "cumulative scoring" claim in the same
  doc. **Fix:** update CLAUDE.md to say reset clears the current round's
  votes/scores/reveal state but preserves identity (Stagiaires, ReclaimTokens,
  cumulative Scores, GameScores) for cross-round competition; fresh-identity
  reset requires leaving and creating a new session. No code change.
- [x] **D21** [Low] systemd unit: a few standard hardening directives missing.
  `debian/vote.service:33-55`. Present hardening is excellent, but absent:
  `CapabilityBoundingSet=` (drop all caps), `AmbientCapabilities=` (empty),
  `PrivateDevices=true`, `UMask=0077`, `ProcSubset=pid`. A Go HTTP/WS server
  binding >1024 needs no Linux capabilities and no `/dev`. With non-root +
  `NoNewPrivileges` + `MemoryDenyWriteExecute` the residual risk is low, so this
  is pure defense-in-depth — but `CapabilityBoundingSet=` is the standard
  cap-floor and its absence is conspicuous given how thorough the rest is.
  **Fix:** add `CapabilityBoundingSet=` `AmbientCapabilities=` `PrivateDevices=true`
  `UMask=0077`.
- [x] **D22** [Low] Caddyfile missing `Permissions-Policy`.
  `debian/Caddyfile.example:47-55` (header block). D12 (DONE) added HSTS /
  nosniff / Referrer-Policy / X-Frame-Options. A voting app never needs
  camera/microphone/geolocation/payment — explicitly disabling them is cheap
  defense-in-depth against a future third-party snippet or compromised asset.
  **Fix:** add `Permissions-Policy "camera=(), microphone=(), geolocation=(),
  payment=()"` to the `header {}` block.
- [x] **D23** [Low] Non-reproducible build via `BUILD_TIME=$(date …)`.
  `Makefile:6`, `.github/workflows/release.yml:142` (`build_time=$(date -u …)`).
  Same source commit → different binary → different image digest, weakening the
  cosign/SBOM/provenance supply-chain story (two builds of `v1.2.3` aren't
  byte-identical). `version`/`gitCommit` are already stable; only the timestamp
  varies. **Fix:** derive from the commit: `BUILD_TIME := $(shell git show -s
  --format=%cI HEAD)` (or honour `SOURCE_DATE_EPOCH`). Timestamp stays
  meaningful + reproducible.
- [x] **D24** [Low] `make build-frontend` uses `npm install`, not `npm ci
  --ignore-scripts`. `Makefile:235`. D4 (DONE) explicitly switched
  `debian/rules` to `npm ci --ignore-scripts` (supply-chain: no lockfile
  mutation, no install-scripts). The Makefile target was missed — it still runs
  `npm install` (can rewrite `package-lock.json`, runs lifecycle scripts).
  Inconsistent contract for the same frontend build. **Fix:** `npm ci
  --ignore-scripts && npm run build`.
- [x] **D25** [Low] `apk add --no-cache git` in Dockerfile backend-builder is
  vestigial. `Dockerfile:10`. `go.mod` has no `replace`, no private modules, no
  VCS directives (verified) — `go mod download` resolves via the module proxy
  with `GOSUMDB`, never invoking git. `GIT_COMMIT` arrives as a build-arg
  (`release.yml:143` / Makefile), not from a repo checkout inside the stage.
  D16 (DONE) pinned digests but this stray dep survived. **Fix:** drop the `RUN
  apk add …` line entirely (builder stage only; zero runtime impact, but removes
  a build dep and one layer).
- [x] **D26** [Low] `make clean-deb` is incomplete. `Makefile:183-192`. Removes
  `debian/.debhelper/`, `debhelper-build-stamp`, `files`, `debian/vote/`, but
  not the top-level `debian/*.substvars` or `debian/*.debhelper` files.
  Confirmed present on disk: `debian/vote.substvars`,
  `debian/vote.postrm.debhelper` (untracked, but stale). `.dockerignore:64-65`
  even lists these patterns — `clean-deb` just doesn't apply them. **Fix:** add
  `rm -f debian/*.substvars debian/*.debhelper` to `clean-deb` (and align the
  existing patterns).
- [x] **D27** [Low] No `.deb` install smoke-test in release.
  `.github/workflows/release.yml:100-121` (`build-deb` builds + verifies arch,
  never installs). A packaging-path regression (wrong install path, broken
  systemd unit, bad perms) builds cleanly (`dpkg-buildpackage` validates
  structure) but ships broken. The amd64 leg runs on a Debian-class runner
  where `sudo dpkg -i vote_*.deb && systemctl start vote && curl /livez` is a
  10-second gate. **Fix:** add an install-and-probe step on the amd64 leg after
  the build (arm64 stays build-only since the runner can't execute the arm64
  binary without QEMU).
- [x] Verification: `go build ./...` clean; `npm run build` clean (no app code
  changed); `make clean-deb` leaves no stale `debian/*.substvars` /
  `*.debhelper` (D26); local `dpkg-buildpackage -uc -us -b` succeeds (D19/D21);
  `docker build` uses the GHA cache (D20). No regression against the archived
  "things checked and found correct" list in `IMPROVEMENTS-DONE.md`.

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
