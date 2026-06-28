# Vote Coloré

Real-time voting app for training sessions — Go backend (WebSocket) + Vanilla JS frontend (Vite).

## Quick Start

```bash
make run              # Backend → :8080
cd frontend && npm run dev  # Frontend → :5173 (proxies /ws to backend)
```

## Features

- **Trainer**: create sessions, pick colors, single/multiple choice, live stats
- **Trainee**: join by code, vote, auto-reconnect
- **Real-time**: WebSocket communication
- **Docker**: multi-stage build (Go + Node → Alpine)

## Project Structure

```
vote/
├── backend/              # Go server (Gin + gorilla/websocket)
│   ├── cmd/server/       # Entry point
│   ├── internal/         # hub/, vote/, server/, config/
│   └── integration/      # WebSocket integration tests
├── frontend/             # Vite multi-page app
│   ├── formateur/        # Trainer HTML entry
│   ├── stagiaire/        # Trainee HTML entry
│   ├── shared/           # Shared JS (colors, icons, validation, websocket-client)
│   ├── scripts/          # Build tools (version gen, asset compression)
│   └── src/              # JS: formateur/ & stagiaire/ modules
├── tests/e2e/            # Playwright E2E tests
└── debian/               # Debian packaging
```

## Makefile

| Target | Description |
|--------|-------------|
| `make run` | Build + start server (:8080) |
| `make build` | Compile Go binary |
| `make dev` | Hot reload (requires [air](https://github.com/air-verse/air)) |
| `make test` | Unit tests (`-race -cover`) |
| `make test-integration` | WebSocket integration tests |
| `make test-e2e` | Playwright E2E (requires running backend) |
| `make test-all` | Unit + Integration + E2E |
| `make test-cover` | Generate HTML coverage report |
| `make lint` | golangci-lint |
| `make docker` | Build Docker image |
| `make build-deb` | Package as .deb |
| `make clean-all` | Remove all artifacts |

## Testing

```bash
# Backend
make test              # Unit
make test-integration  # Integration

# Frontend
cd frontend && npm test

# E2E
make test-e2e          # Requires backend running
```

## Environment

See `.env.example`:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `ALLOWED_ORIGINS` | localhost dev origins | CORS origins (comma-separated; `*` disables credentials) |
| `TRUSTED_PROXIES` | _(empty)_ | Trusted proxy IPs for correct client IP detection behind reverse proxy |
| `VALID_COLORS` | `rouge,vert,bleu,jaune,orange,violet,rose,gris` | Allowed vote colors |
| `VOTE_DASHBOARD_SECRET` | _(empty)_ | Gates `/dashboard`. Unset = dashboard disabled (404). Set to a random string to enable the maintainer dashboard behind cookie auth. |
| `VOTE_DASHBOARD_MAX_AGE` | `168h` (7 days) | How long a dashboard login cookie stays valid. |
| `VOTE_DATA_DIR` | `./data` (dev), `/var/lib/vote` (Docker/Debian) | FHS location for persistent stats. Holds `counters.json` (restore checkpoint) + `stats.jsonl` (append-only history). Created `0700`, files `0600`. |
| `VOTE_STATS_INTERVAL` | `5m` | How often the server flushes counters to disk. Crash loses ≤ one interval. |

## Metrics & Dashboard

`GET /metrics` exposes a [Prometheus](https://prometheus.io/docs/instrumenting/exposition_formats/)-format text endpoint with both runtime gauges (active sessions, connected trainers/stagiaires, goroutines, memory) and product counters/histograms (`vote_sessions_created_total`, `vote_votes_cast_total`, `vote_trainees_joined_total`, feature-adoption counters, and per-session distribution histograms). It is public and scrape-friendly.

`GET /dashboard` is an **authed** self-contained maintainer dashboard (no build step, no external deps) that polls `/metrics`, keeps a compact ring buffer of snapshots in `localStorage`, and renders SVG sparklines + histogram bars. **Usage data persists server-side** in `VOTE_DATA_DIR`: counters survive restarts (restored from `counters.json` on boot) and a 5-min append-only history (`stats.jsonl`) is collected 24/7 regardless of whether anyone has the dashboard open. On login the dashboard fetches `GET /dashboard/history` (authed) and seeds the trend from server data, so a maintainer logging in after a month sees all-time totals + the full usage trend.

Enabled only when `VOTE_DASHBOARD_SECRET` is set; login at `/dashboard/login` with the configured secret. If the data dir cannot be opened, the server runs without persistence (counters reset on restart, as before this feature) — failures are non-fatal.

Generate a strong secret:

```bash
openssl rand -base64 32   # then export VOTE_DASHBOARD_SECRET=<that>
```


## Production Build

```bash
make build                    # Backend binary
cd frontend && npm run build  # Frontend assets → frontend/dist/
docker build -t vote .        # Full Docker image
```

## Colors

| Name | Hex |
|------|-----|
| rouge | `#ef4444` |
| vert | `#22c55e` |
| bleu | `#3b82f6` |
| jaune | `#eab308` |
| orange | `#f97316` |
| violet | `#a855f7` |
| rose | `#ec4899` |
| gris | `#6b7280` |

## License

MIT — Copyright (c) 2025 Robin DUBREUIL
