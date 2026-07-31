# Improvements — vote

Prioritized audit findings, grouped into sessions. Each session is scoped to be
doable with high quality inside one conversation.

Status legend: `[ ]` pending · `[~]` in progress · `[x]` done

Sessions 1–34 (audit rounds 1–6) are archived in
[`IMPROVEMENTS-DONE.md`](./IMPROVEMENTS-DONE.md).

No open findings — the codebase is clean as of the last audit round. When a new
audit is run, add the findings below using the established item-code series
(continuing from S18, F32, R26, A3, X1, P1, D27, CI1, M3).

---

## Verification checklist (per session, before marking `[x]`)

- `make fmt-check` clean; `go vet ./...` clean; `go test -race ./...` green (for
  sessions touching Go).
- `npm run lint` clean; `npm test` green; `npm run build` clean (for sessions
  touching JS).
- No regression against the archived "things checked and found correct" lists in
  `IMPROVEMENTS-DONE.md` (lock ordering `h.mu → Manager.mu → Session.mu`,
  idempotent reveal reversal, atomic counter writes, constant-time token
  compares, `SetTrustedProxies` defaulting to empty, `escapeHtml` on every
  user-supplied name, `sanitizeColor` at every `style="background-color:…"`
  site, `safeLocalGet`/`safeSessionGet` wrappers used everywhere).
