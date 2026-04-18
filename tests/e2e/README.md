# E2E Tests

Two complementary test suites for Vote Coloré.

## Structure

| File | Purpose | Needs Frontend? | CI? |
|------|---------|-----------------|-----|
| `ws-protocol.spec.ts` | WebSocket protocol tests (raw `ws` clients) | No | Yes |
| `ui.spec.ts` | Full browser UI tests (Playwright) | Yes | No |

Supporting files:
- `ws-helper.ts` — `connectTrainer()` / `connectStagiaire()` helpers for raw WS protocol tests
- `fixtures.ts` — shared test utilities (`generateSessionCode`, `TEST_COLORS`)

## Setup

```bash
npm install
npx playwright install --with-deps chromium
```

## Running

```bash
# Protocol tests only (CI-friendly, just backend)
SKIP_VITE=1 npm test -- ws-protocol.spec.ts

# UI tests (needs backend + Vite dev server)
npm test -- ui.spec.ts

# All tests (auto-starts both servers)
npm test

# Or via Makefile
make test-e2e       # Protocol only
make test-e2e-ui    # UI only
```

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `SKIP_VITE` | — | Skip Vite server startup (protocol-only runs) |
| `WS_URL` | `ws://localhost:8080/ws` | WebSocket URL for protocol tests |
| `BASE_URL` | `http://localhost:5173` | Frontend URL for UI tests |
