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
│   └── src/              # JS: formateur/ & stagiaire/ modules
├── shared/               # Shared JS (colors, icons, validation, websocket-client)
├── tests/e2e/            # Playwright E2E tests
├── scripts/              # Build tools (version gen, asset compression)
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
| `ALLOWED_ORIGINS` | `*` | CORS origins (comma-separated) |
| `VALID_COLORS` | `rouge,vert,bleu,jaune,orange,violet,rose,gris` | Allowed vote colors |

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
