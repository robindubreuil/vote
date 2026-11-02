# Audit Notes — Historical Context

Long inline comments and defensive curios that were shortened or removed
during the session-35 review-fixes sweep. The condensed versions live in the
code; the full history lives here so a future reader can reconstruct *why*
without archaeology.

## S13 — Trainer-token gate on every join (hub.go)

`registerTrainerLocked` carries a condensed version. Full history:

The original bug (S1/S13 family): any join to an already-minted Manager
session must present the per-session trainer token. The pre-S13 code gated
the token check on `old != nil` — i.e. only when another trainer connection
was live. Once the legitimate trainer's readPump exited and
`unregisterClient` set `conns.Trainer = nil`, any client who knew the
**public** session code (printed in every stagiaire's QR) could claim the
empty trainer slot via `trainer_join` **and receive the minted token** in
the `session_created` reply — collapsing S1's takeover protection for the
rest of the session. Worse, the token is minted once and never rotated, so
the attacker held it permanently.

Post-S13 invariants, preserved by the refactor:

- The token check runs **before** the `!exists` / `createSessionFn` branch,
  so a tokenless claimant cannot bypass it via the session-create path
  (which would overwrite the minted token).
- Rejections reuse the `"Session introuvable"` message so the response does
  not leak that the code is live (S14 enumeration hygiene).
- Fresh session creation (no Manager session yet) is the **only** path that
  mints and emits a token to a previously unauthenticated trainer — the
  creator is by definition the legitimate trainer.
- A defensive guard keeps the two invariants consistent if a Connections
  entry is reaped while the Manager session survives (not reachable via
  cleanupLoop's normal contract; defense in depth): `createSessionFn` is
  skipped so the existing minted token is never overwritten.

## R2 — GenerateSessionCode exhaustive-walk removal (hub.go)

The pre-session-35 code fell back from 100 random probes to a
lexicographic recursive walk of the code space ("first free code"). That
walk is **exponential in code length** — harmless at 3 chars (23³ =
12,167 nodes worst case), but at 4 chars (23⁴ ≈ 280k) and beyond it would
hold the hub lock for an unbounded sweep. Since randomness saturating 100
probes means the space is effectively full, the fallback was replaced by
returning `""`, which the join path maps to a
`no_codes_available` error. The used-set is now built once (O(sessions),
bounded by `MaxSessionsGlobal`) so each probe is an O(1) membership check.

## R21 — store.go durability recipe

The fsync recipe itself is unchanged (package doc + `SaveCounters` doc in
`store.go`): temp-file write → temp fsync → rename → **directory fsync**.
Two pieces of decoration around it were removed in session 35:

- `var _ fs.FileMode = 0o600` — a "compile-time guard" that guarded
  nothing (assigning an untyped constant to the type always compiles).
- `TestSaveCountersDocAssertsDurabilityRecipe` — a test that grepped
  `store.go`'s comments for the words "fsync"/"directory"/etc. It asserted
  comment *wording*, not behavior; any documentation rephrase would break
  it while any behavioral regression would not. Replaced by
  `TestSyncDirAndSyncPathHelpersRun`, which actually invokes `syncDir` and
  `syncPath` and fails if they error.
- `TestPlatformPathNote` — a skip-only test with no assertions; deleted.

## R29 — Message rate limiter (security.go)

Pre-session-35 structure: fixed 1 s window + burst budget
(`MaxBurstMessages = 20`) + a minimum-interval check once `messageCount`
hit `MaxMessagesPerSecond`. Weakness: a client pacing just over the
sustained rate could carry ~2× the advertised rate forever by straddling
window boundaries (each new window resets the budget). Replaced by a true
sliding window: a timestamp slice pruned in place to the trailing second,
rejecting when `len(fresh) >= MaxMessagesPerSecond`. Bounded memory (the
slice can never exceed the cap), no allocation in steady state. Idle
buckets are reaped by the 5-minute cleanup as before (idle = last
timestamp older than 1 min).

## Dead code removed

- `Hub.GetConnectionCount` — duplicate of `SessionCount` (both returned
  `len(h.Connections)` under RLock); its only caller was
  `integration/cleanup_test.go`, now on `SessionCount`.
- `frontend/coverage/` — stale untracked output referencing a deleted
  `shared/i18n.js`; deleted and ignored in both `.gitignore` files.
- `strings.js` `stagiaire.invalidCode` had no consumers at removal time;
  its text was later re-synced when the 4-char message (S15) landed, and
  `validateSessionCode`'s inline message remains the live one.

## H1 — Error-code protocol (why text matching was removed)

Before session 35, error frames carried only a French `message`, and both
frontends matched prose to detect session death: the formateur UI matched
`'Session introuvable'`, the stagiaire UI matched `'Session not found'` —
an **English string the French-only server never sent**, so that branch
had never fired. Both UIs now branch on the stable `code` field
(`session_not_found`, `reclaim_expired`, …) with the legacy text matches
kept only as pre-upgrade-server fallbacks.
