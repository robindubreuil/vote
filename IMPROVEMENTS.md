# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

Sessions 1–26 (audit rounds 1, 2, 3 & 4) are archived in
[`IMPROVEMENTS-DONE.md`](./IMPROVEMENTS-DONE.md). The findings below are
**audit round 5** — a fresh end-to-end pass over the now-hardened codebase. Round
5 surfaced a small number of residual issues; they cluster into four
conversation-sized sessions. Item codes keep their category prefix (S = security,
F = frontend, R = backend reliability/correctness, A = accessibility, X = XSS,
P = performance, D = deploy/infra, CI = continuous integration, M = docs) and
continue each series (the archived log ends at S16, F25, R21, D18), so there is
no code collision with anything already done.

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

- [ ] **B1** [Medium] `handleUpdateName` is the only mutation handler without a
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
- [ ] **C1** [Low/Medium] `computeRank` returns `rank > totalStagiaires` for
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
- [ ] **C3** [Low] `c.Name = msg.Name` stores untrimmed input.
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
- [ ] **R22** [Low] Rate-limit entries leak when a client cycles `stagiaireId`.
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
- [ ] Tests: `client_test.go` (+handleUpdateName rejects a trainer client with
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

- [ ] **CI1** [Medium] No CI concurrency control — wasted runner minutes, slow
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
- [ ] **D19** [Medium] Debian maintainer scripts double-manage systemd; "don't
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
- [ ] **D20** [Medium] `ci.yml` `docker` job rebuilds cold every push + never
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
- [ ] **M1** [Low] README drift: runtime image documented as "Alpine", is
  actually distroless. `README.md:17` — `**Docker**: multi-stage build (Go +
  Node → Alpine)`. D10 (DONE) switched the runtime stage to
  `gcr.io/distroless/static-debian12:nonroot` (~18 MB, no shell). Alpine is only
  a transient *builder* base now. Misleading for anyone threat-modelling the
  runtime surface or expecting `wget`/shell in the container. **Fix:** `multi-
  stage build (Go + Node → distroless static runtime)`. The `Production Build`
  block (line 119) and `Health & version` note (line 111) are already correct —
  only line 17 drifted.
- [ ] **M2** [Low] README env table missing `VOTE_MAX_SESSIONS_PER_HOUR`.
  `README.md:72-84`. D15 (DONE) added it to `.env.example:51`, but the README
  env reference table (which presents itself as the canonical list) omits it —
  it lists the other three S7 caps. Inconsistent source-of-truth. **Fix:** add
  the row (default `20`, sliding 1h window, `≤0` → hardcoded default).
- [ ] **M3** [Low] CLAUDE.md claims `ResetVote` clears `session.Stagiaires` —
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
- [ ] **D21** [Low] systemd unit: a few standard hardening directives missing.
  `debian/vote.service:33-55`. Present hardening is excellent, but absent:
  `CapabilityBoundingSet=` (drop all caps), `AmbientCapabilities=` (empty),
  `PrivateDevices=true`, `UMask=0077`, `ProcSubset=pid`. A Go HTTP/WS server
  binding >1024 needs no Linux capabilities and no `/dev`. With non-root +
  `NoNewPrivileges` + `MemoryDenyWriteExecute` the residual risk is low, so this
  is pure defense-in-depth — but `CapabilityBoundingSet=` is the standard
  cap-floor and its absence is conspicuous given how thorough the rest is.
  **Fix:** add `CapabilityBoundingSet=` `AmbientCapabilities=` `PrivateDevices=true`
  `UMask=0077`.
- [ ] **D22** [Low] Caddyfile missing `Permissions-Policy`.
  `debian/Caddyfile.example:47-55` (header block). D12 (DONE) added HSTS /
  nosniff / Referrer-Policy / X-Frame-Options. A voting app never needs
  camera/microphone/geolocation/payment — explicitly disabling them is cheap
  defense-in-depth against a future third-party snippet or compromised asset.
  **Fix:** add `Permissions-Policy "camera=(), microphone=(), geolocation=(),
  payment=()"` to the `header {}` block.
- [ ] **D23** [Low] Non-reproducible build via `BUILD_TIME=$(date …)`.
  `Makefile:6`, `.github/workflows/release.yml:142` (`build_time=$(date -u …)`).
  Same source commit → different binary → different image digest, weakening the
  cosign/SBOM/provenance supply-chain story (two builds of `v1.2.3` aren't
  byte-identical). `version`/`gitCommit` are already stable; only the timestamp
  varies. **Fix:** derive from the commit: `BUILD_TIME := $(shell git show -s
  --format=%cI HEAD)` (or honour `SOURCE_DATE_EPOCH`). Timestamp stays
  meaningful + reproducible.
- [ ] **D24** [Low] `make build-frontend` uses `npm install`, not `npm ci
  --ignore-scripts`. `Makefile:235`. D4 (DONE) explicitly switched
  `debian/rules` to `npm ci --ignore-scripts` (supply-chain: no lockfile
  mutation, no install-scripts). The Makefile target was missed — it still runs
  `npm install` (can rewrite `package-lock.json`, runs lifecycle scripts).
  Inconsistent contract for the same frontend build. **Fix:** `npm ci
  --ignore-scripts && npm run build`.
- [ ] **D25** [Low] `apk add --no-cache git` in Dockerfile backend-builder is
  vestigial. `Dockerfile:10`. `go.mod` has no `replace`, no private modules, no
  VCS directives (verified) — `go mod download` resolves via the module proxy
  with `GOSUMDB`, never invoking git. `GIT_COMMIT` arrives as a build-arg
  (`release.yml:143` / Makefile), not from a repo checkout inside the stage.
  D16 (DONE) pinned digests but this stray dep survived. **Fix:** drop the `RUN
  apk add …` line entirely (builder stage only; zero runtime impact, but removes
  a build dep and one layer).
- [ ] **D26** [Low] `make clean-deb` is incomplete. `Makefile:183-192`. Removes
  `debian/.debhelper/`, `debhelper-build-stamp`, `files`, `debian/vote/`, but
  not the top-level `debian/*.substvars` or `debian/*.debhelper` files.
  Confirmed present on disk: `debian/vote.substvars`,
  `debian/vote.postrm.debhelper` (untracked, but stale). `.dockerignore:64-65`
  even lists these patterns — `clean-deb` just doesn't apply them. **Fix:** add
  `rm -f debian/*.substvars debian/*.debhelper` to `clean-deb` (and align the
  existing patterns).
- [ ] **D27** [Low] No `.deb` install smoke-test in release.
  `.github/workflows/release.yml:100-121` (`build-deb` builds + verifies arch,
  never installs). A packaging-path regression (wrong install path, broken
  systemd unit, bad perms) builds cleanly (`dpkg-buildpackage` validates
  structure) but ships broken. The amd64 leg runs on a Debian-class runner
  where `sudo dpkg -i vote_*.deb && systemctl start vote && curl /livez` is a
  10-second gate. **Fix:** add an install-and-probe step on the amd64 leg after
  the build (arm64 stays build-only since the runner can't execute the arm64
  binary without QEMU).
- [ ] Verification: `go build ./...` clean; `npm run build` clean (no app code
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
